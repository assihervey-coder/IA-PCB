// Package config loads the runtime configuration of the backend from the
// environment (YAHRIACAD_* variables), with an optional JSON file
// (YAHRIACAD_CONFIG) merged underneath: environment variables always win, the
// file only fills the variables that were left unset (contracts.md §5).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the resolved runtime configuration of the backend.
type Config struct {
	HTTPPort       string   // port d'écoute HTTP (YAHRIACAD_HTTP_PORT, 8080)
	DBURL          string   // DSN PostgreSQL (YAHRIACAD_DB_URL, "" = adaptateur mémoire)
	AIAddr         string   // adresse gRPC du moteur IA (YAHRIACAD_AI_ADDR, localhost:50051)
	LogLevel       string   // niveau slog (YAHRIACAD_LOG_LEVEL, info)
	DataDir        string   // répertoire des exports (YAHRIACAD_DATA_DIR, ./data)
	AppEnv         string   // environnement logique (YAHRIACAD_APP_ENV, development)
	ConfigPath     string   // chemin du fichier JSON optionnel (YAHRIACAD_CONFIG)
	AllowedOrigins []string // CORS strict (YAHRIACAD_ALLOWED_ORIGINS, "*" par défaut)

	// Authentification JWT (HS256). La protection est activée quand
	// YAHRIACAD_AUTH_ENABLED vaut "true" OU qu'un secret est configuré.
	AuthEnabled bool          // YAHRIACAD_AUTH_ENABLED (false par défaut)
	JWTSecret   string        // YAHRIACAD_JWT_SECRET (obligatoire en production)
	JWTTTL      time.Duration // YAHRIACAD_JWT_TTL (24h par défaut)
	AuthUsers   []string      // YAHRIACAD_AUTH_USERS ("user:pass;user2:$2a$...")

	// Limitation de débit des routes IA coûteuses (place/route/optimize…).
	// RPS <= 0 désactive la limite (utile pour les tests).
	AIRateRPS   float64 // YAHRIACAD_AI_RATE_RPS (2 req/s par IP par défaut)
	AIRateBurst int     // YAHRIACAD_AI_RATE_BURST (10 par défaut)
}

// fileConfig mirrors the JSON configuration file schema (same keys as
// configs/{development,production}/appsettings*.json).
type fileConfig struct {
	HTTPPort       string   `json:"http_port"`
	DBURL          string   `json:"db_url"`
	AIAddr         string   `json:"ai_addr"`
	LogLevel       string   `json:"log_level"`
	DataDir        string   `json:"data_dir"`
	AppEnv         string   `json:"app_env"`
	AllowedOrigins []string `json:"allowed_origins"`

	AuthEnabled *bool    `json:"auth_enabled"`
	JWTSecret   string   `json:"jwt_secret"`
	JWTTTL      string   `json:"jwt_ttl"`
	AuthUsers   []string `json:"auth_users"`
	AIRateRPS   *float64 `json:"ai_rate_rps"`
	AIRateBurst *int     `json:"ai_rate_burst"`
}

