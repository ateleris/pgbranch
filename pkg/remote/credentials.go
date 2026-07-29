package remote

import "context"

// Credentials are the access keys used to authenticate against an object
// storage backend.
type Credentials struct {
	AccessKey string
	SecretKey string
}

// IsZero reports whether no credentials were resolved, in which case the
// backend falls back to its SDK's own default credential chain.
func (c Credentials) IsZero() bool {
	return c.AccessKey == "" && c.SecretKey == ""
}

// CredentialsProvider resolves the credentials for a remote.
//
// Resolving credentials is the host application's policy, not this package's:
// an embedder may read them from its own secret store, a cloud metadata
// service, or a vault. Nothing in pkg/remote reads an OS keyring or prompts
// on a terminal; the pgbranch CLI supplies a provider that does.
//
// Returning zero-valued Credentials is not an error. It means "I have no
// credentials for this remote", and the backend falls back to its SDK's
// default credential chain.
type CredentialsProvider interface {
	Credentials(ctx context.Context, cfg *Config) (Credentials, error)
}

// CredentialsFunc adapts an ordinary function to CredentialsProvider.
type CredentialsFunc func(ctx context.Context, cfg *Config) (Credentials, error)

// Credentials calls f.
func (f CredentialsFunc) Credentials(ctx context.Context, cfg *Config) (Credentials, error) {
	return f(ctx, cfg)
}

// StaticCredentials is a CredentialsProvider that always returns the same
// keys. It is the simplest way for a library caller to supply credentials.
type StaticCredentials Credentials

// Credentials returns the static keys.
func (s StaticCredentials) Credentials(context.Context, *Config) (Credentials, error) {
	return Credentials(s), nil
}

// OptionsCredentials reads "access_key" and "secret_key" straight from a
// remote's Options map. It is the default when Config.Credentials is nil.
type OptionsCredentials struct{}

// Credentials returns the keys stored in cfg.Options, if any.
func (OptionsCredentials) Credentials(_ context.Context, cfg *Config) (Credentials, error) {
	if cfg == nil {
		return Credentials{}, nil
	}
	return Credentials{
		AccessKey: cfg.Options["access_key"],
		SecretKey: cfg.Options["secret_key"],
	}, nil
}

// resolveCredentials returns the credentials for cfg, using OptionsCredentials
// when no provider is configured.
func resolveCredentials(ctx context.Context, cfg *Config) (Credentials, error) {
	if cfg == nil {
		return Credentials{}, nil
	}
	if cfg.Credentials == nil {
		return OptionsCredentials{}.Credentials(ctx, cfg)
	}
	return cfg.Credentials.Credentials(ctx, cfg)
}
