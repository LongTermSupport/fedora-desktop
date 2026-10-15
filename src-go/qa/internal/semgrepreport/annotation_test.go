package semgrepreport

import "testing"

func TestAnnotatedCountsOnlyRuleidAnnotations(t *testing.T) {
	cases := map[string]struct {
		text string
		want []string
	}{
		"hash":         {"# ruleid: a\n", []string{"a"}},
		"slashes":      {"// ruleid: a, b\n", []string{"a", "b"}},
		"todoruleid":   {"// todoruleid: a\n", nil},
		"ok":           {"# ok: a\n", nil},
		"todook":       {"# todook: a\n", nil},
		"word prefix":  {"# notruleid: a\n", nil},
		"after a text": {"x = 1  # ruleid: a\n", []string{"a"}},
	}
	for name, c := range cases {
		got := Annotated(c.text)
		if len(got) != len(c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
			continue
		}
		for _, id := range c.want {
			if !got[id] {
				t.Errorf("%s: %v lacks %s", name, got, id)
			}
		}
	}
}
