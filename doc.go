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
// secret file, read once, size-bounded, and trimmed.
//
// For the caller that must own the malformed-value decision instead of
// accepting warn-and-fallback — reject startup, apply bounds, keep an
// existing value — IntStrict and DurationStrict return the parse result as
// (value, ok, error) and never log.
//
// envx reads the process environment at call time; it holds no state, starts
// no goroutines, and has no dependencies beyond the standard library.
package envx
