# pgbranch

[![Build Status](https://github.com/le-vlad/pgbranch/actions/workflows/go.yml/badge.svg?branch=main)](https://github.com/le-vlad/pgbranch/actions/workflows/go.yml)
[![Latest Release](https://img.shields.io/github/v/release/le-vlad/pgbranch)](https://github.com/le-vlad/pgbranch/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/le-vlad/pgbranch)](https://goreportcard.com/report/github.com/le-vlad/pgbranch)
[![Go Reference](https://pkg.go.dev/badge/github.com/le-vlad/pgbranch.svg)](https://pkg.go.dev/github.com/le-vlad/pgbranch)
[![codecov](https://codecov.io/gh/le-vlad/pgbranch/branch/main/graph/badge.svg)](https://codecov.io/gh/le-vlad/pgbranch)

<p align="center">
  <img src="img/thumb.png" alt="pgbranch" width="400">
</p>

Git branching for your PostgreSQL database.

## Table of Contents

- [The Problem](#the-problem)
- [The Solution](#the-solution)
- [How It Works](#how-it-works)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Commands](#commands)
- [Multiple Databases](#multiple-databases)
- [Clone Strategies & TimescaleDB](#clone-strategies--timescaledb)
- [Schema Diff](#schema-diff)
- [Schema Merge](#schema-merge) *(Beta)*
- [Continuous Migration](#continuous-migration)
- [Automatic Branch Switching](#automatic-branch-switching)
- [Configuration](#configuration)
- [Passwords](#passwords)
- [Remotes](#remotes)
- [Troubleshooting](#troubleshooting)
- [Using pgbranch as a Library](#using-pgbranch-as-a-library)
- [Caveats](#caveats)

## The Problem

You're working on a feature branch. You run migrations. Life is good.

Then you switch back to `main`.

Your database is now in a broken state. The migrations from your feature branch are still applied. Your schema doesn't match your code. Nothing works.

Your options:
1. Drop the database and re-seed (slow, painful)
2. Manually roll back migrations (error-prone, annoying)
3. Maintain multiple databases and remember to switch connection strings (who has time for that)

None of these are good.

## The Solution

```bash
pgbranch init -d myapp_dev
pgbranch branch main           # snapshot your clean main state
pgbranch checkout main         # switch to the main branch

pgbranch branch feature-x      # create a new branch (snapshots current state)
pgbranch checkout feature-x    # switch to it
# ... run migrations, break things ...

pgbranch checkout main         # instantly back to clean state
```

That's it. Your database now has branches. Just like git.

## How It Works

PostgreSQL has a feature called template databases. When you create a database from a template, it does a file-level copy. It's fast.

pgbranch uses this to create instant snapshots of your database. When you "checkout" a branch, it drops your working database and recreates it from the snapshot.

No pg_dump. No restore. No waiting.

## Installation

```bash
go install github.com/le-vlad/pgbranch/cmd/pgbranch@latest
```

## Quick Start

```bash
# Initialize for your dev database
pgbranch init -d myapp_dev

# Create a branch from current state and switch to it
pgbranch branch main
pgbranch checkout main

# Create a new feature branch and switch to it
pgbranch branch feature-auth
pgbranch checkout feature-auth
# equivalently: pgbranch checkout -b feature-auth --from main

# Do some work, run migrations, whatever

# Go back to main
pgbranch checkout main

# Your database is now exactly as it was before

# Optional: keep the DB branch in sync with `git checkout` automatically
pgbranch hook install
```

## Commands

```
pgbranch init -d <database>    Initialize pgbranch (repeat -d for multiple databases)
pgbranch branch                List all branches
pgbranch branch <name>         Create a branch from current state
pgbranch branch <name> --from <b>  Create a branch from another branch's snapshot
pgbranch checkout <name>       Switch to a branch
pgbranch checkout -b <name> [--from <b>]  Create a new branch and switch to it
pgbranch delete <name>         Delete a branch
pgbranch status                Show current branch and per-database info
pgbranch log                   Show all branches with details
pgbranch sync                  Sync the DB branch with the current git branch
pgbranch reset [--from <b>]    Reset working database(s) to a branch's snapshot
pgbranch prune                 Remove stale branches
pgbranch prune --gone          Remove branches whose git branch no longer exists locally
pgbranch hook install          Install the post-checkout git hook
pgbranch hook uninstall        Remove the git hook
pgbranch diff <branch1> [branch2]  Compare schemas between branches (primary database)
pgbranch merge <source> <target>   Merge schema changes (Beta, primary database)
pgbranch migrate -c <config.yaml>  Migrate database via logical replication
```

`push`, `pull`, `diff` and `merge` only ever operate on the primary
database -- the first one listed in `-d`/`databases`.

### Init Options

```
-d, --database stringArray   Database name (repeatable); optionally 'name:strategy' (auto|template|dump)
    --strategy string        Default clone strategy for databases without one (auto|template|dump)
-H, --host string            PostgreSQL host (default "localhost")
-p, --port int               PostgreSQL port (default 5432)
-U, --user string            PostgreSQL user (default "postgres")
-W, --password string        PostgreSQL password (stored in plain text; prefer PGPASSWORD or ~/.pgpass)
    --baseline string        Baseline branch name (default "main")
    --hook                   Install the post-checkout git hook
```

`init` always snapshots the working database(s) as the baseline branch. If
git is currently on a different branch, it also snapshots them as that
branch and makes it current -- so pgbranch never records your working state
under "main" while git says you're on "develop".

## Multiple Databases

A workspace can manage more than one database, useful when your app is split
across databases (e.g. a main app database and a separate identity/auth
database):

```bash
pgbranch init -d app -d app_identity:dump --baseline main
```

Every branch has its own snapshot of *each* configured database. `branch`,
`checkout`, `delete`, `prune` and `reset` all operate on every database in
the workspace, cloning or restoring each one in turn (`status` reports each
database's working name, strategy, snapshot name, existence and size).

`push`, `pull`, `diff` and `merge` are not multi-database aware: they only
ever act on the primary database, i.e. the first one passed to `-d` (or
config `databases[0]`).

An old config with a single `database` field still loads fine -- it's
migrated in memory to a one-entry `databases` list the first time the config
is read.

## Clone Strategies & TimescaleDB

Each database has a clone strategy, set per-database with `-d name:strategy`
or defaulted with `--strategy` (`config.json`'s `strategy` field). Strategies:

- **`auto`** (default) -- resolved per clone: `dump` if the source database
  has the `timescaledb` extension installed, `template` otherwise.
- **`template`** -- `CREATE DATABASE dst TEMPLATE src`. Fast, file-level
  copy. Connections to `src` are terminated first; if Postgres still reports
  the source "is being accessed by other users" (SQLSTATE `55006`, e.g. a
  TimescaleDB background worker reconnecting right after being terminated),
  it retries up to 5 times with a short backoff.
- **`dump`** -- streams `pg_dump -Fc --no-owner --no-privileges` directly
  into `pg_restore --no-owner --no-privileges` (no intermediate file). If the
  source has the `timescaledb` extension, the destination gets
  `CREATE EXTENSION IF NOT EXISTS timescaledb`, then
  `SELECT timescaledb_pre_restore()` before the restore and
  `SELECT timescaledb_post_restore()` after -- the post-restore call always
  runs, even if the restore itself failed, so the destination is never left
  with `timescaledb.restoring` stuck on. After restoring, the destination's
  table count (from `information_schema.tables`, excluding the catalog
  schemas) must be at least the source's, or the clone is treated as failed.

`pg_dump` and `pg_restore` must be on `PATH` and version-compatible with
(specifically, not older than) the server for the `dump` strategy to work;
if they're missing, the error names the missing binary with an install hint.

## Schema Diff

Compare the schema between two database branches to see what changed.

```bash
# Compare two branches
pgbranch diff main feature-auth

# Compare a branch against current working database
pgbranch diff main

# Show summary statistics only
pgbranch diff main feature-auth --stat

# Show SQL statements needed to migrate
pgbranch diff main feature-auth --sql
```

### What It Detects

- **Tables**: Created, dropped
- **Columns**: Added, removed, type changes, nullability, defaults
- **Indexes**: Created, dropped, modified
- **Constraints**: Primary keys, foreign keys, unique, check constraints
- **Enums**: Created, dropped, new values added
- **Functions**: Created, dropped, body changes

### Output Format

```
Comparing 'main' → 'feature-auth'

+ TABLE public.sessions
    id uuid NOT NULL
    user_id uuid NOT NULL
    expires_at timestamp with time zone NOT NULL

~ TABLE public.users
  + COLUMN last_login timestamp with time zone
  ~ COLUMN email: type varchar(100) → varchar(255)

+ INDEX idx_sessions_user_id on public.sessions(user_id)

Summary:
  + 5 addition(s)
  ~ 1 modification(s)
```

Changes are color-coded: `+` green (additions), `-` red (deletions), `~` yellow (modifications). Destructive changes are flagged with a warning indicator.

## Schema Merge

> **Beta**: This feature is in beta. Use with caution and always backup important data.

Merge schema changes from one branch into another. This applies the schema diff as actual DDL statements to the target branch.

```bash
# Merge feature branch into main
pgbranch merge feature-auth main

# Preview changes without applying (dry run)
pgbranch merge feature-auth main --dry-run

# Generate a migration file instead of applying directly
pgbranch merge feature-auth main --migration-file

# Specify custom migration directory
pgbranch merge feature-auth main --migration-file --migration-dir ./db/migrations

# Force merge without confirmation prompts
pgbranch merge feature-auth main --force
```

### Safety Features

- **Dry run mode**: Preview all SQL statements before applying
- **Destructive change warnings**: Explicitly warns about `DROP TABLE`, `DROP COLUMN`, and other data-loss operations
- **Confirmation prompts**: Requires explicit confirmation for destructive changes
- **Validation**: Checks for potential issues before applying

### Migration File Generation

Instead of applying changes directly, generate a timestamped SQL migration file:

```bash
pgbranch merge feature-auth main --migration-file
# Creates: migrations/20240115143022_merge_feature_auth.sql
```

The generated file includes:
- Header with source/target branch info
- All DDL statements in correct dependency order
- Comments for destructive operations

## Continuous Migration

Continuously migrate a PostgreSQL database to another instance using logical replication. Copies schema, performs an initial data snapshot, then streams live changes -- all with table-by-table progress.

```bash
# Full migration (schema + snapshot + live streaming)
pgbranch migrate -c migration.yaml

# Copy schema only
pgbranch migrate -c migration.yaml --schema-only

# Copy schema + initial data, then stop (no live streaming)
pgbranch migrate -c migration.yaml --snapshot-only

# Keep replication slot on exit for later resume
pgbranch migrate -c migration.yaml --keep
```

### Configuration

Create a YAML file describing the source and target:

```yaml
source:
  host: source-db.example.com
  port: 5432
  database: myapp
  user: replicator
  password: secret
  sslmode: require          # disable, allow, prefer (default), require, verify-ca, verify-full

target:
  host: target-db.example.com
  port: 5432
  database: myapp
  user: admin
  password: secret
  sslmode: require

# Tables to migrate (use ["*"] for all user tables)
tables:
  - public.users
  - public.orders
  - public.products

# Optional settings
slot_name: migrate_slot            # replication slot name (default: migrate_slot)
publication_name: migrate_pub      # publication name (default: migrate_pub)
batch_size: 10000                  # rows per batch during snapshot (default: 10000)
```

### How It Works

1. **Validate** -- Checks source has `wal_level=logical`, verifies connectivity, resolves table list
2. **Schema Copy** -- Extracts DDL from source (tables, columns, indexes, constraints, enums, functions) and applies to target
3. **Setup Replication** -- Creates a publication and replication slot on source, exports a consistent snapshot
4. **Initial Snapshot** -- Copies all table data using PostgreSQL's COPY protocol at the consistent snapshot point
5. **WAL Streaming** -- Streams INSERT/UPDATE/DELETE changes in real-time via logical decoding (pgoutput)
6. **Shutdown** -- On Ctrl+C: flushes checkpoint, drops slot + publication (unless `--keep` is set)

### Requirements

- Source PostgreSQL must have `wal_level=logical` (set with `ALTER SYSTEM SET wal_level = 'logical'` + restart)
- Source user must have `REPLICATION` privilege
- Tables should have a `PRIMARY KEY` for UPDATE/DELETE replication
- At least one free replication slot on source

### Resume Support

If interrupted, a checkpoint file is saved alongside your config. Re-running the same command skips completed tables and resumes WAL streaming from the last confirmed position. Use `--keep` to preserve the replication slot across runs.

### Progress Display

Shows a rich TUI with per-table progress bars during the snapshot phase and live counters (inserts, updates, deletes, ops/sec) during streaming. Falls back to periodic log lines in non-TTY environments.

## Requirements

- A reachable PostgreSQL server -- branching and checkout run entirely
  in-process over the wire protocol (via `pgx`), no `psql`/`createdb`/`dropdb`
  needed.
- `pg_dump` and `pg_restore` on `PATH`, version-compatible with the server,
  if any database uses (or resolves to, under `auto`) the `dump` clone
  strategy, or for `push`/`pull`.
- `git`, if you use `pgbranch hook install` / `pgbranch sync`.
- Go 1.21+ (for installation)

## What It Actually Creates

When you run `pgbranch branch feature-x` on a database called `myapp_dev`, it creates a new database called `myapp_dev_pgbranch_feature_x`. That's your snapshot.

Your working database stays as `myapp_dev`. When you checkout, it gets replaced with a copy of the snapshot.

Snapshot names are built as `<database>_pgbranch_<branch>`, where the branch
name is lowercased, every character outside `[a-z0-9_]` becomes `_`, and runs
of `_` are collapsed to one. If the result would exceed 63 bytes (Postgres's
identifier limit), it's truncated so the last 7 bytes are `_` followed by
the first 6 hex characters of the SHA-1 hash of the original (unsanitized)
branch name -- this keeps long or colliding branch names deterministic and
distinct. This only affects newly created branches; existing snapshots keep
whatever name is already recorded in their metadata.

## Automatic Branch Switching

Tired of manually running `pgbranch checkout` every time you switch git branches? Install the git hook:

```bash
pgbranch hook install
```

Now whenever you run `git checkout feature-x` (or `git switch`), pgbranch
runs `pgbranch sync --hook "$@"`, which checks out the matching DB branch,
creating it first if it doesn't exist yet (from the baseline branch by
default, or from the current working state if `new_branch_from: "current"`
is set -- see [Configuration](#configuration)).

`pgbranch sync` (and the hook) is a no-op when:
- HEAD is detached, or the checkout only touched files (not a branch switch)
- a rebase or merge is in progress
- you're in a linked git worktree, unless `follow_worktrees: true` is set in
  `config.json` (worktrees are skipped by default because they usually share
  one working database, which branch switches in other worktrees would
  otherwise fight over)

You can also run `pgbranch sync` by hand at any time -- it reads the current
git branch instead of hook arguments.

### Concurrent operations

Checkout, branch creation, delete, prune, reset and sync all take an
exclusive lock on `.pgbranch/lock` while they run, so two of these commands
(e.g. a hook firing while another one is still in progress) can't race on
the same working databases. In hook mode, if the lock is already held,
sync just prints a message and skips -- it never fails your `git
checkout`. Run outside the hook, a held lock is a normal error. A lock file
left behind by a crashed process is detected as stale (the PID it names is
no longer running) and cleared automatically; if that heuristic ever gets
it wrong, delete `.pgbranch/lock` by hand.

### Installing the hook

`pgbranch hook install` (also available as `pgbranch init --hook`) writes
its hook to the repository's real hooks directory: `core.hooksPath` if set,
otherwise `<git-common-dir>/hooks`, so it works correctly from any linked
worktree.

- No `post-checkout` hook yet -- pgbranch writes a small POSIX `sh` script
  that calls `pgbranch sync --hook "$@"`.
- An existing, foreign `post-checkout` hook -- pgbranch appends a guarded
  line instead of overwriting it:
  ```sh
  if command -v pgbranch >/dev/null 2>&1; then pgbranch sync --hook "$@"; fi
  ```
  This never fails the hook (and so never fails `git checkout`) even when
  pgbranch isn't installed, and is idempotent; running install again won't
  duplicate the line. A hook installed by an older version of pgbranch
  (`command -v pgbranch >/dev/null 2>&1 && pgbranch sync --hook "$@"`, whose
  exit status was `command -v pgbranch`'s -- 1 when pgbranch wasn't on
  PATH) is recognized and upgraded in place. An old-style hook from a
  previous version of this fork (which called `pgbranch checkout` directly)
  is recognized and replaced.
- `.husky/` or a `lefthook.yml`/`lefthook.yaml` present -- pgbranch assumes a
  hook manager owns `post-checkout` and doesn't touch any files. It prints
  the line to add to `.husky/post-checkout`, or the `lefthook.yml` snippet,
  instead.

`pgbranch hook uninstall` removes only what pgbranch itself installed: the
whole file if it's entirely pgbranch's script, or just the guarded line if
it shares the file with a foreign hook.

## Configuration

`.pgbranch/config.json`, written by `init` and updated by the CLI as you add
remotes or change settings:

```json
{
  "databases": [
    { "name": "app", "strategy": "auto" },
    { "name": "app_identity", "strategy": "dump" }
  ],
  "host": "localhost",
  "port": 5432,
  "user": "postgres",
  "baseline_branch": "main",
  "new_branch_from": "baseline",
  "follow_worktrees": false
}
```

| Field | Default | Meaning |
| --- | --- | --- |
| `databases` | -- | List of `{name, strategy}`. `strategy` is `""`/`auto`, `template` or `dump`. |
| `database` | -- | Legacy single-database field; still read, migrated in memory to `databases` on load. |
| `host`, `port`, `user`, `password` | `localhost`, `5432`, `postgres`, `""` | Connection settings. |
| `baseline_branch` | `main` | The branch new branches clone from by default, and the one `prune --gone` never removes. |
| `new_branch_from` | `baseline` | Where `sync` clones a newly auto-created branch from: `baseline` or `current`. |
| `follow_worktrees` | `false` | Whether the git hook also runs inside linked worktrees. |
| `remotes`, `default_remote` | -- | Configured remote storage backends (see [Remotes](#remotes)). |

Configs from before multiple databases (with only a top-level `database`
field) keep working unchanged -- they're normalized to `databases` in memory
every time they're loaded, and rewritten in the new format the next time
pgbranch saves the config.

## Passwords

`init` no longer requires `-W`. pgbranch's own connections (via `pgx`) and
the `pg_dump`/`pg_restore` it shells out to for the `dump` strategy and for
`push`/`pull` all honor the standard libpq mechanisms, so prefer one of:

- the `PGPASSWORD` environment variable
- a `~/.pgpass` entry

If you do pass `-W`, `init` stores the password in `.pgbranch/config.json`
**in plain text** and prints a warning telling you so.

## Remotes

Share database snapshots across machines or with your team using remote storage backends.

### Supported Backends

| Backend | URL Format | Status |
|---------|------------|--------|
| Filesystem | `/path/to/dir` or `file:///path/to/dir` | Supported |
| AWS S3 | `s3://bucket/prefix` | Supported |
| MinIO | `s3://bucket/prefix` (S3-compatible) | Supported |
| Cloudflare R2 | `r2://account-id/bucket/prefix` | Supported |
| Google Cloud Storage | `gs://bucket/prefix` | Supported |
| Azure Blob Storage | - | Planned |

### Remote Commands

```
pgbranch remote add <name> <url>     Add a remote
pgbranch remote list                 List configured remotes
pgbranch remote remove <name>        Remove a remote
pgbranch remote set-default <name>   Set default remote
pgbranch remote ls-remote            List branches on remote
pgbranch remote delete <branch>      Delete branch from remote
pgbranch push <branch>               Push branch to remote
pgbranch pull <branch>               Pull branch from remote
```

### Setting Up a Remote

```bash
# Add a filesystem remote (local network share, mounted drive, etc.)
pgbranch remote add origin /shared/snapshots

# Add an S3 remote (will prompt for credentials)
pgbranch remote add origin s3://my-bucket/pgbranch

# Add a Cloudflare R2 remote
pgbranch remote add origin r2://account-id/my-bucket/pgbranch

# Skip credential prompts and use environment variables instead
pgbranch remote add origin s3://my-bucket/pgbranch --no-credentials
```

### Push and Pull

```bash
# Push a local branch to the remote
pgbranch push main

# Push with a description
pgbranch push main --description "Clean schema with seed data"

# Force overwrite if branch exists on remote
pgbranch push main --force

# Pull a branch from remote
pgbranch pull main

# Pull with a different local name
pgbranch pull main --as main-backup

# Force overwrite if local branch exists
pgbranch pull main --force
```

### Credentials

For S3 and R2 remotes, pgbranch will prompt for your access key and secret key. These credentials are encrypted and stored in your project's `.pgbranch.json` config.

To use environment variables instead (useful for CI/CD), add the remote with `--no-credentials`:

```bash
pgbranch remote add origin s3://bucket/prefix --no-credentials
```

Then set `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` in your environment.

## Troubleshooting

**"database is being accessed by other users"** -- normal transient noise
from the `template` clone strategy (typically a TimescaleDB background
worker reconnecting right after its connection was terminated). pgbranch
retries automatically up to 5 times with a short backoff. If it still fails
after that, something is reconnecting persistently -- e.g. a connection
pooler, or an app process that wasn't actually stopped -- stop it and try
again.

**The hook doesn't run in a worktree** -- linked worktrees are skipped by
`sync` (and therefore the hook) by default, since they typically share one
working database that switches from other worktrees would fight over. Set
`"follow_worktrees": true` in `.pgbranch/config.json` if you want the hook
to run there anyway.

**`pg_dump`/`pg_restore` version mismatch** -- the `dump` clone strategy (and
`push`/`pull`) shells out to `pg_dump`/`pg_restore`, which must be on `PATH`
and should not be older than the server's version; an older client can fail
to dump newer server features. Install matching (or newer) PostgreSQL client
tools if you see version-related dump/restore errors.

## Using pgbranch as a Library

The core packages live under `pkg/` and can be imported directly, so you can
drive branching and migration from your own Go program instead of shelling out
to the CLI.

```bash
go get github.com/le-vlad/pgbranch
```

| Package | What it does |
| --- | --- |
| `pkg/core` | `Brancher`: create, checkout, update, delete and prune branches |
| `pkg/config` | Workspace configuration and connection settings |
| `pkg/storage` | Branch metadata |
| `pkg/postgres` | PostgreSQL client for snapshot and template database operations |
| `pkg/schema` | Schema extraction, diffing and applying (pure Go, no `pg_dump`) |
| `pkg/remote` | S3, R2, GCS and filesystem backends |
| `pkg/archive` | Portable snapshot packaging |
| `pkg/migrate` | Continuous PG-to-PG migration over logical replication |

`internal/cli`, `internal/credentials` and `internal/testutil` stay private.

One deployment note: branching, schema work and migration run entirely
in-process, but **archiving (push/pull) shells out to `pg_dump` and
`pg_restore`**, so those binaries must be on `PATH` and version-compatible with
your server if you use `pkg/archive`.

### Branching

Every workspace directory is explicit, so one process can manage many of them,
and every operation takes a `context.Context`:

```go
brancher, err := core.Open("/srv/tenants/acme")
if errors.Is(err, config.ErrNotInitialized) {
    err = core.Initialize("/srv/tenants/acme", &config.Config{
        Database: "acme_dev",
        Host:     "localhost",
        Port:     5432,
        User:     "postgres",
    })
}
if err != nil {
    return err
}

if err := brancher.CreateBranch(ctx, "feature-x"); err != nil {
    return err
}
if err := brancher.Checkout(ctx, "feature-x"); err != nil {
    return err
}
```

Failures use sentinel errors, so you can branch on them with `errors.Is`:
`config.ErrNotInitialized`, `core.ErrBranchNotFound`, `core.ErrBranchExists`
and `core.ErrCurrentBranch`.

Branch failures also carry the branch name, so you never have to parse a
message to find out which one failed:

```go
err := brancher.DeleteBranch(ctx, "main", false)

var branchErr *core.BranchError
if errors.As(err, &branchErr) && errors.Is(err, core.ErrCurrentBranch) {
    log.Printf("%s is checked out", branchErr.Name)
}
```

These messages name no command line flags, so an embedded pgbranch never tells
your users to "use `--force`". The CLI adds that guidance in its own layer.

### Migration progress

`pkg/migrate` reports progress as typed events and never writes to stdout or
takes over the terminal. The CLI's progress bars are just one consumer of the
same channel:

```go
migrator := migrate.NewMigrator(cfg, false, migrate.RunFull)

go func() {
    for event := range migrator.Events() {
        switch e := event.(type) {
        case migrate.PhaseEvent:
            log.Println("phase:", e.Phase)
        case migrate.TableDoneEvent:
            log.Println("done:", e.Table)
        case migrate.StreamingEvent:
            log.Printf("lsn %s: +%d ~%d -%d", e.LSN, e.Inserts, e.Updates, e.Deletes)
        }
    }
}()

err := migrator.Run(ctx)
```

Events are dropped rather than blocking the migration if you fall behind, so
ignoring `Events()` entirely is safe.

### Remote credentials

The library never reads an OS keyring and never prompts. Supply credentials
through a provider so they come from wherever your application keeps them:

```go
cfg, _ := remote.ParseURL("origin", "s3://my-bucket/pgbranch")

cfg.Credentials = remote.StaticCredentials{
    AccessKey: key,
    SecretKey: secret,
}

r, err := remote.New(cfg)
```

If no provider is set, only the `access_key` and `secret_key` options are read,
and empty credentials fall through to the backend SDK's own default chain. The
CLI installs its own provider for the keyring-encrypted and environment-variable
behavior described under [Credentials](#credentials).

## Caveats

- This is for **local development only**. Don't use this in production.
- Checkout **replaces your working database(s)**. Uncommitted changes are
  gone, and active connections are terminated -- fine as long as your app
  isn't connected or running.
- Checkout is a safe swap: the target snapshot is cloned into a temporary
  database first, and only swapped into place once that clone succeeds. If
  cloning fails, your working database is left untouched and the temporary
  database is dropped.
- Snapshots are full database copies. They take disk space.
- `push`, `pull`, `diff` and `merge` only operate on the primary (first
  configured) database; the rest are ignored by those commands.

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=le-vlad/pgbranch&type=date&legend=bottom-right)](https://www.star-history.com/#le-vlad/pgbranch&type=date&legend=bottom-right)

## License

MIT
