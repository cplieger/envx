package envx

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIntStrict(t *testing.T) {
	tests := []struct {
		name    string
		set     bool
		value   string
		want    int
		wantOK  bool
		wantErr bool
	}{
		{name: "unset", want: 0, wantOK: false},
		{name: "empty", set: true, value: "", want: 0, wantOK: false},
		{name: "whitespace-only", set: true, value: "   ", want: 0, wantOK: false},
		{name: "valid", set: true, value: "7", want: 7, wantOK: true},
		{name: "padded valid", set: true, value: " 9 ", want: 9, wantOK: true},
		{name: "negative", set: true, value: "-3", want: -3, wantOK: true},
		{name: "zero", set: true, value: "0", want: 0, wantOK: true},
		{name: "malformed", set: true, value: "seven", wantErr: true},
		{name: "float rejected", set: true, value: "1.5", wantErr: true},
		{name: "overflow rejected", set: true, value: "9999999999999999999999", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := captureWarns(t)
			if tt.set {
				t.Setenv("ENVX_TEST_INTSTRICT", tt.value)
			}
			got, ok, err := IntStrict("ENVX_TEST_INTSTRICT")
			if got != tt.want || ok != tt.wantOK || (err != nil) != tt.wantErr {
				t.Errorf("IntStrict() = (%d, %v, %v), want (%d, %v, err=%v)",
					got, ok, err, tt.want, tt.wantOK, tt.wantErr)
			}
			if ok && err != nil {
				t.Errorf("ok and err are not mutually exclusive: (%v, %v)", ok, err)
			}
			if len(rec.msgs) != 0 {
				t.Errorf("strict getter logged: %v", rec.msgs)
			}
		})
	}
}

func TestDurationStrict(t *testing.T) {
	tests := []struct {
		name    string
		set     bool
		value   string
		want    time.Duration
		wantOK  bool
		wantErr bool
	}{
		{name: "unset", want: 0, wantOK: false},
		{name: "empty", set: true, value: "", want: 0, wantOK: false},
		{name: "whitespace-only", set: true, value: "   ", want: 0, wantOK: false},
		{name: "seconds", set: true, value: "30s", want: 30 * time.Second, wantOK: true},
		{name: "compound", set: true, value: "1h30m", want: 90 * time.Minute, wantOK: true},
		{name: "padded", set: true, value: " 6h ", want: 6 * time.Hour, wantOK: true},
		{name: "zero", set: true, value: "0s", want: 0, wantOK: true},
		{name: "negative", set: true, value: "-1h", want: -time.Hour, wantOK: true},
		{name: "bare number rejected", set: true, value: "30", wantErr: true},
		{name: "junk rejected", set: true, value: "soon", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := captureWarns(t)
			if tt.set {
				t.Setenv("ENVX_TEST_DURSTRICT", tt.value)
			}
			got, ok, err := DurationStrict("ENVX_TEST_DURSTRICT")
			if got != tt.want || ok != tt.wantOK || (err != nil) != tt.wantErr {
				t.Errorf("DurationStrict() = (%v, %v, %v), want (%v, %v, err=%v)",
					got, ok, err, tt.want, tt.wantOK, tt.wantErr)
			}
			if ok && err != nil {
				t.Errorf("ok and err are not mutually exclusive: (%v, %v)", ok, err)
			}
			if len(rec.msgs) != 0 {
				t.Errorf("strict getter logged: %v", rec.msgs)
			}
		})
	}
}

// TestStrictErrorContract pins what a caller can rely on in the error: it
// names the offending variable (operators grep logs for the key) and wraps
// the underlying parse error for errors.As.
func TestStrictErrorContract(t *testing.T) {
	t.Setenv("ENVX_TEST_STRICT_ERR", "junk")

	_, _, err := IntStrict("ENVX_TEST_STRICT_ERR")
	if err == nil || !strings.Contains(err.Error(), "ENVX_TEST_STRICT_ERR") {
		t.Errorf("IntStrict error does not name the key: %v", err)
	}
	var numErr *strconv.NumError
	if !errors.As(err, &numErr) {
		t.Errorf("IntStrict error does not wrap *strconv.NumError: %v", err)
	}

	_, _, err = DurationStrict("ENVX_TEST_STRICT_ERR")
	if err == nil || !strings.Contains(err.Error(), "ENVX_TEST_STRICT_ERR") {
		t.Errorf("DurationStrict error does not name the key: %v", err)
	}
}
