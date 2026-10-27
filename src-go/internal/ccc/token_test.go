package ccc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestPickTokenTakesTheLatestUsableFileOfTheName(t *testing.T) {
	files := []string{"ccc.2026-10-27.token", "ccc.2027-01-01.token", "ccc.2027-03-01.token", "cccx.2028-01-01.token", "ccc.token", "work.2028-01-01.token"}
	got, ok := pickToken(files, "ccc", testNow)
	if !ok || got.FileName != "ccc.2027-03-01.token" {
		t.Fatalf("got %+v %v", got, ok)
	}
	if _, ok := pickToken([]string{"ccc.2026-10-27.token"}, "ccc", testNow); ok {
		t.Fatal("a token expiring today was picked")
	}
}

func TestReadTokenRefusesAFileThatIsNotAToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.token")
	if err := os.WriteFile(path, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readToken(path); err == nil {
		t.Fatal("want an error")
	}
	if _, err := readToken(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("want an error for a missing file")
	}
}

func TestExtractTokenSeesThroughTerminalEscapes(t *testing.T) {
	out := "Your token:\r\n\x1b[1m" + testToken + "\x1b[0m\r\nStore it safely"
	if got, ok := extractToken(out); !ok || got != testToken {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := extractToken("Login cancelled"); ok {
		t.Fatal("found a token in nothing")
	}
}

func TestSaveTokenWritesAPrivateDatedFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pool")
	path, err := saveToken(dir, "ccc", testToken, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "ccc.2027-01-25.token" {
		t.Fatalf("path %s", path)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", info, err)
	}
	if got, err := readToken(path); err != nil || got != testToken {
		t.Fatalf("read back %q %v", got, err)
	}
}

func TestSaveTokenReportsAPoolItCannotMake(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := saveToken(filepath.Join(file, "pool"), "ccc", testToken, testNow); err == nil {
		t.Fatal("want an error")
	}
}

func TestAPoolThatCannotBeSavedToFailsTheSignIn(t *testing.T) {
	w := newWorld(t)
	if err := os.MkdirAll(filepath.Join(w.home, ".claude-tokens"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.home, ".claude-tokens", "ccy"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	signIn(w, testToken+"\n")
	if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}

func TestASignInThatCannotStartFails(t *testing.T) {
	w := newWorld(t)
	w.run.OnFunc([]string{"podman", "run"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{}, os.ErrPermission
	})
	if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
}

func signIn(w *world, outputs ...string) {
	n := 0
	w.run.OnFunc([]string{"podman", "run"}, func(c execx.Cmd) (execx.Result, error) {
		out := outputs[min(n, len(outputs)-1)]
		n++
		if _, err := c.Stdout.Write([]byte(out)); err != nil {
			return execx.Result{}, err
		}
		return execx.Result{}, nil
	})
}

func TestFirstRunSignsInAndKeepsTheToken(t *testing.T) {
	w := newWorld(t)
	signIn(w, "token: "+testToken+"\n")
	if code := w.app.Run(context.Background(), nil); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr)
	}
	fg := w.run.Foregrounds()
	if len(fg) != 1 || !strings.Contains(fg[0].Line(), "--entrypoint claude") || !strings.HasSuffix(fg[0].Line(), "setup-token") {
		t.Fatalf("sign-in %v", fg)
	}
	if !w.guard.cmds[0].Secrets[0].Is(testToken) {
		t.Fatal("the new token was not used")
	}
	if _, err := os.Stat(filepath.Join(poolDir(w.home), "ccc.2027-01-25.token")); err != nil {
		t.Fatal(err)
	}
}

func TestAFailedSignInIsOfferedAgainThenGivenUp(t *testing.T) {
	w := newWorld(t)
	w.app.Stdin = strings.NewReader("\ny\n")
	signIn(w, "cancelled\n")
	if code := w.app.Run(context.Background(), nil); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if got := len(w.run.Foregrounds()); got != maxSignIns {
		t.Fatalf("sign-ins %d", got)
	}
	if len(w.guard.cmds) != 0 {
		t.Fatal("a session started without a token")
	}
}

func TestASignInRetrySucceeds(t *testing.T) {
	w := newWorld(t)
	w.app.Stdin = strings.NewReader("yes\n")
	signIn(w, "cancelled\n", testToken+"\n")
	if code := w.app.Run(context.Background(), nil); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, w.stderr)
	}
}

func TestDecliningARetryCancels(t *testing.T) {
	w := newWorld(t)
	w.app.Stdin = strings.NewReader("maybe\nn\n")
	signIn(w, "cancelled\n")
	if code := w.app.Run(context.Background(), nil); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), "Please answer") {
		t.Fatalf("stderr %q", w.stderr)
	}
}

func TestEndOfInputAtTheRetryCancels(t *testing.T) {
	w := newWorld(t)
	signIn(w, "cancelled\n")
	if code := w.app.Run(context.Background(), nil); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
}

func TestASoonExpiringTokenIsMentioned(t *testing.T) {
	w := newWorld(t)
	w.putToken(t, "ccc.2026-11-03.token")
	if code := w.app.Run(context.Background(), nil); code != term.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.stderr.String(), "expires in 7 days") {
		t.Fatalf("stderr %q", w.stderr)
	}
}
