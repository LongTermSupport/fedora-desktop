package lxccmd

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func info(f *execx.Fake) LXC { return LXC{Runner: f} }

func TestContainers(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"lxc-ls", "-1"}, execx.Result{Stdout: []byte("b-1\na.x\n")})
	got, err := info(f).Containers(t.Context())
	if err != nil || !slices.Equal(got, []string{"b-1", "a.x"}) {
		t.Fatalf("%v %v", got, err)
	}
	f.On([]string{"lxc-ls", "-1"}, execx.Result{})
	if none, nerr := info(f).Containers(t.Context()); nerr != nil || len(none) != 0 {
		t.Fatalf("none: %v %v", none, nerr)
	}
	for _, out := range []string{"a b\n", "../x\n", "a\n\nb\n", "a\na\n", "-a\n", "a\x1b[31m\n", strings.Repeat("a", 65) + "\n"} {
		f.On([]string{"lxc-ls", "-1"}, execx.Result{Stdout: []byte(out)})
		if _, cerr := info(f).Containers(t.Context()); !errors.Is(cerr, ErrOutput) || !strings.Contains(cerr.Error(), "lxc-ls") {
			t.Errorf("%q: %v", out, cerr)
		}
	}
	f.On([]string{"lxc-ls", "-1"}, execx.Result{ExitCode: 1, Stderr: []byte("no lxcpath\n")})
	if _, ferr := info(f).Containers(t.Context()); ferr == nil || !strings.Contains(ferr.Error(), "lxc-ls") || !strings.Contains(ferr.Error(), "no lxcpath") {
		t.Errorf("a failing lxc-ls: %v", ferr)
	}
}

func TestState(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"lxc-info", "-n", "c1", "-sH"}, execx.Result{Stdout: []byte("RUNNING\n")})
	f.On([]string{"lxc-info", "-n", "c2", "-sH"}, execx.Result{Stdout: []byte("STOPPED\n")})
	f.On([]string{"lxc-info", "-n", "c3", "-sH"}, execx.Result{Stdout: []byte("WEIRD\n")})
	f.On([]string{"lxc-info", "-n", "c4", "-sH"}, execx.Result{Stdout: []byte("RUNNING\nSTOPPED\n")})
	f.On([]string{"lxc-info", "-n", "c5", "-sH"}, execx.Result{ExitCode: 1, Stderr: []byte("c5 doesn't exist\x1b[2J\n")})
	l := info(f)
	if s, err := l.State(t.Context(), "c1"); err != nil || s != Running {
		t.Fatalf("%v %v", s, err)
	}
	if s, err := l.State(t.Context(), "c2"); err != nil || s != Stopped {
		t.Fatalf("%v %v", s, err)
	}
	for _, c := range []string{"c3", "c4"} {
		if _, err := l.State(t.Context(), c); !errors.Is(err, ErrOutput) || !strings.Contains(err.Error(), c) {
			t.Errorf("%s: %v", c, err)
		}
	}
	_, err := l.State(t.Context(), "c5")
	if err == nil || errors.Is(err, ErrOutput) || strings.Contains(err.Error(), "\x1b") || !strings.Contains(err.Error(), "lxc-info") {
		t.Errorf("a failing lxc-info: %q", err)
	}
	before := len(f.Calls())
	if _, berr := l.State(t.Context(), "a b"); !errors.Is(berr, ErrRefused) {
		t.Errorf("a bad name: %v", berr)
	}
	if len(f.Calls()) != before {
		t.Error("ran for a bad name")
	}
}

func TestInitPID(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"lxc-info", "-n", "c", "-pH"}, execx.Result{Stdout: []byte("1234\n")})
	if pid, err := info(f).InitPID(t.Context(), "c"); err != nil || pid != 1234 {
		t.Fatalf("%d %v", pid, err)
	}
	f.On([]string{"lxc-info", "-n", "c", "-pH"}, execx.Result{Stdout: []byte("4194304\n")})
	if pid, err := info(f).InitPID(t.Context(), "c"); err != nil || pid != pidMax {
		t.Fatalf("at the ceiling: %d %v", pid, err)
	}
	for _, out := range []string{"", "\n", "1\n", "0\n", "-5\n", "+5\n", "4194305\n", "12 13\n", "12\n13\n", "0x10\n", "012\n", "99999999999999999999\n"} {
		f.On([]string{"lxc-info", "-n", "c", "-pH"}, execx.Result{Stdout: []byte(out)})
		if _, err := info(f).InitPID(t.Context(), "c"); !errors.Is(err, ErrOutput) {
			t.Errorf("%q: %v", out, err)
		}
	}
}

