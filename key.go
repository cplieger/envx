package envx

import "strconv"

// Key is the NAME of an environment variable — "APP_LISTEN", never its value
// and never a fallback. Every getter in this package takes its key as a Key.
//
// The type exists for the classic transposition, String("default", "MY_VAR"),
// which under (key, fallback string) silently returned "MY_VAR" forever. It
// catches that swap in two layers, and honesty about the first one matters:
//
//   - At compile time it guards VARIABLE-passing sites only. A string variable
//     in the key position no longer compiles, but untyped literals convert
//     implicitly — String("default", "MY_VAR") still compiles, because both
//     arguments are untyped constants. The type alone would catch nothing at a
//     literal call site, which is nearly all of them.
//
//   - At run time, first use validates the spelling: a Key that is not an
//     environment-variable name ([A-Za-z_][A-Za-z0-9_]*, and never empty)
//     PANICS, naming the offending string and the likely transposition. A
//     malformed key at a literal call site is a boot-time programmer error —
//     the regexp.MustCompile class — and the classic swapped fallback is a
//     URL, a path, a port, a duration, or the empty string, none of which is
//     a variable name. The swap that silently misconfigured an app becomes a
//     deterministic panic on the first read.
//
// The residual gap: a swap whose fallback happens to BE a valid variable name
// (String("default", "MY_VAR")) passes both layers and reads the variable
// "default". No signature can catch that shape; name your fallbacks by their
// values, not by names.
type Key string

// validate panics when k is not an environment-variable name. Every getter
// calls it before reading anything, so the panic surfaces at the first use of
// the malformed key — at startup, where config code runs — rather than as a
// silently wrong value. The message names the offending string (a swapped
// fallback is a compile-time constant from the call site, not a secret) and
// the transposition that almost always produced it.
func (k Key) validate() {
	if !k.isName() {
		quoted := strconv.Quote(string(k))
		// Cap the echoed string: the usual trigger is a transposed FALLBACK in
		// key position, and a fallback can be long or sensitive (a URL with
		// userinfo, a token default). 64 bytes names the mistake without
		// echoing the whole value.
		if len(quoted) > 64 {
			quoted = quoted[:64] + `..."`
		}
		panic("envx: " + quoted + " is not an environment variable name; did you swap key and fallback?")
	}
}

// isName reports whether k matches the environment-variable name grammar
// this package enforces ([A-Za-z_][A-Za-z0-9_]*). Deliberately NARROWER than
// what a kernel or POSIX tolerates (POSIX asks applications to TOLERATE odd
// names; os.Setenv accepts "a.b" and "a b") — the fleet writes only this
// grammar, and the narrowness is what makes a transposed fallback detectable:
// [A-Za-z_][A-Za-z0-9_]*. A plain byte loop rather than a regexp — validation
// runs on every getter call, and the grammar is ASCII-only by definition, so
// byte inspection is exact.
func (k Key) isName() bool {
	if k == "" {
		return false
	}
	for i := range len(k) {
		switch c := k[i]; {
		case c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
