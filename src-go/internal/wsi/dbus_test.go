package wsi

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func TestB10GVariantStringLiteral(t *testing.T) {
	cases := map[string]string{
		"":             "''",
		"plain":        "'plain'",
		"it's":         `'it\'s'`,
		`back\slash`:   `'back\\slash'`,
		"two\nlines":   `'two\nlines'`,
		"tab\there":    `'tab\there'`,
		"bell\x07":     `'bell\u0007'`,
		"📝 unicode ok": "'📝 unicode ok'",
	}
	for in, want := range cases {
		if got := gvString(in); got != want {
			t.Errorf("gvString(%q) = %s, want %s", in, got, want)
		}
	}
}

func newTestPanel(t *testing.T) (*panel, *execx.Fake, testLog) {
	t.Helper()
	f := &execx.Fake{}
	f.On([]string{"gdbus", "emit"}, execx.Result{})
	f.On([]string{"gdbus", "call"}, execx.Result{Stdout: []byte("(uint32 7,)\n")})
	l := newTestLogger(t, true)
	return &panel{runner: f, log: l.logger, token: "tok-1", notifID: "0"}, f, l
}

func lines(f *execx.Fake) []string {
	calls := f.Calls()
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Line())
	}
	return out
}

func TestW8SignalsCarryTheTokenAndStopAfterTheSessionEnds(t *testing.T) {
	p, f, l := newTestPanel(t)
	ctx := context.Background()
	p.state(ctx, "RECORDING")
	p.errorText(ctx, "it's bad")
	p.state(ctx, "SUCCESS")
	p.state(ctx, "IDLE")
	p.errorText(ctx, "late")
	want := []string{
		"gdbus emit --session --object-path /org/fedoradesktop/SpeechToText --signal org.fedoradesktop.SpeechToText.StateChanged RECORDING 'tok-1'",
		`gdbus emit --session --object-path /org/fedoradesktop/SpeechToText --signal org.fedoradesktop.SpeechToText.Error 'it\'s bad' 'tok-1'`,
		"gdbus emit --session --object-path /org/fedoradesktop/SpeechToText --signal org.fedoradesktop.SpeechToText.StateChanged SUCCESS 'tok-1'",
	}
	if got := lines(f); !slices.Equal(got, want) {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
	if !strings.Contains(l.errb.String(), "[DEBUG] Not sent, the session ended with SUCCESS: StateChanged IDLE") {
		t.Fatalf("stderr %q", l.errb.String())
	}
}

func TestW8AFailedEmitIsAStderrLineOnly(t *testing.T) {
	p, f, l := newTestPanel(t)
	f.On([]string{"gdbus", "emit"}, execx.Result{ExitCode: 1, Stderr: []byte("no bus")})
	p.state(context.Background(), "PREPARING")
	if !strings.Contains(l.errb.String(), "[WSI] D-Bus signal StateChanged PREPARING not sent: no bus") {
		t.Fatalf("stderr %q", l.errb.String())
	}
}

func TestW10NotificationsReplaceTheLastAndCanBeTurnedOff(t *testing.T) {
	p, f, l := newTestPanel(t)
	ctx := context.Background()
	p.notify(ctx, "first\nline", "dialog-error", 0)
	p.notifyState(ctx, "ERROR", "second", "")
	want := []string{
		`gdbus call --session --dest org.freedesktop.Notifications --object-path /org/freedesktop/Notifications --method org.freedesktop.Notifications.Notify 'speech-to-text' 0 'dialog-error' 'Speech to Text' 'first\nline' [] {} 0`,
		"gdbus emit --session --object-path /org/fedoradesktop/SpeechToText --signal org.fedoradesktop.SpeechToText.StateChanged ERROR 'tok-1'",
		"gdbus call --session --dest org.freedesktop.Notifications --object-path /org/freedesktop/Notifications --method org.freedesktop.Notifications.Notify 'speech-to-text' 7 'audio-input-microphone' 'Speech to Text' 'second' [] {} 3000",
	}
	if got := lines(f); !slices.Equal(got, want) {
		t.Fatalf("got\n%s", strings.Join(got, "\n"))
	}
	if !strings.Contains(l.errb.String(), "[DEBUG] State: ERROR - second") {
		t.Fatalf("stderr %q", l.errb.String())
	}
	p.noNotify = true
	p.notify(ctx, "quiet", "x", 1)
	if len(f.Calls()) != 3 {
		t.Fatal("--no-notify sent a notification")
	}
	p.noNotify = false
	f.On([]string{"gdbus", "call"}, execx.Result{ExitCode: 1})
	p.notify(ctx, "refused", "x", 1)
	if !strings.Contains(l.errb.String(), "[DEBUG] Desktop notification was not accepted (gdbus call failed)") || p.notifID != "7" {
		t.Fatalf("stderr %q, id %s", l.errb.String(), p.notifID)
	}
}
