package yamlenv

import (
	"errors"
	"reflect"

	"go.yaml.in/yaml/v3"
)

// LoadOption configures Load. The zero configuration is the safe default:
// every yaml.v3 error is sanitized and the unknown-key name is redacted.
type LoadOption func(*loadOptions)

// loadOptions carries Load's policy switches.
type loadOptions struct {
	passthrough func(error) bool
	sanitize    []SanitizeOption
}

// WithSanitizeOptions forwards SanitizeOptions (e.g. WithUnknownKeyEcho) to
// every SanitizeDecodeError call Load makes, so the caller sets its
// sanitization policy once for the whole pipeline.
func WithSanitizeOptions(opts ...SanitizeOption) LoadOption {
	return func(o *loadOptions) { o.sanitize = append(o.sanitize, opts...) }
}

// WithErrorPassthrough registers pred as the caller's own-error detector for
// the decode step: a decode error for which pred reports true is returned
// unchanged instead of sanitized. It exists for config types whose
// UnmarshalYAML implementations return errors with an app-owned, value-safe
// vocabulary that the caller wants to keep on its operator surfaces. The
// safety argument is the caller's to make: a predicate that matches yaml.v3's
// own errors re-opens the leak SanitizeDecodeError closes, so pred should
// match only errors the caller's own code constructed. Parse errors and
// unknown-key findings never pass through pred; no caller code produced them.
func WithErrorPassthrough(pred func(error) bool) LoadOption {
	return func(o *loadOptions) { o.passthrough = pred }
}

// Load composes the package's safe config-loading pipeline into one call:
// single-document check and unknown-key strictness on the raw pre-expansion
// bytes, post-parse ${VAR} expansion of string scalar values (Expand, with
// allow as the caller's policy), the decode into out, and fail-closed error
// sanitization — so the composition documented piecewise on the primitives
// cannot be mis-ordered or partially applied. The primitives stay exported
// for callers whose policy the pipeline does not fit (a deliberately
// permissive partial probe, for example).
//
// out must be a non-nil pointer, typically to a struct pre-populated with the
// caller's defaults: the decode overlays the document onto it, and an EMPTY
// document (no YAML content) is not an error — out simply keeps its defaults.
// The unknown-key check probes a fresh throwaway value of out's type against
// the raw bytes; value errors in that probe (a custom UnmarshalYAML rejecting
// a still-literal ${VAR} that expansion will satisfy, a wrong-type scalar)
// are deliberately ignored — the post-expansion decode owns value
// diagnostics and re-raises the genuine ones — so only unknown-key findings
// fail the load here.
//
// unresolved is Expand's return, unchanged: the allowlisted names that stayed
// unresolved, deduplicated in document order, for the caller to warn on.
//
// Every returned error is safe to log: ErrMultipleDocuments is static; parse,
// unknown-key, and decode errors are rebuilt by SanitizeDecodeError (policy
// via WithSanitizeOptions), except a decode error the caller claims through
// WithErrorPassthrough, which is returned as-is.
func Load(data []byte, out any, allow func(name string) bool, opts ...LoadOption) (unresolved []string, err error) {
	var o loadOptions
	for _, opt := range opts {
		opt(&o)
	}
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return nil, errors.New("yamlenv: Load out must be a non-nil pointer")
	}
	if err := CheckSingleDocument(data); err != nil {
		return nil, err // static ErrMultipleDocuments, safe unsanitized
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		// No caller code ran yet, so a parse error is never app-owned;
		// it can embed pasted literal secrets and is always sanitized.
		return nil, SanitizeDecodeError(err, o.sanitize...)
	}
	if doc.Kind == 0 {
		return nil, nil // empty document: out keeps its pre-set defaults
	}
	if err := checkUnknownKeysFiltered(data, rv.Type().Elem(), o); err != nil {
		return nil, err
	}
	unresolved = Expand(&doc, allow)
	if err := doc.Decode(out); err != nil {
		if o.passthrough != nil && o.passthrough(err) {
			return unresolved, err
		}
		return unresolved, SanitizeDecodeError(err, o.sanitize...)
	}
	return unresolved, nil
}

// checkUnknownKeysFiltered runs the CheckUnknownKeys probe against a fresh
// value of the config type and keeps ONLY unknown-key findings, sanitized per
// the caller's policy. Everything else the probe can produce is a
// pre-expansion artifact whose diagnostic the post-expansion decode owns and
// re-raises when genuine: a wrong-type entry (dropped here, raised again by
// the decode if the expanded document still mismatches), and a probe-aborting
// error from a custom UnmarshalYAML rejecting a still-literal ${VAR} that
// expansion will satisfy (which also suppresses unknown-key detection for
// that document — the cost of never false-rejecting a valid env-referenced
// config).
func checkUnknownKeysFiltered(data []byte, cfgType reflect.Type, o loadOptions) error {
	err := CheckUnknownKeys(data, reflect.New(cfgType).Interface())
	if err == nil {
		return nil
	}
	typeErr, ok := errors.AsType[*yaml.TypeError](err)
	if !ok {
		return nil
	}
	var unknown []string
	for _, entry := range typeErr.Errors {
		if _, _, ok := lineEntryBounds(entry, unknownKeyMarker, unknownKeyInType); ok {
			unknown = append(unknown, entry)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return SanitizeDecodeError(&yaml.TypeError{Errors: unknown}, o.sanitize...)
}
