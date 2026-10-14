package systemd

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func newUnits(chatter *strings.Builder) (*Systemd, *execx.Fake) {
	f := &execx.Fake{}
	s := &Systemd{Runner: f}
	if chatter != nil {
		s.Chatter = chatter
	}
	return s, f
}

func lastLine(t *testing.T, f *execx.Fake) string {
	t.Helper()
	calls := f.Calls()
	if len(calls) == 0 {
		t.Fatal("nothing was run")
	}
	return calls[len(calls)-1].Line()
}

func TestShowAsksForOnePropertyAndTrimsTheNewline(t *testing.T) {
	s, f := newUnits(nil)
	f.On([]string{"systemctl", "show", "-P", "ActiveState"}, execx.Result{Stdout: []byte("active\n")})
	got, err := s.Show(context.Background(), "ActiveState", "a.service")
	if err != nil || got != "active" || lastLine(t, f) != "systemctl show -P ActiveState a.service" {
		t.Fatalf("got %q, %v, %q", got, err, lastLine(t, f))
	}
}

func TestShowKeepsTheLinesOfAListProperty(t *testing.T) {
	s, f := newUnits(nil)
	f.On([]string{"systemctl", "show"}, execx.Result{Stdout: []byte("any\nany\n")})
	got, err := s.Show(context.Background(), "SocketBindDeny", "a.service")
	if err != nil || got != "any\nany" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestShowReportsAFailure(t *testing.T) {
	s, f := newUnits(nil)
	f.On([]string{"systemctl", "show"}, execx.Result{ExitCode: 1, Stderr: []byte("boom")})
	_, err := s.Show(context.Background(), "ActiveState", "a.service")
	if err == nil || !strings.Contains(err.Error(), "systemctl show -P ActiveState a.service") {
		t.Fatalf("got %v", err)
	}
	if _, ok := execx.ExitOf(err); !ok {
		t.Fatalf("the exit error is lost: %v", err)
	}
}

func TestIsEnabledTreatsANonZeroExitAsNo(t *testing.T) {
	s, f := newUnits(nil)
	f.On([]string{"systemctl", "is-enabled", "--quiet", "yes.service"}, execx.Result{})
	f.On([]string{"systemctl", "is-enabled", "--quiet", "no.service"}, execx.Result{ExitCode: 1})
	f.On([]string{"systemctl", "is-enabled", "--quiet", "gone.service"}, execx.Result{ExitCode: 4})
	for unit, want := range map[string]bool{"yes.service": true, "no.service": false, "gone.service": false} {
		got, err := s.IsEnabled(context.Background(), unit)
		if err != nil || got != want {
			t.Errorf("IsEnabled(%s) = %v, %v", unit, got, err)
		}
	}
}

func TestIsEnabledFailsWhenSystemctlCannotRun(t *testing.T) {
	s, f := newUnits(nil)
	f.OnError([]string{"systemctl"}, &execx.NotInstalledError{Name: "systemctl"})
	if _, err := s.IsEnabled(context.Background(), "a.service"); !execx.IsNotInstalled(err) {
		t.Fatalf("got %v", err)
	}
}

func TestUnitOperationsRunTheExactArgvAndSendTheChatterAside(t *testing.T) {
	var chatter strings.Builder
	s, f := newUnits(&chatter)
	f.On([]string{"systemctl"}, execx.Result{Stdout: []byte("Created symlink\n")})
	ctx := context.Background()
	steps := []struct {
		want string
		run  func() error
	}{
		{"systemctl enable a.service", func() error { return s.Enable(ctx, "a.service") }},
		{"systemctl disable --now a.service b.timer", func() error { return s.DisableNow(ctx, "a.service", "b.timer") }},
		{"systemctl start a.service", func() error { return s.Start(ctx, "a.service") }},
		{"systemctl restart a.service", func() error { return s.Restart(ctx, "a.service") }},
		{"systemctl stop a.service", func() error { return s.Stop(ctx, "a.service") }},
		{"systemctl daemon-reload", func() error { return s.DaemonReload(ctx) }},
		{"systemctl kill --kill-whom=main --signal=SIGUSR2 a.service", func() error { return s.SignalMain(ctx, "a.service", "SIGUSR2") }},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			t.Fatalf("%s: %v", step.want, err)
		}
		if got := lastLine(t, f); got != step.want {
			t.Fatalf("ran %q, want %q", got, step.want)
		}
	}
	if strings.Count(chatter.String(), "Created symlink") != len(steps) {
		t.Fatalf("chatter %q", chatter.String())
	}
}

func TestUnitOperationsNameTheCommandInTheirFailure(t *testing.T) {
	s, f := newUnits(nil)
	f.On([]string{"systemctl"}, execx.Result{ExitCode: 5, Stderr: []byte("Unit not found.")})
	ctx := context.Background()
	for want, run := range map[string]func() error{
		"systemctl enable a.service":        func() error { return s.Enable(ctx, "a.service") },
		"systemctl disable --now a.service": func() error { return s.DisableNow(ctx, "a.service") },
		"systemctl start a.service":         func() error { return s.Start(ctx, "a.service") },
		"systemctl restart a.service":       func() error { return s.Restart(ctx, "a.service") },
		"systemctl stop a.service":          func() error { return s.Stop(ctx, "a.service") },
		"systemctl daemon-reload":           func() error { return s.DaemonReload(ctx) },
		"systemctl kill":                    func() error { return s.SignalMain(ctx, "a.service", "SIGUSR2") },
	} {
		err := run()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q in %v", want, err)
		}
	}
}

func TestRunTransientPassesEachPropertyAndTheCommandAfterTheSeparator(t *testing.T) {
	var chatter strings.Builder
	s, f := newUnits(&chatter)
	f.On([]string{"systemd-run"}, execx.Result{Stdout: []byte("Running as unit: u.service\n")})
	err := s.RunTransient(context.Background(), "u.service", []string{"User=x", "IPAddressAllow=a b"},
		"/bin/tool", "--flag", "1")
	if err != nil {
		t.Fatal(err)
	}
	want := "systemd-run --unit=u.service --collect --property=User=x --property=IPAddressAllow=a b -- /bin/tool --flag 1"
	if got := lastLine(t, f); got != want {
		t.Fatalf("ran %q", got)
	}
	if !strings.Contains(chatter.String(), "Running as unit") {
		t.Fatalf("chatter %q", chatter.String())
	}
	f.On([]string{"systemd-run"}, execx.Result{ExitCode: 1})
	if err := s.RunTransient(context.Background(), "u.service", nil, "/bin/tool"); err == nil ||
		!strings.Contains(err.Error(), "systemd-run --unit=u.service") {
		t.Fatalf("got %v", err)
	}
}

func TestJournalReadsTheLastLinesOfOneUnit(t *testing.T) {
	s, f := newUnits(nil)
	f.On([]string{"journalctl"}, execx.Result{Stdout: []byte("line one\n")})
	var out strings.Builder
	if err := s.Journal(context.Background(), "a.service", 40, &out); err != nil {
		t.Fatal(err)
	}
	if got := lastLine(t, f); got != "journalctl --unit a.service --lines 40 --no-pager --output short-iso" {
		t.Fatalf("ran %q", got)
	}
	if out.String() != "line one\n" {
		t.Fatalf("out %q", out.String())
	}
	f.On([]string{"journalctl"}, execx.Result{ExitCode: 1})
	if err := s.Journal(context.Background(), "a.service", 40, &out); err == nil {
		t.Fatal("a failed journalctl returned no error")
	}
}
