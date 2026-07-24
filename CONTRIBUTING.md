# Contributing to envx

Notes on the surface, the design contract, and the local workflow. Most of
the guidance is about keeping the library a set of typed getters rather than
letting it grow into a configuration framework.

## Getters, not a framework

`envx` is a standard-library-only package (no runtime or test dependencies)
that reads typed values from the process environment. The whole surface is
three ideas:

- **`String` / `Bool` / `Int` / `Duration`**: fallback-taking getters that
  never fail: unset or empty falls back silently; set-but-malformed falls
  back with one `slog` Warn naming the key, the raw value, the expected
  kind, and the fallback used. Empty-equals-unset is deliberate (compose
  files materialize `KEY=` for blank knobs); tolerant `Bool` spellings
  (`true/1/yes/on`, `false/0/no/off`) are deliberate (that is what
  deployment YAML contains). The strict variants (`IntStrict`,
  `DurationStrict`) return the parse result as data instead and never log;
  the caller owns the malformed-value decision.
- **`Require`**: returns `*MissingError` instead of exiting, so a caller
  can collect every missing variable and fail startup once.
- **`Secret`**: `Require` plus the Docker secrets convention: a `KEY_FILE`
  variable pointing at a mounted file wins over `KEY`; the read is
  single-handle (no stat-then-open race), size-bounded (1 MB), traversal-
  rejected, and whitespace-trimmed. The secret value never appears in an
  error or log line.

Anything beyond that (struct-tag loading, `.env` files, float/slice/map
getters, prefix namespacing, panic-on-missing variants) is out of scope by
design; the README's "Unsupported by Design" table is the contract. Add a
getter only when a real consumer parses that type from the environment.

The `yamlenv` subpackage is its own nested Go module (it alone carries the
YAML dependency; the root module stays dependency-free) providing allowlisted
`${VAR}` expansion and the strict-loading pipeline for YAML-configured apps;
the README documents its surface.

## Behavior invariants

- A getter never fails and never exits; `Require`/`Secret` return errors and
  never exit. Process-lifecycle decisions belong to the caller.
- Malformed values are visible (one Warn through `slog.Default()`) but never
  fatal, and `Secret` never routes a secret value through that Warn.
- No state, no goroutines, no import-time environment reads.

## Local workflow

```sh
go build ./... && go vet ./...
go test -race ./...
golangci-lint run ./...
```

`yamlenv/` is its own module, so `./...` from the root does not reach it;
repeat the same commands inside `yamlenv/`.

Tests are table-driven plus fuzz targets over the parse boundaries
(`FuzzBool`, `FuzzInt`, `FuzzDuration`, `FuzzIntStrict`, `FuzzDurationStrict`,
and `FuzzSecretPath` in the root module; `FuzzExpand`, `FuzzLoad`,
`FuzzSanitizeDecodeError`, and `FuzzCheckSingleDocument` in yamlenv); the
Warn diagnostics are asserted through an in-package recording handler so the
root module stays dependency-free even in tests. CI (`ci / validate`) runs
the same battery via the shared cplieger/ci workflows; conventional commits
drive git-cliff release versioning.
