package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all configuration values for the application.
type Config struct {
	DBDriver   string
	DBHost     string
	DBUser     string
	DBPassword string
	DBName     string
	DBPort     string
	DBSSLMode  string

	DBMongoURI string

	BotToken           string
	JWTSecret          string
	Port               string
	GinMode            string
	DebugLog           bool
	CaptchaProvider    string
	CaptchaSecret      string
	TurnstileSecretKey string

	AuditLogRetentionDays    int
	SecurityLogRetentionDays  int
	ScheduleRunRetentionDays int
}

// LoadConfig loads configuration from a .env file (if present) and
// falls back to system environment variables for any value not set.
func LoadConfig() *Config {
	// Best-effort: load .env if it exists, ignore error if missing.
	_ = godotenv.Load()

	cfg := &Config{
		DBDriver:   getEnv("DB_DRIVER", "postgres"),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBUser:     getEnv("DB_USER", "postgres"),
		DBPassword: getEnv("DB_PASSWORD", "postgres"),
		DBName:     getEnv("DB_NAME", "monitoring"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBSSLMode:  getEnv("DB_SSLMODE", "disable"),

		DBMongoURI: getEnv("DB_MONGO_URI", "mongodb://localhost:27017/monitoring"),

		BotToken:  getEnv("BOT_TOKEN", ""),
		JWTSecret: getEnv("JWT_SECRET", ""),
		Port:      getEnv("PORT", "8080"),
		GinMode:   getEnv("GIN_MODE", "release"),
		DebugLog:  getEnvAsBool("DEBUG_LOG", false) || getEnvAsBool("DEBUG", false) || getEnvAsBool("ENABLE_DEBUG_LOG", false),

		CaptchaProvider:    getEnv("CAPTCHA_PROVIDER", "local"),
		CaptchaSecret:      getEnv("CAPTCHA_SECRET", ""),
		TurnstileSecretKey: getEnv("TURNSTILE_SECRET_KEY", ""),

		AuditLogRetentionDays:    getEnvAsInt("AUDIT_LOG_RETENTION_DAYS", 30),
		SecurityLogRetentionDays:  getEnvAsInt("SECURITY_LOG_RETENTION_DAYS", 30),
		ScheduleRunRetentionDays: getEnvAsInt("SCHEDULE_RUN_RETENTION_DAYS", 30),
	}

	if cfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET must be set")
	}

	// Validate CAPTCHA config
	if cfg.CaptchaProvider == "local" && cfg.CaptchaSecret == "" {
		log.Fatal("CAPTCHA_SECRET must be set when CAPTCHA_PROVIDER=local")
	}
	if cfg.CaptchaProvider == "turnstile" && cfg.TurnstileSecretKey == "" {
		log.Fatal("TURNSTILE_SECRET_KEY must be set when CAPTCHA_PROVIDER=turnstile")
	}

	return cfg
}

// getEnv returns the value of the environment variable named by key,
// or fallback if the variable is empty or unset.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvAsBool parses an environment variable as a boolean.
func getEnvAsBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		switch v {
		case "true", "1", "yes", "TRUE", "True", "on", "ON":
			return true
		case "false", "0", "no", "FALSE", "False", "off", "OFF":
			return false
		}
	}
	return fallback
}

// getEnvAsInt parses an environment variable as an integer.
func getEnvAsInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var val int
		if _, err := fmt.Sscanf(v, "%d", &val); err == nil {
			return val
		}
	}
	return fallback
}

// GetDSN returns the correctly formatted DSN string based on DBDriver.
func (c *Config) GetDSN() string {
	switch c.DBDriver {
	case "mysql", "mariadb":
		auth := c.DBUser
		if c.DBPassword != "" {
			auth += ":" + c.DBPassword
		}
		return fmt.Sprintf("%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			auth, c.DBHost, c.DBPort, c.DBName)
	default: // postgres
		return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s",
			c.DBHost, c.DBUser, c.DBPassword, c.DBName, c.DBPort, c.DBSSLMode)
	}
}
