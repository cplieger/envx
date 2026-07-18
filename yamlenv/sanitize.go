package yamlenv

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Value-independent markers of a yaml.v3 wrong-type TypeError entry
// ("line N: cannot unmarshal !!str `...` into bool"): everything between the
// source tag and the final destination-type marker is the scalar excerpt and
// is dropped.
const (
	unmarshalMarker = "cannot unmarshal !!"
	intoMarker      = " into "
)

// Value-independent markers of the duplicate-key TypeError entry
// ("line N: mapping key "x" already defined at line M"): the key excerpt
// between them is dropped, the two line numbers are kept.
const (
	dupKeyMarker    = ": mapping key "
	dupKeyDefinedAt = " already defined at line "
)

// Value-independent markers of the strict-decode unknown-key entry
// ("line N: field X not found in type main.fileConfig", produced by a
// KnownFields(true) decode): the Go type name after the second marker is
// always dropped; whether the key name between them is kept is the caller's
// policy (WithUnknownKeyEcho), redacted by default.
const (
	unknownKeyMarker = ": field "
	unknownKeyInType = " not found in type "
)

// parseLineRe matches the value-independent "yaml: line N: " prefix a yaml.v3
// syntax error carries; only the digits are kept, never the message.
var parseLineRe = regexp.MustCompile(`^yaml: line (\d+): `)

// withheldMessage is the fixed fallback for any error shape the sanitizer
// cannot rebuild from value-independent parts. It embeds nothing from the
// original error, so it can never carry a fragment of an expanded secret.
const withheldMessage = "configuration could not be decoded (details withheld: they may embed an expanded secret)"

// wrongTypeMessage is the fixed per-entry fallback for a TypeError entry that
// matches none of the known value-independent structures.
const wrongTypeMessage = "configuration contains a value of the wrong type"

// SanitizeOption configures SanitizeDecodeError. The zero configuration is
// the safe default: everything value-derived is redacted.
type SanitizeOption func(*sanitizeOptions)

// sanitizeOptions carries the sanitizer's policy switches.
type sanitizeOptions struct {
	echoUnknownKey bool
}

// WithUnknownKeyEcho keeps the field name of a strict-decode unknown-key entry
// ("line N: field X not found in type T", from a KnownFields(true) decode) in
// the sanitized error: "line N: unknown configuration key "X"". The key name
// is what the operator needs to fix a misspelled or misplaced key, and when
// the strict decode runs on the PRE-expansion document a key cannot carry an
// expanded secret — but that safety argument is the caller's to make, so the
// default stays redact-everything and the echo is an explicit opt-in.
func WithUnknownKeyEcho() SanitizeOption {
	return func(o *sanitizeOptions) { o.echoUnknownKey = true }
}

// SanitizeDecodeError rewrites a yaml.v3 parse or decode error so that no
// fragment of a document value survives into the error message, keeping only
// value-independent structure: line numbers, YAML source tags, destination
// type names, and the fixed message vocabulary. A nil err returns nil.
//
// It exists because Expand creates the risk it closes: expansion substitutes
// ${VAR} secrets into the document's string scalars, and a subsequent decode
// error embeds a backtick-quoted excerpt of the offending scalar — which may
// now be an expanded secret headed for a startup log. Backtick-pair matching
// cannot redact that excerpt safely (yaml.v3 truncates it with any embedded
// backtick unchanged, so a secret containing a backtick defeats a delimiter
// regex and leaks a prefix); instead each *yaml.TypeError entry is rebuilt
// from its value-independent markers, and anything unrecognized falls back to
// a fixed message rather than risking a partial leak — keeping only the
// "yaml: line N:" locator a syntax error carries (structural parser output
// whose digits cannot embed a value).
//
// Three TypeError entry shapes are rebuilt: a wrong-type entry keeps its
// "line N: cannot unmarshal !!<tag>" prefix and " into <type>" suffix around
// a "<redacted>" placeholder; a duplicate-mapping-key entry keeps both line
// numbers and redacts the key (a misindented paste can put a secret in key
// position); a strict-decode unknown-key entry redacts the key name by
// default — pass WithUnknownKeyEcho to keep it. The error is detected with
// errors.As, so a wrapped TypeError is recognized.
//
// The returned error is newly constructed and deliberately does NOT wrap err:
// no Unwrap chain, errors.As probe, or verbose formatting can reach the
// withheld text through it.
func SanitizeDecodeError(err error, opts ...SanitizeOption) error {
	if err == nil {
		return nil
	}
	var o sanitizeOptions
	for _, opt := range opts {
		opt(&o)
	}
	typeErr, ok := errors.AsType[*yaml.TypeError](err)
	if !ok {
		if m := parseLineRe.FindStringSubmatch(err.Error()); m != nil {
			return errors.New("line " + m[1] + ": " + withheldMessage)
		}
		return errors.New(withheldMessage)
	}
	entries := make([]string, 0, len(typeErr.Errors))
	for _, e := range typeErr.Errors {
		entries = append(entries, sanitizeEntry(e, o))
	}
	return errors.New("unmarshal errors: " + strings.Join(entries, "; "))
}

