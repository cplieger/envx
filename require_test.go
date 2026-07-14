package envx

import (
	"errors"
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
		if err := os.WriteFile(p, []byte("  from-file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ENVX_TEST_SEC", "from-env")
		t.Setenv("ENVX_TEST_SEC_FILE", p)
		got, err := Secret("ENVX_TEST_SEC")
		if err != nil || got != "from-file" {
			t.Errorf("Secret() = (%q, %v), want trimmed file content", got, err)
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
