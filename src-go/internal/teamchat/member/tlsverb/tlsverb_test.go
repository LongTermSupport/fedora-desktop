package tlsverb

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/tlsterm"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// `teamchat tls` is the one process of the unit teamchat-tls@<team> (Plan 00246 X18): the leaf
// and its key arrive as systemd credentials, the listen addresses and the port as the drop-in's
// arguments, and the terminator runs until the unit is stopped. These tests run the verb over a
// fake terminator, which records what it was given, and over a real signal.

const (
	certText = "the leaf certificate"
	keyText  = "the leaf key, whose bytes are secret"
)

// terminator is a fake tlsterm.Run: it records its config, says it has started, and ends with
// err, after ctx has ended when block is set.
type terminator struct {
	calls   int
	cfg     tlsterm.Config
	err     error
	block   bool
	started chan struct{}
}

func newTerminator() *terminator { return &terminator{started: make(chan struct{})} }

func (f *terminator) run(ctx context.Context, cfg tlsterm.Config) error {
	f.calls++
	f.cfg = cfg
	close(f.started)
	if f.block {
		<-ctx.Done()
	}
	return f.err
}

// credentials is a credentials directory holding the given files.
func credentials(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func good(t *testing.T) string {
	t.Helper()
	return credentials(t, map[string]string{paths.LeafCertCredential: certText, paths.LeafKeyCredential: keyText})
}

func parsed(t *testing.T, args ...string) cli.Invocation {
	t.Helper()
	inv, err := cli.Parse(append([]string{"tls"}, args...))
	if err != nil {
		t.Fatalf("%q: %v", args, err)
	}
	return inv
}

func env(dir string) (cli.Env, *bytes.Buffer) {
	var stderr bytes.Buffer
	return cli.Env{
		Stderr: &stderr, Getenv: func(k string) string {
			if k == paths.EnvCredentials {
				return dir
			}
			return ""
		},
	}, &stderr
}

// deps is the verb's seams with f as its terminator and a context nothing cancels.
func deps(f *terminator) runDeps {
	return runDeps{
		run:   f.run,
		clock: clock.NewFake(0, 0),
		notify: func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		},
	}
}

var listenArgs = []string{"--team", "ops", "--port", "8448", "--listen", "192.0.2.10", "--listen", "192.0.2.11"}

// V1: the terminator is given the leaf and its key, every listen address on the team's port,
// and the homeserver's loopback address on that port as its upstream.
func TestV1ItServesTheLeafOnEveryListenAddressToTheLoopbackHomeserver(t *testing.T) {
	f := newTerminator()
	e, stderr := env(good(t))
	if code := run(e, parsed(t, listenArgs...), deps(f)); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if f.calls != 1 {
		t.Fatalf("terminator run %d times", f.calls)
	}
	wantListen := []netip.AddrPort{netip.MustParseAddrPort("192.0.2.10:8448"), netip.MustParseAddrPort("192.0.2.11:8448")}
	if got := f.cfg.Listen; len(got) != 2 || got[0] != wantListen[0] || got[1] != wantListen[1] {
		t.Errorf("listen %v, want %v", got, wantListen)
	}
	if got, want := f.cfg.Upstream, netip.MustParseAddrPort("127.0.0.1:8448"); got != want {
		t.Errorf("upstream %v, want %v", got, want)
	}
	if string(f.cfg.CertPEM) != certText || string(f.cfg.KeyPEM) != keyText {
		t.Errorf("the credentials were not passed as read: cert %q key %q", f.cfg.CertPEM, f.cfg.KeyPEM)
	}
	if f.cfg.Clock == nil {
		t.Error("the terminator has no clock for its bounds")
	}
	if f.cfg.Log != stderr {
		t.Error("the terminator's log is not the unit's stderr, the journal")
	}
	if stderr.Len() != 0 {
		t.Errorf("a clean run wrote %q", stderr)
	}
}

// V2: no credentials directory (the unit has no LoadCredential) is refused before anything is
// served.
func TestV2NoCredentialsDirectoryIsRefused(t *testing.T) {
	f := newTerminator()
	e, stderr := env("")
	if code := run(e, parsed(t, listenArgs...), deps(f)); code != term.ExitCode(78) {
		t.Fatalf("exit %d, want 78: %s", code, stderr)
	}
	if f.calls != 0 {
		t.Error("the terminator ran with no credentials")
	}
	if !strings.Contains(stderr.String(), `"code":"runtime"`) || !strings.Contains(stderr.String(), paths.EnvCredentials) {
		t.Errorf("the refusal does not say what is missing: %s", stderr)
	}
}

// V3: a credential that is missing or empty is refused naming it, and the key's bytes are never
// written.
func TestV3AMissingOrEmptyCredentialIsRefusedByName(t *testing.T) {
	cert, key := paths.LeafCertCredential, paths.LeafKeyCredential
	cases := map[string]struct {
		files map[string]string
		named string
	}{
		"no cert":    {map[string]string{key: keyText}, cert},
		"no key":     {map[string]string{cert: certText}, key},
		"empty key":  {map[string]string{cert: certText, key: ""}, key},
		"empty cert": {map[string]string{cert: "", key: keyText}, cert},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newTerminator()
			e, stderr := env(credentials(t, c.files))
			if code := run(e, parsed(t, listenArgs...), deps(f)); code != term.ExitCode(78) {
				t.Fatalf("exit %d, want 78: %s", code, stderr)
			}
			if f.calls != 0 {
				t.Error("the terminator ran without both credentials")
			}
			if !strings.Contains(stderr.String(), c.named) {
				t.Errorf("the refusal does not name %s: %s", c.named, stderr)
			}
			if strings.Contains(stderr.String(), "secret") {
				t.Errorf("the key was written to stderr: %s", stderr)
			}
		})
	}
}

// V4: a terminator that fails is the verb's failure, with its reason on stderr, so the unit
// fails and its journal says why.
func TestV4AFailedTerminatorIsTheVerbsFailure(t *testing.T) {
	f := newTerminator()
	f.err = errors.New("tlsterm: listening on 192.0.2.10:8448: address in use")
	e, stderr := env(good(t))
	if code := run(e, parsed(t, listenArgs...), deps(f)); code == term.ExitOK {
		t.Fatal("a failed terminator exited 0")
	}
	if !strings.Contains(stderr.String(), "address in use") {
		t.Errorf("the reason is not on stderr: %s", stderr)
	}
}

// V5: the unit's stop (SIGTERM) ends the run, through the production signal wiring, and the
// verb answers 0. The signal is sent once the terminator has started, which is after the
// wiring is in place, so it cannot take the test process.
func TestV5TheStopSignalEndsTheRun(t *testing.T) {
	f := newTerminator()
	f.block = true
	e, stderr := env(good(t))
	d := deps(f)
	d.notify = stopOnSignal
	done := make(chan term.ExitCode, 1)
	go func() { done <- run(e, parsed(t, listenArgs...), d) }()
	<-f.started
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := <-done; code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
}
