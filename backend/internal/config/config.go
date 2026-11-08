package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port             string
	DatabaseURL      string
	S3Endpoint       string
	S3AccessKey      string
	S3SecretKey      string
	S3Bucket         string
	S3UseSSL         bool
	S3Region         string
	S3PublicURL      string
	MaxBotToken      string
	MaxBotName       string
	OperatorIDs      map[int64]bool
	MaxUpdateMode    string
	MaxWebhookURL    string
	MaxWebhookSecret string
	MaxRootCAPath    string
	DevMode          bool
}

func Load() *Config {
	operatorIDs := make(map[int64]bool)
	rawOps := os.Getenv("OPERATOR_IDS")
	for _, part := range strings.Split(rawOps, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if id, err := strconv.ParseInt(part, 10, 64); err == nil && id > 0 {
			operatorIDs[id] = true
		}
	}

	return &Config{
		Port:             getEnv("PORT", "8080"),
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://confectioner:confectioner_secret@localhost:5432/confectioner_db?sslmode=disable"),
		S3Endpoint:       getEnv("S3_ENDPOINT", "localhost:9000"),
		S3AccessKey:      getEnv("S3_ACCESS_KEY", "minioadmin"),
		S3SecretKey:      getEnv("S3_SECRET_KEY", "minioadmin_secret"),
		S3Bucket:         getEnv("S3_BUCKET", "confectioner-media"),
		S3UseSSL:         getEnvBool("S3_USE_SSL", false),
		S3Region:         getEnv("S3_REGION", "us-east-1"),
		S3PublicURL:      strings.TrimRight(os.Getenv("S3_PUBLIC_URL"), "/"),
		MaxBotToken:      strings.TrimSpace(os.Getenv("MAX_BOT_TOKEN")),
		MaxBotName:       strings.TrimPrefix(getEnv("MAX_BOT_NAME", "confectioner_bot"), "@"),
		OperatorIDs:      operatorIDs,
		MaxUpdateMode:    strings.ToLower(getEnv("MAX_UPDATE_MODE", "polling")),
		MaxWebhookURL:    strings.TrimSpace(os.Getenv("MAX_WEBHOOK_URL")),
		MaxWebhookSecret: getEnv("MAX_WEBHOOK_SECRET", "change_me_webhook_secret"),
		MaxRootCAPath:    getEnv("MAX_ROOT_CA_PATH", "/etc/ssl/certs/russian_trusted_root_ca.pem"),
		DevMode:          getEnvBool("DEV_MODE", true),
	}
}

func (c *Config) IsOperator(userID int64) bool {
	if len(c.OperatorIDs) == 0 {
		return true
	}
	return c.OperatorIDs[userID]
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	return v == "true" || v == "1" || v == "yes"
}

