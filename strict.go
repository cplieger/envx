package envx

import (
	"fmt"
	"strconv"
	"time"
)

// BoolStrict returns the boolean value of the environment variable key with
// the parse result owned by the caller: a set-but-malformed value is returned
// as an error instead of the tolerant getters' warn-and-fallback.
//
// The accepted spellings are exactly Bool's (true/1/yes/on and false/0/no/off,
// case-insensitive, surrounding whitespace ignored) because both route through
// the same parser and cannot drift apart. The three states match IntStrict:
// unset or empty (false, false, nil), malformed (false, false, err), valid
// (b, true, nil). value is false in two of those three, so ok — never value
// alone — distinguishes "set to false" from "not set".
//
// Unlike IntStrict and DurationStrict, the error carries no fragment of the
// value: there is no parse error to wrap, and the reason to reach for
// BoolStrict is usually that the value must not be repeated anywhere. It names
// the key and the accepted vocabulary. Strict variants never log.
//
// Use Bool when a malformed value should fall back with a Warn naming the raw
// value; use BoolStrict for a key whose value may be sensitive (one an
// operator could wire to a secret by mistake, so no diagnostic may echo it) or
// when the caller owns its own diagnostics.
//
// (value and ok share their bool type in the signature at the linter's
// insistence; they mean what the family's other strict variants mean.)
func BoolStrict(key string) (value, ok bool, err error) {
	b, _, ok, err := parseEnv(key, parseBool)
	if err != nil {
		return false, false, fmt.Errorf("environment variable %s: %w", key, err)
	}
	return b, ok, nil
}

// IntStrict returns the integer value of the environment variable key with
// the parse result owned by the caller: a set-but-malformed value is returned
// as an error instead of the tolerant getters' warn-and-fallback.
//
// ok reports a successfully parsed value; it is false when the variable is
// unset or empty (empty equals unset, as with every getter) and false when
// the value did not parse. err is non-nil only for a set-but-malformed value;
// it names the key and wraps the underlying strconv error. Exactly one of
// the three states holds: unset (0, false, nil), malformed (0, false, err),
// or valid (n, true, nil). Strict variants never log.
//
// Use Int when a malformed value should fall back with a Warn; use IntStrict
// when the caller must decide what a malformed value means (reject startup,
// apply bounds, keep an existing value).
func IntStrict(key string) (value int, ok bool, err error) {
	n, _, ok, err := parseEnv(key, strconv.Atoi)
	if err != nil {
		return 0, false, fmt.Errorf("environment variable %s: %w", key, err)
	}
	return n, ok, nil
}

// DurationStrict returns the value of the environment variable key parsed
// with time.ParseDuration ("30s", "6h", "1h30m"), with the parse result owned
// by the caller: a set-but-malformed value is returned as an error instead of
// the tolerant getters' warn-and-fallback.
//
// The three states match IntStrict: unset or empty (0, false, nil), malformed
// (0, false, err), valid (d, true, nil). As with Duration, a bare number
// without a unit is rejected ("30" is ambiguous between seconds and minutes
// across tools). Strict variants never log.
func DurationStrict(key string) (value time.Duration, ok bool, err error) {
	d, _, ok, err := parseEnv(key, time.ParseDuration)
	if err != nil {
		return 0, false, fmt.Errorf("environment variable %s: %w", key, err)
	}
	return d, ok, nil
}
