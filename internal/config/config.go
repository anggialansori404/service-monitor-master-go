package config

import (
	"fmt"
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

	BotToken  string
	JWTSecret string
	Port      string
}

// LoadConfig loads configuration from a .env file (if present) and
// falls back to system environment variables for any value not set.
func LoadConfig() *Config {
	// Best-effort: load .env if it exists, ignore error if missing.
	_ = godotenv.Load()

	return &Config{
		DBDriver:   getEnv("DB_DRIVER", "postgres"),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBUser:     getEnv("DB_USER", "postgres"),
		DBPassword: getEnv("DB_PASSWORD", "postgres"),
		DBName:     getEnv("DB_NAME", "monitoring"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBSSLMode:  getEnv("DB_SSLMODE", "disable"),

		DBMongoURI: getEnv("DB_MONGO_URI", "mongodb://localhost:27017/monitoring"),

		BotToken:  getEnv("BOT_TOKEN", ""),
		JWTSecret: getEnv("JWT_SECRET", "change-me-in-production"),
		Port:      getEnv("PORT", "8080"),
	}
 }

// getEnv returns the value of the environment variable named by key,
// or fallback if the variable is empty or unset.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
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
