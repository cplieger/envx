package envx

import (
	"cmp"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// String returns the value of the environment variable key, or fallback when
// the variable is unset or empty. An empty value is treated as unset because
// compose files and CI matrices routinely materialize `KEY=` for a knob the
// operator left blank; distinguishing that from absence is almost never what
// a config reader wants (use os.LookupEnv directly when it is).
//
// Unlike the parsing getters (Bool, Int, Duration), String does not trim the
// value: whitespace can be meaningful in a free-form string, and the caller
// knows whether its value is a path, a token, or a list. A whitespace-only
// value therefore counts as set.
func String(key, fallback string) string {
	return cmp.Or(os.Getenv(key), fallback)
}

// Bool returns the boolean value of the environment variable key, or fallback
// when the variable is unset or empty.
//
// Parsing is tolerant of the spellings deployment files actually contain:
// true/1/yes/on and false/0/no/off, case-insensitive, surrounding whitespace
// ignored. Any other value logs one Warn through slog's default logger and
// returns fallback, so a typo ("ture") is visible in the logs instead of
// silently flipping a flag.
func Bool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		warnMalformed(key, v, "boolean", fallback)
		return fallback
	}
}

// Int returns the integer value of the environment variable key, or fallback
// when the variable is unset or empty. A set-but-unparseable value logs one
// Warn through slog's default logger and returns fallback.
func Int(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		warnMalformed(key, v, "integer", fallback)
		return fallback
	}
	return n
}

// Duration returns the value of the environment variable key parsed with
// time.ParseDuration ("30s", "6h", "1h30m"), or fallback when the variable is
// unset or empty. A set-but-unparseable value logs one Warn through slog's
// default logger and returns fallback.
//
// A bare number without a unit is deliberately not accepted: "30" is ambiguous
// between seconds and minutes across tools, and time.ParseDuration rejecting
// it (with the Warn line naming the key) is clearer than guessing.
func Duration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		warnMalformed(key, v, "duration", fallback)
		return fallback
	}
	return d
}

// warnMalformed emits the single shared diagnostic for a set-but-unparseable
// variable. The raw value is included: config values are not secrets (Secret
// never routes here), and the operator fixing the deployment needs to see
// what was actually set.
func warnMalformed(key, value, kind string, fallback any) {
	slog.Warn("envx: malformed value, using fallback",
		"key", key, "value", value, "kind", kind, "fallback", fallback)
}
