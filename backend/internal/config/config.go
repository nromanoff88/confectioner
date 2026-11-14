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
	RegistrationOpen bool
	Timezone         string
	MaxUpdateMode    string
	MaxWebhookURL    string
	MaxWebhookSecret string
	MaxRootCAPath    string
	DevMode          bool
}

func Load() *Config {
	operatorIDs := make(map[int64]bool)
	for _, part := range strings.Split(os.Getenv("OPERATOR_IDS"), ",") {
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
		RegistrationOpen: getEnvBool("REGISTRATION_OPEN", true),
		Timezone:         getEnv("APP_TIMEZONE", "Asia/Yekaterinburg"),
		MaxUpdateMode:    strings.ToLower(getEnv("MAX_UPDATE_MODE", "polling")),
		MaxWebhookURL:    strings.TrimSpace(os.Getenv("MAX_WEBHOOK_URL")),
		MaxWebhookSecret: getEnv("MAX_WEBHOOK_SECRET", "change_me_webhook_secret"),
		MaxRootCAPath:    getEnv("MAX_ROOT_CA_PATH", "/etc/ssl/certs/russian_trusted_root_ca.pem"),
		DevMode:          getEnvBool("DEV_MODE", false),
	}
}

// CanRegister reports whether a new user may create their own workspace.
// When registration is closed only users listed in OPERATOR_IDS may.
func (c *Config) CanRegister(userID int64) bool {
	return c.RegistrationOpen || c.OperatorIDs[userID]
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
