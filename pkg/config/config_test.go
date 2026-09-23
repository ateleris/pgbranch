package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 5432, cfg.Port)
	assert.Equal(t, "postgres", cfg.User)
	assert.Empty(t, cfg.Database)
	assert.Empty(t, cfg.Password)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: &Config{
				Databases: []DatabaseConfig{{Name: "testdb"}},
				Host:      "localhost",
				Port:      5432,
				User:      "postgres",
			},
			wantErr: false,
		},
		{
			name: "valid config with password",
			config: &Config{
				Databases: []DatabaseConfig{{Name: "testdb"}},
				Host:      "localhost",
				Port:      5432,
				User:      "postgres",
				Password:  "secret",
			},
			wantErr: false,
		},
		{
			name: "valid config with multiple databases and strategies",
			config: &Config{
				Databases: []DatabaseConfig{{Name: "app"}, {Name: "app_identity", Strategy: "dump"}},
				Host:      "localhost",
				Port:      5432,
				User:      "postgres",
			},
			wantErr: false,
		},
		{
			name: "missing database",
			config: &Config{
				Host: "localhost",
				Port: 5432,
				User: "postgres",
			},
			wantErr: true,
			errMsg:  "database name is required",
		},
		{
			name: "empty database name",
			config: &Config{
				Databases: []DatabaseConfig{{Name: ""}},
				Host:      "localhost",
				Port:      5432,
				User:      "postgres",
			},
			wantErr: true,
			errMsg:  "database name is required",
		},
		{
			name: "duplicate database name",
			config: &Config{
				Databases: []DatabaseConfig{{Name: "app"}, {Name: "app"}},
				Host:      "localhost",
				Port:      5432,
				User:      "postgres",
			},
			wantErr: true,
			errMsg:  `duplicate database name "app"`,
		},
		{
			name: "invalid strategy",
			config: &Config{
				Databases: []DatabaseConfig{{Name: "app", Strategy: "bogus"}},
				Host:      "localhost",
				Port:      5432,
				User:      "postgres",
			},
			wantErr: true,
			errMsg:  "invalid clone strategy",
		},
		{
			name: "invalid new_branch_from",
			config: &Config{
				Databases:     []DatabaseConfig{{Name: "app"}},
				Host:          "localhost",
				Port:          5432,
				User:          "postgres",
				NewBranchFrom: "bogus",
			},
			wantErr: true,
			errMsg:  "invalid new_branch_from",
		},
		{
			name: "missing host",
			config: &Config{
				Databases: []DatabaseConfig{{Name: "testdb"}},
				Port:      5432,
				User:      "postgres",
			},
			wantErr: true,
			errMsg:  "host is required",
		},
		{
			name: "missing port",
			config: &Config{
				Databases: []DatabaseConfig{{Name: "testdb"}},
				Host:      "localhost",
				User:      "postgres",
			},
			wantErr: true,
			errMsg:  "port is required",
		},
		{
			name: "missing user",
			config: &Config{
				Databases: []DatabaseConfig{{Name: "testdb"}},
				Host:      "localhost",
				Port:      5432,
			},
			wantErr: true,
			errMsg:  "user is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		expected string
	}{
		{
			name: "without password",
			config: &Config{
				Database: "testdb",
				Host:     "localhost",
				Port:     5432,
				User:     "postgres",
			},
			expected: "host=localhost port=5432 user=postgres dbname=testdb sslmode=disable",
		},
		{
			name: "with password",
			config: &Config{
				Database: "testdb",
				Host:     "localhost",
				Port:     5432,
				User:     "postgres",
				Password: "secret",
			},
			expected: "host=localhost port=5432 user=postgres dbname=testdb sslmode=disable password=secret",
		},
		{
			name: "custom port",
			config: &Config{
				Database: "mydb",
				Host:     "db.example.com",
				Port:     5433,
				User:     "admin",
			},
			expected: "host=db.example.com port=5433 user=admin dbname=mydb sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.ConnectionString()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()

	err := os.MkdirAll(RootDir(tmpDir), 0755)
	require.NoError(t, err)

	cfg := &Config{
		Root:     tmpDir,
		Database: "testdb",
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "secret",
	}

	err = cfg.Save()
	require.NoError(t, err)

	_, err = os.Stat(ConfigPath(tmpDir))
	require.NoError(t, err)

	loadedCfg, err := Load(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, tmpDir, loadedCfg.Root)
	assert.Equal(t, cfg.Database, loadedCfg.Database)
	assert.Equal(t, cfg.Host, loadedCfg.Host)
	assert.Equal(t, cfg.Port, loadedCfg.Port)
	assert.Equal(t, cfg.User, loadedCfg.User)
	assert.Equal(t, cfg.Password, loadedCfg.Password)
}

func TestSaveWithoutRoot(t *testing.T) {
	cfg := &Config{Database: "testdb"}

	err := cfg.Save()
	assert.Error(t, err)
}

func TestLoadNotInitialized(t *testing.T) {
	_, err := Load(t.TempDir())

	assert.ErrorIs(t, err, ErrNotInitialized)
}

func TestIsInitialized(t *testing.T) {
	tmpDir := t.TempDir()

	assert.False(t, IsInitialized(tmpDir))

	err := os.MkdirAll(RootDir(tmpDir), 0755)
	require.NoError(t, err)

	assert.True(t, IsInitialized(tmpDir))
}

func TestPathHelpers(t *testing.T) {
	dir := filepath.Join("some", "workspace")

	assert.Equal(t, filepath.Join(dir, DirName), RootDir(dir))
	assert.Equal(t, filepath.Join(dir, DirName, ConfigFileName), ConfigPath(dir))
	assert.Equal(t, filepath.Join(dir, DirName, SnapshotsDirName), SnapshotsDir(dir))
}

func TestConnectionURLForDB(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		dbName   string
		expected string
	}{
		{
			name: "without password",
			config: &Config{
				Host: "localhost",
				Port: 5432,
				User: "postgres",
			},
			dbName:   "mydb",
			expected: "postgres://postgres@localhost:5432/mydb?sslmode=disable",
		},
		{
			name: "with password",
			config: &Config{
				Host:     "localhost",
				Port:     5432,
				User:     "postgres",
				Password: "secret",
			},
			dbName:   "mydb",
			expected: "postgres://postgres:secret@localhost:5432/mydb?sslmode=disable",
		},
		{
			name: "custom host and port",
			config: &Config{
				Host: "db.example.com",
				Port: 5433,
				User: "admin",
			},
			dbName:   "production",
			expected: "postgres://admin@db.example.com:5433/production?sslmode=disable",
		},
		{
			name: "dbName differs from config database",
			config: &Config{
				Database: "original",
				Host:     "localhost",
				Port:     5432,
				User:     "postgres",
			},
			dbName:   "snapshot_branch",
			expected: "postgres://postgres@localhost:5432/snapshot_branch?sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.ConnectionURLForDB(tt.dbName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEnsureDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pgbranch-ensuredir-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	t.Run("creates new directory", func(t *testing.T) {
		dir := filepath.Join(tmpDir, "newdir")
		err := EnsureDir(dir)
		require.NoError(t, err)

		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("creates nested directories", func(t *testing.T) {
		dir := filepath.Join(tmpDir, "a", "b", "c")
		err := EnsureDir(dir)
		require.NoError(t, err)

		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("no error if directory already exists", func(t *testing.T) {
		dir := filepath.Join(tmpDir, "existing")
		require.NoError(t, os.Mkdir(dir, 0755))

		err := EnsureDir(dir)
		assert.NoError(t, err)
	})
}

func TestNormalizeMigratesLegacyDatabase(t *testing.T) {
	cfg := &Config{Database: "legacydb"}
	cfg.Normalize()

	require.Len(t, cfg.Databases, 1)
	assert.Equal(t, "legacydb", cfg.Databases[0].Name)
	assert.Equal(t, "", cfg.Databases[0].Strategy)
	assert.Equal(t, "legacydb", cfg.Database)
	assert.Equal(t, "main", cfg.BaselineBranch)
	assert.Equal(t, "baseline", cfg.NewBranchFrom)
}

func TestNormalizeKeepsDatabaseInSyncWithPrimary(t *testing.T) {
	cfg := &Config{Databases: []DatabaseConfig{{Name: "app"}, {Name: "app_identity"}}}
	cfg.Normalize()

	assert.Equal(t, "app", cfg.Database)
	assert.Equal(t, "app", cfg.PrimaryDatabase())
	assert.Equal(t, []string{"app", "app_identity"}, cfg.DatabaseNames())
}

func TestNormalizeDoesNotOverrideExplicitValues(t *testing.T) {
	cfg := &Config{
		Databases:      []DatabaseConfig{{Name: "app"}},
		BaselineBranch: "develop",
		NewBranchFrom:  "current",
	}
	cfg.Normalize()

	assert.Equal(t, "develop", cfg.BaselineBranch)
	assert.Equal(t, "current", cfg.NewBranchFrom)
}

func TestLoadMigratesLegacyConfig(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(RootDir(tmpDir), 0755))

	legacyJSON := `{"database":"olddb","host":"localhost","port":5432,"user":"postgres"}`
	require.NoError(t, os.WriteFile(ConfigPath(tmpDir), []byte(legacyJSON), 0644))

	cfg, err := Load(tmpDir)
	require.NoError(t, err)

	require.Len(t, cfg.Databases, 1)
	assert.Equal(t, "olddb", cfg.Databases[0].Name)
	assert.Equal(t, "olddb", cfg.Database)
	assert.Equal(t, "main", cfg.BaselineBranch)
	assert.Equal(t, "baseline", cfg.NewBranchFrom)
}

func TestBaselineBranchOrDefault(t *testing.T) {
	assert.Equal(t, "main", (&Config{}).BaselineBranchOrDefault())
	assert.Equal(t, "develop", (&Config{BaselineBranch: "develop"}).BaselineBranchOrDefault())
}

func TestNewBranchFromOrDefault(t *testing.T) {
	assert.Equal(t, "baseline", (&Config{}).NewBranchFromOrDefault())
	assert.Equal(t, "current", (&Config{NewBranchFrom: "current"}).NewBranchFromOrDefault())
}

func TestAddRemote(t *testing.T) {
	t.Run("adds first remote and sets as default", func(t *testing.T) {
		cfg := &Config{}
		remote := &RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket"}

		err := cfg.AddRemote(remote)
		require.NoError(t, err)

		assert.Equal(t, remote, cfg.Remotes["origin"])
		assert.Equal(t, "origin", cfg.DefaultRemote)
	})

	t.Run("second remote does not override default", func(t *testing.T) {
		cfg := &Config{}
		first := &RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket1"}
		second := &RemoteConfig{Name: "backup", Type: "fs", URL: "/backups"}

		require.NoError(t, cfg.AddRemote(first))
		require.NoError(t, cfg.AddRemote(second))

		assert.Equal(t, "origin", cfg.DefaultRemote)
		assert.Equal(t, second, cfg.Remotes["backup"])
	})

	t.Run("rejects empty name", func(t *testing.T) {
		cfg := &Config{}
		remote := &RemoteConfig{Name: "", Type: "s3", URL: "s3://bucket"}

		err := cfg.AddRemote(remote)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remote name is required")
	})

	t.Run("rejects duplicate name", func(t *testing.T) {
		cfg := &Config{}
		remote := &RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket"}
		require.NoError(t, cfg.AddRemote(remote))

		dup := &RemoteConfig{Name: "origin", Type: "fs", URL: "/other"}
		err := cfg.AddRemote(dup)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remote 'origin' already exists")
	})
}

func TestRemoveRemote(t *testing.T) {
	t.Run("removes existing remote", func(t *testing.T) {
		cfg := &Config{}
		require.NoError(t, cfg.AddRemote(&RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket"}))
		require.NoError(t, cfg.AddRemote(&RemoteConfig{Name: "backup", Type: "fs", URL: "/backups"}))

		err := cfg.RemoveRemote("backup")
		require.NoError(t, err)

		assert.Nil(t, cfg.Remotes["backup"])
		assert.Len(t, cfg.Remotes, 1)
	})

	t.Run("reassigns default when default is removed", func(t *testing.T) {
		cfg := &Config{}
		require.NoError(t, cfg.AddRemote(&RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket"}))
		require.NoError(t, cfg.AddRemote(&RemoteConfig{Name: "backup", Type: "fs", URL: "/backups"}))
		assert.Equal(t, "origin", cfg.DefaultRemote)

		err := cfg.RemoveRemote("origin")
		require.NoError(t, err)

		assert.Equal(t, "backup", cfg.DefaultRemote)
	})

	t.Run("default becomes empty when last remote is removed", func(t *testing.T) {
		cfg := &Config{}
		require.NoError(t, cfg.AddRemote(&RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket"}))

		err := cfg.RemoveRemote("origin")
		require.NoError(t, err)

		assert.Empty(t, cfg.DefaultRemote)
		assert.Empty(t, cfg.Remotes)
	})

	t.Run("errors on missing remote", func(t *testing.T) {
		cfg := &Config{}
		require.NoError(t, cfg.AddRemote(&RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket"}))

		err := cfg.RemoveRemote("nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remote 'nonexistent' not found")
	})

	t.Run("errors when remotes map is nil", func(t *testing.T) {
		cfg := &Config{}

		err := cfg.RemoveRemote("origin")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remote 'origin' not found")
	})
}

func TestGetRemote(t *testing.T) {
	t.Run("gets remote by name", func(t *testing.T) {
		cfg := &Config{}
		remote := &RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket"}
		require.NoError(t, cfg.AddRemote(remote))

		result, err := cfg.GetRemote("origin")
		require.NoError(t, err)
		assert.Equal(t, remote, result)
	})

	t.Run("returns default when name is empty", func(t *testing.T) {
		cfg := &Config{}
		remote := &RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket"}
		require.NoError(t, cfg.AddRemote(remote))

		result, err := cfg.GetRemote("")
		require.NoError(t, err)
		assert.Equal(t, remote, result)
	})

	t.Run("errors when no remotes and name is empty", func(t *testing.T) {
		cfg := &Config{}

		_, err := cfg.GetRemote("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no remote specified and no default remote configured")
	})

	t.Run("errors when remote not found", func(t *testing.T) {
		cfg := &Config{}
		require.NoError(t, cfg.AddRemote(&RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket"}))

		_, err := cfg.GetRemote("nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remote 'nonexistent' not found")
	})

	t.Run("errors when remotes map is nil and name given", func(t *testing.T) {
		cfg := &Config{DefaultRemote: "origin"}

		_, err := cfg.GetRemote("origin")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remote 'origin' not found")
	})
}

func TestListRemotes(t *testing.T) {
	t.Run("returns nil when no remotes", func(t *testing.T) {
		cfg := &Config{}
		assert.Nil(t, cfg.ListRemotes())
	})

	t.Run("returns all remotes", func(t *testing.T) {
		cfg := &Config{}
		r1 := &RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket"}
		r2 := &RemoteConfig{Name: "backup", Type: "fs", URL: "/backups"}
		require.NoError(t, cfg.AddRemote(r1))
		require.NoError(t, cfg.AddRemote(r2))

		remotes := cfg.ListRemotes()
		assert.Len(t, remotes, 2)
		assert.ElementsMatch(t, []*RemoteConfig{r1, r2}, remotes)
	})
}

func TestSetDefaultRemote(t *testing.T) {
	t.Run("sets default to existing remote", func(t *testing.T) {
		cfg := &Config{}
		require.NoError(t, cfg.AddRemote(&RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket"}))
		require.NoError(t, cfg.AddRemote(&RemoteConfig{Name: "backup", Type: "fs", URL: "/backups"}))
		assert.Equal(t, "origin", cfg.DefaultRemote)

		err := cfg.SetDefaultRemote("backup")
		require.NoError(t, err)
		assert.Equal(t, "backup", cfg.DefaultRemote)
	})

	t.Run("errors on unknown name", func(t *testing.T) {
		cfg := &Config{}
		require.NoError(t, cfg.AddRemote(&RemoteConfig{Name: "origin", Type: "s3", URL: "s3://bucket"}))

		err := cfg.SetDefaultRemote("nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remote 'nonexistent' not found")
	})

	t.Run("errors when remotes map is nil", func(t *testing.T) {
		cfg := &Config{}

		err := cfg.SetDefaultRemote("origin")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remote 'origin' not found")
	})
}
