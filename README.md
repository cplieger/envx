# envx

[![Go Reference](https://pkg.go.dev/badge/github.com/cplieger/envx.svg)](https://pkg.go.dev/github.com/cplieger/envx)
[![Go version](https://img.shields.io/github/go-mod/go-version/cplieger/envx)](https://github.com/cplieger/envx/blob/main/go.mod)
[![Test coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/cplieger/envx/badges/coverage.json)](https://github.com/cplieger/envx/actions/workflows/coverage.yml)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/13604/badge)](https://www.bestpractices.dev/projects/13604)
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

For apps configured by a YAML file rather than the environment, the
`envx/yamlenv` subpackage expands allowlisted `${VAR}` references inside the
parsed document's string values, so secrets stay in the environment while the
file holds structure — and `SanitizeDecodeError` closes the hole expansion
opens: a failing decode of the expanded document embeds a scalar excerpt that
may now be a secret, so the sanitizer rebuilds the error from its
value-independent structure before it reaches a startup log. It is its own
nested Go module: the YAML dependency (`go.yaml.in/yaml/v3`) lives in
`yamlenv/go.mod`, the root `envx` module is zero-dependency, and yamlenv is
versioned and released independently
(`go get github.com/cplieger/envx/yamlenv@vX.Y.Z`; the backing git tags are
named `yamlenv/vX.Y.Z`).

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

YAML config files reference environment variables with `${VAR}` and expand
them after parsing, inside string values only:

```go
var doc yaml.Node
if err := yaml.Unmarshal(data, &doc); err != nil { ... }
allow := func(name string) bool { return strings.HasPrefix(name, "APP_") }
if unresolved := yamlenv.Expand(&doc, allow); len(unresolved) > 0 {
	slog.Warn("config references unset environment variables",
		"vars", strings.Join(unresolved, ","))
}
if err := doc.Decode(&cfg); err != nil {
	// The raw decode error can embed an expanded secret in its scalar
	// excerpt; sanitize it before it reaches the startup log.
	return yamlenv.SanitizeDecodeError(err)
}
```

## API

- `String(key, fallback string) string` — value or fallback; empty counts as unset.
- `Bool(key string, fallback bool) bool` — tolerant parse (`true/1/yes/on`, `false/0/no/off`, case-insensitive, trimmed); malformed → Warn + fallback.
- `Int(key string, fallback int) int` — `strconv.Atoi` on the trimmed value; malformed → Warn + fallback.
- `Duration(key string, fallback time.Duration) time.Duration` — `time.ParseDuration` syntax (`30s`, `6h`, `1h30m`); a bare unitless number is rejected (ambiguous) → Warn + fallback.
- `IntStrict(key string) (int, bool, error)` / `DurationStrict(key string) (time.Duration, bool, error)` — the parse result owned by the caller: unset/empty → `(0, false, nil)`, malformed → `(0, false, err)` (the error names the key and wraps the parse error), valid → `(v, true, nil)`. Never logs. For the caller that must decide what a malformed value means — reject startup, apply bounds, keep an existing value — instead of accepting Warn + fallback.
- `Require(key string) (string, error)` — value, or `*MissingError` (carries `Key`) when unset or empty. Returns an error rather than exiting so a caller can collect every missing variable and fail once.
- `Secret(key string) (string, error)` — `KEY_FILE` (mounted secret file: single-handle bounded read, 1 MB cap, traversal-rejected, whitespace-trimmed) wins over `KEY`. The secret value never appears in an error or log line.
- `MissingError{Key}` — the typed missing-variable error, detectable with `errors.As`.
- `yamlenv.Expand(root *yaml.Node, allow func(name string) bool) (unresolved []string)` (subpackage `envx/yamlenv`) — expand allowlisted `${VAR}` references inside a parsed YAML document's string scalar values, in place. Post-parse by design: an environment value containing YAML syntax (a quote, a newline, a `#`) lands as an inert string and can never change the document structure, unlike pre-parse text expansion. Braced `${VAR}` only; a non-allowlisted name, an unset variable, and an unbraced `$VAR` stay byte-for-byte literal; mapping keys and non-string scalars are untouched; expansion is a single pass. An empty-but-set variable substitutes (set-vs-unset is the contract here, not the getters' empty-equals-unset). Returns the allowlisted names that stayed unresolved, deduplicated in document order, for the caller to warn on.
- `yamlenv.SanitizeDecodeError(err error, opts ...SanitizeOption) error` (subpackage `envx/yamlenv`) — rewrite a yaml.v3 parse or decode error so no fragment of a document value survives into the message. Expansion creates the risk this closes: a decode that fails AFTER `${VAR}` secrets were substituted embeds a backtick-quoted excerpt of the offending scalar, and such errors are typically logged at startup. Each `*yaml.TypeError` entry is rebuilt from its value-independent structure — a wrong-type entry keeps `line N: cannot unmarshal !!<tag>` and `into <type>` around a `<redacted>` placeholder, a duplicate-key entry keeps both line numbers and redacts the key, a strict-decode unknown-key entry redacts the key name unless `WithUnknownKeyEcho()` opts in to keeping it (the name is the diagnostic that fixes a typo; the Go type name is always dropped) — and any unrecognized shape falls back to a fixed withheld message keeping at most the `yaml: line N:` locator. Nil passes through; the returned error never wraps the original, so no unwrap path can reach the withheld text.
- `yamlenv.CheckUnknownKeys(data []byte, probe any) error` (subpackage `envx/yamlenv`) — fail loudly on a key the config type does not declare: a `KnownFields(true)` re-decode of the raw document into `probe` (a pointer to a fresh throwaway value of the caller's config struct), so a misspelled or misplaced key errors instead of being silently ignored while its intended setting stays at the default. Run it on the pre-expansion bytes: expansion rewrites string values only, so it cannot change which keys exist, and line numbers point at the file the operator wrote. An empty document passes. The returned error is yaml.v3's and may embed document content — log it through `SanitizeDecodeError`, which recognizes the unknown-key shape.
- `yamlenv.CheckSingleDocument(data []byte) error` (subpackage `envx/yamlenv`) — reject input carrying more than one YAML document: single-document pipelines (`yaml.Unmarshal`, one `Decode`) consume only the first, so everything below a stray `---` separator would be silently dropped. A first document that fails to parse returns nil (the caller's parse steps own that diagnostic); the only non-nil return is the static `ErrMultipleDocuments`, which embeds no input content and is safe to log unsanitized.

## Behavior contract

- **Empty equals unset.** Compose files and CI matrices routinely materialize `KEY=` for a knob the operator left blank; every getter treats that as absence. Use `os.LookupEnv` directly in the rare case the distinction matters.
- **Malformed values are visible, never fatal.** The one Warn line (through `slog.Default()`) carries `key`, the raw `value`, the expected `kind`, and the `fallback` used. Config values are not secrets; `Secret` never routes through this path. The strict variants (`IntStrict`, `DurationStrict`) return the malformed value as an error instead and never log — the caller owns the decision.
- **Parsing getters trim; `String` does not.** `Bool`, `Int`, `Duration`, and the strict variants parse the whitespace-trimmed value; `String` returns the raw value because whitespace can be meaningful in a free-form string (a whitespace-only value counts as set).
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
