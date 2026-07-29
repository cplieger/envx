package envx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequire(t *testing.T) {
	t.Run("set returns value", func(t *testing.T) {
		t.Setenv("ENVX_TEST_REQ", "v")
		got, err := Require("ENVX_TEST_REQ")
		if err != nil || got != "v" {
			t.Errorf("Require() = (%q, %v), want (v, nil)", got, err)
		}
	})
	t.Run("unset returns MissingError", func(t *testing.T) {
		_, err := Require("ENVX_TEST_REQ_UNSET")
		var me *MissingError
		if !errors.As(err, &me) {
			t.Fatalf("Require() error = %v, want *MissingError", err)
		}
		if me.Key != "ENVX_TEST_REQ_UNSET" {
			t.Errorf("MissingError.Key = %q", me.Key)
		}
		if !strings.Contains(me.Error(), "ENVX_TEST_REQ_UNSET") {
			t.Errorf("Error() = %q, should name the key", me.Error())
		}
	})
	t.Run("empty returns MissingError", func(t *testing.T) {
		t.Setenv("ENVX_TEST_REQ", "")
		if _, err := Require("ENVX_TEST_REQ"); err == nil {
			t.Error("Require() on empty = nil error, want *MissingError")
		}
	})
}

func TestSecret(t *testing.T) {
	t.Run("plain env value", func(t *testing.T) {
		t.Setenv("ENVX_TEST_SEC", "s3cret")
		got, err := Secret("ENVX_TEST_SEC")
		if err != nil || got != "s3cret" {
			t.Errorf("Secret() = (%q, %v), want (s3cret, nil)", got, err)
		}
	})
	t.Run("unset returns MissingError", func(t *testing.T) {
		_, err := Secret("ENVX_TEST_SEC_UNSET")
		var me *MissingError
		if !errors.As(err, &me) {
			t.Fatalf("Secret() error = %v, want *MissingError", err)
		}
	})
	t.Run("file variant wins over plain", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(p, []byte("from-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ENVX_TEST_SEC", "from-env")
		t.Setenv("ENVX_TEST_SEC_FILE", p)
		got, err := Secret("ENVX_TEST_SEC")
		if err != nil || got != "from-file" {
			t.Errorf("Secret() = (%q, %v), want the file content minus its line ending", got, err)
		}
	})
	t.Run("missing file is an error naming key and path, not the value", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "nope")
		t.Setenv("ENVX_TEST_SEC_FILE", p)
		_, err := Secret("ENVX_TEST_SEC")
		if err == nil {
			t.Fatal("Secret() = nil error for missing file")
		}
		if !strings.Contains(err.Error(), "ENVX_TEST_SEC") {
			t.Errorf("error should name the key: %v", err)
		}
	})
	t.Run("empty file is an error", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "empty")
		if err := os.WriteFile(p, []byte("  \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ENVX_TEST_SEC_FILE", p)
		if _, err := Secret("ENVX_TEST_SEC"); err == nil {
			t.Error("Secret() = nil error for whitespace-only file")
		}
	})
	t.Run("traversal path rejected", func(t *testing.T) {
		t.Setenv("ENVX_TEST_SEC_FILE", "/run/secrets/../../etc/passwd")
		if _, err := Secret("ENVX_TEST_SEC"); err == nil {
			t.Error("Secret() = nil error for traversal path")
		}
	})
	t.Run("oversized file rejected", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "big")
		if err := os.WriteFile(p, make([]byte, maxSecretFileSize+1), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ENVX_TEST_SEC_FILE", p)
		_, err := Secret("ENVX_TEST_SEC")
		if err == nil {
			t.Fatal("Secret() = nil error for oversized file")
		}
		if !strings.Contains(err.Error(), "exceeds") {
			t.Errorf("error should mention the size bound: %v", err)
		}
	})
	t.Run("secret value never in error text", func(t *testing.T) {
		// The empty-file and missing-file errors carry key + path only. This
		// guards the redaction contract for the paths that do error.
		p := filepath.Join(t.TempDir(), "empty2")
		if err := os.WriteFile(p, []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ENVX_TEST_SEC_FILE", p)
		_, err := Secret("ENVX_TEST_SEC")
		if err == nil || !strings.Contains(err.Error(), p) {
			t.Errorf("error should carry the file path for diagnosis: %v", err)
		}
	})
}

