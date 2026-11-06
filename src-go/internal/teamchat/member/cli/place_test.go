package cli

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/placeissued"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/testfiles"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	placeHandle = "repo.1+site.podman"
	placeBase   = "http://127.0.0.1:8448"
	placeLine   = "placed seat " + placeHandle + " of team ops\n"
)

// placeSecret is a credential random for each run, which no output may show.
var placeSecret = rand.Text()

// reachOK answers that every homeserver answers.
type reachOK struct{}

func (reachOK) Reach(context.Context, seat.Bundle) error { return nil }

// placeWorld is a machine under a temporary directory: a home, a runtime directory and a checkout,
// whose environment the run is given, and whose homeserver always answers.
type placeWorld struct {
	world

	dir, home, run, checkout string
	env                      map[string]string
}

func newPlaceWorld(t *testing.T) *placeWorld {
	t.Helper()
	dir := t.TempDir()
	p := &placeWorld{dir: dir, home: dir + "/home", run: dir + "/run", checkout: dir + "/checkout"}
	for _, d := range []string{p.home, p.run, p.checkout} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p.env = map[string]string{"HOME": p.home, "XDG_RUNTIME_DIR": p.run}
	was := placeDeps
	t.Cleanup(func() { placeDeps = was })
	placeDeps = func(state, runtime string, key paths.CheckoutKey) placeissued.Deps {
		d := placeissued.OS(state, runtime, key)
		d.Reach = reachOK{}
		return d
	}
	return p
}

func (p *placeWorld) runIn(stdin string, args ...string) term.ExitCode {
	p.out.Reset()
	p.err.Reset()
	return Run(Env{
		Args: args, Stdin: strings.NewReader(stdin), Stdout: &p.out, Stderr: &p.err, Digest: digest,
		Getenv: func(k string) string { return p.env[k] },
	})
}

// place runs `seat place-issued` for the world's checkout, or the one named.
func (p *placeWorld) place(stdin string, checkout ...string) term.ExitCode {
	dir := p.checkout
	if len(checkout) > 0 {
		dir = checkout[0]
	}
	return p.runIn(stdin, "seat", "place-issued", "--checkout", dir)
}

// poolSeat is the seat's pool directory for the checkout at realPath.
func (p *placeWorld) poolSeat(t *testing.T, realPath string) paths.PoolSeat {
	t.Helper()
	home, err := paths.StateHome(p.home, "")
	if err != nil {
		t.Fatal(err)
	}
	store, err := paths.SeatStore(home)
	if err != nil {
		t.Fatal(err)
	}
	key, err := paths.CheckoutKeyOf(realPath)
	if err != nil {
		t.Fatal(err)
	}
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	name, err := paths.ParseSeatName(placeHandle)
	if err != nil {
		t.Fatal(err)
	}
	return store.PoolSeat(key, team, name)
}

func (p *placeWorld) exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

// exitOf is the exit status the code names.
func exitOf(t *testing.T, code string) term.ExitCode {
	t.Helper()
	exit, ok := exits.ExitOf(code)
	if !ok {
		t.Fatalf("no exit for %s", code)
	}
	return exit
}

// issuedFor is the bare issued document of the test handle, with a password.
func issuedFor(t *testing.T, secret string) string {
	t.Helper()
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := paths.ParseDigest("0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	server := paths.ServerName(team)
	bundle := seat.Bundle{
		V: seat.BundleVersion, Team: team, ServerName: server, UserID: "@" + placeHandle + ":" + server,
		AdminID: "@admin:" + server, MainRoom: "!abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ", BaseURL: placeBase,
		DeviceID: paths.SeatDevice, IssuedMS: 1_700_000_000_000, Issuer: issuer,
	}
	sec, err := seat.ParseSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := seat.Issued{Seat: bundle, Kind: seat.KindPassword, Secret: sec}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return string(doc)
}

func TestPlaceIssuedPlacesTheSeatInTheChecksPool(t *testing.T) {
	p := newPlaceWorld(t)
	if got := p.place(issuedFor(t, placeSecret)); got != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", got, p.err.String())
	}
	if p.out.String() != placeLine || p.err.Len() != 0 {
		t.Errorf("stdout %q stderr %q, want only %q", p.out.String(), p.err.String(), placeLine)
	}
	pool := p.poolSeat(t, p.checkout)
	if got := string(testfiles.Read(t, p.dir, pool.Password())); got != placeSecret {
		t.Errorf("the pool password is %d bytes, want the issued one", len(got))
	}
	if !p.exists(t, pool.Bundle()) {
		t.Error("the pool seat has no seat.json")
	}
}

