package proto

import (
	"strings"
	"testing"
)

func TestSecretShapeFollowsPythonSearch(t *testing.T) {
	tok := strings.Repeat("a", 30)
	cases := []struct{ text, shape string }{
		{"ghp_" + tok, "github-token"},
		{"xghp_" + tok, ""},
		{"x-ghp_" + tok, "github-token"},
		{"\U000000e9ghp_" + tok, ""},
		{"\U00000301ghp_" + tok, "github-token"},
		{"AKIA" + strings.Repeat("A", 16), "aws-access-key-id"},
		{"AKIA" + strings.Repeat("A", 17), ""},
		{"xsk-sk-" + strings.Repeat("a", 20), "sk-api-key"},
		{"Bearer\t" + strings.Repeat("a", 16), "bearer"},
		{"ap\U00000131_key=abcdefgh", "credential-assignment"},
		{"pr\U00000130vate_key=abcdefgh", "credential-assignment"},
		{"bearer " + strings.Repeat("\U0000212a", 16), "bearer"},
		{"passwords=" + tok, ""},
		{"pass\U00000131word: hunter2hunter2", ""},
		{"PASSWORD = 'hunter2hunter2'", "credential-assignment"},
		{"api_key=abcdefgh", "credential-assignment"},
		{"to\U0000212aen=abcdefgh", "credential-assignment"},
		{"https://user:pw@example.org", "url-credentials"},
		{"$ANSIBLE_VAULT;1.1", "ansible-vault"},
		{"-----BEGIN RSA " + "PRIVATE KEY-----", "private-key"},
		{"nothing here", ""},
		{"secret\U00003000=\U00003000abcdefgh", "credential-assignment"},
		{"ghp_" + tok + " github_pat_" + tok, "github-token"},
		{"\U00011f04ghp_" + tok, "github-token"},
	}
	for _, c := range cases {
		if got := SecretShape(c.text); got != c.shape {
			t.Errorf("SecretShape(%q) = %q, want %q", c.text, got, c.shape)
		}
	}
}
