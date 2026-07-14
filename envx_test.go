package envx

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder is a minimal slog.Handler capturing messages + attrs so tests can
// assert on the Warn diagnostics without an external dependency.
type recorder struct {
	mu   sync.Mutex
	msgs []string
	keys []string // the "key" attr of each record
}

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, rec.Message)
	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == "key" {
			r.keys = append(r.keys, a.Value.String())
		}
		return true
	})
	return nil
}

func (r *recorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *recorder) WithGroup(string) slog.Handler      { return r }

func (r *recorder) count(sub string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, m := range r.msgs {
		if strings.Contains(m, sub) {
			n++
		}
	}
	return n
}

// captureWarns swaps in a recording default logger for the test's duration.
// Tests using it must not run in parallel (global logger state); env-var tests
// already can't (t.Setenv forbids t.Parallel).
func captureWarns(t *testing.T) *recorder {
	t.Helper()
	rec := &recorder{}
	prev := slog.Default()
	slog.SetDefault(slog.New(rec))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return rec
}

func TestString(t *testing.T) {
	tests := []struct {
		name     string
		set      bool
		value    string
		fallback string
		want     string
	}{
		{name: "unset returns fallback", set: false, fallback: "def", want: "def"},
		{name: "empty returns fallback", set: true, value: "", fallback: "def", want: "def"},
		{name: "set returns value", set: true, value: "v", fallback: "def", want: "v"},
		{name: "whitespace value returned verbatim", set: true, value: "  ", fallback: "def", want: "  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv("ENVX_TEST_STRING", tt.value)
			}
			if got := String("ENVX_TEST_STRING", tt.fallback); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBool(t *testing.T) {
	tests := []struct {
		name     string
		set      bool
		value    string
		fallback bool
		want     bool
		wantWarn bool
	}{
		{name: "unset returns fallback true", fallback: true, want: true},
		{name: "unset returns fallback false", fallback: false, want: false},
		{name: "empty returns fallback", set: true, value: "", fallback: true, want: true},
		{name: "whitespace-only returns fallback", set: true, value: "   ", fallback: true, want: true},
		{name: "true", set: true, value: "true", want: true},
		{name: "TRUE case-insensitive", set: true, value: "TRUE", want: true},
		{name: "1", set: true, value: "1", want: true},
		{name: "yes", set: true, value: "yes", want: true},
		{name: "on", set: true, value: "on", want: true},
		{name: "padded true", set: true, value: " True ", want: true},
		{name: "false", set: true, value: "false", fallback: true, want: false},
		{name: "0", set: true, value: "0", fallback: true, want: false},
		{name: "no", set: true, value: "no", fallback: true, want: false},
		{name: "off", set: true, value: "off", fallback: true, want: false},
		{name: "malformed returns fallback and warns", set: true, value: "ture", fallback: true, want: true, wantWarn: true},
		{name: "numeric junk warns", set: true, value: "2", fallback: false, want: false, wantWarn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := captureWarns(t)
			if tt.set {
				t.Setenv("ENVX_TEST_BOOL", tt.value)
			}
			if got := Bool("ENVX_TEST_BOOL", tt.fallback); got != tt.want {
				t.Errorf("Bool() = %v, want %v", got, tt.want)
			}
			if warned := rec.count("malformed") > 0; warned != tt.wantWarn {
				t.Errorf("warned = %v, want %v (msgs: %v)", warned, tt.wantWarn, rec.msgs)
			}
		})
	}
}

func TestInt(t *testing.T) {
	tests := []struct {
		name     string
		set      bool
		value    string
		fallback int
		want     int
		wantWarn bool
	}{
		{name: "unset returns fallback", fallback: 42, want: 42},
		{name: "empty returns fallback", set: true, value: "", fallback: 42, want: 42},
		{name: "valid", set: true, value: "7", fallback: 42, want: 7},
		{name: "negative", set: true, value: "-3", fallback: 42, want: -3},
		{name: "padded", set: true, value: " 9 ", fallback: 42, want: 9},
		{name: "zero", set: true, value: "0", fallback: 42, want: 0},
		{name: "malformed warns", set: true, value: "seven", fallback: 42, want: 42, wantWarn: true},
		{name: "float warns", set: true, value: "1.5", fallback: 42, want: 42, wantWarn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := captureWarns(t)
			if tt.set {
				t.Setenv("ENVX_TEST_INT", tt.value)
			}
			if got := Int("ENVX_TEST_INT", tt.fallback); got != tt.want {
				t.Errorf("Int() = %d, want %d", got, tt.want)
			}
			if warned := rec.count("malformed") > 0; warned != tt.wantWarn {
				t.Errorf("warned = %v, want %v", warned, tt.wantWarn)
			}
		})
	}
}

func TestDuration(t *testing.T) {
	tests := []struct {
		name     string
		set      bool
		value    string
		fallback time.Duration
		want     time.Duration
		wantWarn bool
	}{
		{name: "unset returns fallback", fallback: time.Minute, want: time.Minute},
		{name: "empty returns fallback", set: true, value: "", fallback: time.Minute, want: time.Minute},
		{name: "seconds", set: true, value: "30s", fallback: time.Minute, want: 30 * time.Second},
		{name: "compound", set: true, value: "1h30m", fallback: time.Minute, want: 90 * time.Minute},
		{name: "padded", set: true, value: " 6h ", fallback: time.Minute, want: 6 * time.Hour},
		{name: "zero", set: true, value: "0s", fallback: time.Minute, want: 0},
		{name: "bare number warns", set: true, value: "30", fallback: time.Minute, want: time.Minute, wantWarn: true},
		{name: "junk warns", set: true, value: "soon", fallback: time.Minute, want: time.Minute, wantWarn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := captureWarns(t)
			if tt.set {
				t.Setenv("ENVX_TEST_DUR", tt.value)
			}
			if got := Duration("ENVX_TEST_DUR", tt.fallback); got != tt.want {
				t.Errorf("Duration() = %v, want %v", got, tt.want)
			}
			if warned := rec.count("malformed") > 0; warned != tt.wantWarn {
				t.Errorf("warned = %v, want %v", warned, tt.wantWarn)
			}
		})
	}
}

// TestWarnCarriesKey pins the diagnostic contract: the Warn line names the
// offending variable so an operator can find it in the deployment.
func TestWarnCarriesKey(t *testing.T) {
	rec := captureWarns(t)
	t.Setenv("ENVX_TEST_KEYATTR", "junk")
	Bool("ENVX_TEST_KEYATTR", false)
	if len(rec.keys) != 1 || rec.keys[0] != "ENVX_TEST_KEYATTR" {
		t.Errorf("warn key attrs = %v, want [ENVX_TEST_KEYATTR]", rec.keys)
	}
}
