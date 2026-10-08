package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"

	"monitoring-master/internal/alert"
	"monitoring-master/internal/api/handlers"
	"monitoring-master/internal/api/middleware"
	"monitoring-master/internal/bot"
	"monitoring-master/internal/cleanup"
	"monitoring-master/internal/config"
	"monitoring-master/internal/crypto"
	"monitoring-master/internal/database"
	"monitoring-master/internal/digest"
	"monitoring-master/internal/logutil"
	"monitoring-master/internal/models"
	"monitoring-master/internal/repository"
	"monitoring-master/internal/scheduler"
	"monitoring-master/internal/version"
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
	// --- Rotating Log: auto-switch file at midnight ---
	logWriter, err := logutil.Setup("logs")
	if err != nil {
		log.Fatalf("Failed to setup rotating log: %v", err)
	}
	defer logWriter.Close()

	// Redirect Gin's default logger to same rotating writer
	gin.DefaultWriter = io.MultiWriter(logWriter, os.Stdout)
	gin.DefaultErrorWriter = io.MultiWriter(logWriter, os.Stderr)

	// Set timezone to Asia/Jakarta for consistent timestamp handling
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		log.Fatalf("Failed to load timezone Asia/Jakarta: %v", err)
	}
	time.Local = loc
	log.Println("Timezone set to Asia/Jakarta")

	// --- Configuration ---
	cfg := config.LoadConfig()

	// --- Set Gin Mode (Default: release) ---
	if cfg.GinMode == "" {
		cfg.GinMode = "release"
	}
	gin.SetMode(cfg.GinMode)

	// --- Configure Debug Logging ---
	logutil.SetDebug(cfg.DebugLog)

	// --- Log Application Startup Banner & Versioning ---
	version.LogStartupBanner(cfg.Port, cfg.DBDriver, cfg.GinMode, cfg.DebugLog)

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
			&models.IncidentLog{},
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

	// --- At-rest encryption for credential fields (agent secrets, Telegram tokens) ---
	encKey, err := crypto.LoadKey()
	if err != nil {
		log.Fatalf("failed to load encryption key: %v", err)
	}
	cryptor, err := crypto.NewCipher(encKey)
	if err != nil {
		log.Fatalf("failed to init cipher: %v", err)
	}
	// Clear key material from memory as soon as possible.
	for i := range encKey {
		encKey[i] = 0
	}
	switch r := repo.(type) {
	case *repository.MongoRepository:
		r.SetCryptor(cryptor)
	case *repository.SQLRepository:
		r.SetCryptor(cryptor)
	}
	// Best-effort migration of legacy plaintext secrets (idempotent).
	type secretMigrator interface {
		MigrateSecrets(context.Context) (int, error)
	}
	if m, ok := repo.(secretMigrator); ok {
		mctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		n, merr := m.MigrateSecrets(mctx)
		cancel()
		if merr != nil {
			log.Printf("WARNING: secret migration failed: %v (server continues; plaintext rows still readable)", merr)
		} else if n > 0 {
			log.Printf("Secret migration: encrypted %d legacy plaintext credential row(s)", n)
		}
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

	// --- Blacklist Manager (In-Memory Cache) ---
	blacklistMgr := middleware.NewBlacklistManager(repo)

	// --- Gin Router ---
	r := gin.Default()

	// --- Security Middleware ---
	r.Use(middleware.IPBlacklist(blacklistMgr))
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.GlobalRateLimiter())
	r.Use(middleware.SuspiciousPathLogger(blacklistMgr, repo))
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

	// --- Scheduler ---
	sched, err := scheduler.New(repo, registry)
	if err != nil {
		log.Fatalf("scheduler init: %v", err)
	}

	// --- Log Retention Daily Cleanup Worker ---
	cleanup.StartDailyCleanup(repo, cfg)

	// --- Daily Health Digest Scheduled Worker ---
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		var lastSentDate string
		for range ticker.C {
			now := time.Now().Local()
			todayDate := now.Format("2006-01-02")
			currentTimeStr := now.Format("15:04")

			settings, err := repo.GetSettings(context.Background())
			if err != nil || settings == nil || !settings.EnableDailyDigest {
				continue
			}

			digestTime := settings.DailyDigestTime
			if digestTime == "" {
				digestTime = "08:00"
			}

			if currentTimeStr == digestTime && lastSentDate != todayDate {
				lastSentDate = todayDate
				log.Printf("[DAILY DIGEST] Triggering scheduled daily digest at %s...", currentTimeStr)
				if err := digest.SendDailyDigest(context.Background(), repo); err != nil {
					log.Printf("[DAILY DIGEST ERROR] Failed to send scheduled digest: %v", err)
				}
			}
		}
	}()

	// --- Handlers ---
	agentHandler := handlers.NewAgentHandler(repo, alertManager)
	frontendHandler := handlers.NewFrontendHandler(repo, registry, cfg.JWTSecret, cfg.CaptchaSecret)
	handlers.SetAllowedWSOrigins(cfg.AllowedOrigins)
	wsHandler := handlers.NewWSHandler(registry, repo, alertManager)
	frontendHandler.WSHandler = wsHandler
	settingHandler := handlers.NewSettingHandler(repo)
	scheduleHandler := handlers.NewScheduleHandler(repo, sched)
	scheduleHandler.Registry = registry

	// --- Health handler ---
	healthHandler := handlers.NewHealthHandler(repo)
	healthHandler.Registry = registry

	// --- Routes ---
	// Health endpoints (no auth required)
	{
		r.GET("/health", healthHandler.Health)
		r.GET("/ready", healthHandler.Ready)
	}

	// Agent endpoints (authenticated via X-Agent-ID / X-Secret-Key)
	agentGroup := r.Group("/api/v1/agent", middleware.AgentAuth(repo))
	{
		agentGroup.POST("/sync", agentHandler.Sync)
		agentGroup.POST("/alert", agentHandler.Alert)
	}

	// Frontend auth (with strict login rate limit)
	r.POST("/api/v1/auth/login", middleware.LoginRateLimiter(), frontendHandler.Login)

	// CAPTCHA endpoints (no auth required)
	r.GET("/api/v1/auth/captcha/challenge", frontendHandler.GetCaptchaChallenge)

	// Frontend endpoints (JWT-protected)
	protectedGroup := r.Group("/api/v1", middleware.JWTAuth(cfg.JWTSecret))
	{
		protectedGroup.PUT("/users/password", frontendHandler.ChangePassword)
		protectedGroup.GET("/audit-logs", frontendHandler.GetAuditLogs)
		protectedGroup.GET("/audit-logs/export", frontendHandler.ExportAuditLogs)

		// Global Settings
		protectedGroup.GET("/settings", settingHandler.GetSettings)
		protectedGroup.POST("/settings", settingHandler.UpdateSettings)
		protectedGroup.POST("/settings/test-alert", settingHandler.TestAlert)
		protectedGroup.POST("/settings/test-daily-digest", settingHandler.TestDailyDigest)
		protectedGroup.GET("/security-logs", settingHandler.GetSecurityLogs)
		protectedGroup.POST("/settings/purge-logs", settingHandler.PurgeLogs)
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
		institutionsGroup.POST("/:id/test-alert", frontendHandler.TestInstitutionAlert)
		institutionsGroup.POST("/:id/maintenance", frontendHandler.SetMaintenanceMode)
		institutionsGroup.GET("/:id/uptime", frontendHandler.GetInstitutionUptime)
		institutionsGroup.GET("/:id/incidents", frontendHandler.GetInstitutionIncidents)
	}

	// Schedule endpoints (JWT-protected)
	schedulesGroup := r.Group("/api/v1/schedules", middleware.JWTAuth(cfg.JWTSecret))
	{
		schedulesGroup.POST("", scheduleHandler.CreateSchedule)
		schedulesGroup.GET("", scheduleHandler.ListSchedules)
		schedulesGroup.GET("/:id", scheduleHandler.GetSchedule)
		schedulesGroup.PATCH("/:id", scheduleHandler.UpdateSchedule)
		schedulesGroup.DELETE("/:id", scheduleHandler.DeleteSchedule)
		schedulesGroup.POST("/:id/enable", scheduleHandler.EnableSchedule)
		schedulesGroup.POST("/:id/disable", scheduleHandler.DisableSchedule)
		schedulesGroup.POST("/:id/trigger", scheduleHandler.TriggerSchedule)
		schedulesGroup.GET("/:id/runs", scheduleHandler.ListScheduleRuns)
	}

	// WebSocket endpoint (agent-authenticated)
	r.GET("/api/v1/ws/agent", middleware.AgentAuth(repo), wsHandler.AgentWS)

	// --- Start Scheduler ---
	go sched.Start()

	// --- Graceful Shutdown ---
	defer sched.Stop()

	// --- Start Server ---
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}
	go func() {
		log.Printf("Starting server on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown error: %v", err)
	}
}
