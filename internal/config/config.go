package config

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/shhac/lib-agent-cli/creds"
	"github.com/shhac/lib-agent-cli/xdg"
)

const DefaultHost = "https://us.posthog.com"

type Config struct {
	DefaultProfile string             `json:"default_profile,omitempty"`
	Defaults       Defaults           `json:"defaults,omitempty"`
	Profiles       map[string]Profile `json:"profiles"`
	// MCPWrites lets commands run through the MCP server change PostHog. It is
	// off until a person turns it on, so connecting an agent grants reads only.
	MCPWrites bool `json:"mcp_writes,omitempty"`
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
	// ProjectAliases name this profile's projects (say staging and prod), so
	// --project staging stays tied to the profile's host and organization.
	ProjectAliases map[string]int `json:"project_aliases,omitempty"`
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
	return xdg.ConfigDir("agent-posthog")
}

func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

// store is config.json's file: 0600 writes into a 0700 parent, atomic
// replacement, and Update for a locked read-modify-write. This used to be
// hand-rolled with os.ReadFile/os.WriteFile, which carried a lost-update
// race — two concurrent CLI invocations (e.g. `auth add` racing
// `auth set-default`) could each build their write from a snapshot taken
// before the other landed, silently erasing one of them.
func store() creds.Store {
	return creds.Store{Path: ConfigPath()}
}

func Read() *Config {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cache != nil {
		return cache
	}
	cache = loadConfig()
	return cache
}

// loadConfig reads config.json fresh from disk, bypassing the package cache.
// It is the single definition of "what a from-scratch read looks like",
// shared by Read (cached) and updateConfig (which must never hand a mutate
// callback the stale in-memory cache while holding the store's lock).
func loadConfig() *Config {
	var cfg Config
	if err := store().Load(&cfg); err != nil {
		return defaultConfig()
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}
	return &cfg
}

func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	cache = nil
}

// updateConfig applies mutate to a freshly loaded config under ONE exclusive
// lock spanning read, mutate, and write, so two concurrent invocations
// serialize instead of each building its write from a stale snapshot. The
// package-level cache is bypassed entirely while the lock is held — mutate
// always sees what store().Update just loaded from disk, never the cache —
// and is invalidated afterward so a later Read() cannot hand back the
// pre-write value.
//
// An error from mutate aborts before anything is persisted, which is what
// keeps the "unknown alias / unknown key" paths from touching config.json.
func updateConfig(mutate func(cfg *Config) error) error {
	var cfg Config
	err := store().Update(&cfg, func() error {
		if cfg.Profiles == nil {
			cfg.Profiles = make(map[string]Profile)
		}
		return mutate(&cfg)
	})

	cacheMu.Lock()
	cache = nil
	cacheMu.Unlock()

	return err
}

func StoreProfile(alias string, profile Profile) error {
	return updateConfig(func(cfg *Config) error {
		cfg.Profiles[alias] = normalizeProfile(profile)
		if cfg.DefaultProfile == "" {
			cfg.DefaultProfile = alias
		}
		return nil
	})
}

func RemoveProfile(alias string) error {
	return updateConfig(func(cfg *Config) error {
		delete(cfg.Profiles, alias)
		if cfg.DefaultProfile == alias {
			cfg.DefaultProfile = ""
			for name := range cfg.Profiles {
				cfg.DefaultProfile = name
				break
			}
		}
		return nil
	})
}

func SetDefault(alias string) error {
	return updateProfile(alias, func(cfg *Config, _ *Profile) error {
		cfg.DefaultProfile = alias
		return nil
	})
}

func UpdateProfile(alias string, update func(Profile) Profile) error {
	return updateProfile(alias, func(_ *Config, profile *Profile) error {
		*profile = normalizeProfile(update(*profile))
		return nil
	})
}

// SetProjectAlias names a project ID within a profile.
func SetProjectAlias(alias, name string, projectID int) error {
	return updateProfile(alias, func(_ *Config, profile *Profile) error {
		if profile.ProjectAliases == nil {
			profile.ProjectAliases = map[string]int{}
		}
		profile.ProjectAliases[name] = projectID
		return nil
	})
}

// UnsetProjectAlias removes a project alias from a profile.
func UnsetProjectAlias(alias, name string) error {
	return updateProfile(alias, func(_ *Config, profile *Profile) error {
		if _, ok := profile.ProjectAliases[name]; !ok {
			return fmt.Errorf("profile %q has no project alias %q", alias, name)
		}
		delete(profile.ProjectAliases, name)
		return nil
	})
}

// updateProfile changes a configured profile under the config lock, refusing
// one that does not exist.
func updateProfile(alias string, update func(*Config, *Profile) error) error {
	return updateConfig(func(cfg *Config) error {
		profile, ok := cfg.Profiles[alias]
		if !ok {
			return fmt.Errorf("profile %q is not configured", alias)
		}
		if err := update(cfg, &profile); err != nil {
			return err
		}
		cfg.Profiles[alias] = profile
		return nil
	})
}

// SetMCPWrites turns writes through the MCP server on or off.
func SetMCPWrites(enabled bool) error {
	return updateConfig(func(cfg *Config) error {
		cfg.MCPWrites = enabled
		return nil
	})
}

// UpdateDefaults changes the command defaults under the config lock.
func UpdateDefaults(update func(*Defaults)) error {
	return updateConfig(func(cfg *Config) error {
		update(&cfg.Defaults)
		return nil
	})
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
