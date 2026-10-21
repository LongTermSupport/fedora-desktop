package matrix

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/matrix/fakehs"
)

const (
	usersPath = "/_synapse/admin/v2/users"
	bobID     = "@bob:example.org"
)

// hostAdmin is an admin client with the clock it waits on and the admin account it is.
type hostAdmin struct {
	*Admin

	clk *fakeClock
	uid string
}

// admin registers an admin account through the shared secret and gives a client signed in as
// it.
func admin(t *testing.T, srv *fakehs.Server) hostAdmin {
	t.Helper()
	clk := &fakeClock{}
	a, err := NewAdmin(Config{BaseURL: srv.URL(), Clock: clk})
	if err != nil {
		t.Fatalf("NewAdmin: %v", err)
	}
	t.Cleanup(a.Close)
	sess, err := a.Register(t.Context(), srv.RegistrationKey(), Registration{Username: "host", Password: "pw", DisplayName: "Host", Admin: true})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	signed, err := a.WithToken(sess.AccessToken.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	return hostAdmin{Admin: signed, clk: clk, uid: sess.UserID}
}

func TestRegistrationMAC(t *testing.T) {
	// Python's hmac.new(b"key", b"abc\0u\0p\0notadmin" (or admin), hashlib.sha1).hexdigest().
	if got := RegistrationMAC("key", "abc", "u", "p", false); got != "39385defa93645d65f907c9d93eff42666248712" {
		t.Errorf("notadmin mac = %q", got)
	}
	if got := RegistrationMAC("key", "abc", "u", "p", true); got != "65ded49fbf1f9b02077de05833e68820f8290dc0" {
		t.Errorf("admin mac = %q", got)
	}
}

func TestRegister(t *testing.T) {
	srv := newServer(t)
	a := admin(t, srv)
	if a.uid != "@host:example.org" {
		t.Errorf("registered %q", a.uid)
	}
	if got := lastRequest(t, srv).Header.Get("User-Agent"); got != "agent-bus" {
		t.Errorf("User-Agent = %q", got)
	}
	_, err := a.Register(t.Context(), "wrong-secret", Registration{Username: "eve", Password: "pw"})
	wantErr(t, err, ErrForbidden, "")
	inject(srv, http.MethodGet, "/v1/register", http.StatusOK, `{"nonce":"a b"}`)
	_, err = a.Register(t.Context(), srv.RegistrationKey(), Registration{Username: "eve", Password: "pw"})
	wantErr(t, err, ErrUnreachable, detailMalformed)
	inject(srv, http.MethodGet, "/v1/register", http.StatusNotFound, `{}`)
	_, err = a.Register(t.Context(), srv.RegistrationKey(), Registration{Username: "eve", Password: "pw"})
	wantErr(t, err, ErrNotFound, "")
}

func TestAccountLifecycle(t *testing.T) {
	srv := newServer(t)
	a := admin(t, srv)
	if acct, found, err := a.Account(t.Context(), bobID); err != nil || found {
		t.Fatalf("account before creation: %+v %v, %v", acct, found, err)
	}
	made, err := a.CreateAccount(t.Context(), bobID, "pw1", "Bob")
	if err != nil || made.UserID != bobID || made.DisplayName != "Bob" {
		t.Fatalf("createAccount = %+v, %v", made, err)
	}
	got, found, err := a.Account(t.Context(), bobID)
	if err != nil || !found || got != made {
		t.Fatalf("account = %+v %v, %v", got, found, err)
	}
	if err = a.SetLocked(t.Context(), bobID, true); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if err = a.SetDeactivated(t.Context(), bobID, true); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	got, found, err = a.Account(t.Context(), bobID)
	if err != nil || !found || !got.Locked || !got.Deactivated {
		t.Fatalf("account after lock and deactivate = %+v, %v", got, err)
	}
}

func TestTokensAndDevices(t *testing.T) {
	srv := newServer(t)
	a := admin(t, srv)
	if _, err := a.CreateAccount(t.Context(), bobID, "pw1", "Bob"); err != nil {
		t.Fatal(err)
	}
	bobToken, err := a.MintToken(t.Context(), bobID)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	bob := newMember(t, srv.URL(), bobToken.Reveal())
	if _, err = bob.Whoami(t.Context()); err != nil {
		t.Fatalf("the minted token: %v", err)
	}
	devices, err := a.Devices(t.Context(), bobID)
	if err != nil || len(devices) != 1 || devices[0].ID == "" {
		t.Fatalf("devices = %+v, %v", devices, err)
	}
	if err = a.ResetPassword(t.Context(), bobID, "pw2"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	_, err = bob.Whoami(t.Context())
	wantErr(t, err, ErrAuthRefused, "")
	_, err = a.MintToken(t.Context(), "@nobody:example.org")
	wantErr(t, err, ErrNotFound, "")
	inject(srv, http.MethodPost, "/login", http.StatusOK, `{"access_token":""}`)
	_, err = a.MintToken(t.Context(), bobID)
	wantErr(t, err, ErrUnreachable, detailMalformed)
}

func TestAdminSegmentsRefused(t *testing.T) {
	srv := newServer(t)
	a := admin(t, srv)
	for _, bad := range []func() error{
		func() error { _, _, e := a.Account(t.Context(), ""); return e },
		func() error { _, e := a.CreateAccount(t.Context(), "", "pw", "x"); return e },
		func() error { return a.ResetPassword(t.Context(), "", "pw") },
		func() error { _, e := a.MintToken(t.Context(), ""); return e },
		func() error { _, e := a.Devices(t.Context(), ""); return e },
	} {
		wantErr(t, bad(), ErrInvalid, detailBadSegment)
	}
}

func TestCreateAccountRefusesAnAdmin(t *testing.T) {
	srv := newServer(t)
	a := admin(t, srv)
	for _, body := range []string{
		`{"name":"@bob:example.org","admin":true}`,
		`{"name":"@other:example.org","admin":false}`,
	} {
		inject(srv, http.MethodPut, "/v2/users/", http.StatusOK, body)
		_, err := a.CreateAccount(t.Context(), bobID, "pw", "Bob")
		wantErr(t, err, ErrRefused, "the answer is not the plain account asked for")
	}
	inject(srv, http.MethodPut, "/v2/users/", http.StatusInternalServerError, `{}`)
	_, err := a.CreateAccount(t.Context(), bobID, "pw", "Bob")
	wantErr(t, err, ErrRefused, "server error")
	for _, body := range []string{`{"name":"@bob:example.org","admin":"no"}`, `{"name":"@carol:example.org"}`} {
		inject(srv, http.MethodGet, "/v2/users/", http.StatusOK, body)
		_, _, err = a.Account(t.Context(), bobID)
		wantErr(t, err, ErrUnreachable, detailMalformed)
	}
	inject(srv, http.MethodGet, "/v2/users/", http.StatusForbidden, `{}`)
	_, _, err = a.Account(t.Context(), bobID)
	wantErr(t, err, ErrForbidden, "")
}

func TestServerAdmins(t *testing.T) {
	srv := newServer(t)
	a := admin(t, srv)
	srv.AddUser("plain", "pw", false)
	names, err := a.ServerAdmins(t.Context())
	if err != nil || !slices.Equal(names, []string{a.uid}) {
		t.Fatalf("serverAdmins = %v, %v", names, err)
	}
	if q := lastRequest(t, srv).Query; q.Get("admins") != "true" {
		t.Errorf("query %v", q)
	}
	for _, body := range []string{`{"users":{},"next_token":null}`, `{"users":[],"next_token":"10"}`, `{"next_token":null}`, `{"users":[{"name":""}]}`} {
		inject(srv, http.MethodGet, usersPath, http.StatusOK, body)
		_, err = a.ServerAdmins(t.Context())
		wantErr(t, err, ErrUnreachable, "")
	}
	inject(srv, http.MethodGet, usersPath, http.StatusForbidden, `{}`)
	_, err = a.ServerAdmins(t.Context())
	wantErr(t, err, ErrForbidden, "")
}

func TestDevices(t *testing.T) {
	srv := newServer(t)
	a := admin(t, srv)
	inject(srv, http.MethodGet, "/devices", http.StatusOK, `{"devices":[{"device_id":"A","last_seen_ts":1760000000000},{"device_id":"B","last_seen_ts":null},{"device_id":"C"}],"total":3}`)
	devices, err := a.Devices(t.Context(), a.uid)
	if err != nil || len(devices) != 3 {
		t.Fatalf("devices = %+v, %v", devices, err)
	}
	if want := time.UnixMilli(1760000000000).UTC(); !devices[0].LastSeen.Equal(want) || !devices[1].LastSeen.IsZero() || !devices[2].LastSeen.IsZero() {
		t.Errorf("last seen %+v", devices)
	}
	for _, body := range []string{`{"devices":{}}`, `{}`, `{"devices":[{"device_id":""}]}`, `{"devices":[{"device_id":"A","last_seen_ts":1.5}]}`} {
		inject(srv, http.MethodGet, "/devices", http.StatusOK, body)
		_, err = a.Devices(t.Context(), a.uid)
		wantErr(t, err, ErrUnreachable, "")
	}
	inject(srv, http.MethodGet, "/devices", http.StatusNotFound, `{}`)
	_, err = a.Devices(t.Context(), a.uid)
	wantErr(t, err, ErrNotFound, "")
}

func TestAdminRateLimitAndLimit(t *testing.T) {
	srv := newServer(t)
	a := admin(t, srv)
	srv.Inject(fakehs.Fault{Method: http.MethodGet, Path: usersPath, Status: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"2"}}})
	if _, err := a.ServerAdmins(t.Context()); err != nil {
		t.Fatalf("serverAdmins after a 429: %v", err)
	}
	if got := a.clk.waits(); !slices.Equal(got, []time.Duration{2 * time.Second}) {
		t.Errorf("waits %v", got)
	}
	if a.limit != adminLimit {
		t.Errorf("admin limit %v", a.limit)
	}
	if _, err := a.WithToken(""); err == nil {
		t.Error("an empty token accepted")
	}
	if _, err := NewAdmin(Config{BaseURL: srv.URL(), UserAgent: "\x01"}); err == nil {
		t.Error("a bad User-Agent accepted")
	}
}
