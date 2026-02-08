package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	OpenAlex OpenAlexConfig `yaml:"openalex"`
	Gemini   GeminiConfig   `yaml:"gemini"`
	Agent    AgentConfig    `yaml:"agent"`
	Scanner  ScannerConfig  `yaml:"scanner"`
	Resend   ResendConfig   `yaml:"resend"`
}

type ServerConfig struct {
	Port         int    `yaml:"port"`
	DatabasePath string `yaml:"database_path"`
}

type OpenAlexConfig struct {
	Email  string `yaml:"email"`
	APIKey string `yaml:"api_key"`
}

type GeminiConfig struct {
	APIKey string `yaml:"api_key"`
	Model  string `yaml:"model"`
}

type AgentConfig struct {
	EnrichmentPrompt  string `yaml:"enrichment_prompt"`
	CitedAuthorPrompt string `yaml:"cited_author_prompt"`
}

type ScannerConfig struct {
	DefaultThreshold float64 `yaml:"default_threshold"`
	MaxTopics        int     `yaml:"max_topics"`
	MaxCitedAuthors  int     `yaml:"max_cited_authors"`
	LookbackDays     int     `yaml:"lookback_days"`
	ImpactWeight     float64 `yaml:"impact_weight"`
}

type ResendConfig struct {
	APIKey string `yaml:"api_key"`
	From   string `yaml:"from"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{
		Server: ServerConfig{
			Port:         8080,
			DatabasePath: "./science-newsletter.db",
		},
		Scanner: ScannerConfig{
			DefaultThreshold: 0.5,
			MaxTopics:        10,
			MaxCitedAuthors:  20,
			LookbackDays:     7,
			ImpactWeight:     0.3,
		},
		Gemini: GeminiConfig{
			Model: "gemini-2.5-flash",
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Load .env file (does not overwrite existing env vars)
	loadDotenv(".env")

	// Environment variable overrides
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		cfg.Gemini.APIKey = v
	}
	if v := os.Getenv("OPENALEX_EMAIL"); v != "" {
		cfg.OpenAlex.Email = v
	}
	if v := os.Getenv("OPENALEX_API_KEY"); v != "" {
		cfg.OpenAlex.APIKey = v
	}
	if v := os.Getenv("RESEND_API_KEY"); v != "" {
		cfg.Resend.APIKey = v
	}
	if v := os.Getenv("PORT"); v != "" {
		var port int
		if _, err := fmt.Sscanf(v, "%d", &port); err == nil {
			cfg.Server.Port = port
		}
	}

	return cfg, nil
}

// loadDotenv reads a .env file and sets any variables not already present in the environment.
func loadDotenv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // missing .env is fine
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		// Strip surrounding quotes
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		// Don't overwrite existing env vars
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
}
