package vmtest

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// A consumer-install scenario (Plan 00228): the checkout pinned where run.bash --consumer pulls
// it, then two attended runs, the install and the re-run an update is, in the session's
// environment.

const menuPrompt = "Numbers to install (e.g. 1 3), or Enter for none: "

func consumerRunning(t *testing.T, attended func(remote string) execx.Result) (*kit, *running) {
	t.Helper()
	k, r := attendedKit(t, attended)
	r.s.Audience = audienceConsumer
	return k, r
}

func TestConsumerProvision(t *testing.T) {
	k, r := consumerRunning(t, printing("PLAY RECAP\r\n"+menuPrompt+"\r\n", 0))
	rc, err := r.provision(context.Background())
	if err != nil || rc != 0 {
		t.Fatalf("got %d %v", rc, err)
	}
	if k.count("run.env") != 0 {
		t.Fatal("a consumer install was handed to the headless session runner")
	}
	checkout := "sudo dnf -y -q install git-core && git clone -q '" + testRepoURL + "' ~/Projects/fedora-desktop" +
		" && git -C ~/Projects/fedora-desktop branch vmtest-commit '" + testCommit + "'" +
		" && git -C ~/Projects/fedora-desktop checkout -q --track -b vmtest-pinned vmtest-commit"
	session := "export XDG_RUNTIME_DIR=/run/user/$(id -u) DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$(id -u)/bus TERM=dumb" +
		" && while IFS='=' read -r k v; do case \"$k\" in WAYLAND_DISPLAY|DISPLAY|XDG_CURRENT_DESKTOP|XDG_SESSION_DESKTOP|XDG_SESSION_TYPE|DESKTOP_SESSION) export \"$k=$v\" ;; esac; done </var/lib/vmtest/session.env && cd && "
	install := session + "~/run.bash --consumer"
	rerun := session + "set -o pipefail && ~/Projects/fedora-desktop/run.bash 2>&1 | tee /var/lib/vmtest/rerun.log"
	assertInOrder(t, k, []string{"loginctl list-sessions --no-legend && test -r /var/lib/vmtest/session.env", checkout, install, rerun})
	for _, run := range []string{install, rerun} {
		if !strings.HasPrefix(k.mustCall(run).Line(), "ssh -tt ") {
			t.Fatalf("not run at a terminal: %s", k.mustCall(run).Line())
		}
	}
	got := transcriptOf(t, r)
	for _, want := range []string{
		"# harness: the checkout cloned over HTTPS and pinned at " + testCommit[:12] + ", where run.bash --consumer pulls it\n",
		"# harness: attended run 1 of 2: ~/run.bash --consumer\nPLAY RECAP\n" + menuPrompt + "\n",
		"# harness: attended run 2 of 2: ~/Projects/fedora-desktop/run.bash, the update a consumer runs\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("transcript lacks %q:\n%s", want, got)
		}
	}
}

// testMenu is the optional-app menu as run.bash --consumer prints it: the numbered plays, then
// the question's heading, then the question.
const testMenu = "\n━━━ Optional apps ━━━\n  1) Hd Audio\n  2) Qobuz\n  3) Speech To Text\n  4) Displaylink (hardware)\n" +
	"consumer-install › Optional apps\n"

// The menu is answered with the numbers of the scenario's picks, in its order, a hardware play
// by its labelled name.
func TestConsumerMenuAnswer(t *testing.T) {
	got, err := menuAnswer([]string{"play-speech-to-text.yml", "play-displaylink.yml", "play-hd-audio.yml"})([]byte("PLAY RECAP\n  9) not the menu\nok\n" + testMenu))
	if err != nil || got != "3 4 1\n" {
		t.Fatalf("got %q %v", got, err)
	}
}

