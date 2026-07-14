# envx

[![Go Reference](https://pkg.go.dev/badge/github.com/cplieger/envx.svg)](https://pkg.go.dev/github.com/cplieger/envx)
[![Go version](https://img.shields.io/github/go-mod/go-version/cplieger/envx)](https://github.com/cplieger/envx/blob/main/go.mod)
[![Test coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/cplieger/envx/badges/coverage.json)](https://github.com/cplieger/envx/actions/workflows/coverage.yml)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/PROJECT_ID/badge)](https://www.bestpractices.dev/projects/PROJECT_ID)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/cplieger/envx/badge)](https://scorecard.dev/viewer/?uri=github.com/cplieger/envx)

> Typed environment-variable configuration for containerized Go apps

A tiny, standard-library-only reader for the way containerized apps are
actually configured: environment variables with sensible defaults. Every
getter takes a fallback and never fails — an unset or empty variable falls
back silently, and a set-but-malformed value falls back with one `slog` Warn
naming the variable, so a deployment typo shows up in the logs instead of
silently changing behavior.

Two calls cover the values an app cannot default: `Require` returns a typed
error for a missing mandatory variable, and `Secret` adds the Docker secrets
convention (`KEY_FILE` pointing at a mounted file, read once, size-bounded,
trimmed) on top.

## Install

```sh
go get github.com/cplieger/envx@latest
```

## Usage

```go
addr := envx.String("APP_LISTEN", ":8080")
debug := envx.Bool("APP_DEBUG", false)          // true/1/yes/on · false/0/no/off
retries := envx.Int("APP_RETRIES", 3)
interval := envx.Duration("APP_INTERVAL", 6*time.Hour) // Go duration syntax

token, err := envx.Require("APP_TOKEN") // *envx.MissingError when unset/empty
if err != nil {
	slog.Error("startup", "error", err)
	os.Exit(1)
}

// Docker secrets: reads APP_API_KEY_FILE when set, else APP_API_KEY.
apiKey, err := envx.Secret("APP_API_KEY")
```

## API

- `String(key, fallback string) string` — value or fallback; empty counts as unset.
- `Bool(key string, fallback bool) bool` — tolerant parse (`true/1/yes/on`, `false/0/no/off`, case-insensitive, trimmed); malformed → Warn + fallback.
- `Int(key string, fallback int) int` — `strconv.Atoi` on the trimmed value; malformed → Warn + fallback.
- `Duration(key string, fallback time.Duration) time.Duration` — `time.ParseDuration` syntax (`30s`, `6h`, `1h30m`); a bare unitless number is rejected (ambiguous) → Warn + fallback.
- `Require(key string) (string, error)` — value, or `*MissingError` (carries `Key`) when unset or empty. Returns an error rather than exiting so a caller can collect every missing variable and fail once.
- `Secret(key string) (string, error)` — `KEY_FILE` (mounted secret file: single-handle bounded read, 1 MB cap, traversal-rejected, whitespace-trimmed) wins over `KEY`. The secret value never appears in an error or log line.
- `MissingError{Key}` — the typed missing-variable error, detectable with `errors.As`.

## Behavior contract

- **Empty equals unset.** Compose files and CI matrices routinely materialize `KEY=` for a knob the operator left blank; every getter treats that as absence. Use `os.LookupEnv` directly in the rare case the distinction matters.
- **Malformed values are visible, never fatal.** The one Warn line (through `slog.Default()`) carries `key`, the raw `value`, the expected `kind`, and the `fallback` used. Config values are not secrets; `Secret` never routes through this path.
- **No state, no goroutines, no import-time reads.** The process environment is read at call time only.

## Unsupported by design

Deliberate non-goals, not TODOs:

| Feature | Rationale |
| --- | --- |
| Struct tags / reflection-based config loading | This is a getter library, not a config framework. An app's config struct assembles itself from explicit calls, which keeps every default and key name greppable. |
| `.env` file loading | The container runtime (compose, Kubernetes) owns materializing the environment; a second loader creates precedence questions with no consumer need. |
| Float / slice / map getters | No consumer parses these from the environment. Added only when a real app needs one. |
| Prefix namespacing (`WithPrefix("APP_")`) | Key names stay greppable verbatim; a prefix helper saves a few characters and costs discoverability. |
| Panic-on-missing (`MustX`) | `Require` returns an error so startup can report every missing variable at once instead of dying on the first. |

## Disclaimer

This project is built with care and follows security best practices, but it is
intended for personal / self-hosted use. No guarantees of fitness for production
environments. Use at your own risk.

This project was built with AI-assisted tooling using [Claude Opus](https://www.anthropic.com/claude)
and [Kiro](https://kiro.dev). The human maintainer defines architecture,
supervises implementation, and makes all final decisions.

## License

GPL-3.0 — see [LICENSE](LICENSE).
