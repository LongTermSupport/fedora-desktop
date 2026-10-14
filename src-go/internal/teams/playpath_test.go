package teams

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// A play runs under Ansible with the user's sudo, so a path the environment supplies must not
// be run unless it is the checkout's own play or the environment is deliberately trusted.
func TestAPlayOutsideTheCheckoutIsRefusedAndNotRun(t *testing.T) {
	for _, play := range []string{
		"/tmp/evil-play", "/rootkit/play.yml", "relative/play.yml", "/root/../tmp/evil", "",
	} {
		r := newOpsRig(t)
		r.ops.Cfg.Play = play
		r.ops.Cfg.ElementPlay = play
		if err := r.ops.runPlay(context.Background(), []string{"a"}); err == nil {
			t.Errorf("%q: runPlay accepted a play outside the checkout", play)
		}
		if err := r.ops.runElementPlay(context.Background(), []string{"a"}); err == nil {
			t.Errorf("%q: runElementPlay accepted a play outside the checkout", play)
		}
		if len(r.fake.Calls()) != 0 {
			t.Errorf("%q: ran %v", play, r.fake.Calls())
		}
	}
}

func TestARefusedPlayNamesItselfAndHowToTrustIt(t *testing.T) {
	r := newOpsRig(t)
	r.ops.Cfg.Play = "/tmp/evil-play"
	if err := r.ops.runPlay(context.Background(), []string{"a"}); err == nil || !strings.Contains(err.Error(), "/tmp/evil-play") {
		t.Fatalf("got %v", err)
	}
}

func TestThePlayInsideTheCheckoutRuns(t *testing.T) {
	r := newOpsRig(t)
	r.on(opsPlay, execx.Result{})
	if err := r.ops.runPlay(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
}

func TestATrustedEnvironmentMayNameAPlayAnywhere(t *testing.T) {
	r := newOpsRig(t)
	r.ops.Cfg.TrustPlays = true
	r.ops.Cfg.Play = "/tmp/stub-play"
	r.on("/tmp/stub-play", execx.Result{})
	if err := r.ops.runPlay(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigTrustsPlaysOnlyWhenTold(t *testing.T) {
	env := map[string]string{"HOME": "/h"}
	if LoadConfig("/r", func(k string) string { return env[k] }).TrustPlays {
		t.Fatal("trusted by default")
	}
	env["CCY_TEAMS_TRUST_PLAYS"] = "1"
	if !LoadConfig("/r", func(k string) string { return env[k] }).TrustPlays {
		t.Fatal("CCY_TEAMS_TRUST_PLAYS=1 was ignored")
	}
}
