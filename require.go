package envx

import (
	"errors"
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

// The failure classes of a KEY_FILE secret read. Every error Secret and
// SecretWithSource return for a configured secret file wraps exactly one of
// these or ErrBlankSecretFile, so a caller can name WHY the file was unusable
// with errors.Is and apply a different policy per class.
//
// The classes exist because the alternative was worse: without them the only
// way to tell a rejected path from an oversized file was matching this
// package's error TEXT, so callers folded every non-blank failure into one
// generic message instead. That mattered here more than in most packages,
// because these errors embed the operator-supplied PATH and a caller whose
// KEY_FILE was misconfigured to hold the secret ITSELF (rather than a path to
// it) must be able to describe the failure without echoing the value into a
// log. Naming the class no longer requires touching the path.
//
// The classes are deliberately narrow rather than one "unusable file" sentinel:
// their remediations differ (fix the variable, shrink the file, stop rewriting
// it, fix the mount) and only a caller writing an operator-facing message knows
// which of those it wants to say.
var (
	// ErrSecretFilePathRejected means KEY_FILE named a path this package
	// refuses to open: not already clean, or containing "..".
	ErrSecretFilePathRejected = errors.New("envx: secret file path rejected")
	// ErrSecretFileTooLarge means the file was already over maxSecretFileSize
	// when it was opened.
	ErrSecretFileTooLarge = errors.New("envx: secret file exceeds the size limit")
	// ErrSecretFileGrew means the file passed the size gate and then grew past
	// maxSecretFileSize while it was being read, so the content would have been
	// silently truncated.
	ErrSecretFileGrew = errors.New("envx: secret file grew past the size limit during read")
	// ErrSecretFileUnreadable means the operating system refused the open, stat
	// or read. The underlying *os.PathError stays reachable with errors.As, so
	// a caller can still report the syscall and its reason (without the path,
	// which the PathError also carries).
	ErrSecretFileUnreadable = errors.New("envx: secret file could not be read")
)

// classifiedError attaches a failure class to an error message without altering
// the message.
//
// Every class above is reached with errors.Is, and where wrapping the sentinel
// with fmt.Errorf("%w …") reproduces the existing message byte for byte that is
// what readSecretFile does. The remaining messages predate the sentinels and
// interpolate values in the middle of the sentence ("secret file is 5 bytes,
// exceeds …"), so splicing a sentinel into them would rewrite text that
// consumers and this package's own tests read. This carries the class beside
// the message instead of inside it.
//
// Unwrap returns a slice so the OS-failure class can hold both its sentinel and
// the *os.PathError underneath: errors.Is finds the class, errors.As still
// finds the PathError.
type classifiedError struct {
	class error
	cause error  // nil unless there is an underlying error to preserve
	msg   string // the message exactly as it read before the class existed
}

// Error implements the error interface, returning the message unchanged.
func (e *classifiedError) Error() string { return e.msg }

// Unwrap reports the failure class and, when there is one, the underlying cause.
func (e *classifiedError) Unwrap() []error {
	if e.cause == nil {
		return []error{e.class}
	}
	return []error{e.class, e.cause}
}

// unreadableSecretFile classifies an OS-level open, stat or read failure. The
// message is the operating system's own, unchanged: it is already a
// *os.PathError naming the syscall and the reason.
func unreadableSecretFile(cause error) error {
	return &classifiedError{msg: cause.Error(), class: ErrSecretFileUnreadable, cause: cause}
}

// Secret returns a required secret from the environment, supporting the
// Docker secrets convention: when KEY_FILE is set to a non-empty value, the
// secret is read from that file (size-bounded, with at most one trailing line
// ending removed); otherwise the value of KEY itself is returned. An unset or
// empty result is a *MissingError, and a configured file whose content is
// blank is ErrBlankSecretFile.
//
// The file channel returns the secret as written apart from that single
// trailing newline, so a value whose leading or trailing SPACES are part of the
// credential survives the round trip: the two channels must not disagree about
// what the secret IS, and KEY is never rewritten either. Only the line ending
// an editor or `kubectl create secret` appends is removed, because that one is
// an artifact of storing a value in a file rather than part of the value.
//
// The KEY_FILE indirection keeps the secret value out of `docker inspect`
// output and compose files; the file path must be clean (no ".." traversal),
// and the read uses a single handle so the size check and the read cannot
// race. The secret value itself never appears in an error or a log line;
// errors carry the key name and file path only. Each failure class is named by
// a sentinel (ErrSecretFilePathRejected, ErrSecretFileTooLarge,
// ErrSecretFileGrew, ErrSecretFileUnreadable, ErrBlankSecretFile) so a caller
// can report WHY the file was unusable without echoing the path.
//
// Use SecretWithSource when the caller needs to know WHICH channel supplied the
// value — for example to warn that a KEY it also set was ignored in favour of
// KEY_FILE. Secret delegates to it, so the two cannot drift. Use
// IsBlankSecretFilePath when a KEY_FILE that is present but blank must be
// refused instead of read as absent.
func Secret(key string) (string, error) {
	v, _, err := SecretWithSource(key)
	return v, err
}

// readSecretFile reads a secret file through one handle (no stat-then-open
// TOCTOU window) and rejects a path containing traversal or a file over the
// size bound.
//
// Every failure it returns is classified: ErrSecretFilePathRejected,
// ErrSecretFileTooLarge, ErrSecretFileGrew or ErrSecretFileUnreadable, so a
// caller can name the class without matching these messages or handling the
// path. The messages themselves are unchanged.
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
		return nil, fmt.Errorf("%w (must be clean and contain no \"..\"): %s", ErrSecretFilePathRejected, path)
	}
	f, err := os.Open(cleaned)
	if err != nil {
		return nil, unreadableSecretFile(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, unreadableSecretFile(err)
	}
	if info.Size() > maxSecretFileSize {
		return nil, &classifiedError{
			msg:   fmt.Sprintf("secret file is %d bytes, exceeds %d byte limit", info.Size(), maxSecretFileSize),
			class: ErrSecretFileTooLarge,
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSecretFileSize+1))
	if err != nil {
		return nil, unreadableSecretFile(err)
	}
	// Re-check after reading: a file that grows between Stat and read would
	// otherwise pass the size gate and return silently truncated content.
	if len(data) > maxSecretFileSize {
		return nil, &classifiedError{
			msg:   fmt.Sprintf("secret file grew past the %d byte limit during read", maxSecretFileSize),
			class: ErrSecretFileGrew,
		}
	}
	return data, nil
}
