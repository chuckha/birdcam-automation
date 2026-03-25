# CLAUDE.md — birdcam-automation

## What this project does

Automates a birdcam YouTube channel: runs day/night live broadcasts keyed to sunrise/sunset, downloads VODs via yt-dlp, detects bird activity with a Python script (`detect_birds.py`), and uploads highlight compilations.

## Commands

- `go run ./cmd/stream-manager` — long-running daemon (needs all env vars)
- `go run ./cmd/backfill --from YYYY-MM-DD --to YYYY-MM-DD` — backfill past dates
- `go run ./cmd/login` — OAuth token refresh

## Running locally

```bash
set -a && source .env && set +a
```

Python venv lives at `./venv/`. The `PYTHON_PATH` env var should point to `./venv/bin/python3`.

## Architecture

Event-driven: `scheduler` emits Sunrise/Sunset events, `manager` handles the pipeline (broadcast -> download -> detect -> upload). Each step emits the next event.

Key interfaces are defined in `internal/manager/manager.go` (Broadcaster, Downloader, Processor, Uploader). Tests use fakes, not mocks.

## Project conventions

- All env var loading happens in `internal/config/` (stream-manager) or directly in `main.go` (backfill, login)
- `internal/auth/` handles OAuth: loading client secrets, token files (supports both Go and Python token formats), and auto-refresh
- `downloader.Download` returns the actual filepath (yt-dlp picks the extension)
- `processor.ErrNoBirds` (exit code 2 from detect_birds.py) means no bird activity — callers should skip upload, not treat as failure
- Broadcasts are matched by scheduled start date, not title (titles can be identical across days)
- Highlight uploads are set to private with a scheduled publish time

## Build and verify

```bash
go build ./...
go test ./...
go vet ./...
```

## Files not in git

- `.env`, `token.json`, `client_secret.json` — credentials
- `*.mp4` — video files
- `venv/` — Python virtualenv

## Code Design

### Dependency direction
- Core logic must not import transport, CLI, storage-driver, or vendor SDK details.
- External systems call into the app through exported concrete APIs.
- The app calls outward through small interfaces owned by the consumer package.
- Concrete wiring happens at the edge, usually in `main` or a small composition root.

This is how hexagonal architecture usually shows up in Go: not as special folder names, but as **inward dependency flow**.

### Separate decisions from side effects
Do not mix "what should happen" with "do it now".

Let one function decide the action.
Let the caller perform the side effects.

This keeps decision logic easy to test and side effects easy to see.

BAD:

    func reconcileCluster(ctx context.Context, cluster Cluster, store Store, cloud Cloud) error {
        if cluster.Deleted {
            if err := cloud.DeleteCluster(ctx, cluster.ID); err != nil {
                return err
            }
            return store.MarkDeleted(ctx, cluster.ID)
        }

        if !cluster.Ready {
            if err := cloud.CreateCluster(ctx, cluster.ID); err != nil {
                return err
            }
            return store.MarkProvisioning(ctx, cluster.ID)
        }

        return nil
    }

GOOD:

    type ClusterAction int

    const (
        ClusterNoop ClusterAction = iota
        ClusterCreate
        ClusterDelete
    )

    func decideClusterAction(cluster Cluster) ClusterAction {
        switch {
        case cluster.Deleted:
            return ClusterDelete
        case !cluster.Ready:
            return ClusterCreate
        default:
            return ClusterNoop
        }
    }

    func reconcileCluster(ctx context.Context, cluster Cluster, store Store, cloud Cloud) error {
        switch decideClusterAction(cluster) {
        case ClusterDelete:
            if err := cloud.DeleteCluster(ctx, cluster.ID); err != nil {
                return err
            }
            return store.MarkDeleted(ctx, cluster.ID)

        case ClusterCreate:
            if err := cloud.CreateCluster(ctx, cluster.ID); err != nil {
                return err
            }
            return store.MarkProvisioning(ctx, cluster.ID)

        case ClusterNoop:
            return nil

        default:
            panic("unreachable")
        }
    }

The point is not "purity" for its own sake.
The point is that:
- the decision is small and obvious
- the side effects are visible at the call site
- changing infrastructure behavior does not force a rewrite of the decision logic

### Push logic into focused types
When code has multiple phases, give each phase a focused type or function with a clear job.

The orchestrator should read like:
- decide
- act
- inspect result
- update state
- continue

Do not let one long function mix:
- workflow coordination
- state transitions
- persistence
- external calls
- presentation

If a function is getting long because it is coordinating several distinct phases, extract the phases until the top-level flow reads clearly.

### Dependencies and interfaces
- Pass dependencies via struct fields or explicit parameters, not globals.
- Use concrete types by default.
- Introduce an interface when the code is calling outward to an external dependency, or when a consumer genuinely needs an abstraction.
- Interfaces should be small, consumer-owned, and shaped around actual use.