// A pick the menu does not offer, no menu at all, or a menu whose numbers are not 1 to n cannot
// be answered: each says why, so the run is ended rather than answered with none.
func TestConsumerMenuAnswerRefusals(t *testing.T) {
	for name, tc := range map[string]struct{ printed, want string }{
		"not offered": {testMenu, "the optional-app menu offers no Rust Dev (play-rust-dev.yml); it offers Hd Audio, Qobuz, Speech To Text, Displaylink (hardware)"},
		"no menu":     {"PLAY RECAP\nconsumer-install › Optional apps\n", "no numbered menu was printed before the optional-app question"},
		"numbering":   {"  1) Hd Audio\n  3) Rust Dev\n", "the optional-app menu is not numbered 1 to 2: 3) Rust Dev"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := menuAnswer([]string{"play-rust-dev.yml"})([]byte(tc.printed))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

// The install's menu is answered with the picks; the re-run's with Enter, as an update that
// adds nothing; with no picks, both are Enter.
func TestConsumerPrompts(t *testing.T) {
	for name, tc := range map[string]struct {
		picks []string
		want  string
	}{
		"picks":    {[]string{"play-qobuz.yml"}, "2\n"},
		"no picks": {nil, "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			var keys bytes.Buffer
			a := &attendant{out: io.Discard, keys: &keys, prompts: consumerPrompts(tc.picks), stop: func() {}, wake: make(chan struct{}, 1)}
			if _, err := a.Write([]byte(testMenu + menuPrompt)); err != nil {
				t.Fatal(err)
			}
			if keys.String() != tc.want || a.halt.ended() {
				t.Fatalf("typed %q halt %+v", keys.String(), a.halt)
			}
		})
	}
}

// A menu that does not offer a pick ends the install as a fail, saying what it lacked, and
// the re-run never runs.
func TestConsumerProvisionMenuLacksAPick(t *testing.T) {
	k, r := consumerRunning(t, printing(strings.ReplaceAll(testMenu, "\n", "\r\n")+menuPrompt, 255))
	r.s.OptionalPlays = []string{"play-rust-dev.yml"}
	rc, err := r.provision(context.Background())
	if err != nil || rc != 1 {
		t.Fatalf("got %d %v", rc, err)
	}
	if k.count("ssh -tt") != 1 {
		t.Fatal("the re-run ran after an unanswerable menu")
	}
	want := "\nERROR: attended run 1 of 2 asked a question the harness could not answer: the optional-app menu offers no Rust Dev (play-rust-dev.yml); it offers Hd Audio, Qobuz, Speech To Text, Displaylink (hardware); the harness ended it\n"
	if !strings.Contains(transcriptOf(t, r), want) {
		t.Fatalf("transcript %q", transcriptOf(t, r))
	}
}

// The install failing ends the provisioning with its status; the re-run never runs.
func TestConsumerProvisionFailure(t *testing.T) {
	k, r := consumerRunning(t, printing("a play failed\r\n", 2))
	rc, err := r.provision(context.Background())
	if err != nil || rc != 2 {
		t.Fatalf("got %d %v", rc, err)
	}
	if k.count("ssh -tt") != 1 {
		t.Fatalf("the re-run ran after a failed install: %d attended runs", k.count("ssh -tt"))
	}
}

// A run that asks for a password fails the provisioning at once and says what it asked.
func TestConsumerProvisionRefusedPrompt(t *testing.T) {
	k, r := consumerRunning(t, printing("[sudo] password for fedora: ", 255))
	rc, err := r.provision(context.Background())
	if err != nil || rc != 1 {
		t.Fatalf("got %d %v", rc, err)
	}
	if k.count("ssh -tt") != 1 {
		t.Fatal("the re-run ran after a refused prompt")
	}
	want := "ERROR: attended run 1 of 2 asked \"[sudo] password for \", which a consumer install must never ask here; the harness ended it\n"
	if !strings.Contains(transcriptOf(t, r), want) {
		t.Fatalf("transcript %q", transcriptOf(t, r))
	}
}

// A run that stops at a question no prompt answers fails the provisioning once it has printed
// nothing for promptIdle, and the transcript says what it asked.
func TestConsumerProvisionUnansweredQuestion(t *testing.T) {
	k, r := waitingKit(t, "TASK [Prompt for PS1 colour]\r\nEnter Prompt Colour. Must be one of:\r\n  red / redBold\r\n:")
	r.s.Audience = audienceConsumer
	rc, err := r.provision(context.Background())
	if err != nil || rc != 1 {
		t.Fatalf("got %d %v", rc, err)
	}
	if k.count("ssh -tt") != 1 {
		t.Fatal("the re-run ran after an unanswered question")
	}
	want := "ERROR: attended run 1 of 2 stopped at a question the harness has no answer for, and printed nothing for 60s; the harness ended it. Its last lines: \"Enter Prompt Colour. Must be one of:\\n  red / redBold\\n:\"\n"
	if !strings.Contains(transcriptOf(t, r), want) {
		t.Fatalf("transcript %q", transcriptOf(t, r))
	}
}

// consumer-install's own password question is refused too: the lab's sudo is passwordless, so a
// run that asks it has misjudged sudo.
func TestConsumerProvisionRefusesTheInstallersPasswordQuestion(t *testing.T) {
	_, r := consumerRunning(t, printing("i The install needs your login password once.\r\nYour login password (nothing shows as you type): ", 255))
	rc, err := r.provision(context.Background())
	if err != nil || rc != 1 {
		t.Fatalf("got %d %v", rc, err)
	}
	if !strings.Contains(transcriptOf(t, r), "asked \"Your login password") {
		t.Fatalf("transcript %q", transcriptOf(t, r))
	}
}

// The checkout that cannot be made is the harness failing, not the product.
func TestConsumerProvisionCheckoutFails(t *testing.T) {
	k, r := consumerRunning(t, printing("", 0))
	k.fake.OnFunc([]string{"ssh", "-o"}, func(c execx.Cmd) (execx.Result, error) {
		if strings.Contains(c.Line(), "git clone") {
			return execx.Result{ExitCode: 128}, nil
		}
		return seat0(c.Args[len(c.Args)-1]), nil
	})
	if _, err := r.provision(context.Background()); err == nil || !strings.Contains(err.Error(), "git clone") {
		t.Fatalf("got %v", err)
	}
	if k.count("ssh -tt") != 0 {
		t.Fatal("run.bash ran on a checkout that was never made")
	}
}

// A whole consumer run: no session runner, two attended runs, the consumer checker, and the
// judge told the run's divergences.
func TestConsumerRun(t *testing.T) {
	k := runKit(t, "pass")
	k.scenario("desktop-consumer-install", map[string]string{"base": "desktop", "base_name": "desktop-44", "profile": "desktop", "audience": "consumer"})
	k.write(allowlistFile, "desktop-consumer-install\n")
	k.write("guest-acceptance-desktop-consumer-install.bash", "checker\n")
	k.builtBase("desktop-44")
	k.write("bases/desktop-44/luks-passphrase", "pp\n")
	k.gw.remote = func(remote string) (execx.Result, bool) {
		if strings.HasPrefix(remote, "loginctl") {
			return seat0(remote), true
		}
		return execx.Result{}, false
	}
	k.wantCode(k.run("run", "desktop-consumer-install"), term.ExitOK)
	if k.count("run.env") != 0 || k.count("ssh -tt") != 2 {
		t.Fatalf("session runner %d, attended runs %d", k.count("run.env"), k.count("ssh -tt"))
	}
	k.mustCall("guest-acceptance-desktop-consumer-install.bash fedora@")
	k.wantErr("==> provisioning (attended ./run.bash --consumer, then its re-run; this is the long part — up to 5400s)")
	if !strings.Contains(k.mustCall("judge_run").Line(), ",run-bash-attended-over-ssh-terminal,checkout-pinned-by-harness,lab-sudoers-nopasswd") {
		t.Fatal("the consumer divergences")
	}
}

// No session to type in is the harness failing at boot, before anything is run.
func TestConsumerProvisionNoSession(t *testing.T) {
	k := stepKit(t, func(string) execx.Result { return execx.Result{} })
	r := newRunning(t, k, profileDesktop)
	r.s.Audience = audienceConsumer
	_, err := r.provision(context.Background())
	if err == nil || err.Error() != "no autologin session on seat0 within 180s with a session.env recorded (stage: boot)" {
		t.Fatalf("got %v", err)
	}
	if k.count("git clone") != 0 || k.count("ssh -tt") != 0 {
		t.Fatal("the install started with no session")
	}
}

// The judge is told how a consumer run differs from a real one.
func TestConsumerDivergences(t *testing.T) {
	plain := divergences(profileDesktop, false)
	consumer := divergences(profileDesktop, true)
	if strings.Contains(plain, "attended") {
		t.Fatalf("a headless run lists the attended divergences: %s", plain)
	}
	for _, want := range []string{",run-bash-attended-over-ssh-terminal", ",checkout-pinned-by-harness", ",lab-sudoers-nopasswd"} {
		if !strings.Contains(consumer, want) || !strings.HasPrefix(consumer, plain) {
			t.Fatalf("%s lacks %s", consumer, want)
		}
	}
}