func TestAddresses(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"lxc-info", "-n", "c", "-iH"}, execx.Result{Stdout: []byte("192.0.2.5\n2001:db8::5\n")})
	got, err := info(f).Addresses(t.Context(), "c")
	want := []netip.Addr{netip.MustParseAddr("192.0.2.5"), netip.MustParseAddr("2001:db8::5")}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("%v %v", got, err)
	}
	f.On([]string{"lxc-info", "-n", "c", "-iH"}, execx.Result{})
	if none, nerr := info(f).Addresses(t.Context(), "c"); nerr != nil || len(none) != 0 {
		t.Fatalf("none: %v %v", none, nerr)
	}
	for _, out := range []string{"x\n", "192.0.2.5 \n", "2001:db8:0::5\n", "192.0.2.5\n\n"} {
		f.On([]string{"lxc-info", "-n", "c", "-iH"}, execx.Result{Stdout: []byte(out)})
		if _, aerr := info(f).Addresses(t.Context(), "c"); !errors.Is(aerr, ErrOutput) {
			t.Errorf("%q: %v", out, aerr)
		}
	}
}

func TestConfig(t *testing.T) {
	f := &execx.Fake{}
	argv := []string{"lxc-info", "-n", "c", "-c", "lxc.start.auto", "-c", "lxc.idmap", "-c", "lxc.net.0.link"}
	f.On(argv, execx.Result{Stdout: []byte(
		"lxc.start.auto = 1\nlxc.idmap = u 0 100000 65536\ng 0 100000 65536\n\nlxc.net.0.link = lxcbr0\n")})
	l := info(f)
	got, err := l.Config(t.Context(), "c", "lxc.start.auto", "lxc.idmap", "lxc.net.0.link")
	if err != nil {
		t.Fatal(err)
	}
	if got["lxc.start.auto"] != "1" || got["lxc.idmap"] != "u 0 100000 65536\ng 0 100000 65536" || got["lxc.net.0.link"] != "lxcbr0" || len(got) != 3 {
		t.Fatalf("%q", got)
	}
	f.On([]string{"lxc-info", "-n", "c", "-c", "lxc.idmap"}, execx.Result{Stdout: []byte("lxc.idmap = \n")})
	if unset, uerr := l.Config(t.Context(), "c", "lxc.idmap"); uerr != nil || unset["lxc.idmap"] != "" || len(unset) != 1 {
		t.Fatalf("an unset key: %q %v", unset, uerr)
	}
}

func TestConfigRefusesWhatIsNotItsGrammar(t *testing.T) {
	f := &execx.Fake{}
	l := info(f)
	bad := map[string]string{
		"a key missing":             "lxc.start.auto = 1\n",
		"a key not asked for":       "lxc.start.auto = 1\nlxc.idmap = \nlxc.rootfs.path = /x\n",
		"a key twice":               "lxc.start.auto = 1\nlxc.start.auto = 0\nlxc.idmap = \n",
		"a value line before a key": "u 0 1 1\nlxc.start.auto = 1\nlxc.idmap = \n",
		"a control":                 "lxc.start.auto = 1\x1b\nlxc.idmap = \n",
		"a carriage return":         "lxc.start.auto = 1\r\nlxc.idmap = \n",
	}
	for name, out := range bad {
		f.On([]string{"lxc-info", "-n", "c", "-c", "lxc.start.auto", "-c", "lxc.idmap"}, execx.Result{Stdout: []byte(out)})
		if _, err := l.Config(t.Context(), "c", "lxc.start.auto", "lxc.idmap"); !errors.Is(err, ErrOutput) {
			t.Errorf("%s: %v", name, err)
		}
	}
	before := len(f.Calls())
	for _, keys := range [][]string{nil, {"start.auto"}, {"lxc.start.auto", "lxc.start.auto"}, {"lxc.a b"}, {"-lxc"}} {
		if _, err := l.Config(t.Context(), "c", keys...); !errors.Is(err, ErrRefused) {
			t.Errorf("keys %q: %v", keys, err)
		}
	}
	if len(f.Calls()) != before {
		t.Error("ran for bad keys")
	}
}

func TestAProgramThatCannotStart(t *testing.T) {
	f := &execx.Fake{}
	boom := errors.New("boom")
	f.OnError([]string{"lxc-info"}, boom)
	f.OnError([]string{"lxc-ls"}, boom)
	l := info(f)
	if _, err := l.State(t.Context(), "c"); !errors.Is(err, boom) {
		t.Errorf("State: %v", err)
	}
	if _, err := l.InitPID(t.Context(), "c"); !errors.Is(err, boom) {
		t.Errorf("InitPID: %v", err)
	}
	if _, err := l.Addresses(t.Context(), "c"); !errors.Is(err, boom) {
		t.Errorf("Addresses: %v", err)
	}
	if _, err := l.Config(t.Context(), "c", "lxc.idmap"); !errors.Is(err, boom) {
		t.Errorf("Config: %v", err)
	}
	if _, err := l.Containers(t.Context()); !errors.Is(err, boom) {
		t.Errorf("Containers: %v", err)
	}
}

func TestContainerNameGrammar(t *testing.T) {
	for _, ok := range []string{"a", "A9", "a_b.c-d", strings.Repeat("a", 64)} {
		if err := checkContainer(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-a", ".a", "_a", "a/b", "a b", strings.Repeat("a", 65), "a\n"} {
		if err := checkContainer(bad); !errors.Is(err, ErrRefused) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
