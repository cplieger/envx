package envx

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MissingError reports a required environment variable that is unset or
// empty. It carries the key so a caller can aggregate several missing
// variables into one startup failure.
type MissingError struct {
	// Key is the environment variable name that was required.
	Key string
}

// Error implements the error interface.
func (e *MissingError) Error() string {
	return "required environment variable is missing: " + e.Key
}

// Require returns the value of the environment variable key, or a
// *MissingError when it is unset or empty. It returns an error rather than
// exiting so the caller controls startup failure (collect every missing key,
// log through the configured handler, then exit once).
func Require(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", &MissingError{Key: key}
	}
	return v, nil
}

// maxSecretFileSize bounds a KEY_FILE secret read. Real secrets are tens of
// bytes; the 1 MB ceiling only guards against pointing the variable at a
// device file or a runaway log.
const maxSecretFileSize = 1 << 20

// Secret returns a required secret from the environment, supporting the
// Docker secrets convention: when KEY_FILE is set, the secret is read from
// that file (size-bounded, whitespace-trimmed); otherwise the value of KEY
// itself is returned. An unset or empty result is a *MissingError.
//
// The KEY_FILE indirection keeps the secret value out of `docker inspect`
// output and compose files; the file path must be clean (no ".." traversal),
// and the read uses a single handle so the size check and the read cannot
// race. The secret value itself never appears in an error or a log line;
// errors carry the key name and file path only.
func Secret(key string) (string, error) {
	if path := os.Getenv(key + "_FILE"); path != "" {
		data, err := readSecretFile(path)
		if err != nil {
			return "", fmt.Errorf("read secret file for %s: %w", key, err)
		}
		v := strings.TrimSpace(string(data))
		if v == "" {
			return "", fmt.Errorf("secret file for %s is empty: %s", key, path)
		}
		return v, nil
	}
	return Require(key)
}

// readSecretFile reads a secret file through one handle (no stat-then-open
// TOCTOU window) and rejects a path containing traversal or a file over the
// size bound.
//
// The ".." rejection is deliberately substring-broad: it also refuses a
// legitimate filename that merely contains two consecutive dots (e.g.
// /run/secrets/key..v2), beyond what the Clean-equality check guarantees.
// Secret file paths are operator-written and fail loud with the path named,
// so the stricter-than-necessary check is kept in preference to reasoning
// about which ".."-bearing shapes are safe.
func readSecretFile(path string) ([]byte, error) {
	cleaned := filepath.Clean(path)
	if cleaned != path || strings.Contains(path, "..") {
		return nil, fmt.Errorf("secret file path rejected (must be clean and contain no \"..\"): %s", path)
	}
	f, err := os.Open(cleaned)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxSecretFileSize {
		return nil, fmt.Errorf("secret file is %d bytes, exceeds %d byte limit", info.Size(), maxSecretFileSize)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSecretFileSize+1))
	if err != nil {
		return nil, err
	}
	// Re-check after reading: a file that grows between Stat and read would
	// otherwise pass the size gate and return silently truncated content.
	if len(data) > maxSecretFileSize {
		return nil, fmt.Errorf("secret file grew past the %d byte limit during read", maxSecretFileSize)
	}
	return data, nil
}
