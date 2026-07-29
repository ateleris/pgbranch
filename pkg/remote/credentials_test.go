package remote

import (
	"context"
	"errors"
	"testing"
)

func TestResolveCredentialsDefaultsToOptions(t *testing.T) {
	cfg := &Config{
		Name: "origin",
		Type: "s3",
		Options: map[string]string{
			"access_key": "AKIAEXAMPLE",
			"secret_key": "s3cret",
		},
	}

	creds, err := resolveCredentials(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if creds.AccessKey != "AKIAEXAMPLE" || creds.SecretKey != "s3cret" {
		t.Errorf("got %+v, want the keys from Options", creds)
	}
}

// The default provider must not read ambient environment variables: resolving
// credentials from the process environment is the host application's policy,
// and the pgbranch CLI supplies its own provider for it.
func TestResolveCredentialsIgnoresEnvironment(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "from-env")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "from-env")
	t.Setenv("R2_ACCESS_KEY_ID", "from-env")
	t.Setenv("R2_SECRET_ACCESS_KEY", "from-env")

	creds, err := resolveCredentials(context.Background(), &Config{Name: "origin", Type: "r2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !creds.IsZero() {
		t.Errorf("got %+v, want zero credentials so the SDK default chain applies", creds)
	}
}

func TestResolveCredentialsUsesProvider(t *testing.T) {
	cfg := &Config{
		Name:        "origin",
		Type:        "s3",
		Options:     map[string]string{"access_key": "ignored"},
		Credentials: StaticCredentials{AccessKey: "static-key", SecretKey: "static-secret"},
	}

	creds, err := resolveCredentials(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if creds.AccessKey != "static-key" || creds.SecretKey != "static-secret" {
		t.Errorf("got %+v, want the provider to win over Options", creds)
	}
}

func TestResolveCredentialsPropagatesProviderError(t *testing.T) {
	wantErr := errors.New("vault unreachable")

	cfg := &Config{
		Name: "origin",
		Type: "s3",
		Credentials: CredentialsFunc(func(context.Context, *Config) (Credentials, error) {
			return Credentials{}, wantErr
		}),
	}

	_, err := resolveCredentials(context.Background(), cfg)
	if !errors.Is(err, wantErr) {
		t.Errorf("got %v, want the provider error", err)
	}
}
