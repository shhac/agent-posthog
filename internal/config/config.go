package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const DefaultHost = "https://us.posthog.com"

type Config struct {
	DefaultProfile string             `json:"default_profile,omitempty"`
	Defaults       Defaults           `json:"defaults,omitempty"`
	Profiles       map[string]Profile `json:"profiles"`
}

type Defaults struct {
	TimeoutMS  *int `json:"timeout_ms,omitempty"`
	MaxRetries *int `json:"max_retries,omitempty"`
}

type Profile struct {
	Host           string `json:"host,omitempty"`
	OrganizationID string `json:"organization_id,omitempty"`
	ProjectID      int    `json:"project_id,omitempty"`
	EnvironmentID  int    `json:"environment_id,omitempty"`
}

var (
	cache       *Config
	cacheMu     sync.Mutex
	overrideDir string
)

func SetConfigDir(dir string) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	overrideDir = dir
	cache = nil
}

func ConfigDir() string {
	if overrideDir != "" {
		return overrideDir
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "agent-posthog")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "agent-posthog")
}

func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

func Read() *Config {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cache != nil {
		return cache
	}
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		cache = defaultConfig()
		return cache
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		cache = defaultConfig()
		return cache
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}
	cache = &cfg
	return cache
}

func Write(cfg *Config) error {
	cacheMu.Lock()
	cache = nil
	cacheMu.Unlock()

	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(), append(data, '\n'), 0o644)
}

func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache = nil
}

func StoreProfile(alias string, profile Profile) error {
	cfg := Read()
	profile = normalizeProfile(profile)
	cfg.Profiles[alias] = profile
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = alias
	}
	return Write(cfg)
}

func RemoveProfile(alias string) error {
	cfg := Read()
	delete(cfg.Profiles, alias)
	if cfg.DefaultProfile == alias {
		cfg.DefaultProfile = ""
		for name := range cfg.Profiles {
			cfg.DefaultProfile = name
			break
		}
	}
	return Write(cfg)
}

func SetDefault(alias string) error {
	cfg := Read()
	if _, ok := cfg.Profiles[alias]; !ok {
		return fmt.Errorf("profile %q is not configured", alias)
	}
	cfg.DefaultProfile = alias
	return Write(cfg)
}

func UpdateProfile(alias string, update func(Profile) Profile) error {
	cfg := Read()
	profile, ok := cfg.Profiles[alias]
	if !ok {
		return fmt.Errorf("profile %q is not configured", alias)
	}
	cfg.Profiles[alias] = normalizeProfile(update(profile))
	return Write(cfg)
}

func SetDefaultValue(key string, value int) error {
	cfg := Read()
	switch key {
	case "timeout_ms":
		cfg.Defaults.TimeoutMS = intPtr(value)
	case "max_retries":
		cfg.Defaults.MaxRetries = intPtr(value)
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return Write(cfg)
}

func UnsetDefaultValue(key string) error {
	cfg := Read()
	switch key {
	case "timeout_ms":
		cfg.Defaults.TimeoutMS = nil
	case "max_retries":
		cfg.Defaults.MaxRetries = nil
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return Write(cfg)
}

func defaultConfig() *Config {
	return &Config{Profiles: make(map[string]Profile)}
}

func normalizeProfile(profile Profile) Profile {
	if profile.Host == "" {
		profile.Host = DefaultHost
	}
	return profile
}

func intPtr(value int) *int {
	return &value
}
