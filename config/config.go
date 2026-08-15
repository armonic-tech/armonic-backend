package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
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
	PublicBaseURL     string
	AllowedOrigins    []string
	RTC               struct {
		StunServers []string `json:"stun_servers"`
		TurnServers []string `json:"turn_servers"`
	} `json:"rtc"`
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
		PublicBaseURL:     strings.TrimRight(strings.TrimSpace(getEnv("PUBLIC_BASE_URL", "")), "/"),
	}

	// RTC configuration
	stunServers := getEnv("STUN_SERVERS", "stun:stun.l.google.com:19302")
	cfg.RTC.StunServers = strings.Split(stunServers, ",")

	turnServers := getEnv("TURN_SERVERS", "")
	if turnServers != "" {
		cfg.RTC.TurnServers = strings.Split(turnServers, ",")
	}

	if origins := getEnv("CORS_ALLOWED_ORIGINS", ""); origins != "" {
		for o := range strings.SplitSeq(origins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				cfg.AllowedOrigins = append(cfg.AllowedOrigins, o)
			}
		}
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
