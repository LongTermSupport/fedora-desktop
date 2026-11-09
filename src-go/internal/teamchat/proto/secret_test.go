package proto

import (
	"strings"
	"testing"
)

// ownSample is a made-up member token for the own-token check.
var ownSample = strings.Join([]string{"syt", "dGVzdA", "abcdefghijklmnopqrst", "0aBcDe"}, "_")

func TestSecretShapes(t *testing.T) {
	shapes := []struct{ name, text string }{
		{"private-key", strings.Repeat("-", 5) + "BEGIN OPENSSH PRIVATE" + " KEY" + strings.Repeat("-", 5)},
		{"github-token", "it is ghp_" + strings.Repeat("a", 30)},
		{"github-pat", "github_pat_" + strings.Repeat("A_", 10)},
		{"aws-access-key-id", "key AKIA" + strings.Repeat("Z", 16) + "."},
		{"slack-token", "xoxb-1234567890"},
		{"sk-api-key", "sk-" + strings.Repeat("x", 20)},
		{"google-api-key", "AIza" + strings.Repeat("0", 35)},
		{"jwt", "eyJhbGciOiJI.eyJzdWIiOiIx.SflKxwRJSMeK"},
		{"bearer", "Authorization: Bearer " + strings.Repeat("a", 16)},
		{"ansible-vault", "$ANSIBLE_VAULT;1.1;AES256"},
		{"url-credentials", "https://user:pass@example.com/"},
		{"credential-assignment", "pass" + `word = "hunter2hunter2"`},
	}
	if len(shapes) != len(secretPatterns) {
		t.Fatalf("%d patterns", len(secretPatterns))
	}
	for _, s := range shapes {
		if got, found := SecretShape(s.text, ""); !found || got != s.name {
			t.Errorf("SecretShape(%q) = %q, %v; want %q", s.text, got, found, s.name)
		}
	}
}

func TestSecretShapeBoundariesAreUnicode(t *testing.T) {
	nbsp := string(rune(0xa0))
	ideographicSpace := string(rune(0x3000))
	cases := map[string]bool{
		"xghp_" + strings.Repeat("a", 30):                     false, // ASCII word character before
		"éghp_" + strings.Repeat("a", 30):                     false, // a letter outside ASCII is a word character
		"٣ghp_" + strings.Repeat("a", 30):                     false, // so is a digit outside ASCII
		"·ghp_" + strings.Repeat("a", 30):                     true,  // punctuation is not
		"xghp_a ghp_" + strings.Repeat("a", 30):               true,  // a later match after a refused one
		"bearer" + nbsp + strings.Repeat("a", 16):             true,  // \s holds no-break space
		"bearer" + ideographicSpace + strings.Repeat("a", 16): true,
		"AKIA" + strings.Repeat("Z", 16) + "é":                false, // word character after
		"AKIA" + strings.Repeat("Z", 16) + "Z":                false,
		"plain prose about tokens and passwords":              false,
		"token: short":                                        false,
		"api_key" + nbsp + "=" + nbsp + "abcdefgh12":          true,
	}
	for text, want := range cases {
		if _, found := SecretShape(text, ""); found != want {
			t.Errorf("SecretShape(%q) found = %v, want %v", text, found, want)
		}
	}
}

func TestSecretOwnToken(t *testing.T) {
	if got, found := SecretShape("here: "+ownSample+" ok", ownSample); !found || got != "own-token" {
		t.Fatalf("own token = %q, %v", got, found)
	}
	if _, found := SecretShape("nothing here", ownSample); found {
		t.Fatal("found a token in plain text")
	}
}
