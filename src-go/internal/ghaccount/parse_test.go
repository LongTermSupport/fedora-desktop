package ghaccount

import (
	"slices"
	"testing"
)

func TestGhVersionFloor(t *testing.T) {
	cases := map[string]bool{
		"gh version 2.81.0 (2025-10-01)\nhttps://x\n": true,
		"gh version 2.103.0 (2026-10-21)\n":           true,
		"gh version 3.0.1\n":                          true,
		"gh version 2.80.9 (2025-09-01)\n":            false,
		"gh version 2.40.0 (2023-12-07)\n":            false,
		"gh version 1.99.99\n":                        false,
	}
	for out, want := range cases {
		v, err := ghVersion(out)
		if err != nil {
			t.Fatalf("%q: %v", out, err)
		}
		if got := v.atLeast(minGhVersion); got != want {
			t.Errorf("%q: %v, want %v", out, got, want)
		}
	}
	if _, err := ghVersion("gh version unknown\n"); err == nil {
		t.Error("no version accepted")
	}
}

func TestStatusReadsLoggedInAndActive(t *testing.T) {
	out := `{"hosts":{"github.com":[` +
		`{"login":"Alice","active":true,"state":"success"},` +
		`{"login":"bobby","active":false,"state":"success"},` +
		`{"login":"carol","active":false,"state":"error"}]}}`
	st, err := parseStatus([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if st.active != "Alice" {
		t.Errorf("active %q", st.active)
	}
	for login, want := range map[string]bool{"alice": true, "ALICE": true, "bobby": true, "bob": false, "carol": false} {
		if got := st.loggedIn(login); got != want {
			t.Errorf("loggedIn(%s) = %v, want %v", login, got, want)
		}
	}
}

func TestStatusActiveMustBeLoggedIn(t *testing.T) {
	st, err := parseStatus([]byte(`{"hosts":{"github.com":[{"login":"a","active":true,"state":"error"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if st.active != "" {
		t.Errorf("active %q", st.active)
	}
	st, err = parseStatus([]byte(`{"hosts":{}}`))
	if err != nil || st.active != "" || st.loggedIn("a") {
		t.Errorf("empty: %+v %v", st, err)
	}
}

func TestStatusRefusesWhatIsNotItsJSON(t *testing.T) {
	for _, out := range []string{"", "You are not logged in", `[]`, `{"hosts":{"github.com":{}}}`, `{"other":1}`} {
		if _, err := parseStatus([]byte(out)); err == nil {
			t.Errorf("%q accepted", out)
		}
	}
}

func TestAuditReadsOneLinePerLoginInOrder(t *testing.T) {
	out := "SCOPES-OK alice\nSCOPES-MISSING bob alpha,beta\nSCOPES-NOT-AUTHENTICATED carol\nSCOPES-UNREADABLE dave HTTP 401: Bad credentials\n"
	got, err := parseAudit(out, []string{"alice", "bob", "carol", "dave"})
	if err != nil {
		t.Fatal(err)
	}
	want := []verdict{
		{login: "alice", kind: scopesOK},
		{login: "bob", kind: scopesMissing, detail: "alpha,beta"},
		{login: "carol", kind: scopesNotAuthenticated},
		{login: "dave", kind: scopesUnreadable, detail: "HTTP 401: Bad credentials"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestAuditRefusesAnAnswerItCannotTrust(t *testing.T) {
	cases := map[string]string{
		"missing line":      "SCOPES-OK alice\n",
		"wrong order":       "SCOPES-OK bob\nSCOPES-OK alice\n",
		"another login":     "SCOPES-OK alice\nSCOPES-OK mallory\n",
		"unknown form":      "SCOPES-OK alice\nSCOPES-MAYBE bob\n",
		"missing no scopes": "SCOPES-OK alice\nSCOPES-MISSING bob\n",
		"extra line":        "SCOPES-OK alice\nSCOPES-OK bob\nSCOPES-OK bob\n",
	}
	for name, out := range cases {
		if _, err := parseAudit(out, []string{"alice", "bob"}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestVaultOutputGivesTheMessage(t *testing.T) {
	got, err := parseVaultOutput("localhost | SUCCESS => {\n    \"changed\": false,\n    \"msg\": \"s3 cr=>t\"\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "s3 cr=>t" {
		t.Fatalf("got %q", got)
	}
	for _, out := range []string{"", "localhost | FAILED", "x => {\"msg\": 3}", "x => {\"msg\": \"\"}", "x => {not json"} {
		if _, err := parseVaultOutput(out); err == nil {
			t.Errorf("%q accepted", out)
		}
	}
}

func TestPublicKeyField(t *testing.T) {
	got, err := publicKeyField("ssh-ed25519 AAAAC3Nza+/= jane@github\n")
	if err != nil || got != "AAAAC3Nza+/=" {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range []string{"", "ssh-ed25519", "\n"} {
		if _, err := publicKeyField(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestKeyListedMatchesTheWholeField(t *testing.T) {
	listing := "ssh-ed25519 AAAAone\nssh-rsa AAAAtwo extra\n"
	if !keyListed(listing, "AAAAtwo") {
		t.Error("listed key not found")
	}
	if keyListed(listing, "AAAAon") || keyListed(listing, "AAAA") || keyListed(listing, "ssh-rsa") {
		t.Error("a part matched")
	}
}

func TestSSHGreetingLogin(t *testing.T) {
	if got := sshLogin("Hi Jane-Doe! You've successfully authenticated, but GitHub does not provide shell access.\n"); got != "Jane-Doe" {
		t.Errorf("got %q", got)
	}
	if got := sshLogin("git@github.com: Permission denied (publickey).\n"); got != "" {
		t.Errorf("got %q", got)
	}
}
