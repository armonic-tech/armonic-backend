package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port              string
	Host              string
	ServerName        string
	ServerDescription string
	JWTSecret         string
	LogLevel          string
	LogFile           string
	DBPath            string
	MaxMsgLen         int
	ClaimPassword     string
	AllowedOrigins    []string
	TrustedProxies    []string
	Upload            UploadConfig
	Pow               PowConfig
	RTC               struct {
		StunServers []string `json:"stun_servers"`
		TurnServers []string `json:"turn_servers"`
	} `json:"rtc"`
}

type UploadConfig struct {
	Dir            string
	MaxBytes       int64
	MaxImageWidth  int
	MaxImageHeight int
	MaxImagePixels int64
	ThumbnailSize  int
	RatePerMin     int
	Burst          int
	MaxConcurrent  int
}

type PowConfig struct {
	Enabled   bool
	MaxNumber int64
	TTL       time.Duration
}

type fileConfig struct {
	App struct {
		Password string `json:"password"`
	} `json:"app"`
}

func Load() (Config, error) {
	maxLen, err := strconv.Atoi(getEnv("MAX_MSG_LEN", "2000"))
	if err != nil || maxLen <= 0 {
		maxLen = 2000
	}

	cfg := Config{
		Port:              getEnv("PORT", "8080"),
		Host:              getEnv("HOST", "localhost"),
		ServerName:        getEnv("SERVER_NAME", "Armonic"),
		ServerDescription: getEnv("SERVER_DESCRIPTION", ""),
		JWTSecret:         getEnv("JWT_SECRET", "change-me"),
		LogLevel:          getEnv("LOG_LEVEL", "info"),
		LogFile:           getEnv("LOG_FILE", ""),
		DBPath:            getEnv("DATABASE_URL", "postgres://armonic:armonic@localhost:5432/armonic?sslmode=disable"),
		MaxMsgLen:         maxLen,
	}

	// RTC configuration
	stunServers := getEnv("STUN_SERVERS", "stun:stun.l.google.com:19302")
	cfg.RTC.StunServers = strings.Split(stunServers, ",")

	turnServers := getEnv("TURN_SERVERS", "")
	if turnServers != "" {
		cfg.RTC.TurnServers = strings.Split(turnServers, ",")
	}

	cfg.AllowedOrigins = getEnvList("CORS_ALLOWED_ORIGINS")
	cfg.TrustedProxies = getEnvList("TRUSTED_PROXIES")

	cfg.Upload = UploadConfig{
		Dir:            getEnv("UPLOAD_DIR", "./data/uploads"),
		MaxBytes:       getEnvInt64("MAX_UPLOAD_BYTES", 25*1024*1024),
		MaxImageWidth:  getEnvInt("MAX_IMAGE_WIDTH", 8000),
		MaxImageHeight: getEnvInt("MAX_IMAGE_HEIGHT", 8000),
		MaxImagePixels: getEnvInt64("MAX_IMAGE_PIXELS", 40_000_000),
		ThumbnailSize:  getEnvInt("THUMBNAIL_SIZE", 256),
		RatePerMin:     getEnvInt("UPLOAD_RATE_PER_MIN", 20),
		Burst:          getEnvInt("UPLOAD_BURST", 5),
		MaxConcurrent:  getEnvInt("MAX_CONCURRENT_UPLOADS", 4),
	}

	cfg.Pow = PowConfig{
		Enabled:   getEnvBool("POW_ENABLED", false),
		MaxNumber: getEnvInt64("POW_MAX_NUMBER", 50_000),
		TTL:       time.Duration(getEnvInt64("POW_TTL_SECONDS", 300)) * time.Second,
	}

	configPath := getEnv("CONFIG_FILE", "config.json")
	fc, err := loadFileConfig(configPath)
	if err != nil {
		return Config{}, err
	}
	if fc.App.Password == "" {
		return Config{}, fmt.Errorf("%s: app.password is required to claim the server", configPath)
	}
	cfg.ClaimPassword = fc.App.Password

	return cfg, nil
}

func loadFileConfig(path string) (fileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileConfig{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return fileConfig{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return fc, nil
}

func (c Config) BaseURL() string {
	return "http://" + c.Host + ":" + c.Port
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	return int(getEnvInt64(key, int64(fallback)))
}

func getEnvInt64(key string, fallback int64) int64 {
	v, err := strconv.ParseInt(os.Getenv(key), 10, 64)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func getEnvBool(key string, fallback bool) bool {
	v, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return fallback
	}
	return v
}

func getEnvList(key string) []string {
	var out []string
	for v := range strings.SplitSeq(os.Getenv(key), ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
