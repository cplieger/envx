// Package yamlenv expands allowlisted ${VAR} environment-variable references
// inside the string values of a parsed YAML document, so secrets can stay in
// the environment while the file holds structure.
//
// Expansion runs after parsing, on string scalars only, so an environment
// value cannot change the document's structure. Only the braced form is
// recognized; an unbraced $VAR, a rejected name and an unset variable stay
// literal, and expansion is a single pass. SanitizeDecodeError strips value
// excerpts (possibly expanded secrets) from yaml.v3 decode errors.
// CheckUnknownKeys and CheckSingleDocument are the strict-load checks.
//
// Load composes the checks, parse, expansion, decode and sanitization in the
// safe order and is the default path. The package is its own Go module so
// that importing envx never links the YAML dependency.
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
//
// Expand rewrites node.Value only and leaves node.Style untouched, so the
// expanded document is meant to be DECODED, not re-serialized: re-encoding it
// can mis-render a substituted value containing YAML-significant characters
// (a newline, a quote) whose original style no longer suits it.
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
