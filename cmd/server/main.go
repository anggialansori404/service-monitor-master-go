package main

import (
	"context"
	"log"
	"net/http"
	"reflect"
	"time"

	"monitoring-master/internal/api/handlers"
	"monitoring-master/internal/api/middleware"
	"monitoring-master/internal/bot"
	"monitoring-master/internal/config"
	"monitoring-master/internal/database"
	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/alert"
	"monitoring-master/internal/ws"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	// Set timezone to Asia/Jakarta for consistent timestamp handling
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		log.Fatalf("Failed to load timezone Asia/Jakarta: %v", err)
	}
	time.Local = loc
	log.Println("Timezone set to Asia/Jakarta")

	// --- Configuration ---
	cfg := config.LoadConfig()

	// --- Database ---
	var repo repository.Repository
	var mongoDB *mongo.Database

	if cfg.DBDriver == "mongo" {
		log.Println("Connecting to MongoDB Atlas...")

		registry := bson.NewRegistry()
		registry.RegisterTypeMapEntry(bson.TypeObjectID, reflect.TypeOf(""))

		clientOptions := options.Client().
			ApplyURI(cfg.DBMongoURI).
			SetRegistry(registry).
			SetBSONOptions(&options.BSONOptions{
				ObjectIDAsHexString: true,
			})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		client, err := mongo.Connect(clientOptions)
		if err != nil {
			log.Fatalf("failed to create MongoDB client: %v", err)
		}

		if err := client.Ping(ctx, readpref.Primary()); err != nil {
			log.Fatalf("failed to ping MongoDB Atlas: %v", err)
		}

		log.Println("Successfully connected to MongoDB Atlas!")
		mongoDB = client.Database("monitoring_service")
		repo = repository.NewMongoRepository(mongoDB)

		// Create MongoDB indexes for schedules and schedule_runs
		schedulesColl := mongoDB.Collection("schedules")
		_, err = schedulesColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
			{Keys: bson.D{{Key: "institution_id", Value: 1}}},
			{Keys: bson.D{{Key: "enabled", Value: 1}}},
		})
		if err != nil {
			log.Printf("WARNING: failed to create schedules indexes: %v", err)
		}

		runsColl := mongoDB.Collection("schedule_runs")
		_, err = runsColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
			{Keys: bson.D{{Key: "schedule_id", Value: 1}}},
			{Keys: bson.D{{Key: "run_at", Value: -1}}},
		})
		if err != nil {
			log.Printf("WARNING: failed to create schedule_runs indexes: %v", err)
		}
	} else {
		log.Printf("Connecting to SQL Database (%s)...", cfg.DBDriver)

		var dialector gorm.Dialector
		switch cfg.DBDriver {
		case "postgres":
			dialector = postgres.Open(cfg.GetDSN())
		case "mysql":
			dialector = mysql.Open(cfg.GetDSN())
		default:
			log.Fatalf("unsupported SQL driver: %s", cfg.DBDriver)
		}

		db, err := gorm.Open(dialector, &gorm.Config{})
		if err != nil {
			log.Fatalf("failed to connect to SQL database: %v", err)
		}

		log.Println("Successfully connected to SQL Database!")

		// AutoMigrate models
		err = db.AutoMigrate(
			&models.Institution{},
			&models.Service{},
			&models.AuditLog{},
			&models.User{},
			&models.GlobalSetting{},
			&models.SecurityLog{},
			&models.BlacklistedIP{},
			&models.Schedule{},
			&models.ScheduleRun{},
		)
		if err != nil {
			log.Fatalf("failed to auto-migrate database: %v", err)
		}

		repo = repository.NewSQLRepository(db)
	}

	if cfg.DBDriver == "mongo" {
		database.SeedDatabase(mongoDB)
	} else {
		// Call SQL Seeder if necessary (Not migrating old mongo seeding logic for brevity, assuming standard setup)
	}

	// --- WebSocket Registry ---
	registry := ws.NewRegistry()

	// --- Telegram Bot ---
	if cfg.BotToken == "" {
		log.Println("WARNING: BOT_TOKEN not set, skipping Telegram bot initialization")
	} else {
		monitoringBot, err := bot.NewBot(cfg.BotToken, repo, registry)
		if err != nil {
			log.Printf("WARNING: failed to initialize Telegram bot: %v", err)
		} else {
			go monitoringBot.TBot.Start()
			log.Println("Telegram bot started")
		}
	}

	// --- Gin Router ---
	r := gin.Default()

	// --- Security Middleware ---
	r.Use(middleware.IPBlacklist(repo))
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.GlobalRateLimiter())
	r.Use(middleware.SuspiciousPathLogger(repo))
	// --- Custom Tarpit 404 NoRoute ---
	r.NoRoute(func(c *gin.Context) {
		// Delay 1 second to slow down scanners (Masscan, FFuf, Nuclei, Nikto)
		time.Sleep(1 * time.Second)
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
	})

	// --- CORS Middleware ---
	r.Use(cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Agent-ID", "X-Secret-Key"},
		AllowCredentials: true,
	}))

	// Start Alert Engine (Debounce & Batching)
	alertManager := alert.NewManager(repo, 2*time.Minute)
	go alertManager.Start(context.Background())

	// --- Handlers ---
	agentHandler := handlers.NewAgentHandler(repo, alertManager)
	frontendHandler := handlers.NewFrontendHandler(repo, registry, cfg.JWTSecret)
	wsHandler := handlers.NewWSHandler(registry, repo, alertManager)
	settingHandler := handlers.NewSettingHandler(repo)

	// --- Routes ---
	// Agent endpoints (authenticated via X-Agent-ID / X-Secret-Key)
	agentGroup := r.Group("/api/v1/agent", middleware.AgentAuth(repo))
	{
		agentGroup.POST("/sync", agentHandler.Sync)
		agentGroup.POST("/alert", agentHandler.Alert)
	}

	// Frontend auth (with strict login rate limit)
	r.POST("/api/v1/auth/login", middleware.LoginRateLimiter(), frontendHandler.Login)

	// Frontend endpoints (JWT-protected)
	protectedGroup := r.Group("/api/v1", middleware.JWTAuth(cfg.JWTSecret))
	{
		protectedGroup.PUT("/users/password", frontendHandler.ChangePassword)
		protectedGroup.GET("/audit-logs", frontendHandler.GetAuditLogs)

		// Global Settings
		protectedGroup.GET("/settings", settingHandler.GetSettings)
		protectedGroup.POST("/settings", settingHandler.UpdateSettings)
	}

	institutionsGroup := r.Group("/api/v1/institutions", middleware.JWTAuth(cfg.JWTSecret))
	{
		institutionsGroup.GET("", frontendHandler.ListInstitutions)
		institutionsGroup.GET("/summary", frontendHandler.GetDashboardSummary)
		institutionsGroup.POST("", frontendHandler.CreateInstitution)
		institutionsGroup.GET("/:id/services", frontendHandler.ListServices)
		institutionsGroup.POST("/:id/services/:name/restart", frontendHandler.RestartService)
		institutionsGroup.PATCH("/:id/services/:name", frontendHandler.UpdateServiceAutoRestart)
		institutionsGroup.GET("/:id", frontendHandler.GetInstitution)
		institutionsGroup.PUT("/:id", frontendHandler.UpdateInstitution)
		institutionsGroup.DELETE("/:id", frontendHandler.DeleteInstitution)
		institutionsGroup.POST("/:id/regenerate-keys", frontendHandler.RegenerateKeys)
		institutionsGroup.GET("/:id/credentials", frontendHandler.GetInstitutionCredentials)
	}

	// WebSocket endpoint (agent-authenticated)
	r.GET("/api/v1/ws/agent", middleware.AgentAuth(repo), wsHandler.AgentWS)

	// --- Start Server ---
	log.Printf("Starting server on :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
