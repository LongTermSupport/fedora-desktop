package seat

import (
	"context"
	"crypto/rand"
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

// poolToken is DESIGN-v2 7.1's (d1) behind the seam: the pool keeps a token, Mint answers it
// and Retire ends nothing.
type poolToken struct{ retired int }

func (*poolToken) Kind() Kind { return KindToken }

func (*poolToken) Mint(_ context.Context, _ Bundle, pool Secret) (Secret, error) {
	return pool, nil
}

func (p *poolToken) Retire(context.Context, Bundle, Secret) error {
	p.retired++
	return nil
}

// SC1, n2c-D7: another credential is a store configuration, not a change to the store: a token
// credential is placed into the pool's token file, taken and released by the same code.
func TestATokenCredentialBehindTheSeam(t *testing.T) {
	w := newWorld(t)
	cred := &poolToken{}
	cfg := w.config()
	cfg.Credential = cred
	s, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := s.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	w.store = s
	token := rand.Text()
	a := w.lock()
	c, err := s.Claim(a, w.seat)
	if err == nil {
		err = c.Fill(Issued{Seat: w.bundle(), Kind: KindToken, Secret: mustSecret(t, token)})
	}
	w.unlock(a)
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, w.pool().Token()) != token || exists(t, w.pool().Password()) {
		t.Fatalf("the pool keeps its credential in the wrong file")
	}
	taken := w.take("s1")
	if readFile(t, taken.Dir+"/token") != token {
		t.Errorf("the session copy's token is not what Mint answered")
	}
	if err := w.release("s1", true); err != nil || cred.retired != 1 {
		t.Errorf("release: %v, retired %d times", err, cred.retired)
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
