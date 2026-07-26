package envx

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrBlankSecretFile is returned by Secret and SecretWithSource when KEY_FILE names a
// readable file whose trimmed content is empty.
//
// It is a distinct sentinel because a caller often has a policy for a blank secret that
// differs from its policy for an unusable one. cert-converter is the motivating case: it
// offers PFX_ALLOW_EMPTY_PASSWORD as an explicit opt-in to running without a password,
// and that opt-in has to mean the same thing whether the blank value arrived through the
// environment or through a mounted file. Without a sentinel the file channel could only
// be classified by matching this package's error TEXT, so the opt-in silently applied to
// one channel and not the other.
//
// It is deliberately NOT a *MissingError: the operator did configure a secret file, and
// treating "you forgot to set this" the same as "the file you mounted is blank" would
// hide a broken secret mount behind a caller's default.
var ErrBlankSecretFile = errors.New("envx: secret file is blank")

// SecretSource reports which channel a secret value came from.
type SecretSource string

// The secret delivery channels SecretWithSource distinguishes.
const (
	// SourceNone means no value was found in either channel.
	SourceNone SecretSource = "none"
	// SourceEnv means the value came from KEY itself.
	SourceEnv SecretSource = "env"
	// SourceFile means the value came from the file named by KEY_FILE.
	SourceFile SecretSource = "file"
)

// SecretWithSource is Secret, additionally reporting which channel supplied the value.
//
// Secret resolves KEY_FILE ahead of KEY and returns only the value, so a caller cannot
// tell whether an environment variable it also set was used or silently ignored. That
// matters when both are set: the operator expressed two intentions and exactly one takes
// effect. Only the caller knows whether that is worth a warning in its own configuration
// vocabulary, so this reports the fact rather than logging it here — consistent with this
// package's rule that the caller owns the policy and envx owns the parse.
//
// A caller that needs the both-channels-set condition composes it in one line:
//
//	v, src, err := envx.SecretWithSource("PFX_PASSWORD")
//	if src == envx.SourceFile && os.Getenv("PFX_PASSWORD") != "" { /* warn */ }
//
// That works on the error paths too, which is where it matters most: an unreadable file
// still reports SourceFile, so the caller can tell the operator that the environment
// variable they also set is not a fallback.
//
// Precedence is unchanged from Secret: KEY_FILE wins when set, because the entire point
// of the file channel is keeping the value out of the process environment.
func SecretWithSource(key string) (value string, source SecretSource, err error) {
	if path := os.Getenv(key + "_FILE"); path != "" {
		data, readErr := readSecretFile(path)
		if readErr != nil {
			return "", SourceFile, fmt.Errorf("read secret file for %s: %w", key, readErr)
		}
		v := strings.TrimSpace(string(data))
		if v == "" {
			// The path is named because a blank secret file cannot be diagnosed
			// without it; the (absent) VALUE is what must never be logged.
			return "", SourceFile, fmt.Errorf("%w for %s: %s", ErrBlankSecretFile, key, path)
		}
		return v, SourceFile, nil
	}
	v, reqErr := Require(key)
	if reqErr != nil {
		return "", SourceNone, reqErr
	}
	return v, SourceEnv, nil
}
