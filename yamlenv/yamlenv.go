// Package yamlenv expands allowlisted ${VAR} environment-variable references
// inside the string values of a parsed YAML document, so secrets can stay in
// the environment (an .env file, a Docker secret) while the YAML file holds
// structure.
//
// Expansion runs AFTER parsing, on string scalar values only. Expanding the
// raw document text before parsing (os.Expand over the file bytes) lets an
// environment value containing YAML syntax — a quote, a newline, a '#' — change
// the document structure or truncate the value; post-parse expansion makes
// that impossible by construction. Mapping keys and non-string scalars are
// deliberately left untouched.
//
// Only the braced ${VAR} form is recognized. An unbraced $VAR, a reference the
// allowlist rejects, and a reference to an unset variable are all kept
// byte-for-byte literal, so a bare '$' inside a secret or URL is never
// rewritten and an unset variable is never silently blanked. Expansion is a
// single pass: a ${VAR} produced by an expanded value is not re-expanded.
//
// Expansion creates one hazard of its own, which SanitizeDecodeError closes:
// after secrets are substituted into the document, a failing decode of it
// produces yaml.v3 errors that embed a backtick-quoted excerpt of the
// offending scalar — possibly an expanded secret — and such errors are
// typically logged at startup. SanitizeDecodeError rebuilds a decode or parse
// error from its value-independent structure (line numbers, source tags,
// destination types) and withholds anything it cannot prove value-free, so
// the error stays safe to log while remaining actionable.
//
// This package is its own nested Go module on purpose: it is the one part of
// envx that needs a YAML dependency, so the dependency lives in this module's
// go.mod (the root envx module is zero-require), it is released independently
// as yamlenv/vX.Y.Z tags, and importing plain envx never links it.
package yamlenv

import (
	"os"
	"regexp"

	"go.yaml.in/yaml/v3"
)

// refRe matches a ${VAR} reference, the only supported expansion form.
var refRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Expand expands ${VAR} references inside every string scalar value of the
// parsed document rooted at root, in place. A reference is replaced only when
// allow(name) reports true AND the variable is set in the environment (an
// empty-but-set value does substitute — set-vs-unset is the contract here, not
// envx's empty-equals-unset getter rule, so an operator can deliberately blank
// a field); every other reference stays literal.
//
// It returns the allowlisted names still unresolved after expansion (a
// reference the operator allowlisted but never set), deduplicated in
// first-seen document order, so the caller can log one warning naming them.
// Set-ness is re-checked before a name is reported: a ${VAR} introduced BY an
// expanded value stays literal (single pass, no recursion) but names a SET
// variable, and reporting it as "never set" would misname it — such a
// reference is simply not reported. A nil root or nil allow expands nothing
// and returns nil.
func Expand(root *yaml.Node, allow func(name string) bool) (unresolved []string) {
	if root == nil || allow == nil {
		return nil
	}
	seen := map[string]bool{}
	walkStringValues(root, func(node *yaml.Node) {
		node.Value = expand(node.Value, allow)
		for _, name := range unresolvedRefs(node.Value, allow) {
			if !seen[name] {
				seen[name] = true
				unresolved = append(unresolved, name)
			}
		}
	})
	return unresolved
}

// expand replaces each allowlisted, set ${VAR} reference in s with its
// environment value and keeps every other byte literal.
func expand(s string, allow func(string) bool) string {
	return refRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1]
		if !allow(name) {
			return m
		}
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return m
	})
}

// walkStringValues invokes fn on every !!str scalar VALUE in the document.
// Mapping keys and non-string scalars are deliberately skipped; alias nodes
// carry no content of their own, so an anchored value is visited exactly once
// at its anchor.
func walkStringValues(node *yaml.Node, fn func(*yaml.Node)) {
	if node.Kind == yaml.MappingNode {
		for i := 1; i < len(node.Content); i += 2 {
			walkStringValues(node.Content[i], fn)
		}
		return
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
		fn(node)
	}
	for _, child := range node.Content {
		walkStringValues(child, fn)
	}
}

// unresolvedRefs returns the allowlisted ${VAR} names still literal in s after
// expansion AND unset in the environment, deduplicated in order of
// appearance. The LookupEnv re-check keeps the "allowlisted but never set"
// contract honest: after the single expansion pass, a remaining allowlisted
// reference is either genuinely unset (reported) or was introduced by an
// expanded value naming a set variable (kept literal by design, not
// reported — it is not "never set").
func unresolvedRefs(s string, allow func(string) bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range refRe.FindAllStringSubmatch(s, -1) {
		if !allow(m[1]) || seen[m[1]] {
			continue
		}
		if _, ok := os.LookupEnv(m[1]); ok {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	return out
}
