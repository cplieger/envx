package envx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSecretWithSource_reports_the_channel pins the whole point of this function: a
// caller must be able to tell WHICH channel supplied the value, because Secret's
// KEY_FILE-wins precedence means an environment variable the operator also set is
// silently ignored.
func TestSecretWithSource_reports_the_channel(t *testing.T) {
	t.Run("file wins over env and is reported as such", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(p, []byte("  from-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ENVX_SWS", "from-env")
		t.Setenv("ENVX_SWS_FILE", p)

		v, src, err := SecretWithSource("ENVX_SWS")
		if err != nil {
			t.Fatalf("SecretWithSource = %v, want nil", err)
		}
		if v != "from-file" {
			t.Errorf("value = %q, want %q (trimmed file content, file wins)", v, "from-file")
		}
		if src != SourceFile {
			t.Errorf("source = %q, want %q", src, SourceFile)
		}
	})

	t.Run("env is reported when no file is configured", func(t *testing.T) {
		t.Setenv("ENVX_SWS", "from-env")
		t.Setenv("ENVX_SWS_FILE", "")

		v, src, err := SecretWithSource("ENVX_SWS")
		if err != nil {
			t.Fatalf("SecretWithSource = %v, want nil", err)
		}
		if v != "from-env" || src != SourceEnv {
			t.Errorf("SecretWithSource = (%q, %q), want (%q, %q)", v, src, "from-env", SourceEnv)
		}
	})

	t.Run("neither channel set reports SourceNone with MissingError", func(t *testing.T) {
		t.Setenv("ENVX_SWS", "")
		t.Setenv("ENVX_SWS_FILE", "")

		_, src, err := SecretWithSource("ENVX_SWS")
		var missing *MissingError
		if !errors.As(err, &missing) {
			t.Errorf("SecretWithSource = %v, want *MissingError", err)
		}
		if src != SourceNone {
			t.Errorf("source = %q, want %q", src, SourceNone)
		}
	})
}

// TestSecretWithSource_blank_file_is_a_distinct_sentinel pins the reason
// ErrBlankSecretFile exists: a caller with an explicit opt-in for running without a
// secret has to apply that policy identically to both channels, and it can only do so if
// "the file you mounted is blank" is distinguishable from "the file is unusable" WITHOUT
// matching this package's error text.
func TestSecretWithSource_blank_file_is_a_distinct_sentinel(t *testing.T) {
	p := filepath.Join(t.TempDir(), "blank")
	if err := os.WriteFile(p, []byte("  \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENVX_SWS", "")
	t.Setenv("ENVX_SWS_FILE", p)

	_, src, err := SecretWithSource("ENVX_SWS")
	if !errors.Is(err, ErrBlankSecretFile) {
		t.Fatalf("SecretWithSource(blank file) = %v, want ErrBlankSecretFile", err)
	}
	// Not a MissingError: the operator DID configure a file, and collapsing the two
	// would hide a broken secret mount behind a caller's default for absence.
	var missing *MissingError
	if errors.As(err, &missing) {
		t.Error("blank file reported as *MissingError; a configured-but-blank file is not an absent one")
	}
	if src != SourceFile {
		t.Errorf("source = %q, want %q even on the error path", src, SourceFile)
	}
}

// TestSecretWithSource_unreadable_file_keeps_the_channel pins that the source is
// meaningful on every error path. This is when it matters most: the caller needs to tell
// the operator that the environment variable they also set is NOT a fallback.
func TestSecretWithSource_unreadable_file_keeps_the_channel(t *testing.T) {
	t.Setenv("ENVX_SWS", "from-env")
	t.Setenv("ENVX_SWS_FILE", filepath.Join(t.TempDir(), "does-not-exist"))

	v, src, err := SecretWithSource("ENVX_SWS")
	if err == nil {
		t.Fatalf("SecretWithSource(missing file) = %q, nil; want an error rather than the env fallback", v)
	}
	if v != "" {
		t.Errorf("value = %q, want empty: an unusable file must never fall back to the env value", v)
	}
	if src != SourceFile {
		t.Errorf("source = %q, want %q", src, SourceFile)
	}
}

// TestSecretWithSource_never_leaks_the_value pins this package's standing rule against
// the new error path: errors carry the key and the path, never the secret.
func TestSecretWithSource_never_leaks_the_value(t *testing.T) {
	p := filepath.Join(t.TempDir(), "blank")
	if err := os.WriteFile(p, []byte("   "), 0o600); err != nil {
		t.Fatal(err)
	}
	const sentinel = "sentinel-secret-value"
	t.Setenv("ENVX_SWS", sentinel)
	t.Setenv("ENVX_SWS_FILE", p)

	_, _, err := SecretWithSource("ENVX_SWS")
	if err == nil {
		t.Fatal("want an error for a blank file")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Errorf("error %q leaked the secret value", err)
	}
	if !strings.Contains(err.Error(), "ENVX_SWS") || !strings.Contains(err.Error(), p) {
		t.Errorf("error %q must name the key and the path so the mount is diagnosable", err)
	}
}

// TestSecret_delegates_to_SecretWithSource pins that the two cannot drift: Secret is a
// thin wrapper, so every resolution rule and every error is shared by construction.
func TestSecret_delegates_to_SecretWithSource(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(p, []byte("shared\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, env, file string }{
		{"file channel", "ignored", p},
		{"env channel", "plain", ""},
		{"neither", "", ""},
		{"unreadable file", "ignored", filepath.Join(t.TempDir(), "nope")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENVX_SWS", tc.env)
			t.Setenv("ENVX_SWS_FILE", tc.file)

			wantV, _, wantErr := SecretWithSource("ENVX_SWS")
			gotV, gotErr := Secret("ENVX_SWS")

			if gotV != wantV {
				t.Errorf("Secret = %q, SecretWithSource = %q; they must agree", gotV, wantV)
			}
			if (gotErr == nil) != (wantErr == nil) {
				t.Errorf("Secret err = %v, SecretWithSource err = %v; they must agree", gotErr, wantErr)
			}
		})
	}
}