Good examples:
- `Repository`
- `Clock`
- `Publisher`
- `Mailer`

Bad examples:
- one interface per struct
- wide "service" interfaces
- interfaces introduced only out of habit

#### Where to define interfaces

Define interfaces next to the code that depends on them, not next to the code that satisfies them.

BAD — interface defined in the domain package, consumed elsewhere:

    // package social
    type MessageRepository interface {
        SaveMessage(ctx context.Context, msg Message) error
    }

    // package sqlite
    var _ social.MessageRepository = (*MessageStore)(nil)

The interface lives in `social` but nothing in `social` calls its methods. The `var _` line is a sign the dependency flows backwards — the implementation is reaching back to the definer.

GOOD — interface defined where it is consumed:

    // package server (or wherever the interface is actually used)
    type MessageRepository interface {
        SaveMessage(ctx context.Context, msg social.Message) error
    }

    // package sqlite — just implements the methods, no compile-time assertion needed
    // because the consumer package owns the interface and Go's implicit satisfaction handles the rest

The interface moves to the package that injects and calls it. The implementing package does not need to know the interface exists.

### Types and errors
- Prefer `(T, error)` or `error`.
- Use early returns for failures; keep the happy path flat.
- Make zero values useful when possible; otherwise provide `NewX(...)`.
- Export types and fields only when necessary. Start private, open later.
- Prefer concrete result types and named actions over vague booleans like `OK` when the outcome has real meaning.

## Architecture

### Package organization
Organize packages around business capabilities or subsystems, not diagram labels.

Avoid catch-all names like:
- `domain`
- `common`
- `utils`
- `models`
- `types`

Split packages only for a real reason:
- enforcing access control with `internal/`
- isolating a distinct subsystem
- separating infrastructure concerns
- breaking a circular dependency

The package name should tell you what the code is about, not which box in a diagram it came from.

This repository is an application, not a public library.

Default to `internal/` for new code. Keep packages unexported to the outside world unless there is a concrete need to expose an API. If a package only exists to support this app, it belongs under `internal/`.

Prefer a small number of larger, coherent packages over many tiny feature packages. Do not create a new package just because a task mentions a new feature. Add code to an existing package when the feature is part of that package's responsibility.

Use package creation as a last resort, not a default move.

Good reasons to create a new package here:
- the code introduces a genuinely separate subsystem with its own persistence and workflow
- the code would otherwise create import cycles that cannot be removed by better factoring
- the code needs `internal/` boundaries to prevent inappropriate access
- the existing package would become hard to understand because it now contains multiple unrelated responsibilities

Bad reasons to create a new package here:
- the task title names a new concept
- the feature could fit into an existing package with a few more types or methods
- you want one package per noun in the product
- you are following generic hexagonal-architecture folder habits

When in doubt, change an existing package instead of creating a new one.

For this repo specifically, bias toward a layout like this until it becomes clearly insufficient:
- `internal/user` for user identity and account state
- `internal/social` for connections, requests, blocks, invites, discovery, and profile visibility rules
- `internal/content` for posts, feed assembly, comments, and reactions
- `internal/notify` for notification preferences and notification decision logic

These are examples of grouping, not a command to create all of them immediately. Start with the fewest packages needed.

Before creating a new package, explicitly check:
- can this live in an existing package without making that package incoherent?
- is this package boundary buying real access control or dependency isolation?
- will this reduce imports and coupling, or just spread one workflow across more directories?

If the answer is unclear, do not create the package.

### Hexagonal architecture in Go
Hexagonal architecture in Go is not about folders named `domain`, `ports`, and `adapters`.

It is about dependency direction:
- core logic does not know about HTTP, SQL, RPC frameworks, CLI parsing, or vendor SDK details
- external systems call into the app through exported concrete APIs
- the app calls outward through small consumer-owned interfaces
- concrete wiring happens at the edge

In Go, this usually means:
- inbound side: exported concrete services, constructors, and entry points
- outbound side: small interfaces like `Repository`, `Clock`, or `Publisher`
- composition root: `main` or a small wiring package

The architecture should show up in imports and dependency flow, not in ceremony or folder names.

### Infrastructure at the edge
Keep transport and infrastructure concerns at the boundary:
- HTTP request/response
- CLI flags and parsing
- queue message formats
- DB row scanning
- vendor SDK request/response types

Translate boundary inputs into plain Go values or command structs before they reach core logic.

Core logic should not accept framework-specific types unless that framework is itself the subject of the package.

Examples of leaks to avoid:
- `http.Request` deep in business logic
- SQL row types escaping repositories
- SDK-specific models threaded through the whole app