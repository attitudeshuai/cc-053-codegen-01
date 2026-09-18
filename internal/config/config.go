package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	// Server
	ServerPort    string
	ServerHost    string
	ReadTimeout   int
	WriteTimeout  int

	// Database
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	// Redis
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// MinIO
	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
	MinIOBucket    string
	MinIOUseSSL    bool
	MinIOLocation  string

	// Asynq
	AsynqConcurrency int

	// App
	MaxUploadSize int64 // bytes
	ExternalURL   string
}

func Load() *Config {
	godotenv.Load()

	cfg := &Config{
		ServerPort:    getEnv("SERVER_PORT", "8000"),
		ServerHost:    getEnv("SERVER_HOST", "0.0.0.0"),
		ReadTimeout:   getEnvInt("READ_TIMEOUT", 600),
		WriteTimeout:  getEnvInt("WRITE_TIMEOUT", 600),
		DBHost:        getEnv("DB_HOST", "localhost"),
		DBPort:        getEnv("DB_PORT", "5432"),
		DBUser:        getEnv("DB_USER", "postgres"),
		DBPassword:    getEnv("DB_PASSWORD", "postgres"),
		DBName:        getEnv("DB_NAME", "dialect_corpus"),
		DBSSLMode:     getEnv("DB_SSLMODE", "disable"),
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),
		MinIOEndpoint:  getEnv("MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKey: getEnv("MINIO_ACCESS_KEY", "minioadmin"),
		MinIOSecretKey: getEnv("MINIO_SECRET_KEY", "minioadmin"),
		MinIOBucket:    getEnv("MINIO_BUCKET", "dialect-corpus"),
		MinIOUseSSL:    getEnvBool("MINIO_USE_SSL", false),
		MinIOLocation:  getEnv("MINIO_LOCATION", "cn-south-1"),
		AsynqConcurrency: getEnvInt("ASYNQ_CONCURRENCY", 2),
		MaxUploadSize:   int64(getEnvInt("MAX_UPLOAD_SIZE", 500*1024*1024)),
		ExternalURL:    getEnv("EXTERNAL_URL", "http://localhost:9053"),
	}
	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}