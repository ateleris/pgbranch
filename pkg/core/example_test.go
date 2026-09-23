package core_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/le-vlad/pgbranch/pkg/config"
	"github.com/le-vlad/pgbranch/pkg/core"
)

// Initializing a workspace from an application, without touching the process
// working directory.
func ExampleInitialize() {
	err := core.Initialize("/srv/tenants/acme", &config.Config{
		Database: "acme_dev",
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
	})
	if err != nil {
		log.Fatal(err)
	}
}

// Opening an existing workspace and branching the database. Every operation
// takes a context, so a server can bound or cancel the work.
func ExampleOpen() {
	brancher, err := core.Open("/srv/tenants/acme")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	if err := brancher.CreateBranch(ctx, "feature-x", ""); err != nil {
		log.Fatal(err)
	}

	if err := brancher.Checkout(ctx, "feature-x"); err != nil {
		log.Fatal(err)
	}

	for _, info := range brancher.ListBranches() {
		fmt.Println(info.Name, info.IsCurrent)
	}
}

// Failures are reported with sentinel errors, so callers can branch on them
// instead of matching message text.
func ExampleOpen_errors() {
	brancher, err := core.Open("/srv/tenants/acme")
	if errors.Is(err, config.ErrNotInitialized) {
		log.Fatal("no pgbranch workspace here yet")
	} else if err != nil {
		log.Fatal(err)
	}

	err = brancher.Checkout(context.Background(), "does-not-exist")
	if errors.Is(err, core.ErrBranchNotFound) {
		fmt.Println("no such branch")
	}
}

// Driving pgbranch entirely from memory, for a caller that keeps its own
// configuration rather than reading pgbranch's config file.
func ExampleNew() {
	cfg := &config.Config{
		Root:     "/srv/tenants/acme",
		Database: "acme_dev",
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
	}

	meta, err := core.Open(cfg.Root)
	if err != nil {
		log.Fatal(err)
	}

	brancher := core.New(cfg, meta.Metadata)

	if err := brancher.CreateBranch(context.Background(), "feature-y", ""); err != nil {
		log.Fatal(err)
	}
}

// Recovering the branch name from a failure, so a caller can build its own
// message without parsing the error text.
func ExampleBranchError() {
	brancher, err := core.Open("/srv/tenants/acme")
	if err != nil {
		log.Fatal(err)
	}

	err = brancher.DeleteBranch(context.Background(), "main", false)

	var branchErr *core.BranchError
	if errors.As(err, &branchErr) && errors.Is(err, core.ErrCurrentBranch) {
		fmt.Printf("%s is checked out; pass force to delete it\n", branchErr.Name)
	}
}
