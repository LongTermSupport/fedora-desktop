//go:build tuwunel

package matrix

// The client against a real Tuwunel (SPEC M44): `go test -tags tuwunel` with
// FAKEHS_TUWUNEL_BIN naming the binary. It checks that fakehs and the homeserver it stands in
// for agree on the paths the client takes; it is the one test here that runs on the wall
// clock, as a real server starts and answers in real time.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const (
	tuwunelBinEnv     = "FAKEHS_TUWUNEL_BIN"
	tuwunelStartLimit = 30 * time.Second
	tuwunelStopLimit  = 10 * time.Second
	tuwunelTestLimit  = 2 * time.Minute
	messageType       = "m.room.message"
)

// tuwunel is a running homeserver on a loopback port, stopped when the test ends; regKey is
// its registration shared secret.
type tuwunel struct {
	base   string
	regKey string
}

// served is how the server's run ended: its error and everything it printed.
type served struct {
	err    error
	output string
}

func startTuwunel(ctx context.Context, t *testing.T) tuwunel {
	t.Helper()
	bin := os.Getenv(tuwunelBinEnv)
	if bin == "" {
		t.Skipf("%s names no Tuwunel binary, so the client is not run against a real homeserver", tuwunelBinEnv)
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		t.Fatalf("%s=%q: %v", tuwunelBinEnv, bin, err)
	}
	dir := t.TempDir()
	hs := tuwunel{regKey: randomKey(t)}
	port := freePort(t)
	hs.base = "http://" + net.JoinHostPort(loopback, strconv.Itoa(port))
	config := writeTuwunelConfig(t, dir, port, hs.regKey)

	runCtx, stop := context.WithCancel(ctx)
	ended := make(chan served, 1)
	go func() {
		res, runErr := execx.OS{Grace: tuwunelStopLimit}.Run(runCtx, execx.Cmd{
			Name: path, Env: []string{"TUWUNEL_CONFIG=" + config}, Interrupt: syscall.SIGTERM,
		})
		ended <- served{err: runErr, output: string(res.Stdout) + string(res.Stderr)}
	}()
	var end *served
	t.Cleanup(func() {
		stop()
		if end == nil {
			got := <-ended
			end = &got
		}
		if !stoppedCleanly(end.err) {
			t.Errorf("tuwunel: %v\n%s", end.err, end.output)
		}
	})

	if awaitErr := hs.await(ctx, ended, &end); awaitErr != nil {
		output := ""
		if end != nil {
			output = end.output
		}
		t.Fatalf("tuwunel: %v\n%s", awaitErr, output)
	}
	return hs
}

// stoppedCleanly is a run that ended on the test's SIGTERM: exit 0 after the cancel (which
// exec reports as context.Canceled; the test's deadline is DeadlineExceeded and fails), or the
// signal itself.
func stoppedCleanly(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return true
	}
	exitErr, ok := errors.AsType[*execx.ExitError](err)
	return ok && exitErr.Signal == syscall.SIGTERM
}

// await polls the versions endpoint until the server answers, it exits (recorded in *end), or
// the start limit passes.
func (hs tuwunel) await(ctx context.Context, ended <-chan served, end **served) error {
	probe, err := NewAdmin(Config{BaseURL: hs.base})
	if err != nil {
		return err
	}
	defer probe.Close()
	ctx, cancel := context.WithTimeout(ctx, tuwunelStartLimit)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, versionsErr := probe.Versions(ctx); versionsErr == nil {
			return nil
		}
		select {
		case got := <-ended:
			*end = &got
			return fmt.Errorf("exited before it answered: %w", got.err)
		case <-ctx.Done():
			return fmt.Errorf("no answer within %v: %w", tuwunelStartLimit, ctx.Err())
		case <-tick.C:
		}
	}
}

// randomKey is 32 random bytes in hex, for a password or the registration key.
func randomKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random key: %v", err)
	}
	return hex.EncodeToString(b)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(loopback, "0"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", ln.Addr())
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr.Port
}