// Load resolves the configuration: defaults <- optional JSON file <- env.
func Load() Config {
	cfg := Config{
		HTTPPort:       "8080",
		DBURL:          "",
		AIAddr:         "localhost:50051",
		LogLevel:       "info",
		DataDir:        "./data",
		AppEnv:         "development",
		AllowedOrigins: []string{"*"},
		JWTTTL:         24 * time.Hour,
		AIRateRPS:      2,
		AIRateBurst:    10,
	}

	// Fichier JSON optionnel : seuls les champs non définis par
	// l'environnement seront retenus.
	cfgPath := strings.TrimSpace(os.Getenv("YAHRIACAD_CONFIG"))
	cfg.ConfigPath = cfgPath
	if cfgPath != "" {
		if fc, err := loadFile(cfgPath); err != nil {
			// Un fichier illisible ne doit pas empêcher le démarrage :
			// l'erreur est signalée via l'environnement applicatif.
			fmt.Fprintf(os.Stderr, "config : fichier %s ignoré : %v\n", cfgPath, err)
		} else {
			applyFile(&cfg, fc)
		}
	}

	// L'environnement a la priorité finale.
	if v := os.Getenv("YAHRIACAD_HTTP_PORT"); v != "" {
		cfg.HTTPPort = v
	}
	if v := os.Getenv("YAHRIACAD_DB_URL"); v != "" {
		cfg.DBURL = v
	}
	if v := os.Getenv("YAHRIACAD_AI_ADDR"); v != "" {
		cfg.AIAddr = v
	}
	if v := os.Getenv("YAHRIACAD_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("YAHRIACAD_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("YAHRIACAD_APP_ENV"); v != "" {
		cfg.AppEnv = v
	}
	if v := os.Getenv("YAHRIACAD_ALLOWED_ORIGINS"); v != "" {
		cfg.AllowedOrigins = splitList(v)
	}

	if v := os.Getenv("YAHRIACAD_AUTH_ENABLED"); v != "" {
		cfg.AuthEnabled = isTruthy(v)
	}
	if v := os.Getenv("YAHRIACAD_JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	}
	if v := os.Getenv("YAHRIACAD_JWT_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.JWTTTL = d
		}
	}
	if v := os.Getenv("YAHRIACAD_AUTH_USERS"); v != "" {
		cfg.AuthUsers = splitList(v)
	}
	if v := os.Getenv("YAHRIACAD_AI_RATE_RPS"); v != "" {
		if r, err := strconv.ParseFloat(v, 64); err == nil && r >= 0 {
			cfg.AIRateRPS = r
		}
	}
	if v := os.Getenv("YAHRIACAD_AI_RATE_BURST"); v != "" {
		if b, err := strconv.Atoi(v); err == nil && b >= 0 {
			cfg.AIRateBurst = b
		}
	}

	// Un secret configuré signale l'intention de protéger l'API : le
	// drapeau explicite n'est requis ni par la CI ni par le dev local.
	if strings.TrimSpace(cfg.JWTSecret) != "" {
		cfg.AuthEnabled = true
	}
	return cfg
}

// isTruthy parses a boolean environment value (1/true/yes/on, insensible à
// la casse) ; toute autre valeur vaut false.
func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// loadFile reads and parses the optional JSON configuration file.
func loadFile(path string) (*fileConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fc := &fileConfig{}
	if err := json.Unmarshal(raw, fc); err != nil {
		return nil, fmt.Errorf("JSON invalide : %w", err)
	}
	return fc, nil
}

// applyFile fills only the fields of cfg that the environment has not set.
func applyFile(cfg *Config, fc *fileConfig) {
	if fc == nil {
		return
	}
	if fc.HTTPPort != "" && os.Getenv("YAHRIACAD_HTTP_PORT") == "" {
		cfg.HTTPPort = fc.HTTPPort
	}
	if fc.DBURL != "" && os.Getenv("YAHRIACAD_DB_URL") == "" {
		cfg.DBURL = fc.DBURL
	}
	if fc.AIAddr != "" && os.Getenv("YAHRIACAD_AI_ADDR") == "" {
		cfg.AIAddr = fc.AIAddr
	}
	if fc.LogLevel != "" && os.Getenv("YAHRIACAD_LOG_LEVEL") == "" {
		cfg.LogLevel = fc.LogLevel
	}
	if fc.DataDir != "" && os.Getenv("YAHRIACAD_DATA_DIR") == "" {
		cfg.DataDir = fc.DataDir
	}
	if fc.AppEnv != "" && os.Getenv("YAHRIACAD_APP_ENV") == "" {
		cfg.AppEnv = fc.AppEnv
	}
	if len(fc.AllowedOrigins) > 0 && os.Getenv("YAHRIACAD_ALLOWED_ORIGINS") == "" {
		cfg.AllowedOrigins = fc.AllowedOrigins
	}
	if fc.AuthEnabled != nil && os.Getenv("YAHRIACAD_AUTH_ENABLED") == "" {
		cfg.AuthEnabled = *fc.AuthEnabled
	}
	if fc.JWTSecret != "" && os.Getenv("YAHRIACAD_JWT_SECRET") == "" {
		cfg.JWTSecret = fc.JWTSecret
	}
	if fc.JWTTTL != "" && os.Getenv("YAHRIACAD_JWT_TTL") == "" {
		if d, err := time.ParseDuration(fc.JWTTTL); err == nil && d > 0 {
			cfg.JWTTTL = d
		}
	}
	if len(fc.AuthUsers) > 0 && os.Getenv("YAHRIACAD_AUTH_USERS") == "" {
		cfg.AuthUsers = fc.AuthUsers
	}
	if fc.AIRateRPS != nil && os.Getenv("YAHRIACAD_AI_RATE_RPS") == "" {
		cfg.AIRateRPS = *fc.AIRateRPS
	}
	if fc.AIRateBurst != nil && os.Getenv("YAHRIACAD_AI_RATE_BURST") == "" {
		cfg.AIRateBurst = *fc.AIRateBurst
	}
	if strings.TrimSpace(cfg.JWTSecret) != "" {
		cfg.AuthEnabled = true
	}
}

// splitList parses a comma-separated list, trimming blanks.
func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"*"}
	}
	return out
}
