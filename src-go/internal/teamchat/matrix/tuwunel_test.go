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
	"net/http"
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
	presenceIdleS     = 3
	presenceOfflineS  = 3
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
// that the client meets, with room creation allowed so a member can make the test's room, and
// the login limits a v2 team gets (C 3.2.4: a seat's takes are password logins, which
// seatCycle makes six of). Two keys differ from a team's, for the test alone: db_pool_workers
// is fixed small, as Tuwunel's default pool, sized to the host's cores, hits EAGAIN under a
// container's pids limit (host check run, Task 1.4); and the presence timers are short, so
// presenceCycle sees them fire.
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
db_pool_workers = 4
presence_idle_timeout_s = %d
presence_offline_timeout_s = %d
log = "warn"

[rate_limiting.login.failed]
per_second = 0.0033
burst_count = 3

[rate_limiting.login.account]
per_second = 0.006
burst_count = 10
`, serverName, loopback, port, filepath.Join(dir, "db"), keyFile, presenceIdleS, presenceOfflineS)
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
	seatCycle(ctx, t, hs, admin, alice, room)
	presenceCycle(ctx, t, alice, bob, aliceID)
	retire(ctx, t, admin, bob, bobID)
}

// presenceOf is uid's presence as the observer reads it (GET /presence/{uid}/status, which
// the client has no caller for, so the test asks it through the client's own call path).
func presenceOf(ctx context.Context, t *testing.T, observer *Client, uid string) string {
	t.Helper()
	path, err := pathf(clientPrefix+"/presence/%s/status", uid)
	if err != nil {
		t.Fatalf("presence path: %v", err)
	}
	var got struct {
		Presence string `json:"presence"`
	}
	if err := observer.object(ctx, call{method: http.MethodGet, path: path}, &got); err != nil {
		t.Fatalf("read the presence of %s: %v", uid, err)
	}
	return got.Presence
}

// presenceCycle is host check HC-A1 as a lasting regression (A 6.6, C 3.2.4): a default sync
// marks the member online, a sync with set_presence=offline sets it offline (Tuwunel 1.9.4
// does not leave it unchanged), a PUT of offline sets it, and a member that stops syncing goes
// unavailable and then offline by the server's timers.
func presenceCycle(ctx context.Context, t *testing.T, member, observer *Client, uid string) {
	t.Helper()
	filter := &RoomFilter{Rooms: []string{}}
	syncAs := func(presence string) {
		t.Helper()
		if _, err := member.Sync(ctx, SyncRequest{Filter: filter, SetPresence: presence}); err != nil {
			t.Fatalf("Sync with set_presence %q: %v", presence, err)
		}
	}
	want := func(what, presence string) {
		t.Helper()
		if got := presenceOf(ctx, t, observer, uid); got != presence {
			t.Fatalf("presence %s = %q, want %q", what, got, presence)
		}
	}
	syncAs("")
	want("after a default sync", "online")
	syncAs("offline")
	want("after a sync with set_presence=offline", "offline")
	syncAs("")
	want("after a default sync again", "online")
	if err := member.SetPresence(ctx, uid, "offline"); err != nil {
		t.Fatalf("SetPresence: %v", err)
	}
	want("after a PUT of offline", "offline")
	syncAs("")
	want("after a default sync before the timers", "online")
	presenceTimers(ctx, t, observer, uid)
}

// presenceTimers polls the presence of a member that has stopped syncing until it is offline,
// and checks it went through unavailable and took no longer than the timers allow.
func presenceTimers(ctx context.Context, t *testing.T, observer *Client, uid string) {
	t.Helper()
	start := time.Now()
	limit := time.Duration(presenceIdleS+presenceOfflineS+10) * time.Second
	seen := []string{"online"}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for seen[len(seen)-1] != "offline" {
		select {
		case <-ctx.Done():
			t.Fatalf("presence timers: %v after %v", ctx.Err(), seen)
		case <-tick.C:
		}
		if now := presenceOf(ctx, t, observer, uid); now != seen[len(seen)-1] {
			seen = append(seen, now)
		}
		if took := time.Since(start); took > limit {
			t.Fatalf("presence %v after %v, want offline within %v", seen, took, limit)
		}
	}
	if !slices.Equal(seen, []string{"online", "unavailable", "offline"}) {
		t.Fatalf("presence went %v, want online, unavailable, offline", seen)
	}
	if took := time.Since(start); took < time.Duration(presenceIdleS+presenceOfflineS-1)*time.Second {
		t.Fatalf("offline after %v, before the timers (%d s and %d s) could fire", took, presenceIdleS, presenceOfflineS)
	}
}

// seatRun signs the account "seat" in and checks its tokens, for seatCycle.
type seatRun struct {
	t    *testing.T
	anon *Client
}

func (r seatRun) signIn(ctx context.Context, password, deviceID string) *Client {
	r.t.Helper()
	sess, err := r.anon.LoginDevice(ctx, "seat", password, deviceID)
	if err != nil {
		r.t.Fatalf("LoginDevice %s: %v", deviceID, err)
	}
	held, err := r.anon.WithToken(sess.AccessToken.Reveal())
	if err != nil {
		r.t.Fatalf("WithToken: %v", err)
	}
	r.t.Cleanup(held.Close)
	return held
}

// dead checks that held's token answers M_UNKNOWN_TOKEN.
func (r seatRun) dead(ctx context.Context, what string, held *Client) {
	r.t.Helper()
	_, err := held.Whoami(ctx)
	r.unknownToken(what, err)
}

// unknownToken checks that err is a token refused with M_UNKNOWN_TOKEN.
func (r seatRun) unknownToken(what string, err error) {
	r.t.Helper()
	if e, isErr := errors.AsType[*Error](err); !isErr || !errors.Is(err, ErrAuthRefused) || e.Errcode != "M_UNKNOWN_TOKEN" {
		r.t.Fatalf("%s = %v, want M_UNKNOWN_TOKEN", what, err)
	}
}

// deleteAndReset deletes the seat's device from another device by password, which ends the
// device's token and not the deleting one, then has that token delete its own device (the C
// 3.7.3 fallback), then resets the password, which ends a fresh token; it gives the new password.
func (r seatRun) deleteAndReset(ctx context.Context, admin *Admin, seatID, password, device string) string {
	r.t.Helper()
	held, taker := r.signIn(ctx, password, device), r.signIn(ctx, password, "TCTAKER")
	if err := taker.DeleteDevice(ctx, "seat", "wrong", device); !errors.Is(err, ErrForbidden) {
		r.t.Fatalf("a device delete with a wrong password = %v, want ErrForbidden", err)
	}
	if err := taker.DeleteDevice(ctx, "seat", password, device); err != nil {
		r.t.Fatalf("DeleteDevice: %v", err)
	}
	r.dead(ctx, "the token of the deleted device", held)
	if _, err := taker.Whoami(ctx); err != nil {
		r.t.Fatalf("the deleting token: %v", err)
	}
	if err := taker.DeleteDevice(ctx, "seat", password, "TCTAKER"); err != nil {
		r.t.Fatalf("DeleteDevice of its own device: %v", err)
	}
	r.dead(ctx, "a token that deleted its own device", taker)
	fresh := r.signIn(ctx, password, device)
	reissued := randomKey(r.t)
	if err := admin.ResetPassword(ctx, seatID, reissued); err != nil {
		r.t.Fatalf("ResetPassword: %v", err)
	}
	r.dead(ctx, "a token after a password reset", fresh)
	return reissued
}

// seatCycle is a pool seat's takes and releases (C 3.7.3, the facts host check H-C13 recorded):
// a second login as the seat's fixed device leaves the first token valid, a logout ends every
// token of the device, a device delete by password ends them from another device or its own, a
// password reset ends the rest, and the seat's teamchat.floor marker outlives them all.
func seatCycle(ctx context.Context, t *testing.T, hs tuwunel, admin *Admin, inviter *Client, room string) {
	t.Helper()
	seatID, password, device := "@seat:"+serverName, randomKey(t), "TCSEAT"
	if _, err := admin.CreateAccount(ctx, seatID, password, ""); err != nil {
		t.Fatalf("CreateAccount %s: %v", seatID, err)
	}
	if err := inviter.Invite(ctx, room, seatID); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	anon, err := NewMember(Config{BaseURL: hs.base, PlainHTTPHosts: []string{loopback}, Token: "unused"})
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	t.Cleanup(anon.Close)
	run := seatRun{t: t, anon: anon}
	first := run.signIn(ctx, password, device)
	if _, err = first.Join(ctx, room); err != nil {
		t.Fatalf("Join: %v", err)
	}
	marker := json.RawMessage(`{"floor_ms":1,"restore_epoch":0,"v":1}`)
	if err = first.PutRoomAccountData(ctx, seatID, room, "teamchat.floor", marker); err != nil {
		t.Fatalf("PutRoomAccountData: %v", err)
	}
	second := run.signIn(ctx, password, device)
	if _, err = first.Whoami(ctx); err != nil {
		t.Fatalf("the first token after a second login as the device: %v", err)
	}
	if _, err = anon.LoginDevice(ctx, "seat", "wrong", device); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a wrong password = %v, want ErrForbidden", err)
	}
	if err = second.Logout(ctx); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	run.dead(ctx, "the logged-out token", second)
	run.dead(ctx, "the other token of the logged-out device", first)
	run.unknownToken("a second logout", second.Logout(ctx))
	reissued := run.deleteAndReset(ctx, admin, seatID, password, device)
	content, found, err := run.signIn(ctx, reissued, device).RoomAccountData(ctx, seatID, room, "teamchat.floor")
	var read struct {
		FloorMS int64 `json:"floor_ms"`
	}
	if err != nil || !found || json.Unmarshal(content, &read) != nil || read.FloorMS != 1 {
		t.Fatalf("the marker after the releases and a reset = %s, %v, %v", content, found, err)
	}
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
