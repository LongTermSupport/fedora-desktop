//go:build tuwunel

// Package tuwunelhs starts a real Tuwunel for the tests built with `-tags tuwunel` (SPEC M44),
// with FAKEHS_TUWUNEL_BIN naming the binary: the matrix client's run and the rooms engine's.
// It imports nothing of the client, so the client's own tests can use it. It is the one
// test helper that runs on the wall clock, as a real server starts and answers in real time.
package tuwunelhs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const (
	// BinEnv names the Tuwunel binary; with it unset, Start skips the test.
	BinEnv = "FAKEHS_TUWUNEL_BIN"
	// Loopback is the address the server listens on.
	Loopback = "127.0.0.1"
	// StartLimit is how long the server has to answer.
	StartLimit = 30 * time.Second
	// PresenceIdleS is the presence idle timer, short so a test sees it fire.
	PresenceIdleS = 3
	// PresenceOfflineS is the presence offline timer, short so a test sees it fire.
	PresenceOfflineS = 3

	stopLimit    = 10 * time.Second
	versionsPath = "/_matrix/client/versions"
)

// Server is a running homeserver on a loopback port, stopped when the test ends; RegKey is its
// registration shared secret.
type Server struct {
	Base   string
	RegKey string
}

// served is how the server's run ended: its error and everything it printed.
type served struct {
	err    error
	output string
}

// Start runs the binary BinEnv names for serverName and waits until it answers.
func Start(ctx context.Context, t *testing.T, serverName string) Server {
	t.Helper()
	bin := os.Getenv(BinEnv)
	if bin == "" {
		t.Skipf("%s names no Tuwunel binary, so nothing is run against a real homeserver", BinEnv)
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		t.Fatalf("%s=%q: %v", BinEnv, bin, err)
	}
	dir := t.TempDir()
	hs := Server{RegKey: RandomKey(t)}
	port := freePort(t)
	hs.Base = "http://" + net.JoinHostPort(Loopback, strconv.Itoa(port))
	config := writeConfig(t, dir, serverName, port, hs.RegKey)

	runCtx, stop := context.WithCancel(ctx)
	ended := make(chan served, 1)
	go func() {
		res, runErr := execx.OS{Grace: stopLimit}.Run(runCtx, execx.Cmd{
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

// await polls the versions endpoint until the server answers 200, it exits (recorded in *end),
// or the start limit passes.
func (hs Server) await(ctx context.Context, ended <-chan served, end **served) error {
	ctx, cancel := context.WithTimeout(ctx, StartLimit)
	defer cancel()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if hs.answers(ctx) {
			return nil
		}
		select {
		case got := <-ended:
			*end = &got
			return fmt.Errorf("exited before it answered: %w", got.err)
		case <-ctx.Done():
			return fmt.Errorf("no answer within %v: %w", StartLimit, ctx.Err())
		case <-tick.C:
		}
	}
}

// answers is whether the versions endpoint answers 200; a refused connection is not yet.
func (hs Server) answers(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hs.Base+versionsPath, http.NoBody)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	return resp.Body.Close() == nil && resp.StatusCode == http.StatusOK
}

// RandomKey is 32 random bytes in hex, for a password or the registration key.
func RandomKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random key: %v", err)
	}
	return hex.EncodeToString(b)
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(Loopback, "0"))
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

// writeConfig renders the settings the installer gives a team (render.py toml_values) that the
// client meets, with room creation allowed so a member can make a room, and the login limits a
// v2 team gets (C 3.2.4: a seat's takes are password logins, which the client's seat cycle
// makes six of). Two keys differ from a team's, for the tests alone: db_pool_workers is fixed
// small, as Tuwunel's default pool, sized to the host's cores, hits EAGAIN under a container's
// pids limit (host check run, Task 1.4); and the presence timers are short.
func writeConfig(t *testing.T, dir, serverName string, port int, regKey string) string {
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
`, serverName, Loopback, port, filepath.Join(dir, "db"), keyFile, PresenceIdleS, PresenceOfflineS)
	path := filepath.Join(dir, "tuwunel.toml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
