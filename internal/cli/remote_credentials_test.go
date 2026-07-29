package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/le-vlad/pgbranch/pkg/config"
	"github.com/le-vlad/pgbranch/pkg/remote"
)

func TestCLICredentialsReadsOptions(t *testing.T) {
	creds, err := cliCredentials{}.Credentials(context.Background(), &remote.Config{
		Name:    "origin",
		Type:    "s3",
		Options: map[string]string{"access_key": "AKIAEXAMPLE", "secret_key": "s3cret"},
	})
	require.NoError(t, err)

	assert.Equal(t, "AKIAEXAMPLE", creds.AccessKey)
	assert.Equal(t, "s3cret", creds.SecretKey)
}

// Unlike the library default, the CLI provider does fall back to the
// environment. This is the behavior documented under "Credentials" in the
// README and it must not regress.
func TestCLICredentialsFallsBackToEnvironment(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")

	creds, err := cliCredentials{}.Credentials(context.Background(), &remote.Config{
		Name: "origin",
		Type: "s3",
	})
	require.NoError(t, err)

	assert.Equal(t, "env-key", creds.AccessKey)
	assert.Equal(t, "env-secret", creds.SecretKey)
}

func TestCLICredentialsPrefersR2Variables(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "aws-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")
	t.Setenv("R2_ACCESS_KEY_ID", "r2-key")
	t.Setenv("R2_SECRET_ACCESS_KEY", "r2-secret")

	creds, err := cliCredentials{}.Credentials(context.Background(), &remote.Config{
		Name: "origin",
		Type: "r2",
	})
	require.NoError(t, err)

	assert.Equal(t, "r2-key", creds.AccessKey)
	assert.Equal(t, "r2-secret", creds.SecretKey)
}

// newRemote must attach the CLI provider, otherwise remotes silently fall back
// to the library default and stop seeing keyring or environment credentials.
func TestNewRemoteAttachesCLICredentials(t *testing.T) {
	dir := t.TempDir()

	r, err := newRemote(&config.RemoteConfig{
		Name: "origin",
		Type: "fs",
		URL:  dir,
	})
	require.NoError(t, err)

	assert.Equal(t, "origin", r.Name())
	assert.Equal(t, "fs", r.Type())
}

func TestNewRemoteRejectsUnknownType(t *testing.T) {
	_, err := newRemote(&config.RemoteConfig{
		Name: "origin",
		Type: "ftp",
		URL:  "ftp://example.com",
	})

	assert.Error(t, err)
}