// The pool is keyed on the checkout's real path, so a link to a checkout and the checkout are one
// pool, which is the one the launcher finds from the checkout it runs in.
func TestPlaceIssuedKeysThePoolOnTheResolvedCheckout(t *testing.T) {
	p := newPlaceWorld(t)
	link := p.dir + "/link"
	if err := os.Symlink(p.checkout, link); err != nil {
		t.Fatal(err)
	}
	if got := p.place(issuedFor(t, placeSecret), link); got != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", got, p.err.String())
	}
	if !p.exists(t, p.poolSeat(t, p.checkout).Password()) {
		t.Error("the seat is not in the pool of the checkout's real path")
	}
	if p.exists(t, p.poolSeat(t, link).Dir()) {
		t.Error("the seat is in the pool of the link's own path")
	}
}

func TestPlaceIssuedRefusesADocumentAndMakesNothing(t *testing.T) {
	p := newPlaceWorld(t)
	for _, doc := range []string{"", "{", `{"seat":{}}`, issuedFor(t, placeSecret) + " trailing"} {
		if got := p.place(doc); got != exits.ExitConfig || p.out.Len() != 0 {
			t.Errorf("%q: exit %d stdout %q", doc, got, p.out.String())
		}
		if e := p.lastError(t); e.Code != exits.CodeBundle {
			t.Errorf("%q: %+v", doc, e)
		}
		if strings.Contains(p.err.String(), placeSecret) {
			t.Errorf("%q: stderr shows the credential", doc)
		}
	}
	if p.exists(t, p.home+"/.local") {
		t.Error("a refused document made the state home")
	}
}

func TestPlaceIssuedNeedsAnAbsoluteHomeAndRuntimeDirectory(t *testing.T) {
	for _, c := range []struct {
		name string
		env  map[string]string
		code string
	}{
		{"no HOME", map[string]string{"XDG_RUNTIME_DIR": "/run/user/1"}, exits.CodeHome},
		{"relative HOME", map[string]string{"HOME": "home", "XDG_RUNTIME_DIR": "/run/user/1"}, exits.CodeHome},
		{"no runtime directory", map[string]string{"HOME": "/home/a"}, exits.CodeRuntime},
		{"relative runtime directory", map[string]string{"HOME": "/home/a", "XDG_RUNTIME_DIR": "run"}, exits.CodeRuntime},
	} {
		p := newPlaceWorld(t)
		p.env = c.env
		want := exitOf(t, c.code)
		if got := p.place(issuedFor(t, placeSecret)); got != want || p.out.Len() != 0 {
			t.Errorf("%s: exit %d, want %d; stdout %q", c.name, got, want, p.out.String())
		}
		if e := p.lastError(t); e.Code != c.code {
			t.Errorf("%s: %+v", c.name, e)
		}
	}
}

func TestPlaceIssuedWithoutAnEnvironmentIsRefusedNotPanicked(t *testing.T) {
	p := newPlaceWorld(t)
	got := Run(Env{
		Args: []string{"seat", "place-issued", "--checkout", p.checkout}, Stdin: strings.NewReader(issuedFor(t, placeSecret)),
		Stdout: &p.out, Stderr: &p.err, Digest: digest,
	})
	if want := exitOf(t, exits.CodeHome); got != want {
		t.Errorf("exit %d, want %d; stderr %q", got, want, p.err.String())
	}
}

func TestPlaceIssuedNeedsAnExistingCheckout(t *testing.T) {
	p := newPlaceWorld(t)
	if got := p.place(issuedFor(t, placeSecret), p.dir+"/absent"); got != exits.ExitUsage || p.out.Len() != 0 {
		t.Fatalf("exit %d stdout %q", got, p.out.String())
	}
	if e := p.lastError(t); e.Code != exits.CodeUsage {
		t.Errorf("%+v", e)
	}
}

func TestPlaceIssuedNeedsItsCheckoutFlag(t *testing.T) {
	p := newPlaceWorld(t)
	if got := p.runIn(issuedFor(t, placeSecret), "seat", "place-issued"); got != exits.ExitUsage {
		t.Fatalf("exit %d, stderr %q", got, p.err.String())
	}
}

func TestThePlaceDepsSeamIsTheRealMachine(t *testing.T) {
	key, err := paths.CheckoutKeyOf("/srv/example/checkout")
	if err != nil {
		t.Fatal(err)
	}
	d := placeDeps("/state", "/run", key)
	if d.StateHome != "/state" || d.RuntimeDir != "/run" || d.Checkout != key || d.Reach == nil || d.Credential == nil || d.Clock == nil {
		t.Errorf("deps %+v", d)
	}
}
