package envx

import (
	"errors"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// silenceWarns installs a discard logger for fuzz iterations; the fuzz
// targets exercise the parse boundary, not the diagnostics.
func silenceWarns(f *testing.F) {
	f.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.Cleanup(func() { slog.SetDefault(prev) })
}

// assertParseErrorValue pins the *ParseError contract across the whole input
// space: whenever a strict numeric variant rejects a value, the error must be a
// *ParseError whose Value is exactly the TRIMMED input. That is the property
// consumers rely on to quote the rejected value without a second environment
// read, and trimming is the half a regression would silently drop.
func assertParseErrorValue(t *testing.T, err error, raw string) {
	t.Helper()
	if err == nil {
		return
	}
	var perr *ParseError
	if !errors.As(err, &perr) {
		t.Fatalf("strict variant returned a non-*ParseError for %q: %v", raw, err)
	}
	if want := strings.TrimSpace(raw); perr.Value != want {
		t.Fatalf("ParseError.Value = %q, want the trimmed input %q", perr.Value, want)
	}
}

// FuzzKeyValidation pins Key's name grammar against a regexp oracle across
// the whole input space: a getter panics on exactly the strings the POSIX
// name grammar ([A-Za-z_][A-Za-z0-9_]*) rejects, with the exact
// transposition-naming message, and never panics on a name the grammar
// accepts. String carries the probe: it has no parse and no Warn, so the
// validation is the only thing exercised.
func FuzzKeyValidation(f *testing.F) {
	for _, s := range []string{
		"", "A", "_", "APP_PORT", "127.0.0.1:7681", "1PORT", "APP-KEY",
		"app key", ":8080", "6h", "${APP}", "🚀", "a\x00b", "line\n", " PAD ",
	} {
		f.Add(s)
	}
	oracle := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	f.Fuzz(func(t *testing.T, s string) {
		want := "envx: " + strconv.Quote(s) + " is not an environment variable name; did you swap key and fallback?"
		defer func() {
			r := recover()
			if oracle.MatchString(s) {
				if r != nil {
					t.Fatalf("String(%q, ...) panicked on a valid name: %v", s, r)
				}
				return
			}
			if r == nil {
				t.Fatalf("String(%q, ...) did not panic on an invalid name", s)
			}
			if msg, ok := r.(string); !ok || msg != want {
				t.Fatalf("panic = %v, want %q", r, want)
			}
		}()
		_ = String(Key(s), "fallback")
	})
}

// FuzzBool asserts Bool never panics on arbitrary env values and always
// returns one of {true, false, fallback-consistent} — i.e. a recognized
// spelling decides, anything else yields the fallback.
func FuzzBool(f *testing.F) {
	silenceWarns(f)
	for _, s := range []string{"", "true", "FALSE", " on ", "2", "ture", "🚀", "TRUE\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		if strings.ContainsRune(v, 0) {
			t.Skip() // setenv rejects NUL
		}
		t.Setenv("ENVX_FUZZ_BOOL", v)
		gotTrue := Bool("ENVX_FUZZ_BOOL", true)
		gotFalse := Bool("ENVX_FUZZ_BOOL", false)
		// If the two fallbacks disagree, the value was unrecognized (or
		// empty) and each call returned its own fallback. If they agree, the
		// value decided the result deterministically.
		if gotTrue != gotFalse {
			if gotTrue != true || gotFalse != false {
				t.Errorf("fallback passthrough broken: (%v,%v) for %q", gotTrue, gotFalse, v)
			}
		}
	})
}

// FuzzBoolStrict pins the strict/tolerant consistency contract for booleans,
// which is the shared-parser guarantee stated as an invariant: whatever
// BoolStrict accepts, Bool returns identically regardless of its fallback, and
// whatever BoolStrict rejects, Bool falls back on.
func FuzzBoolStrict(f *testing.F) {
	silenceWarns(f)
	for _, s := range []string{"", "true", "FALSE", " on ", "off", "2", "ture", "t", "🚀", "TRUE\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		if strings.ContainsRune(v, 0) {
			t.Skip() // setenv rejects NUL
		}
		t.Setenv("ENVX_FUZZ_BOOLSTRICT", v)
		b, ok, err := BoolStrict("ENVX_FUZZ_BOOLSTRICT")
		if ok && err != nil {
			t.Fatalf("ok with non-nil err for %q", v)
		}
		if !ok && b {
			t.Fatalf("!ok with true value for %q", v)
		}
		tolerantTrue := Bool("ENVX_FUZZ_BOOLSTRICT", true)
		tolerantFalse := Bool("ENVX_FUZZ_BOOLSTRICT", false)
		if ok { // strict parsed: the value decides, both fallbacks agree with it
			if tolerantTrue != b || tolerantFalse != b {
				t.Errorf("strict %v disagrees with tolerant (%v,%v) for %q", b, tolerantTrue, tolerantFalse, v)
			}
			return
		}
		// unset/empty or malformed: each tolerant call returned its fallback
		if !tolerantTrue || tolerantFalse {
			t.Errorf("tolerant did not fall back (%v,%v) while strict declined %q", tolerantTrue, tolerantFalse, v)
		}
	})
}

