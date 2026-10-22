package hostnet

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

func portOf(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func team(t *testing.T, id string) paths.TeamID {
	t.Helper()
	got, err := paths.ParseTeamID(id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestReadinessAsksTheServerVersionAndWantsA200(t *testing.T) {
	var path atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path.Store(r.URL.Path)
		// More than the cap reads: the client stops reading, so a write error is expected.
		if _, err := w.Write([]byte(strings.Repeat("x", 200<<10))); err != nil {
			path.Store(r.URL.Path)
		}
	}))
	defer srv.Close()
	if err := (Readiness{}).ServerVersion(context.Background(), portOf(t, srv)); err != nil {
		t.Fatal(err)
	}
	if path.Load() != "/_tuwunel/server_version" {
		t.Fatalf("asked for %v", path.Load())
	}
}

func TestProberAsksClientVersionsOfTheTeamsPort(t *testing.T) {
	var path atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		path.Store(r.URL.Path)
	}))
	defer srv.Close()
	prober := Prober{Port: func(id paths.TeamID) (int, error) {
		if id != team(t, "alpha") {
			return 0, errors.New("wrong team")
		}
		return portOf(t, srv), nil
	}}
	if err := prober.Probe(context.Background(), team(t, "alpha")); err != nil {
		t.Fatal(err)
	}
	if path.Load() != "/_matrix/client/versions" {
		t.Fatalf("asked for %v", path.Load())
	}
}

func TestAProbeAnswersAnErrorForAnythingButA200(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"500":      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"404":      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
		"204":      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
		"redirect": func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", http.StatusFound) },
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()
			if err := (Readiness{}).ServerVersion(context.Background(), portOf(t, srv)); err == nil {
				t.Fatal("ServerVersion succeeded")
			}
			prober := Prober{Port: func(paths.TeamID) (int, error) { return portOf(t, srv), nil }}
			if err := prober.Probe(context.Background(), team(t, "alpha")); err == nil {
				t.Fatal("Probe succeeded")
			}
		})
	}
}

func TestARedirectIsNotFollowed(t *testing.T) {
	var followed atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed.Store(true)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	if err := (Readiness{}).ServerVersion(context.Background(), portOf(t, srv)); err == nil {
		t.Fatal("ServerVersion succeeded")
	}
	if followed.Load() {
		t.Fatal("the redirect was followed")
	}
}

func TestTheProbeClientUsesNoProxyAndKeepsNoRedirect(t *testing.T) {
	client := newClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatalf("transport = %#v", client.Transport)
	}
	if client.Timeout != 5*time.Second {
		t.Fatalf("timeout = %v", client.Timeout)
	}
}

func TestAProbeOfNothingListeningFails(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tcp, ok := lis.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address is %T", lis.Addr())
	}
	port := tcp.Port
	if err = lis.Close(); err != nil {
		t.Fatal(err)
	}
	if err = (Readiness{}).ServerVersion(context.Background(), port); err == nil {
		t.Fatal("ServerVersion succeeded")
	}
}

func TestACancelledProbeFails(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (Readiness{}).ServerVersion(ctx, portOf(t, srv)); !errors.Is(err, context.Canceled) {
		t.Fatalf("ServerVersion error = %v", err)
	}
}

func TestProbeFailsWhenThePortIsUnknown(t *testing.T) {
	boom := errors.New("no team file")
	prober := Prober{Port: func(paths.TeamID) (int, error) { return 0, boom }}
	if err := prober.Probe(context.Background(), team(t, "alpha")); !errors.Is(err, boom) {
		t.Fatalf("Probe error = %v", err)
	}
}

func TestReadinessRefusesAPortOutOfRange(t *testing.T) {
	for _, port := range []int{0, -1, 65536} {
		if err := (Readiness{}).ServerVersion(context.Background(), port); err == nil {
			t.Errorf("ServerVersion accepted port %d", port)
		}
	}
}

func writeTeamFile(t *testing.T, root string, id string, port int) {
	t.Helper()
	doc := `{"team":{"team":"` + id + `","state":"present","port":` + strconv.Itoa(port) + `,"description":"d",` +
		`"listen":["192.0.2.10"],"allow_from":["192.0.2.0/24"],"humans":["alice"],` +
		`"trust":{"kind":"github","org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}},` +
		`"lxc_members":[],"address":"192.0.2.10"}`
	in, err := declare.DecodeInput([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, paths.TeamDeclaration(team(t, id)))
	if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, in.Team.Canonical(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTeamPortIsTheDeclaredPort(t *testing.T) {
	root := t.TempDir()
	writeTeamFile(t, root, "alpha", 8448)
	port, err := TeamPort(root)(team(t, "alpha"))
	if err != nil || port != 8448 {
		t.Fatalf("TeamPort = %d, %v", port, err)
	}
}

func TestTeamPortFailsOnWhatItCannotTrust(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		"no file": func(*testing.T, string) {},
		"not json": func(t *testing.T, root string) {
			t.Helper()
			path := filepath.Join(root, paths.TeamDeclaration(team(t, "alpha")))
			writeFile(t, path, "{")
		},
		"another team's file": func(t *testing.T, root string) {
			t.Helper()
			writeTeamFile(t, root, "beta", 8449)
			beta, alpha := filepath.Join(root, paths.TeamDeclaration(team(t, "beta"))), filepath.Join(root, paths.TeamDeclaration(team(t, "alpha")))
			raw, _, err := state.ReadFile(beta, teamFileCap)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, alpha, string(raw))
		},
		"a link": func(t *testing.T, root string) {
			t.Helper()
			writeTeamFile(t, root, "beta", 8449)
			alpha := filepath.Join(root, paths.TeamDeclaration(team(t, "alpha")))
			if err := os.MkdirAll(filepath.Dir(alpha), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, paths.TeamDeclaration(team(t, "beta"))), alpha); err != nil {
				t.Fatal(err)
			}
		},
		"over the cap": func(t *testing.T, root string) {
			t.Helper()
			path := filepath.Join(root, paths.TeamDeclaration(team(t, "alpha")))
			writeFile(t, path, strings.Repeat(" ", teamFileCap+1))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			setup(t, root)
			if _, err := TeamPort(root)(team(t, "alpha")); err == nil {
				t.Fatal("TeamPort succeeded")
			}
		})
	}
}
