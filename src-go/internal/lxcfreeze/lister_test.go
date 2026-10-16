package lxcfreeze

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

var errNotFound = errors.New("not found")

func newLister(f *execx.Fake) *Lister {
	return &Lister{
		Sudo:     newSudo(f, false),
		LookPath: func(name string) (string, error) { return "/usr/bin/" + name, nil },
	}
}

func TestListerSplitsRunningFromStoppedContainers(t *testing.T) {
	f := &execx.Fake{}
	// The last rule that matches answers, so the narrower one comes last.
	f.On([]string{"sudo", "-n", "lxc-ls", "-1"}, execx.Result{Stdout: []byte("box-a\nbox-b\nbox-c\n\n")})
	f.On([]string{"sudo", "-n", "lxc-ls", "-1", "--running"}, execx.Result{Stdout: []byte("box-b\nbox-a\n")})
	running, stopped, err := newLister(f).Containers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "running", running, []string{"box-a", "box-b"})
	eqStrings(t, "stopped", stopped, []string{"box-c"})
}

func TestListerSaysLxcIsNotInstalledNamingTheMissingTools(t *testing.T) {
	l := newLister(&execx.Fake{})
	l.LookPath = func(name string) (string, error) {
		if name == "lxc-ls" || name == "lxc-attach" {
			return "", errNotFound
		}
		return "/usr/bin/" + name, nil
	}
	_, _, err := l.Containers(context.Background())
	if err == nil {
		t.Fatal("no complaint")
	}
	for _, want := range []string{"LXC is not installed on this host", "lxc-ls lxc-attach", "play-lxc-install-config.yml"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%q lacks %q", err.Error(), want)
		}
	}
}

func TestListerAFailedListIsAnErrorNotAnEmptyMachine(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-ls"}, exit1("lxc-ls: boom\n"))
	_, _, err := newLister(f).Containers(context.Background())
	if err == nil || !strings.Contains(err.Error(), "lxc-ls failed") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v", err)
	}
}

const passwd = "root:x:0:0:root:/root:/bin/bash\n" +
	"daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin\n" +
	"agent:x:1000:1000:Agent,,,:/srv/agent:/bin/bash\n" +
	"svc:x:1001:1001::/var/lib/svc:/bin/false\n" +
	"other:x:1002:1002::/srv/other:/bin/sh\n" +
	"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\n"

func TestListerUsersAreTheOrdinaryOnesWithAShell(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{
		"sudo", "-n", "lxc-attach", "-n", "box-a", "--clear-env", "--", "env", "-i",
		"PATH=/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin", "HOME=/root", "getent", "passwd",
	}, execx.Result{Stdout: []byte(passwd)})
	got, err := newLister(f).Users(context.Background(), "box-a")
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "users", got, []string{"agent", "other"})
}

func TestListerUsersOnlyNamesThePlayAcceptsReachTheTerminal(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-attach", "-n", "box-a"}, execx.Result{Stdout: []byte(
		"ok:x:1003:1003::/h:/bin/bash\n" +
			"Upper:x:1004:1004::/h:/bin/bash\n" +
			"esc\x1b[2J:x:1005:1005::/h:/bin/bash\n" +
			"two words:x:1006:1006::/h:/bin/bash\n" +
			"-lead:x:1007:1007::/h:/bin/bash\n" +
			strings.Repeat("a", 33) + ":x:1008:1008::/h:/bin/bash\n" +
			"_under-score9:x:1009:1009::/h:/bin/bash\n")})
	got, err := newLister(f).Users(context.Background(), "box-a")
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "users", got, []string{"ok", "_under-score9"})
}

func TestListerUsersSkipsMalformedLinesAndFailsOnAFailedRead(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-attach", "-n", "box-a"},
		execx.Result{Stdout: []byte("junk\nagent:x:notanumber:1::/h:/bin/bash\nok:x:1003:1003::/h:/bin/bash\n")})
	got, err := newLister(f).Users(context.Background(), "box-a")
	if err != nil {
		t.Fatal(err)
	}
	eqStrings(t, "users", got, []string{"ok"})

	g := &execx.Fake{}
	g.On([]string{"sudo", "-n", "lxc-attach", "-n", "box-a"}, exit1("lxc-attach: box-a: not running\n"))
	if _, err = newLister(g).Users(context.Background(), "box-a"); err == nil ||
		!strings.Contains(err.Error(), "box-a") || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("got %v", err)
	}
}

const readerScript = "import sys\nprint(sys.argv)\n"

