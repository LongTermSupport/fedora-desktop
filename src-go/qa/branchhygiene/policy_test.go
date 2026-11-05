package branchhygiene

import (
	"errors"
	"strings"
	"testing"
)

// ReadPolicy is the one reader of scripts/branch-policy.toml, shared with branch-delete-list.
func TestReadPolicyReadsThePolicyUnderTheRoot(t *testing.T) {
	var asked string
	pol, err := ReadPolicy(func(p string) ([]byte, error) {
		asked = p
		return []byte(goodPolicy), nil
	}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if asked != "/repo/"+PolicyFile || pol.Cap != 1 || len(pol.Exempt) != 1 {
		t.Fatalf("asked %q, got %+v", asked, pol)
	}
}

func TestReadPolicyRefusesAMissingUnknownOrInvalidPolicy(t *testing.T) {
	for name, read := range map[string]func(string) ([]byte, error){
		"missing": func(string) ([]byte, error) { return nil, errors.New("no such file") },
		"unknown": func(string) ([]byte, error) { return []byte(goodPolicy + "colour = 1\n"), nil },
		"invalid": func(string) ([]byte, error) { return []byte("cap = 0\nwarn_days = 3\nfail_days = 7\n"), nil },
	} {
		if _, err := ReadPolicy(read, "/repo"); err == nil || !strings.Contains(err.Error(), "policy") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
