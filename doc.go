// Package envx reads typed configuration from environment variables, the
// standard way a containerized app is configured.
//
// Every getter takes a fallback and never fails: an unset or empty variable
// returns the fallback silently, and a set-but-malformed value returns the
// fallback with one Warn line through slog's default logger, so a typo in a
// deployment surfaces in the logs instead of silently changing behavior.
// Boolean parsing is tolerant (true/1/yes/on, false/0/no/off,
// case-insensitive, trimmed) because that is what deployment YAML tends to
// contain.
//
// Two calls handle the values an app cannot default: Require returns an error
// for a missing mandatory variable, and Secret additionally supports the
// Docker convention of an adjacent KEY_FILE variable pointing at a mounted
// secret file, read once, size-bounded, and returned as written apart from one
// trailing line ending. Each way a secret file can be unusable is named by a
// sentinel, so a caller can report the class with errors.Is instead of matching
// message text or echoing the configured path. A KEY_FILE that is
// present but blank names no file, so it resolves as if unset;
// IsBlankSecretFilePath reports that state for the caller who must refuse a
// broken secret pointer rather than fall back to the plain variable.
//
// For the caller that must own the malformed-value decision instead of
// accepting warn-and-fallback — reject startup, apply bounds, keep an
// existing value — BoolStrict, IntStrict and DurationStrict return the parse
// result as (value, ok, error) and never log. BoolStrict shares Bool's parser,
// so the two accept exactly the same spellings; prefer it over Bool for a key
// whose value may be sensitive (the Warn line names the raw value) or when the
// caller owns its own diagnostics.
//
// envx reads the process environment at call time; it holds no state, starts
// no goroutines, and depends on nothing beyond the standard library and
// github.com/cplieger/pathinside, the standard-library-only path-name
// predicates the Secret file rule is built from.
package envx