// writeTuwunelConfig renders the settings the installer gives a team (render.py toml_values)
// that the client meets, with room creation allowed so a member can make the test's room.
func writeTuwunelConfig(t *testing.T, dir string, port int, regKey string) string {
	t.Helper()
	keyFile := filepath.Join(dir, "registration-key")
	if err := os.WriteFile(keyFile, []byte(regKey), 0o600); err != nil {
		t.Fatalf("write the registration key: %v", err)
	}
	config := fmt.Sprintf(`server_name = %q
address = [%q]
port = %d
database_path = %q
allow_registration = false
allow_guest_registration = false
registration_shared_secret_file = %q
grant_admin_to_first_user = false
allow_federation = false
allow_encryption = false
allow_room_creation = true
auto_accept_invites = false
default_room_version = "12"
max_request_size = 10000000
log = "warn"
`, serverName, loopback, port, filepath.Join(dir, "db"), keyFile)
	path := filepath.Join(dir, "tuwunel.toml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// operator registers the server admin through the registration key and checks the admin API
// lists it.
func (hs tuwunel) operator(ctx context.Context, t *testing.T) *Admin {
	t.Helper()
	anon, err := NewAdmin(Config{BaseURL: hs.base})
	if err != nil {
		t.Fatalf("NewAdmin: %v", err)
	}
	t.Cleanup(anon.Close)
	sess, err := anon.Register(ctx, hs.regKey, Registration{Username: "operator", Password: randomKey(t), Admin: true})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	admin, err := anon.WithToken(sess.AccessToken.Reveal())
	if err != nil {
		t.Fatalf("WithToken: %v", err)
	}
	admins, err := admin.ServerAdmins(ctx)
	if err != nil {
		t.Fatalf("ServerAdmins: %v", err)
	}
	if !slices.Contains(admins, sess.UserID) {
		t.Fatalf("server admins %v lack %s", admins, sess.UserID)
	}
	return admin
}

// signedIn makes the account uid, reads it and its devices back, and gives a member client
// signed in as it through an admin-minted token.
func (hs tuwunel) signedIn(ctx context.Context, t *testing.T, admin *Admin, uid string) *Client {
	t.Helper()
	if _, err := admin.CreateAccount(ctx, uid, randomKey(t), ""); err != nil {
		t.Fatalf("CreateAccount %s: %v", uid, err)
	}
	acct, found, err := admin.Account(ctx, uid)
	if err != nil || !found || acct.UserID != uid || acct.Admin {
		t.Fatalf("Account %s = %+v, %v, %v", uid, acct, found, err)
	}
	token, err := admin.MintToken(ctx, uid)
	if err != nil {
		t.Fatalf("MintToken %s: %v", uid, err)
	}
	devices, err := admin.Devices(ctx, uid)
	if err != nil || len(devices) == 0 {
		t.Fatalf("Devices %s = %+v, %v; want the minted token's device", uid, devices, err)
	}
	c, err := NewMember(Config{BaseURL: hs.base, PlainHTTPHosts: []string{loopback}, Token: token.Reveal()})
	if err != nil {
		t.Fatalf("NewMember %s: %v", uid, err)
	}
	t.Cleanup(c.Close)
	who, err := c.Whoami(ctx)
	if err != nil || who.UserID != uid {
		t.Fatalf("Whoami = %+v, %v; want %s", who, err, uid)
	}
	return c
}

// sharedRoom is a room alice made and bob joined.
func sharedRoom(ctx context.Context, t *testing.T, alice, bob *Client, bobID string) string {
	t.Helper()
	room, err := alice.CreateRoom(ctx, CreateRoomRequest{Preset: "private_chat", Invite: []string{bobID}})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if _, joinErr := bob.Join(ctx, room); joinErr != nil {
		t.Fatalf("Join: %v", joinErr)
	}
	membership, err := alice.Membership(ctx, room, bobID)
	if err != nil || membership != "join" {
		t.Fatalf("Membership = %q, %v; want join", membership, err)
	}
	return room
}

// sendTwice sends one message twice under one transaction ID, which must give one event.
func sendTwice(ctx context.Context, t *testing.T, c *Client, room string) string {
	t.Helper()
	txn, err := NewTxnID()
	if err != nil {
		t.Fatalf("NewTxnID: %v", err)
	}
	content := json.RawMessage(`{"msgtype":"m.text","body":"hello"}`)
	sent, err := c.Send(ctx, room, messageType, txn, content)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	again, err := c.Send(ctx, room, messageType, txn, content)
	if err != nil || again != sent {
		t.Fatalf("Send with the same txn = %q, %v; want %q again", again, err, sent)
	}
	return sent
}

// countOf is how many of the events have the event ID.
func countOf(t *testing.T, events []json.RawMessage, eventID string) int {
	t.Helper()
	n := 0
	for _, raw := range events {
		var ev struct {
			EventID string `json:"event_id"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatalf("event %s: %v", raw, err)
		}
		if ev.EventID == eventID {
			n++
		}
	}
	return n
}

func TestTuwunel(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), tuwunelTestLimit)
	defer cancel()
	hs := startTuwunel(ctx, t)
	admin := hs.operator(ctx, t)
	aliceID, bobID := "@alice:"+serverName, "@bob:"+serverName
	alice := hs.signedIn(ctx, t, admin, aliceID)
	bob := hs.signedIn(ctx, t, admin, bobID)
	room := sharedRoom(ctx, t, alice, bob, bobID)
	sent := sendTwice(ctx, t, alice, room)

	filter := &RoomFilter{Rooms: []string{room}, TimelineTypes: []string{messageType}}
	synced, err := bob.Sync(ctx, SyncRequest{Filter: filter})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if n := countOf(t, synced.Join[room].Timeline.Events, sent); n != 1 {
		t.Fatalf("the sync timeline holds %s %d times, want once", sent, n)
	}
	page, err := bob.Messages(ctx, room, MessagesRequest{Dir: Backward, Limit: 10, Types: []string{messageType}})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if n := countOf(t, page.Chunk, sent); n != 1 {
		t.Fatalf("messages hold %s %d times, want once", sent, n)
	}
	if err := bob.ReadReceipt(ctx, room, sent); err != nil {
		t.Fatalf("ReadReceipt: %v", err)
	}
	retire(ctx, t, admin, bob, bobID)
}

// retire locks, unlocks and deactivates the account, reading each state back, and checks the
// deactivation revoked its token (fakehs models that much; what a locked or deactivated
// account's own requests are answered it leaves unmodelled).
func retire(ctx context.Context, t *testing.T, admin *Admin, c *Client, uid string) {
	t.Helper()
	for _, step := range []struct {
		name  string
		set   func() error
		check func(Account) bool
	}{
		{"lock", func() error { return admin.SetLocked(ctx, uid, true) }, func(a Account) bool { return a.Locked }},
		{"unlock", func() error { return admin.SetLocked(ctx, uid, false) }, func(a Account) bool { return !a.Locked }},
		{"deactivate", func() error { return admin.SetDeactivated(ctx, uid, true) }, func(a Account) bool { return a.Deactivated }},
	} {
		if err := step.set(); err != nil {
			t.Fatalf("%s %s: %v", step.name, uid, err)
		}
		acct, found, err := admin.Account(ctx, uid)
		if err != nil || !found || !step.check(acct) {
			t.Fatalf("after %s, Account %s = %+v, %v, %v", step.name, uid, acct, found, err)
		}
	}
	if _, err := c.Whoami(ctx); !errors.Is(err, ErrAuthRefused) {
		t.Fatalf("whoami after deactivation = %v, want the token refused", err)
	}
}
