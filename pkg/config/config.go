// Package config provides configuration management for pgbranch.
// It handles loading, saving, and validating configuration files
// that store database connection settings and remote storage backends.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// DirName is the name of the pgbranch configuration directory.
	DirName = ".pgbranch"
	// ConfigFileName is the name of the main configuration file.
	ConfigFileName = "config.json"
	// SnapshotsDirName is the name of the directory containing snapshot metadata.
	SnapshotsDirName = "snapshots"
)

// ErrNotInitialized is returned when a workspace directory has no .pgbranch
// directory. Callers can test for it with errors.Is.
var ErrNotInitialized = errors.New("pgbranch not initialized")

// RemoteConfig holds configuration for a remote storage backend.
type RemoteConfig struct {
	// Name is the name of this remote (e.g., "origin")
	Name string `json:"name"`

	// Type is the remote type (fs, s3, gcs)
	Type string `json:"type"`

	// URL is the remote URL
	URL string `json:"url"`

	// Options contains type-specific options
	Options map[string]string `json:"options,omitempty"`
}

// Config holds the main configuration for pgbranch, including
// database connection settings and remote storage configurations.
type Config struct {
	// Root is the workspace directory that holds the .pgbranch directory.
	// It is set by Load and used by Save; it is never serialized. Library
	// callers that build a Config in memory must set it before calling Save.
	Root string `json:"-"`

	Database string `json:"database"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password,omitempty"`

	Remotes map[string]*RemoteConfig `json:"remotes,omitempty"`

	DefaultRemote string `json:"default_remote,omitempty"`
}

// DefaultConfig returns a new Config with default values for PostgreSQL connection.
func DefaultConfig() *Config {
	return &Config{
		Host: "localhost",
		Port: 5432,
		User: "postgres",
	}
}

// RootDir returns the path to the pgbranch configuration directory inside
// the given workspace directory.
func RootDir(dir string) string {
	return filepath.Join(dir, DirName)
}

// ConfigPath returns the path to the configuration file inside the given
// workspace directory.
func ConfigPath(dir string) string {
	return filepath.Join(RootDir(dir), ConfigFileName)
}

// SnapshotsDir returns the path to the snapshots directory inside the given
// workspace directory.
func SnapshotsDir(dir string) string {
	return filepath.Join(RootDir(dir), SnapshotsDirName)
}

// WorkingDir returns the current working directory, the default workspace
// for the command line client. Library callers should pass an explicit
// directory instead of relying on process state.
func WorkingDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	return cwd, nil
}

// IsInitialized reports whether pgbranch has been initialized in the given
// workspace directory.
func IsInitialized(dir string) bool {
	_, err := os.Stat(RootDir(dir))
	return err == nil
}

// Load reads and parses the configuration file from the given workspace
// directory. The returned Config records dir as its Root, so Save writes
// back to the same place.
func Load(dir string) (*Config, error) {
	data, err := os.ReadFile(ConfigPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w in %s", ErrNotInitialized, dir)
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	cfg.Root = dir

	return &cfg, nil
}

// Save writes the configuration to the configuration file under Root.
func (c *Config) Save() error {
	if c.Root == "" {
		return fmt.Errorf("config Root is not set")
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	if err := os.WriteFile(ConfigPath(c.Root), data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ConnectionString returns a PostgreSQL connection string for the configured database.
func (c *Config) ConnectionString() string {
	connStr := fmt.Sprintf("host=%s port=%d user=%s dbname=%s sslmode=disable",
		c.Host, c.Port, c.User, c.Database)
	if c.Password != "" {
		connStr += fmt.Sprintf(" password=%s", c.Password)
	}
	return connStr
}

// ConnectionURLForDB returns a PostgreSQL connection URL for the specified database name.
func (c *Config) ConnectionURLForDB(dbName string) string {
	if c.Password != "" {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
			c.User, c.Password, c.Host, c.Port, dbName)
	}
	return fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=disable",
		c.User, c.Host, c.Port, dbName)
}

// Validate checks that all required configuration fields are set.
func (c *Config) Validate() error {
	if c.Database == "" {
		return fmt.Errorf("database name is required")
	}
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}
	if c.Port == 0 {
		return fmt.Errorf("port is required")
	}
	if c.User == "" {
		return fmt.Errorf("user is required")
	}
	return nil
}

// EnsureDir creates the specified directory and any necessary parents if they don't exist.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// AddRemote adds a new remote configuration. If this is the first remote,
// it will be set as the default.
func (c *Config) AddRemote(remote *RemoteConfig) error {
	if remote.Name == "" {
		return fmt.Errorf("remote name is required")
	}
	if c.Remotes == nil {
		c.Remotes = make(map[string]*RemoteConfig)
	}
	if _, exists := c.Remotes[remote.Name]; exists {
		return fmt.Errorf("remote '%s' already exists", remote.Name)
	}
	c.Remotes[remote.Name] = remote

	if c.DefaultRemote == "" {
		c.DefaultRemote = remote.Name
	}

	return nil
}

// RemoveRemote removes a remote configuration by name. If the removed remote
// was the default, another remote will be set as default if available.
func (c *Config) RemoveRemote(name string) error {
	if c.Remotes == nil {
		return fmt.Errorf("remote '%s' not found", name)
	}
	if _, exists := c.Remotes[name]; !exists {
		return fmt.Errorf("remote '%s' not found", name)
	}
	delete(c.Remotes, name)

	if c.DefaultRemote == name {
		c.DefaultRemote = ""
		for remoteName := range c.Remotes {
			c.DefaultRemote = remoteName
			break
		}
	}

	return nil
}

// GetRemote returns the remote configuration by name. If name is empty,
// the default remote is returned.
func (c *Config) GetRemote(name string) (*RemoteConfig, error) {
	if name == "" {
		name = c.DefaultRemote
	}
	if name == "" {
		return nil, fmt.Errorf("no remote specified and no default remote configured")
	}
	if c.Remotes == nil {
		return nil, fmt.Errorf("remote '%s' not found", name)
	}
	remote, exists := c.Remotes[name]
	if !exists {
		return nil, fmt.Errorf("remote '%s' not found", name)
	}
	return remote, nil
}

// ListRemotes returns all configured remotes.
func (c *Config) ListRemotes() []*RemoteConfig {
	if c.Remotes == nil {
		return nil
	}
	remotes := make([]*RemoteConfig, 0, len(c.Remotes))
	for _, remote := range c.Remotes {
		remotes = append(remotes, remote)
	}
	return remotes
}

// SetDefaultRemote sets the default remote to use when no remote is specified.
func (c *Config) SetDefaultRemote(name string) error {
	if c.Remotes == nil || c.Remotes[name] == nil {
		return fmt.Errorf("remote '%s' not found", name)
	}
	c.DefaultRemote = name
	return nil
}
