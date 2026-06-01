# Contributing to Muse

Thanks for your interest in Muse! This guide covers the repo layout, how to
build and test, and the (deliberately small) workflow for extending the engine.

## Prerequisites

- **Go 1.22+** (two modules — see below)
- **Docker** (for the hosted-API stack and integration tests via testcontainers)
- **buf** (`go install github.com/bufbuild/buf/cmd/buf@latest`) — only if you
  change `proto/`
- **Node 18+** (only if you change the docs site under `docs/website/`)

## The two Go modules

Muse is split so the engine can be embedded with zero hosting dependencies:

- **`gamekit/`** (`github.com/muse/gamekit`) — the pure engine SDK. Its **own Go
  module**, **stdlib-only**, no I/O and no transport. This is what Mode-A users
  import.
- **`github.com/muse`** (repo root) — the hosted composition: proto-generated
  contracts, SQL/Redis adapters, the Core gRPC+REST service, `bffkit`, and the
  reference BFFs + embed example under `examples/`. It imports `gamekit` and
  wires concrete adapters around it.

A `go.work` at the root ties the two together for local development. `go build
./...` from the repo root covers everything in the root module (including
`examples/...`); run it again inside `gamekit/` to cover the SDK module.

## Build & test

```bash
make build            # build all packages in both modules
make test             # gamekit unit tests (no infra needed — pure SDK)
make test-race        # gamekit tests under the race detector
make vet              # go vet, both modules
make embed            # run the in-memory Start → Play example (Mode A)
```

Integration tests spin up **real Postgres + MySQL + Redis** via testcontainers
(needs Docker):

```bash
make test-integration   # adapter port-contract suite against both SQL engines
```

End-to-end against the full hosted stack:

```bash
make up                 # docker compose: datastores + core + both BFFs + observability
make seed               # demo campaign, games, prizes
make e2e                # scripted spin-wheel flow; see `make help` for e2e-* variants
make down
```

## Code style

- **gofmt is enforced in CI.** Run `gofmt -w` on your changes (or `make vet`
  workflows) before pushing — the CI `gofmt` job fails on any unformatted file.
- Match the surrounding code: existing comment density, naming, and idioms. The
  codebase favors small, well-commented functions and raw SQL over ORMs.
- Keep `gamekit` dependency-free (stdlib only). I/O belongs in `adapters/`,
  transport in `core/`, edge concerns in `bffkit/`.

## Changing the proto contract

The `game.v1` contract lives in `proto/`; generated Go is committed under
`pkg/gen/`. After editing a `.proto`:

```bash
make generate         # buf lint + buf generate + envelope-wrap the OpenAPI spec
```

Commit the regenerated files alongside your proto change.

## Extending the engine (the common case)

Adding gameplay should **not** touch the engine core:

- **A new game of an existing shape** = JSON config only (a new `handler_config`
  / `validator_config`). No code. See the
  [add-a-game guide](docs/website/docs/guides/add-a-game.md).
- **A genuinely new shape** = register one `seed_generator`, `reward_handler`,
  and/or `validator` in `gamekit`, then wire it in `std`. The engine's `Play`
  transaction never changes. See the
  [add-a-shape guide](docs/website/docs/guides/add-a-shape.md).

If you find yourself editing `gamekit/engine` to add a game, that's a signal the
change belongs in a handler/validator instead — open an issue to discuss.

## Pull requests

1. Branch off `main`.
2. Keep the change focused; include tests (unit for `gamekit`, the contract
   suite for adapters).
3. Ensure `make build`, `make test`, and `gofmt -l` are clean.
4. Describe the change and how you verified it.

## Architecture & docs

The [Docusaurus site](docs/website/) (`make docs`) is the best map of the
system — components, data model, gameplay/anti-cheat/reward flows, and the
extension guides. Start there if you're unsure where a change belongs.
