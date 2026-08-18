package envx

import "strconv"

// Key is the NAME of an environment variable — "APP_LISTEN", never its value.
// Every getter in this package takes its key as a Key.
//
// The type states the role, and its validation catches a malformed name. What
// it deliberately no longer carries is the old key/fallback transposition
// story: [String] takes no fallback, and the parsing getters' fallbacks are
// typed (bool, int, time.Duration), so no getter in this package has two
// adjacent parameters a caller could swap. That hazard is closed by the
// signatures, not by this type.
//
// What the type does buy, in two layers:
//
//   - At compile time it guards VARIABLE-passing sites: a plain string
//     variable in a key position does not compile. Untyped literals still
//     convert implicitly, so existing literal call sites are unaffected — the
//     type alone was never the guard, which is why the signatures had to carry
//     it.
//
//   - At run time, first use validates the spelling: a Key that is not an
//     environment-variable name ([A-Za-z_][A-Za-z0-9_]*, and never empty)
//     PANICS, naming the offending string. This catches a typo ("MY VAR",
//     "app.listen") and a badly built dynamic name, both of which would
//     otherwise read as a permanently unset variable and return a default
//     forever. It is the regexp.MustCompile class: a programmer error at a
//     usually-literal call site, deterministic from the first read.
type Key string

// validate panics when k is not an environment-variable name. Every getter
// calls it before reading anything, so the panic surfaces at the first use of
// the malformed key — at startup, where config code runs — rather than as a
// silently wrong value. The message names the offending string, which is a
// compile-time constant from the call site rather than operator data.
func (k Key) validate() {
	if !k.isName() {
		quoted := strconv.Quote(string(k))
		// Cap the echoed string: a dynamically built name can be long or
		// carry a value fragment. 64 bytes names the mistake without echoing
		// the whole thing.
		if len(quoted) > 64 {
			quoted = quoted[:64] + `..."`
		}
		panic("envx: " + quoted + " is not an environment variable name")
	}
}

// isName reports whether k matches the environment-variable name grammar
// this package enforces ([A-Za-z_][A-Za-z0-9_]*). Deliberately NARROWER than
// what a kernel or POSIX tolerates (POSIX asks applications to TOLERATE odd
// names; os.Setenv accepts "a.b" and "a b") — the fleet writes only this
// grammar, and the narrowness is what makes a typo detectable instead of
// silently unset. A plain byte loop rather than a regexp — validation runs on
// every getter call, and the grammar is ASCII-only by definition, so byte
// inspection is exact.
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
