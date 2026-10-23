package link

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestTheRunDirectoryNamesTheLayoutAndTheRun(t *testing.T) {
	ccy := paths.CCYLayout()
	h := newHome(t)
	env := map[string]string{"HOME": h.dir, "XDG_RUNTIME_DIR": h.dir + "/r"}
	for name, c := range map[string]struct {
		dir  string
		env  map[string]string
		want paths.Layout
	}{
		"a ccy session":     {ccy.Run(h.run).Dir(), env, ccy},
		"a home member":     {h.layout.Run(h.run).Dir(), env, h.layout},
		"a ccy without env": {ccy.Run(h.run).Dir(), map[string]string{}, ccy},
	} {
		layout, run, err := layoutFor(c.dir, "", func(k string) string { return c.env[k] })
		check(t, err)
		if run != h.run || layout.Run(run).Dir() != c.dir || layout.State() != c.want.State() {
			t.Fatalf("%s: layout %s run %s", name, layout.Run(run).Dir(), run)
		}
	}
	for name, dir := range map[string]string{
		"another directory":  h.dir + "/elsewhere/" + h.run.String(),
		"not a run id":       h.layout.RunRoot() + "/not-a-run",
		"no home to compare": h.layout.Run(h.run).Dir() + "x",
	} {
		_, _, err := layoutFor(dir, "", func(k string) string { return env[k] })
		if xe, ok := errors.AsType[*exits.Error](err); !ok || xe.Code != exits.CodeUsage {
			t.Fatalf("%s: err %v", name, err)
		}
	}
}

// The keeper passes run's --state on, so the link reads the seats it holds (RN1).
func TestStateIsTheKeepersStateRoot(t *testing.T) {
	h := newHome(t)
	env := map[string]string{"HOME": h.dir, "XDG_RUNTIME_DIR": h.dir + "/r"}
	layout, _, err := layoutFor(h.layout.Run(h.run).Dir(), "/srv/state", func(k string) string { return env[k] })
	check(t, err)
	if layout.State() != "/srv/state" || layout.Seat(tid(t, "alpha")).Dir() != "/srv/state/alpha" {
		t.Fatalf("state %s", layout.State())
	}
	_, _, err = layoutFor(h.layout.Run(h.run).Dir(), "state", func(k string) string { return env[k] })
	if xe, ok := errors.AsType[*exits.Error](err); !ok || xe.Code != exits.CodeUsage {
		t.Fatalf("a relative state: err %v", err)
	}
}

func TestTheControlSocketsPeerMustBeTheParent(t *testing.T) {
	keeperEnd, linkEnd, err := wire.Pair()
	check(t, err)
	defer func() { check(t, keeperEnd.Close()) }()
	c, err := control(linkEnd, os.Getpid())
	check(t, err)
	check(t, c.Close())

	keeperEnd2, linkEnd2, err := wire.Pair()
	check(t, err)
	defer func() { check(t, keeperEnd2.Close()) }()
	if _, err = control(linkEnd2, os.Getpid()+1); err == nil {
		t.Fatal("a peer that is not the parent was taken")
	}
	notSocket, err := os.Open(os.DevNull)
	check(t, err)
	if _, err = control(notSocket, os.Getpid()); err == nil {
		t.Fatal("a descriptor that is not a socket was taken")
	}
}

func TestMainRefusesWhatIsNotTheKeepersStart(t *testing.T) {
	h := newHome(t)
	env := map[string]string{"HOME": h.dir, "XDG_RUNTIME_DIR": h.dir + "/r"}
	for name, c := range map[string]struct {
		dir, digest string
		want        term.ExitCode
	}{
		"not a run directory": {h.dir + "/elsewhere/" + h.run.String(), kitHex, exits.ExitUsage},
		"no digest":           {h.layout.Run(h.run).Dir(), "", exits.ExitConfig},
	} {
		inv, err := cli.Parse([]string{"link", "--run", c.dir})
		check(t, err)
		var stderr bytes.Buffer
		e := cli.Env{Stderr: &stderr, Digest: c.digest, Getenv: func(k string) string { return env[k] }}
		if code := Main(Wiring{})(e, inv); code != c.want || stderr.Len() == 0 {
			t.Fatalf("%s: exit %d, stderr %q", name, code, stderr.String())
		}
	}
}
