package envx

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

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
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false, fmt.Errorf("environment variable %s: %w", key, err)
	}
	return n, true, nil
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
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0, false, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, false, fmt.Errorf("environment variable %s: %w", key, err)
	}
	return d, true, nil
}
