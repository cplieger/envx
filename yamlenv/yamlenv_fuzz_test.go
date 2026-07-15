package yamlenv_test

import (
	"regexp"
	"testing"

	"github.com/cplieger/envx/yamlenv"
	"go.yaml.in/yaml/v3"
)

// validName mirrors the ${VAR} name grammar: what Expand may ever report.
var validName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// FuzzExpand asserts the invariants every config loader relies on against
// arbitrary YAML documents: Expand never panics, an allow-nothing expansion is
// a byte-for-byte no-op on the re-encoded document, and every reported
// unresolved name is a syntactically valid environment-variable name.
func FuzzExpand(f *testing.F) {
	for _, seed := range []string{
		"",
		"a: 1\n",
		"api_key: ${APP_KEY}\n",
		"a: ${APP_A}\nb: [${APP_B}, x]\nc:\n  d: ${APP_A}\n",
		"${APP_KEY}: key-position\n",
		"anchor: &k ${APP_K}\nalias: *k\n",
		"s: |\n  ${APP_BLOCK}\n  line\n",
		"v: \"a ${not-a-name} b\"\n",
		"n: 5\nb: true\nx: null\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, doc string) {
		var root yaml.Node
		if err := yaml.Unmarshal([]byte(doc), &root); err != nil {
			t.Skip()
		}
		baseline, baseErr := yaml.Marshal(&root)

		// Allow-nothing must be a pure no-op: nothing reported, nothing changed.
		if got := yamlenv.Expand(&root, func(string) bool { return false }); len(got) != 0 {
			t.Fatalf("allow-nothing Expand reported %v, want none", got)
		}
		if baseErr == nil {
			after, err := yaml.Marshal(&root)
			if err != nil || string(after) != string(baseline) {
				t.Fatalf("allow-nothing Expand changed the document:\nbefore: %q\nafter: %q (err %v)", baseline, after, err)
			}
		}

		// Allow-everything must never panic, and every unresolved name it
		// reports must be a valid env-var name (the ref grammar).
		for _, name := range yamlenv.Expand(&root, func(string) bool { return true }) {
			if !validName.MatchString(name) {
				t.Fatalf("unresolved name %q does not match the ${VAR} grammar", name)
			}
		}
	})
}