// TestSecret_failure_classes_are_nameable pins the reason the class sentinels exist: a
// caller must be able to say WHY a secret file was unusable without matching this
// package's error text and without echoing the operator-supplied path, which is the one
// thing that must not reach a log (a KEY_FILE misconfigured to hold the secret ITSELF is
// the failure mode). Each case also pins that the pre-existing message text survived the
// change byte for byte, and that the classes do not overlap.
func TestSecret_failure_classes_are_nameable(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, make([]byte, maxSecretFileSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	blank := filepath.Join(dir, "blank")
	if err := os.WriteFile(blank, []byte(" \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	unclean := "/run/secrets/../token"
	dotdot := filepath.Join(dir, "token..v2")

	allClasses := []error{
		ErrSecretFilePathRejected,
		ErrSecretFileTooLarge,
		ErrSecretFileGrew,
		ErrSecretFileUnreadable,
		ErrBlankSecretFile,
	}

	for name, tc := range map[string]struct {
		path string
		want error
		text string // must survive verbatim inside the message
	}{
		"an unclean path is a policy rejection": {
			path: unclean,
			want: ErrSecretFilePathRejected,
			text: `secret file path rejected (must be clean and contain no ".."): ` + unclean,
		},
		"a clean path carrying .. is still a policy rejection": {
			path: dotdot,
			want: ErrSecretFilePathRejected,
			text: `secret file path rejected (must be clean and contain no ".."): ` + dotdot,
		},
		"an oversized file is its own class": {
			path: big,
			want: ErrSecretFileTooLarge,
			text: fmt.Sprintf("secret file is %d bytes, exceeds %d byte limit", maxSecretFileSize+1, maxSecretFileSize),
		},
		"blank content keeps its own sentinel": {
			path: blank,
			want: ErrBlankSecretFile,
		},
		"an absent file is an OS failure": {
			path: filepath.Join(dir, "nope"),
			want: ErrSecretFileUnreadable,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("ENVX_TEST_SEC", "from-env")
			t.Setenv("ENVX_TEST_SEC_FILE", tc.path)

			v, err := Secret("ENVX_TEST_SEC")
			if err == nil {
				t.Fatalf("Secret() = %q, nil; want an error", v)
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("Secret() = %v, want errors.Is %v", err, tc.want)
			}
			for _, other := range allClasses {
				if other != tc.want && errors.Is(err, other) {
					t.Errorf("Secret() = %v also matches %v; the classes must not overlap", err, other)
				}
			}
			if tc.text != "" && !strings.Contains(err.Error(), tc.text) {
				t.Errorf("Secret() = %q, want it to still contain %q verbatim", err, tc.text)
			}
		})
	}
}

// TestSecret_OS_failure_keeps_the_PathError pins the half of the OS-failure class the
// sentinel cannot carry: consumers key on *os.PathError to report the syscall and its
// reason in their own words, so classifying the failure must not hide the cause. The
// message is asserted for exact equality because the class was added without rewriting
// any existing text.
func TestSecret_OS_failure_keeps_the_PathError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope")
	t.Setenv("ENVX_TEST_SEC", "from-env")
	t.Setenv("ENVX_TEST_SEC_FILE", p)

	_, err := Secret("ENVX_TEST_SEC")
	if err == nil {
		t.Fatal("Secret() = nil error for an absent file")
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("Secret() = %v, want a reachable *os.PathError", err)
	}
	if !errors.Is(pathErr.Err, fs.ErrNotExist) {
		t.Errorf("PathError.Err = %v, want fs.ErrNotExist", pathErr.Err)
	}
	if pathErr.Path != p {
		t.Errorf("PathError.Path = %q, want %q", pathErr.Path, p)
	}
	if want := "read secret file for ENVX_TEST_SEC: " + pathErr.Error(); err.Error() != want {
		t.Errorf("Secret() = %q, want %q: classifying the failure must not alter the message", err, want)
	}
}

// TestSecret_stream_longer_than_its_stat_size is the grew-during-read class. A character
// device is the reachable shape of it: Stat reports 0 bytes so the size gate passes, and
// the bounded read then returns more than the limit — exactly the silent-truncation case
// the post-read length check exists to refuse, and the reason the size ceiling mentions
// device files at all.
func TestSecret_stream_longer_than_its_stat_size(t *testing.T) {
	const dev = "/dev/zero"
	if _, err := os.Stat(dev); err != nil {
		t.Skipf("no %s on this platform: %v", dev, err)
	}
	t.Setenv("ENVX_TEST_SEC", "from-env")
	t.Setenv("ENVX_TEST_SEC_FILE", dev)

	v, err := Secret("ENVX_TEST_SEC")
	if err == nil {
		t.Fatalf("Secret() = %q, nil; want the size-limit refusal rather than truncated content", v)
	}
	if !errors.Is(err, ErrSecretFileGrew) {
		t.Errorf("Secret() = %v, want errors.Is ErrSecretFileGrew", err)
	}
	if v != "" {
		t.Errorf("value = %q, want empty", v)
	}
	if want := fmt.Sprintf("secret file grew past the %d byte limit during read", maxSecretFileSize); !strings.Contains(err.Error(), want) {
		t.Errorf("Secret() = %q, want it to still contain %q verbatim", err, want)
	}
}
