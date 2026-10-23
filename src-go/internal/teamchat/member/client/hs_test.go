package client

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/rooms"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
)

const (
	hsTeam   = "alpha"
	hsWorker = "myrepo.2+desk.podman"
)

var hsServer = "alpha." + paths.Product + ".internal"

func hsUser(local string) string { return proto.UserID(local, hsServer) }

// noWait is a matrix client's retry wait that never sleeps.
type noWait struct{}

func (noWait) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// hsRecorder keeps the room ids rooms.Converge creates.
type hsRecorder struct{ main string }

func (*hsRecorder) Intent(string) error { return nil }

func (r *hsRecorder) Record(purpose, id string) error {
	if purpose == proto.PurposeMain {
		r.main = id
	}
	return nil
}

// hs is team alpha's homeserver on fakehs: human alice and worker hsWorker, joined to main
// with its own token.
type hs struct {
	srv   *fakehs.Server
	main  string
	token string
}

// newHS is fakehs alone; rooms adds alpha's rooms and the worker.
func newHS(t *testing.T) *hs {
	t.Helper()
	srv := fakehs.New(fakehs.Options{
		ServerName: hsServer, Quirks: fakehs.TuwunelQuirks(),
		Now: func() time.Time { return time.UnixMilli(1_760_000_000_000).UTC() },
	})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := srv.CheckModelled(); err != nil {
			t.Errorf("fakehs: %v", err)
		}
		if err := srv.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return &hs{srv: srv}
}

func (h *hs) rooms(t *testing.T) *hs {
	t.Helper()
	h.srv.AddUser(proto.AdminLocalpart, "pw", true)
	h.srv.AddUser("alice", "pw", false)
	h.srv.AddUser(hsWorker, "pw", false)
	a, err := matrix.NewAdmin(matrix.Config{BaseURL: h.srv.URL(), Token: h.mint(t, proto.AdminID(hsServer)), Clock: noWait{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	roster := proto.Roster{
		Team: hsTeam, Name: "Alpha", Humans: []string{hsUser("alice")},
		Members: map[string]proto.RosterMember{hsUser(hsWorker): {Role: proto.RoleWorker, HumanText: true}},
		Forge:   proto.Forge{Kind: proto.ForgeGitHub, Org: "example-org"}, Limits: proto.DefaultLimits(),
	}
	rec := &hsRecorder{}
	if _, err := rooms.Converge(t.Context(), a, rec, rooms.Team{Server: hsServer, Team: hsTeam, Roster: roster}, proto.RoomIDs{}); err != nil {
		t.Fatalf("Converge: %v", err)
	}
	h.main = rec.main
	h.token = h.mint(t, hsUser(hsWorker))
	if _, err := h.member(t, h.token).Join(t.Context(), h.main); err != nil {
		t.Fatalf("join main: %v", err)
	}
	return h
}

func (h *hs) mint(t *testing.T, user string) string {
	t.Helper()
	token, err := h.srv.MintToken(user)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (h *hs) member(t *testing.T, token string) *matrix.Client {
	t.Helper()
	c, err := matrix.NewMember(matrix.Config{BaseURL: h.srv.URL(), PlainHTTPHosts: []string{"127.0.0.1"}, Token: token, Clock: noWait{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

// disconnects are the disconnected notices in main.
func (h *hs) disconnects(t *testing.T) int {
	t.Helper()
	page, err := h.member(t, h.token).Messages(t.Context(), h.main, matrix.MessagesRequest{Dir: matrix.Backward, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, raw := range page.Chunk {
		if strings.Contains(string(raw), `"`+proto.EventDisconnected+`"`) {
			n++
		}
	}
	return n
}

// sends are the message sends fakehs saw.
func (h *hs) sends() int {
	n := 0
	for _, r := range h.srv.Requests() {
		if r.Method == http.MethodPut && strings.Contains(r.Path, "/send/m.room.message/") {
			n++
		}
	}
	return n
}

// bundleFor is alpha's pool seat.json for the worker, its homeserver at base.
func bundleFor(t *testing.T, base, main string) seat.Bundle {
	t.Helper()
	issuer, err := paths.ParseDigest(kit)
	if err != nil {
		t.Fatal(err)
	}
	team, err := paths.ParseTeamID(hsTeam)
	if err != nil {
		t.Fatal(err)
	}
	return seat.Bundle{
		V: 1, Team: team, ServerName: hsServer, UserID: hsUser(hsWorker), AdminID: proto.AdminID(hsServer),
		MainRoom: main, BaseURL: base, PlainHTTPHosts: []string{"127.0.0.1"},
		DeviceID: paths.SeatDevice, IssuedMS: 1_700_000_000_000, Issuer: issuer,
	}
}

// writeSeat writes a seat directory dir, 0700, holding b and, when token is not empty, the
// token 0600.
func writeSeat(t *testing.T, dir string, b seat.Bundle, token string) {
	t.Helper()
	data, err := b.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "seat.json"), data)
	if token != "" {
		writeFile(t, filepath.Join(dir, "token"), []byte(token))
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
}

// deadURL is a loopback URL nothing listens on.
func deadURL(t *testing.T) string {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return "http://" + addr
}
