# envx

[![Go Reference](https://pkg.go.dev/badge/github.com/cplieger/envx.svg)](https://pkg.go.dev/github.com/cplieger/envx)
[![Go version](https://img.shields.io/github/go-mod/go-version/cplieger/envx)](https://github.com/cplieger/envx/blob/main/go.mod)
[![Test coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/cplieger/envx/badges/coverage.json)](https://github.com/cplieger/envx/actions/workflows/coverage.yml)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/13604/badge)](https://www.bestpractices.dev/projects/13604)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/cplieger/envx/badge)](https://scorecard.dev/viewer/?uri=github.com/cplieger/envx)

> Typed environment-variable configuration for containerized Go apps

A tiny reader for the way containerized apps are actually configured:
environment variables with sensible defaults. Every getter takes a fallback
and never fails. An unset or empty variable falls back silently; a
set-but-malformed value falls back with one `slog` Warn naming the variable,
so a deployment typo shows up in the logs instead of silently changing
behavior.

Two calls cover the values an app cannot default: `Require` returns a typed
error for a missing mandatory variable, and `Secret` adds the Docker secrets
convention (`KEY_FILE` pointing at a mounted file, read once, size-bounded,
returned as written apart from one trailing line ending) on top.

For apps configured by a YAML file rather than the environment, the
`envx/yamlenv` subpackage expands allowlisted `${VAR}` references inside the
parsed document's string values, so secrets stay in the environment while the
file holds structure. Expansion opens one hole: a failing decode of the
expanded document can embed a secret in its error message.
`SanitizeDecodeError` closes it, rebuilding the error so it is safe to log
at startup. yamlenv is its own nested Go module,
versioned and released independently; it alone carries the YAML dependency,
which the root `envx` module never links — plain `envx` requires only
`github.com/cplieger/pathinside` (standard-library-only, and the source of the
`KEY_FILE` path rule). Install yamlenv separately:
`go get github.com/cplieger/envx/yamlenv@vX.Y.Z`.

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

// A present-but-empty APP_API_KEY_FILE names no file, so it resolves as if
// unset. Refuse it when a broken secret mount must not fall back:
if envx.IsBlankSecretFilePath("APP_API_KEY") {
	slog.Error("startup", "error", "APP_API_KEY_FILE is set but empty")
	os.Exit(1)
}
```

YAML config files reference environment variables with `${VAR}` and expand
them after parsing, inside string values only. `Load` is the one-call safe
pipeline: single-document check, unknown-key strictness, parse, expansion,
decode, and fail-closed error sanitization, in the right order:

```go
cfg := defaultConfig() // decode overlays the file onto your defaults
allow := func(name string) bool { return strings.HasPrefix(name, "APP_") }
unresolved, err := yamlenv.Load(data, &cfg, allow)
if len(unresolved) > 0 {
	slog.Warn("config references unset environment variables",
		"vars", strings.Join(unresolved, ","))
}
if err != nil {
	// Already safe to log: a misspelled key, a second document, and any
	// parse/decode failure come back sanitized (no expanded secret can
	// survive into the message).
	return err
}
```

The pieces `Load` composes (`Expand`, `SanitizeDecodeError`,
`CheckUnknownKeys`, `CheckSingleDocument`) stay exported for pipelines with
different policy and are documented individually below.

## API

- `String(key, fallback string) string`: value or fallback; empty counts as unset.
- `Bool(key string, fallback bool) bool`: tolerant parse (`true/1/yes/on`, `false/0/no/off`, case-insensitive, trimmed); malformed → Warn + fallback.
- `Int(key string, fallback int) int`: `strconv.Atoi` on the trimmed value; malformed → Warn + fallback.
- `Duration(key string, fallback time.Duration) time.Duration`: `time.ParseDuration` syntax (`30s`, `6h`, `1h30m`); a bare unitless number is rejected (ambiguous) → Warn + fallback.
- `BoolStrict(key string) (bool, bool, error)`: `Bool`'s parser with the result owned by the caller instead of Warn + fallback: unset/empty → `(false, false, nil)`, malformed → `(false, false, err)`, valid → `(b, true, nil)`. `ok`, not the value, distinguishes "set to false" from "not set". Never logs, and the error names the key and the accepted spellings but never the value. Prefer it over `Bool` for a key whose value may be sensitive — one an operator could wire to a secret by mistake, since `Bool`'s Warn line carries the raw value — or when the caller owns its own diagnostics.
- `IntStrict(key string) (int, bool, error)` / `DurationStrict(key string) (time.Duration, bool, error)`: the parse result owned by the caller instead of Warn + fallback: unset/empty → `(0, false, nil)`, malformed → `(0, false, err)` naming the key, valid → `(v, true, nil)`. Never logs. A malformed value returns a `*ParseError` (see below).
- `ParseError{Err, Key, Value}`: the malformed-value error from `IntStrict` / `DurationStrict`, detected with `errors.As`. `Value` is the **trimmed** value the parser rejected, so a caller quoting the rejected input needs no second `os.Getenv` — which returns the value untrimmed and can therefore name a string the parser never parsed. `Unwrap` exposes the underlying error, so `errors.As` still reaches `*strconv.NumError`. `BoolStrict` deliberately does **not** return it: that variant exists for a key whose value must never be echoed, and a typed error carrying the value would hand it to every caller that logs the error.
- `Require(key string) (string, error)`: value, or `*MissingError` (carries `Key`) when unset or empty. Returns an error rather than exiting so a caller can collect every missing variable and fail once.
- `Secret(key string) (string, error)`: `KEY_FILE` (mounted secret file: single-handle bounded read, 1 MB cap, traversal-rejected, with at most one trailing line ending removed) wins over `KEY`. The secret value never appears in an error or log line.
- `SecretWithSource(key string) (string, SecretSource, error)`: `Secret` plus the channel that supplied the value (`SourceFile`, `SourceEnv`, `SourceNone`), reported on the error paths too, so a caller can warn that a `KEY` it also set was ignored in favour of `KEY_FILE`.
- `IsBlankSecretFilePath(key string) bool`: reports a `KEY_FILE` that is present but blank (empty or whitespace only) and therefore names no file. Resolution is unchanged — it still falls through as if unset — but the caller can refuse it, which matters when the secret is optional and the fallthrough is fail-open.
- `ErrBlankSecretFile`: the sentinel for a `KEY_FILE` naming a readable file whose content is blank (empty or whitespace only), so a caller's allow-empty policy can cover both delivery channels identically.
- `ErrSecretFilePathRejected`, `ErrSecretFileTooLarge`, `ErrSecretFileGrew`, `ErrSecretFileUnreadable`: the remaining secret-file failure classes, each detectable with `errors.Is`, so a caller can report WHY the file was unusable without matching error text and without echoing the operator-supplied path (which is the leak risk when `KEY_FILE` was misconfigured to hold the secret itself). The OS-failure class keeps its `*os.PathError` reachable with `errors.As`.
- `MissingError{Key}`: the typed missing-variable error, detectable with `errors.As`.
- `yamlenv.Load(data []byte, out any, allow func(name string) bool, opts ...LoadOption) (unresolved []string, err error)`: the composed safe loading pipeline in one call: single-document check, unknown-key strictness, parse, `Expand` with the caller's allowlist, decode into `out` (a non-nil pointer pre-populated with defaults), and sanitized errors on every failure path. Options: `WithSanitizeOptions(...)` forwards sanitizer policy; `WithErrorPassthrough(pred)` returns caller-owned decode errors unchanged.
- `yamlenv.Expand(root *yaml.Node, allow func(name string) bool) (unresolved []string)`: in-place expansion of allowlisted, set `${VAR}` references inside a parsed document's string scalar values; post-parse, so an environment value can never change the document structure; everything else stays byte-for-byte literal. Returns the allowlisted names left unresolved, for the caller to warn on.
- `yamlenv.SanitizeDecodeError(err error, opts ...SanitizeOption) error`: rebuild a yaml.v3 parse or decode error from its value-independent structure (line numbers, source tags, destination types) so no fragment of a document value, possibly an expanded secret, survives into the message; `WithUnknownKeyEcho()` opts into keeping the unknown-key name. The returned error never wraps the original.
- `yamlenv.CheckUnknownKeys(data []byte, probe any) error`: fail loudly on a key the config type does not declare, via a `KnownFields(true)` re-decode of the raw pre-expansion document into `probe`. The returned error may embed document content; log it through `SanitizeDecodeError`.
- `yamlenv.CheckSingleDocument(data []byte) error`: reject input carrying more than one YAML document, so nothing below a stray `---` separator is silently dropped. The only non-nil return is the static `ErrMultipleDocuments`, safe to log unsanitized.

Full contracts (trim rules, the expansion grammar, the sanitizer's entry
shapes, the probe's value-error filtering) are in the package documentation:
[envx](https://pkg.go.dev/github.com/cplieger/envx) and
[yamlenv](https://pkg.go.dev/github.com/cplieger/envx/yamlenv).

## Behavior contract

- **Empty equals unset.** Compose files and CI matrices routinely materialize `KEY=` for a knob the operator left blank; every getter treats that as absence. Use `os.LookupEnv` directly in the rare case the distinction matters. The one distinction the package answers itself is a blank `KEY_FILE` (`IsBlankSecretFilePath`): that variable holds a pointer, not a value, so its blankness is a statement about this package's own channel selection rather than about the app's value semantics.
- **Malformed values are visible, never fatal.** The one Warn line (through `slog.Default()`) carries `key`, the raw `value`, the expected `kind`, and the `fallback` used. Config values are not secrets; `Secret` never routes through this path. The strict variants (`BoolStrict`, `IntStrict`, `DurationStrict`) return the malformed value as an error instead and never log; the caller owns the decision, which is also the way to read a key whose value could turn out to be sensitive.
- **A tolerant getter and its strict variant share one parser.** `Bool`/`BoolStrict`, `Int`/`IntStrict` and `Duration`/`DurationStrict` accept exactly the same values by construction, not by convention, so the two layers cannot drift apart; only the malformed-value policy differs (Warn + fallback, or an error).
- **Parsing getters trim; `String` does not.** `Bool`, `Int`, `Duration`, and the strict variants parse the whitespace-trimmed value; `String` returns the raw value because whitespace can be meaningful in a free-form string (a whitespace-only value counts as set).
- **A rejected value comes back with the error, so nobody re-reads the environment.** `IntStrict` and `DurationStrict` already trimmed the value to parse it, so `*ParseError.Value` hands it back. Reaching for `os.Getenv(key)` again to build the diagnostic was never equivalent: it returns the value untrimmed, so a message could quote `" 5x "` beside a parse error about `5x`, and it reads the environment a second time to learn something the first read already knew.
- **A secret is never rewritten.** Both `Secret` channels return the credential as configured: `KEY` verbatim, and `KEY_FILE`'s content with at most ONE trailing line ending (`\n` or `\r\n`) removed, because an editor or `kubectl create secret --from-file` appends one and a file cannot store a value without that ambiguity. Edge spaces, tabs, non-breaking spaces and a second trailing newline are content and survive, so a caller that validates a credential verbatim gets the same verdict from either channel. Blankness is the one judgement made on the trimmed content: a file holding only whitespace is `ErrBlankSecretFile`, a broken mount rather than a secret.
- **No state, no goroutines, no import-time reads.** The process environment is read at call time only.

## Unsupported by Design

Deliberate non-goals, not TODOs:

| Feature | Rationale |
| --- | --- |
| Struct tags / reflection-based config loading | This is a getter library, not a config framework. An app's config struct assembles itself from explicit calls, which keeps every default and key name greppable. |
| `.env` file loading | The container runtime (compose, Kubernetes) owns materializing the environment; a second loader creates precedence questions with no consumer need. |
| Float / slice / map getters | No consumer parses these from the environment. Added only when a real app needs one. |
| Prefix namespacing (`WithPrefix("APP_")`) | Key names stay greppable verbatim; a prefix helper saves a few characters and costs discoverability. |
| Panic-on-missing (`MustX`) | `Require` returns an error so startup can report every missing variable at once instead of dying on the first. |

## Contributing

Issues and PRs are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for the
conventions and how to run the checks locally.

## Disclaimer

This project is built with care and follows security best practices, but it is
intended for personal / self-hosted use. No guarantees of fitness for production
environments. Use at your own risk.

This project was built with AI-assisted tooling using
[Claude](https://claude.com), [GPT](https://openai.com), and
[Kiro](https://kiro.dev). The human maintainer defines architecture,
supervises implementation, and makes all final decisions.

## License

Apache-2.0. See [LICENSE](LICENSE).