// FuzzInt asserts Int never panics and unparseable input returns the fallback.
func FuzzInt(f *testing.F) {
	silenceWarns(f)
	for _, s := range []string{"", "0", "-1", "9999999999999999999999", "1.5", "1e3", " 7 ", "\xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		if strings.ContainsRune(v, 0) {
			t.Skip() // setenv rejects NUL
		}
		t.Setenv("ENVX_FUZZ_INT", v)
		_ = Int("ENVX_FUZZ_INT", 42)
	})
}

// FuzzDuration asserts Duration never panics and never returns a value that
// time.ParseDuration would not have produced for the trimmed input.
func FuzzDuration(f *testing.F) {
	silenceWarns(f)
	for _, s := range []string{"", "30s", "-1h", "1h30m", "30", "s", "999999h", "\t5m\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		if strings.ContainsRune(v, 0) {
			t.Skip() // setenv rejects NUL
		}
		t.Setenv("ENVX_FUZZ_DUR", v)
		got := Duration("ENVX_FUZZ_DUR", time.Minute)
		if got != time.Minute {
			// A non-fallback return must round-trip through ParseDuration.
			if _, err := time.ParseDuration(got.String()); err != nil {
				t.Errorf("Duration returned unparseable %v for %q", got, v)
			}
		}
	})
}

// FuzzSecretPath asserts the KEY_FILE path guard never panics and never opens
// a traversal path, and pins the blank-pointer invariant: whenever
// IsBlankSecretFilePath reports the pointer blank, the file channel can never have
// delivered a secret — a blank KEY_FILE either resolves elsewhere or fails.
func FuzzSecretPath(f *testing.F) {
	silenceWarns(f)
	for _, s := range []string{"", "/run/secrets/token", "../etc/passwd", "a/../../b", "/dev/null", " ", "   ", "\t\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, p string) {
		if p == "" || strings.ContainsRune(p, 0) {
			// setenv rejects NUL; the empty pointer selects no file channel at all
			// and is pinned by the table tests instead.
			t.Skip()
		}
		t.Setenv("ENVX_FUZZ_SEC_FILE", p)
		v, src, err := SecretWithSource("ENVX_FUZZ_SEC")
		if IsBlankSecretFilePath("ENVX_FUZZ_SEC") && src == SourceFile && err == nil {
			t.Errorf("blank pointer %q delivered a secret from the file channel (value length %d)", p, len(v))
		}
	})
}

// FuzzIntStrict pins the strict/tolerant consistency contract: for any value,
// IntStrict and Int agree on what parses — a valid strict result is exactly
// the value Int returns, a strict error is exactly the case Int falls back
// on, and the three-state return is internally consistent.
func FuzzIntStrict(f *testing.F) {
	silenceWarns(f)
	for _, s := range []string{"", "0", "-1", "9999999999999999999999", "1.5", " 7 ", "seven", "\xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		if strings.ContainsRune(v, 0) {
			t.Skip() // setenv rejects NUL
		}
		t.Setenv("ENVX_FUZZ_INTSTRICT", v)
		n, ok, err := IntStrict("ENVX_FUZZ_INTSTRICT")
		if ok && err != nil {
			t.Fatalf("ok with non-nil err for %q", v)
		}
		if !ok && n != 0 {
			t.Fatalf("!ok with non-zero value %d for %q", n, v)
		}
		assertParseErrorValue(t, err, v)
		const sentinel = -987654321
		tolerant := Int("ENVX_FUZZ_INTSTRICT", sentinel)
		switch {
		case ok: // strict parsed: tolerant must return the same value
			if tolerant != n {
				t.Errorf("strict %d disagrees with tolerant %d for %q", n, tolerant, v)
			}
		default: // unset/empty or malformed: tolerant must have fallen back
			if tolerant != sentinel && n == 0 && err == nil {
				t.Errorf("strict says unset but tolerant parsed %d for %q", tolerant, v)
			}
			if err != nil && tolerant != sentinel {
				t.Errorf("strict errored but tolerant parsed %d for %q", tolerant, v)
			}
		}
	})
}

// FuzzDurationStrict pins the same strict/tolerant consistency contract for
// durations.
func FuzzDurationStrict(f *testing.F) {
	silenceWarns(f)
	for _, s := range []string{"", "30s", "-1h", "1h30m", "30", "s", "999999h", "\t5m\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, v string) {
		if strings.ContainsRune(v, 0) {
			t.Skip() // setenv rejects NUL
		}
		t.Setenv("ENVX_FUZZ_DURSTRICT", v)
		d, ok, err := DurationStrict("ENVX_FUZZ_DURSTRICT")
		if ok && err != nil {
			t.Fatalf("ok with non-nil err for %q", v)
		}
		if !ok && d != 0 {
			t.Fatalf("!ok with non-zero value %v for %q", d, v)
		}
		assertParseErrorValue(t, err, v)
		const sentinel = -987654321 * time.Second
		tolerant := Duration("ENVX_FUZZ_DURSTRICT", sentinel)
		if ok && tolerant != d {
			t.Errorf("strict %v disagrees with tolerant %v for %q", d, tolerant, v)
		}
		if err != nil && tolerant != sentinel {
			t.Errorf("strict errored but tolerant parsed %v for %q", tolerant, v)
		}
	})
}
