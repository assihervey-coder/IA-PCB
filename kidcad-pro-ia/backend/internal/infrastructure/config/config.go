// Package config loads the runtime configuration of the backend from the
// environment (KIDCAD_* variables), with an optional JSON file
// (KIDCAD_CONFIG) merged underneath: environment variables always win, the
// file only fills the variables that were left unset (contracts.md §5).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Config is the resolved runtime configuration of the backend.
type Config struct {
	HTTPPort       string   // port d'écoute HTTP (KIDCAD_HTTP_PORT, 8080)
	DBURL          string   // DSN PostgreSQL (KIDCAD_DB_URL, "" = adaptateur mémoire)
	AIAddr         string   // adresse gRPC du moteur IA (KIDCAD_AI_ADDR, localhost:50051)
	LogLevel       string   // niveau slog (KIDCAD_LOG_LEVEL, info)
	DataDir        string   // répertoire des exports (KIDCAD_DATA_DIR, ./data)
	AppEnv         string   // environnement logique (KIDCAD_APP_ENV, development)
	ConfigPath     string   // chemin du fichier JSON optionnel (KIDCAD_CONFIG)
	AllowedOrigins []string // CORS (KIDCAD_ALLOWED_ORIGINS, *)
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
	}

	// Fichier JSON optionnel : seuls les champs non définis par
	// l'environnement seront retenus.
	cfgPath := strings.TrimSpace(os.Getenv("KIDCAD_CONFIG"))
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
	if v := os.Getenv("KIDCAD_HTTP_PORT"); v != "" {
		cfg.HTTPPort = v
	}
	if v := os.Getenv("KIDCAD_DB_URL"); v != "" {
		cfg.DBURL = v
	}
	if v := os.Getenv("KIDCAD_AI_ADDR"); v != "" {
		cfg.AIAddr = v
	}
	if v := os.Getenv("KIDCAD_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("KIDCAD_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("KIDCAD_APP_ENV"); v != "" {
		cfg.AppEnv = v
	}
	if v := os.Getenv("KIDCAD_ALLOWED_ORIGINS"); v != "" {
		cfg.AllowedOrigins = splitList(v)
	}
	return cfg
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
	if fc.HTTPPort != "" && os.Getenv("KIDCAD_HTTP_PORT") == "" {
		cfg.HTTPPort = fc.HTTPPort
	}
	if fc.DBURL != "" && os.Getenv("KIDCAD_DB_URL") == "" {
		cfg.DBURL = fc.DBURL
	}
	if fc.AIAddr != "" && os.Getenv("KIDCAD_AI_ADDR") == "" {
		cfg.AIAddr = fc.AIAddr
	}
	if fc.LogLevel != "" && os.Getenv("KIDCAD_LOG_LEVEL") == "" {
		cfg.LogLevel = fc.LogLevel
	}
	if fc.DataDir != "" && os.Getenv("KIDCAD_DATA_DIR") == "" {
		cfg.DataDir = fc.DataDir
	}
	if fc.AppEnv != "" && os.Getenv("KIDCAD_APP_ENV") == "" {
		cfg.AppEnv = fc.AppEnv
	}
	if len(fc.AllowedOrigins) > 0 && os.Getenv("KIDCAD_ALLOWED_ORIGINS") == "" {
		cfg.AllowedOrigins = fc.AllowedOrigins
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
