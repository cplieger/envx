package envx

import (
	"strconv"
	"testing"
	"time"
)

// wantKeyPanic is the exact message validate emits for a malformed key; the
// tests build it the same way so a wording change is a deliberate edit here,
// not an accident there.
func wantKeyPanic(k Key) string {
	return "envx: " + strconv.Quote(string(k)) + " is not an environment variable name; did you swap key and fallback?"
}

// mustPanicWith asserts fn panics with exactly the wanted message. The exact
// match matters: the message IS the diagnostic contract — it must name the
// offending string and the likely transposition.
func mustPanicWith(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Error("no panic; a malformed key must panic on first use")
			return
		}
		got, ok := r.(string)
		if !ok {
			t.Errorf("panic value = %v (%T), want a string message", r, r)
			return
		}
		if got != want {
			t.Errorf("panic message = %q, want %q", got, want)
		}
	}()
	fn()
}

// TestKeySwapPanicMessage pins the guard against the exact defect the Key
// type exists for: the classic key/fallback transposition at a LITERAL call
// site, which the type system cannot catch (both arguments are untyped
// constants and convert implicitly). First use panics deterministically at
// boot, naming the swapped value and the cause.
func TestKeySwapPanicMessage(t *testing.T) {
	const want = `envx: "127.0.0.1:7681" is not an environment variable name; did you swap key and fallback?`
	mustPanicWith(t, want, func() { String("127.0.0.1:7681", "KWEB_LISTEN") })
}

// TestMalformedKeyPanicsInEveryGetter sweeps the panic across the whole
// getter surface — the fallback-taking family, the strict family, the
// require/secret family, and every Source method — for the two swap shapes
// every getter can receive (a non-name value and the empty string). No getter
// may read anything, log anything, or fall back on a malformed key.
func TestMalformedKeyPanicsInEveryGetter(t *testing.T) {
	getters := map[string]func(Key){
		"String":                func(k Key) { String(k, "fallback") },
		"Bool":                  func(k Key) { Bool(k, false) },
		"Int":                   func(k Key) { Int(k, 0) },
		"Duration":              func(k Key) { Duration(k, time.Second) },
		"BoolStrict":            func(k Key) { _, _, _ = BoolStrict(k) },
		"IntStrict":             func(k Key) { _, _, _ = IntStrict(k) },
		"DurationStrict":        func(k Key) { _, _, _ = DurationStrict(k) },
		"Require":               func(k Key) { _, _ = Require(k) },
		"Secret":                func(k Key) { _, _ = Secret(k) },
		"SecretWithSource":      func(k Key) { _, _, _ = SecretWithSource(k) },
		"IsBlankSecretFilePath": func(k Key) { _ = IsBlankSecretFilePath(k) },
		"Source.String":         func(k Key) { Source{}.String(k, "fallback") },
		"Source.Bool":           func(k Key) { Source{}.Bool(k, false) },
		"Source.Int":            func(k Key) { Source{}.Int(k, 0) },
		"Source.Duration":       func(k Key) { Source{}.Duration(k, time.Second) },
		"Source.BoolStrict":     func(k Key) { _, _, _ = Source{}.BoolStrict(k) },
		"Source.IntStrict":      func(k Key) { _, _, _ = Source{}.IntStrict(k) },
		"Source.DurationStrict": func(k Key) { _, _, _ = Source{}.DurationStrict(k) },
	}
	for name, get := range getters {
		for _, k := range []Key{"127.0.0.1:7681", ""} {
			t.Run(name+"/"+strconv.Quote(string(k)), func(t *testing.T) {
				rec := captureWarns(t)
				mustPanicWith(t, wantKeyPanic(k), func() { get(k) })
				if len(rec.msgs) != 0 {
					t.Errorf("getter logged before panicking: %v", rec.msgs)
				}
			})
		}
	}
}

// TestKeyNameGrammar pins the boundary of the POSIX name rule
// ([A-Za-z_][A-Za-z0-9_]*) on the shapes a swapped fallback actually takes —
// addresses, paths, durations, flags — plus the grammar's own edges. String
// carries the sweep: it is the widest-used getter and the only one with no
// parse and no Warn, so a non-panic outcome asserts exactly one thing.
func TestKeyNameGrammar(t *testing.T) {
	invalid := []Key{
		" ",
		"  APP_KEY", // padded: a name is never trimmed into validity
		"APP_KEY ",
		":8080",
		"/run/secrets/token",
		"6h",
		"1PORT", // leading digit
		"APP-KEY",
		"APP KEY",
		"APP.KEY",
		"${APP_KEY}",
		"APP=1",
		"🚀",
		"APP\nKEY",
		"pas\x00nul",
	}
	for _, k := range invalid {
		t.Run("invalid/"+strconv.Quote(string(k)), func(t *testing.T) {
			mustPanicWith(t, wantKeyPanic(k), func() { String(k, "fallback") })
		})
	}

	valid := []Key{"A", "a", "_", "_A", "_9", "lower_case", "APP_PORT_2", "ENVX_TEST_GRAMMAR_OK"}
	for _, k := range valid {
		t.Run("valid/"+strconv.Quote(string(k)), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("String(%q, ...) panicked on a valid name: %v", k, r)
				}
			}()
			_ = String(k, "fallback")
		})
	}
}

// TestKeyResidualSwapIsNotCatchable documents the accepted gap in the guard,
// so a future "tighten the validation" reads what the boundary already is: a
// transposition whose fallback happens to BE a valid variable name passes
// both layers — it compiles (untyped constants convert) and it validates
// ("default" is a well-formed name) — and silently reads the wrong variable.
// No signature or run-time rule can catch that shape; the Key doc says so.
func TestKeyResidualSwapIsNotCatchable(t *testing.T) {
	t.Setenv("default", "") // pin the swapped read to the empty-equals-unset path
	if got := String("default", "MY_VAR"); got != "MY_VAR" {
		t.Errorf(`String("default", "MY_VAR") = %q, want the documented residual: the intended KEY comes back as the value`, got)
	}
}
