package wsi

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The panel's D-Bus names (W8, W30).
const (
	gdbusBin       = "gdbus"
	optSession     = "--session"
	optObjectPath  = "--object-path"
	signalPath     = "/org/fedoradesktop/SpeechToText"
	signalIface    = "org.fedoradesktop.SpeechToText"
	panelPath      = "/org/fedoradesktop/SpeechToText/Panel"
	panelIface     = "org.fedoradesktop.SpeechToText.Panel"
	micIcon        = "audio-input-microphone"
	errorIcon      = "dialog-error"
	stateNotifyMS  = 3000
	stateSuccess   = "SUCCESS"
	stateError     = "ERROR"
	stateIdle      = "IDLE"
	stateClaude    = "CLAUDE_PROCESSING" // the transcript is done; Claude is working on it
	stateChanged   = "StateChanged"
	notifyStaysMS  = 0
	notifyAppName  = "speech-to-text"
	notifySummary  = "Speech to Text"
	notifyReplyPat = `\(uint32 ([0-9]+),\)`
)

var notifyReply = regexp.MustCompile(notifyReplyPat)

// gvString is s as a GVariant string literal, so gdbus never reads it as another type or
// rewrites its backslashes (B10). Line breaks and control characters are escaped.
func gvString(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch {
		case r == '\\' || r == '\'':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < ' ' || r == 0x7f:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// panel is the D-Bus side of a dictation: the signals the panel follows (W8) and the desktop
// notifications (W10).
type panel struct {
	runner   execx.Runner
	log      *logger
	token    string
	ended    string // the state that ended the session; nothing is signalled after it
	notifID  string
	noNotify bool
}

// emit sends one signal, in the foreground so the panel gets them in order (W8).
func (p *panel) emit(ctx context.Context, name, value string) {
	if p.ended != "" {
		p.log.debug("Not sent, the session ended with " + p.ended + ": " + name + " " + value)
		return
	}
	if name == stateChanged && (value == stateSuccess || value == stateError || value == stateIdle) {
		p.ended = value
	}
	cmd := execx.Cmd{Name: gdbusBin, Args: []string{
		"emit", optSession, optObjectPath, signalPath,
		"--signal", signalIface + "." + name, value, gvString(p.token),
	}}
	if out, err := execx.Combined(ctx, p.runner, cmd); err != nil {
		reason := strings.TrimSpace(out)
		if reason == "" {
			reason = err.Error()
		}
		p.log.say("[WSI] D-Bus signal " + name + " " + value + " not sent: " + reason)
	}
}

func (p *panel) state(ctx context.Context, state string) { p.emit(ctx, stateChanged, state) }

// errorText is the text the panel shows beside its error state.
func (p *panel) errorText(ctx context.Context, text string) { p.emit(ctx, "Error", gvString(text)) }

// notify is one desktop notification replacing the last (W10). One that stays reports a
// dictation that failed after it was spoken, so --no-notify (the panel's default) keeps it.
func (p *panel) notify(ctx context.Context, message, icon string, ms int) {
	if p.noNotify && ms != notifyStaysMS {
		return
	}
	cmd := execx.Cmd{Name: gdbusBin, Args: []string{
		"call", optSession, "--dest", "org.freedesktop.Notifications",
		optObjectPath, "/org/freedesktop/Notifications",
		"--method", "org.freedesktop.Notifications.Notify",
		gvString(notifyAppName), p.notifID, gvString(icon), gvString(notifySummary), gvString(message),
		"[]", "{}", strconv.Itoa(ms),
	}}
	res, err := p.runner.Run(ctx, cmd)
	if err != nil {
		p.log.debug("Desktop notification was not accepted (gdbus call failed)")
		return
	}
	if m := notifyReply.FindStringSubmatch(string(res.Stdout)); m != nil {
		p.notifID = m[1]
	}
}

// notifyState is a state change with its notification; an empty icon is the microphone.
func (p *panel) notifyState(ctx context.Context, state, message, icon string) {
	if icon == "" {
		icon = micIcon
	}
	p.log.debug("State: " + state + " - " + message)
	p.state(ctx, state)
	p.notify(ctx, message, icon, stateNotifyMS)
}