// lineEntryBounds locates one structured TypeError entry shape: startMarker
// must appear after a bare "line N" prefix (the isLinePrefix guard — a
// wrong-type scalar excerpt embedding the same marker pair starts with the
// unmarshal shape instead, so it never matches) and endMarker must follow it.
// It is the single home of the boundary validation the duplicate-key and
// unknown-key branches of sanitizeEntry share.
func lineEntryBounds(entry, startMarker, endMarker string) (start, end int, ok bool) {
	start = strings.Index(entry, startMarker)
	if start < 0 || !isLinePrefix(entry[:start]) {
		return 0, 0, false
	}
	end = strings.LastIndex(entry, endMarker)
	return start, end, end > start
}

// sanitizeEntry rebuilds one TypeError entry keeping only its
// value-independent parts. A duplicate-mapping-key entry ("line N: mapping
// key "x" already defined at line M") keeps both line numbers and redacts the
// key excerpt. An unknown-key entry from a strict KnownFields(true) decode
// ("line N: field X not found in type T") drops the Go type name and keeps
// the key name only under WithUnknownKeyEcho. A wrong-type entry keeps the
// "line N: cannot unmarshal !!<tag>" prefix and the " into <type>" suffix;
// strings.LastIndex locates the suffix so backticks or newlines inside the
// scalar excerpt are irrelevant. All three shapes validate their kept prefix
// through isLinePrefix: on the first two it ensures a wrong-type scalar
// excerpt that happens to embed their marker pairs is never mistaken for
// them (such an entry starts with the unmarshal shape, not a bare "line N",
// so it falls through to the redacting wrong-type branch); on the wrong-type
// branch it ensures a hand-built TypeError entry cannot smuggle crafted text
// before "cannot unmarshal !!" into the sanitized output (a genuine yaml.v3
// entry always starts "line N: "). Anything matching no shape falls back to
// a fixed message.
func sanitizeEntry(entry string, o sanitizeOptions) string {
	if k, at, ok := lineEntryBounds(entry, dupKeyMarker, dupKeyDefinedAt); ok {
		return entry[:k] + ": mapping key <redacted>" + entry[at:]
	}
	if k, at, ok := lineEntryBounds(entry, unknownKeyMarker, unknownKeyInType); ok {
		if o.echoUnknownKey {
			return fmt.Sprintf("%s: unknown configuration key %q",
				entry[:k], entry[k+len(unknownKeyMarker):at])
		}
		return entry[:k] + ": unknown configuration key <redacted>"
	}
	start := strings.Index(entry, unmarshalMarker)
	end := strings.LastIndex(entry, intoMarker)
	if start < 0 || end < start {
		return wrongTypeMessage
	}
	if prefix, cut := strings.CutSuffix(entry[:start], ": "); !cut || !isLinePrefix(prefix) {
		return wrongTypeMessage
	}
	tagEnd := start + len(unmarshalMarker)
	for tagEnd < len(entry) && entry[tagEnd] != ' ' {
		tagEnd++
	}
	return entry[:tagEnd] + " <redacted>" + entry[end:]
}

// isLinePrefix reports whether s is exactly "line <digits>", the prefix a
// genuine yaml.v3 TypeError entry carries before its first marker. It guards
// every rebuild that keeps text from the entry: the duplicate-key branch
// (keeps entry[:k] and entry[at:]) and the unknown-key branch (may keep the
// key name between its markers) against a wrong-type scalar excerpt
// embedding the same marker pair — that entry's prefix is the unmarshal shape
// ("line N: cannot unmarshal !!str `..."), never a bare "line N" — and the
// wrong-type branch (keeps entry[:tagEnd], minus the ": " separator its
// marker excludes) against a hand-built TypeError entry carrying crafted
// prefix text.
func isLinePrefix(s string) bool {
	digits, ok := strings.CutPrefix(s, "line ")
	if !ok || digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