// reader answers the two commands ReadCheckout runs: where LXC keeps its containers, and the
// host-side reader run as root with the script as one argument (no shell, no file).
func reader(f *execx.Fake, lxcPath string, out execx.Result) {
	f.On([]string{"sudo", "-n", "lxc-config", "lxc.lxcpath"}, execx.Result{Stdout: []byte(lxcPath)})
	f.On([]string{"sudo", "-n", "python3", "-I", "-c", readerScript, "probe", "/var/lib/lxc", "box-a", "agent", "/srv/p"}, out)
}

func TestListerReadsACheckoutFromTheHostSideWithAnIsolatedPythonAndNoShell(t *testing.T) {
	f := &execx.Fake{}
	reader(f, "/var/lib/lxc\n", execx.Result{Stdout: []byte(`{"path": "/srv/p"}`)})
	got, err := newLister(f).ReadCheckout(context.Background(), readerScript, "probe", "box-a", "agent", "/srv/p")
	if err != nil || got != `{"path": "/srv/p"}` {
		t.Fatalf("%q %v", got, err)
	}
	for _, c := range f.Calls() {
		if strings.Contains(c.Line(), "lxc-attach") || strings.Contains(c.Line(), " sh ") {
			t.Fatalf("the container was entered or a shell was used: %q", c.Line())
		}
	}
}

func TestListerReadCheckoutAFailureCarriesTheReadersWords(t *testing.T) {
	f := &execx.Fake{}
	reader(f, "/var/lib/lxc\n", exit1("/srv/p/.git/config is a symbolic link; the container's tree is not followed through one\n"))
	_, err := newLister(f).ReadCheckout(context.Background(), readerScript, "probe", "box-a", "agent", "/srv/p")
	if err == nil || !strings.Contains(err.Error(), "symbolic link") || !strings.Contains(err.Error(), "box-a") {
		t.Fatalf("got %v", err)
	}
}

func TestListerLxcPathIsWhereRootsLxcKeepsItsContainers(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-config", "lxc.lxcpath"}, execx.Result{Stdout: []byte("/var/lib/lxc\n")})
	got, err := newLister(f).LxcPath(context.Background())
	if err != nil || got != "/var/lib/lxc" {
		t.Fatalf("%q %v", got, err)
	}
	g := &execx.Fake{}
	g.On([]string{"sudo", "-n", "lxc-config", "lxc.lxcpath"}, execx.Result{Stdout: []byte("relative\n")})
	if _, err = newLister(g).LxcPath(context.Background()); err == nil {
		t.Fatal("a relative lxcpath was accepted")
	}
}

func TestListerReadCheckoutAFailedHomeSearchNamesTheHomeNotADash(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"sudo", "-n", "lxc-config", "lxc.lxcpath"}, execx.Result{Stdout: []byte("/var/lib/lxc\n")})
	f.On([]string{"sudo", "-n", "python3", "-I", "-c", readerScript, "checkouts", "/var/lib/lxc", "box-a", "agent", "-"},
		exit1("container box-a has no /etc/passwd\n"))
	_, err := newLister(f).ReadCheckout(context.Background(), readerScript, "checkouts", "box-a", "agent", "-")
	if err == nil || !strings.HasPrefix(err.Error(), "the home of agent@box-a could not be read from the host: ") {
		t.Fatalf("got %v", err)
	}
}

func TestListerReadCheckoutAFailureCannotSteerTheTerminal(t *testing.T) {
	f := &execx.Fake{}
	reader(f, "/var/lib/lxc\n", exit1("/srv/p/\x1b[2J\x07 is a symbolic link\n"))
	_, err := newLister(f).ReadCheckout(context.Background(), readerScript, "probe", "box-a", "agent", "/srv/p")
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\x07") || !strings.Contains(err.Error(), "is a symbolic link") {
		t.Fatalf("got %q", err)
	}
}

func TestListerReadCheckoutRefusesALxcPathThatIsNotAPath(t *testing.T) {
	for _, printed := range []string{"relative\n", "\n", "/var/lib/\x1b[2Jlxc\n"} {
		f := &execx.Fake{}
		f.On([]string{"sudo", "-n", "lxc-config", "lxc.lxcpath"}, execx.Result{Stdout: []byte(printed)})
		if _, err := newLister(f).ReadCheckout(context.Background(), readerScript, "probe", "box-a", "agent", "/srv/p"); err == nil {
			t.Fatalf("%q was accepted as LXC's path", printed)
		}
	}
}

func TestListerReadCheckoutNamesAMissingPython(t *testing.T) {
	l := newLister(&execx.Fake{})
	l.LookPath = func(name string) (string, error) {
		if name == "python3" {
			return "", errNotFound
		}
		return "/usr/bin/" + name, nil
	}
	_, err := l.ReadCheckout(context.Background(), readerScript, "probe", "box-a", "agent", "/srv/p")
	if err == nil || !strings.Contains(err.Error(), "python3") {
		t.Fatalf("got %v", err)
	}
}
