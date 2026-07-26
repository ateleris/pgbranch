package cli

import (
	"context"

	"github.com/le-vlad/pgbranch/internal/credentials"
	"github.com/le-vlad/pgbranch/pkg/config"
	"github.com/le-vlad/pgbranch/pkg/remote"
)

// cliCredentials resolves remote credentials the way the pgbranch CLI does:
// from the encrypted keys stored in the config file (decrypted with the local
// keyring), then plain options, then pgbranch's own environment variables.
//
// This policy lives in the CLI rather than pkg/remote so that library callers
// are never surprised by a keyring lookup or an ambient environment variable.
// They supply their own remote.CredentialsProvider instead.
type cliCredentials struct{}

func (cliCredentials) Credentials(_ context.Context, cfg *remote.Config) (remote.Credentials, error) {
	creds, err := credentials.GetCredentials(cfg.Options, cfg.Type)
	if err != nil {
		return remote.Credentials{}, err
	}
	return remote.Credentials{
		AccessKey: creds.AccessKey,
		SecretKey: creds.SecretKey,
	}, nil
}

// newRemote builds a remote backend from a stored remote configuration,
// wired up with the CLI's credential resolution.
func newRemote(remoteCfg *config.RemoteConfig) (remote.Remote, error) {
	return remote.New(&remote.Config{
		Name:        remoteCfg.Name,
		Type:        remoteCfg.Type,
		URL:         remoteCfg.URL,
		Options:     remoteCfg.Options,
		Credentials: cliCredentials{},
	})
}
