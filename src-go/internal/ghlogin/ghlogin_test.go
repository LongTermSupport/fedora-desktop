package ghlogin

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestParseReadsLoggedInAndActive(t *testing.T) {
	out := `{"hosts":{"github.com":[` +
		`{"login":"Alice","active":true,"state":"success"},` +
		`{"login":"bobby","active":false,"state":"success"},` +
		`{"login":"carol","active":false,"state":"error"}]}}`
	st, err := Parse([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if st.Active != "Alice" {
		t.Errorf("active %q", st.Active)
	}
	if !slices.Equal(st.Working, []string{"Alice", "bobby"}) {
		t.Errorf("working %q", st.Working)
	}
	for login, want := range map[string]bool{"alice": true, "ALICE": true, "bobby": true, "bob": false, "carol": false} {
		if got := st.LoggedIn(login); got != want {
			t.Errorf("LoggedIn(%s) = %v, want %v", login, got, want)
		}
	}
	if got := st.Login("ALICE"); got != "Alice" {
		t.Errorf("Login(ALICE) = %q, want gh's spelling", got)
	}
}

func TestParseActiveMustBeLoggedIn(t *testing.T) {
	st, err := Parse([]byte(`{"hosts":{"github.com":[{"login":"a","active":true,"state":"error"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if st.Active != "" || len(st.Working) != 0 {
		t.Errorf("a login whose token failed counted: %+v", st)
	}
	st, err = Parse([]byte(`{"hosts":{}}`))
	if err != nil || st.Active != "" || st.LoggedIn("a") {
		t.Errorf("empty: %+v %v", st, err)
	}
}

func TestParseRefusesWhatIsNotItsJSON(t *testing.T) {
	for _, out := range []string{"", "You are not logged in", `[]`, `{"hosts":{"github.com":{}}}`, `{"other":1}`} {
		if _, err := Parse([]byte(out)); err == nil {
			t.Errorf("%q accepted", out)
		}
	}
}

func TestReadAsksGhForGithubAsJSON(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gh", "auth", "status", "--hostname", "github.com", "--json", "hosts"},
		execx.Result{Stdout: []byte(`{"hosts":{"github.com":[{"login":"account-a","active":true,"state":"success"}]}}`)})
	st, err := Read(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if st.Active != "account-a" || !slices.Equal(st.Working, []string{"account-a"}) {
		t.Errorf("status %+v", st)
	}
}

func TestReadKeepsGhsRefusalAsAnExit(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gh", "auth", "status"}, execx.Result{ExitCode: 1, Stderr: []byte("You are not logged into any GitHub hosts.\n")})
	_, err := Read(context.Background(), f)
	if exit, ok := errors.AsType[*execx.ExitError](err); !ok || !strings.Contains(exit.Stderr, "not logged into") {
		t.Fatalf("err %v, want gh's exit kept", err)
	}
}

func TestReadRefusesOutputThatIsNotStatus(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gh", "auth", "status"}, execx.Result{Stdout: []byte("hello\n")})
	if _, err := Read(context.Background(), f); err == nil || !strings.Contains(err.Error(), "hello") {
		t.Fatalf("err %v, want the output named", err)
	}
}

func TestTokenIsTheLoginsOwnAsGhToken(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gh", "auth", "token", "--hostname", "github.com", "--user", "account-b"}, execx.Result{Stdout: []byte("placeholder-b\n")})
	s, err := Token(context.Background(), f, "account-b")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "GH_TOKEN" || !s.Is("placeholder-b") {
		t.Errorf("secret %v", s)
	}
}

func TestTokenFailsWhenGhHoldsNone(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"gh", "auth", "token"}, execx.Result{ExitCode: 1, Stderr: []byte("no oauth token found\n")})
	if _, err := Token(context.Background(), f, "account-b"); err == nil || !strings.Contains(err.Error(), "account-b") {
		t.Fatalf("err %v, want the login named", err)
	}
	f = &execx.Fake{}
	f.On([]string{"gh", "auth", "token"}, execx.Result{Stdout: []byte("\n")})
	if _, err := Token(context.Background(), f, "account-b"); err == nil {
		t.Fatal("an empty token accepted")
	}
}
