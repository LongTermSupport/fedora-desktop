package seat

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
)

// SC1: the password credential keeps its secret in the pool's password file.
func TestPasswordIsThePasswordKind(t *testing.T) {
	if (Password{}).Kind() != KindPassword {
		t.Errorf("Password's kind is %q", Password{}.Kind())
	}
}

// SC2, ST6: Mint's token is live, and every token the device had before it is dead.
func TestPasswordMintEndsEveryEarlierToken(t *testing.T) {
	clock := newClock()
	h := newHomeserver(t, clock)
	first, second := h.stale(), h.stale()
	if !h.alive(first) || !h.alive(second) {
		t.Fatalf("fakehs does not keep a re-login's earlier token, as Tuwunel 1.9.4 does")
	}
	got, err := Password{Clock: clock}.Mint(t.Context(), h.bundle(), mustSecret(t, testPassword))
	if err != nil {
		t.Fatal(err)
	}
	if !h.alive(got.Reveal()) {
		t.Errorf("the minted token is dead")
	}
	if h.alive(first) || h.alive(second) {
		t.Errorf("a token minted before the take is still live")
	}
}

// SC2: a refused first login is a password reset outside the pool, ErrReissue; no call's error
// carries the password.
func TestPasswordMintRefusedIsReissue(t *testing.T) {
	clock := newClock()
	h := newHomeserver(t, clock)
	const wrong = "not-the-password"
	_, err := Password{Clock: clock}.Mint(t.Context(), h.bundle(), mustSecret(t, wrong))
	if !errors.Is(err, ErrReissue) {
		t.Fatalf("a refused login: %v", err)
	}
	if strings.Contains(err.Error(), wrong) {
		t.Errorf("the error shows the password: %v", err)
	}
}

// SC2: any other failure is not a reissue: the server failing a step, or a server not there.
func TestPasswordMintOtherFailures(t *testing.T) {
	clock := newClock()
	h := newHomeserver(t, clock)
	for name, fault := range map[string]fakehs.Fault{
		"first login": {Method: http.MethodPost, Path: "/login", Status: http.StatusForbidden, Body: []byte(`{"errcode":"M_USER_DEACTIVATED"}`)},
		"logout":      {Method: http.MethodPost, Path: "/logout", Status: http.StatusBadRequest, Body: []byte(`{"errcode":"M_UNKNOWN"}`)},
	} {
		h.hs.Inject(fault)
		_, err := Password{Clock: clock}.Mint(t.Context(), h.bundle(), mustSecret(t, testPassword))
		if err == nil || errors.Is(err, ErrReissue) || strings.Contains(err.Error(), testPassword) {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad := h.bundle()
	bad.BaseURL = "http://example.org:8448"
	if _, err := (Password{Clock: clock}).Mint(t.Context(), bad, mustSecret(t, testPassword)); err == nil {
		t.Errorf("a member URL the client refuses was used")
	}
}

// SC3: Retire logs the held token out; a token already dead counts as done.
func TestPasswordRetire(t *testing.T) {
	clock := newClock()
	h := newHomeserver(t, clock)
	token := mustSecret(t, h.stale())
	cred := Password{Clock: clock}
	if err := cred.Retire(t.Context(), h.bundle(), token); err != nil {
		t.Fatal(err)
	}
	if h.alive(token.Reveal()) {
		t.Errorf("a retired token is live")
	}
	if err := cred.Retire(t.Context(), h.bundle(), token); err != nil {
		t.Errorf("retiring a dead token: %v", err)
	}
	h.hs.Inject(fakehs.Fault{Method: http.MethodPost, Path: "/logout", Status: http.StatusBadGateway})
	if err := cred.Retire(t.Context(), h.bundle(), mustSecret(t, h.stale())); err == nil {
		t.Errorf("a failed logout was taken as done")
	}
}
