package remote_test

import (
	"context"
	"log"

	"github.com/le-vlad/pgbranch/pkg/remote"
)

// Supplying credentials from the host application's own secret store, rather
// than letting the library read an OS keyring or the environment.
func ExampleCredentialsProvider() {
	cfg, err := remote.ParseURL("origin", "s3://my-bucket/pgbranch")
	if err != nil {
		log.Fatal(err)
	}

	cfg.Credentials = remote.CredentialsFunc(
		func(ctx context.Context, cfg *remote.Config) (remote.Credentials, error) {
			key, secret, err := lookupInVault(ctx, cfg.Name)
			if err != nil {
				return remote.Credentials{}, err
			}
			return remote.Credentials{AccessKey: key, SecretKey: secret}, nil
		},
	)

	r, err := remote.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	branches, err := r.List(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	_ = branches
}

// Static keys are enough when the caller already has them in hand.
func ExampleStaticCredentials() {
	cfg, err := remote.ParseURL("origin", "s3://my-bucket/pgbranch")
	if err != nil {
		log.Fatal(err)
	}

	cfg.Credentials = remote.StaticCredentials{
		AccessKey: "AKIAEXAMPLE",
		SecretKey: "s3cret",
	}

	if _, err := remote.New(cfg); err != nil {
		log.Fatal(err)
	}
}

func lookupInVault(context.Context, string) (string, string, error) {
	return "", "", nil
}
