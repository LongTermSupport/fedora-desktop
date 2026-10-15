package wsi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The paste target's timing (W30, W33).
const (
	pasteKeyTimeout       = 2 * time.Second
	focusPoll             = 100 * time.Millisecond
	focusPolls            = 20
	focusSettlePolls      = 5
	enterDelayAfterRetake = time.Second
	enterDelay            = 300 * time.Millisecond
	selectionDelay        = 50 * time.Millisecond
)

// The window the paste goes to, as the panel last described it.
const (
	targetReady     = "ready"
	targetClosed    = "closed"
	targetUnfocused = "unfocused"
)

// The ydotool chords (linux/input-event-codes.h: 29 Ctrl, 42 Shift, 47 V, 110 Insert,
// 28 Enter, 31 S; :1 press, :0 release).
const (
	ctrlDown, ctrlUp   = "29:1", "29:0"
	shiftDown, shiftUp = "42:1", "42:0"
	vDown, vUp         = "47:1", "47:0"
	gvTrue             = "true"
	ydotoolBin         = "ydotool"
)

var (
	keysCtrlV      = []string{ctrlDown, vDown, vUp, ctrlUp}
	keysCtrlShiftV = []string{ctrlDown, shiftDown, vDown, vUp, shiftUp, ctrlUp}
	keysShiftIns   = []string{shiftDown, "110:1", "110:0", shiftUp}
	keysEnter      = []string{"28:1", "28:0"}
	keysCtrlS      = []string{ctrlDown, "31:1", "31:0", ctrlUp}
)

// pasteAnswer is PasteKey's reply: ('<wm_class>', with_shift, save_after[, focused, gone]).
var pasteAnswer = regexp.MustCompile(`^\('((?:\\.|[^'\\])*)', (true|false), (true|false)(?:, (true|false), (true|false))?\)$`)

// answer is one PasteKey reply; focused is "" from a panel that does not pin focus.
type answer struct {
	wm, withShift, save, focused, gone string
}

// pasteKey asks the panel once; nil and the reason when it cannot say (W30).
func (r *run) pasteKey(ctx context.Context) (*answer, string) {
	tctx, cancel := context.WithTimeout(ctx, pasteKeyTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	_, err := r.env.Runner.Run(tctx, execx.Cmd{
		Name: gdbusBin,
		Args: []string{
			"call", optSession, "--dest", "org.gnome.Shell",
			optObjectPath, panelPath, "--method", panelIface + ".PasteKey",
		},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	reply := strings.TrimRight(stdout.String(), "\n")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, "no answer within 2 s"
		}
		if why := strings.TrimSpace(stderr.String()); why != "" {
			return nil, why
		}
		return nil, err.Error()
	}
	m := pasteAnswer.FindStringSubmatch(reply)
	if m == nil {
		return nil, reply
	}
	return &answer{wm: m[1], withShift: m[2], save: m[3], focused: m[4], gone: m[5]}, ""
}

// focusWait counts the answers of one askPanel.
type focusWait struct {
	unfocused, settled int
	enter              bool
}

// askPanel is W30 (before the paste) and W33 (enter: before the Enter after a retake): it
// sets the target, and before the paste the keys and the save.
func (r *run) askPanel(ctx context.Context, enter bool) {
	w := &focusWait{enter: enter}
	for {
		a, why := r.pasteKey(ctx)
		if a == nil {
			r.cannotSay(why, enter)
			return
		}
		if a.gone == gvTrue {
			r.target = targetClosed
			return
		}
		if r.judge(w, a) {
			return
		}
		r.env.Sleep(focusPoll)
	}
}

func (r *run) cannotSay(why string, enter bool) {
	r.target = targetReady
	if enter {
		r.log.info("WARN: The panel did not say whether the window has focus before the Enter (" + why + "); sending it")
		return
	}
	r.log.info("WARN: The panel did not say how to paste into the focused window (" + why + "); using the key chosen at Insert")
	r.saveAfter = false
}

// judge takes one answer from a panel that still has the window; true once decided.
func (r *run) judge(w *focusWait, a *answer) bool {
	action := "pasting"
	if w.enter {
		action = "sending the Enter"
	}
	if a.focused != "false" {
		if w.unfocused == 0 || w.settled == focusSettlePolls {
			r.target = targetReady
			r.settle(w, a)
			return true
		}
		if w.settled == 0 {
			r.log.info(fmt.Sprintf("'%s' has focus back; %s once it has kept it for %d more checks, 0.1 s apart", a.wm, action, focusSettlePolls))
		}
		w.settled++
		return false
	}
	switch {
	case w.unfocused == 0 && w.enter:
		r.log.info("WARN: '" + a.wm + "' lost focus again after the paste; giving it focus back before the Enter")
	case w.unfocused == 0:
		r.log.info("WARN: '" + a.wm + "' lost focus during the dictation; waiting for the panel to give it focus back")
	case w.settled > 0:
		before := "paste"
		if w.enter {
			before = "Enter"
		}
		r.log.info("WARN: '" + a.wm + "' lost focus again before the " + before)
	}
	r.refocused = true
	w.settled = 0
	w.unfocused++
	if w.unfocused > focusPolls {
		r.target = targetUnfocused
		return true
	}
	return false
}

// settle records what a focused answer decides.
func (r *run) settle(w *focusWait, a *answer) {
	if w.enter {
		switch {
		case a.focused == "":
			r.log.info("'" + a.wm + "': the panel does not pin focus, so it cannot say whether the window still has it; sending the Enter")
		case w.unfocused == 0:
			r.log.info("'" + a.wm + "' still has focus before the Enter")
		default:
			r.log.info("'" + a.wm + "' has kept focus again; sending the Enter")
		}
		return
	}
	r.pasteWithShift = "0"
	keys := "Ctrl+V"
	if a.withShift == gvTrue {
		r.pasteWithShift, keys = "1", "Ctrl+Shift+V"
	}
	r.saveAfter = a.save == gvTrue
	if r.saveAfter {
		keys += ", then Ctrl+S"
	}
	r.log.info("Pasting into '" + a.wm + "': " + keys)
}

// targetWhy is why the window pinned at Insert cannot take the keys (W31).
func (r *run) targetWhy() string {
	if r.target == targetClosed {
		return "the window this dictation started in was closed"
	}
	return "the window this dictation started in would not take focus back"
}

// press is one ydotool chord through the daemon's socket.
func (r *run) press(ctx context.Context, keys []string) error {
	_, err := r.env.Runner.Run(ctx, execx.Cmd{
		Name: ydotoolBin,
		Args: append([]string{"key"}, keys...),
		Env:  []string{"YDOTOOL_SOCKET=" + r.socket},
	})
	return err
}

// autoPaste is W29-W35.
func (r *run) autoPaste(ctx context.Context, text string) error {
	if r.pasteWithShift != "" {
		r.askPanel(ctx, false)
	}
	r.log.debug(fmt.Sprintf("Text length: %d characters", len([]rune(text))))
	if !r.env.IsSocket(r.socket) {
		err := r.fail(ctx, "ydotool socket disappeared before paste")
		r.log.toFile("Service may have crashed during recording")
		return err
	}
	selection := selectionClipboard
	if r.pasteWithShift == "" {
		selection = selectionPrimary
	}
	if !r.knownSession() {
		return r.fail(ctx, "Unknown window manager for clipboard: "+r.session)
	}
	if err := r.copyOrFail(ctx, selection, text, true); err != nil {
		return err
	}
	if r.target != targetReady {
		why := r.targetWhy()
		err := r.fail(ctx, "Nothing was pasted: "+why)
		r.panel.notify(ctx, "Nothing was pasted: "+why+". The text is on the clipboard: Ctrl+V.", errorIcon, notifyStaysMS)
		return err
	}
	r.env.Sleep(selectionDelay)
	keys, name := keysShiftIns, "key"
	switch r.pasteWithShift {
	case "1":
		keys, name = keysCtrlShiftV, "Ctrl+Shift+V"
	case "0":
		keys, name = keysCtrlV, "Ctrl+V"
	}
	if err := r.press(ctx, keys); err != nil {
		return r.fail(ctx, fmt.Sprintf("ydotool %s simulation failed (exit code: %d)", name, exitCodeOf(err)))
	}
	r.log.debug("Paste simulated successfully")
	if err := r.beforeEnter(ctx); err != nil {
		return err
	}
	r.enterAndSave(ctx)
	r.panel.notifyState(ctx, stateSuccess, preview(text), "")
	return nil
}

// beforeEnter is W33's wait, and the second question after a retake.
func (r *run) beforeEnter(ctx context.Context) error {
	if !r.refocused || r.o.skipEnter {
		r.env.Sleep(enterDelay)
		return nil
	}
	r.log.info("Waiting 1.0 s before the Enter: focus was given back for this paste")
	r.env.Sleep(enterDelayAfterRetake)
	r.askPanel(ctx, true)
	if r.target == targetReady {
		return nil
	}
	why := r.targetWhy()
	err := r.fail(ctx, "The text was pasted but not sent: "+why)
	r.panel.notify(ctx, "The text was pasted but not sent: "+why+". No Enter was pressed.", errorIcon, notifyStaysMS)
	return err
}

// enterAndSave is W33's Enter and W34's save; a failure of either leaves the paste standing.
func (r *run) enterAndSave(ctx context.Context) {
	if r.o.skipEnter {
		r.log.debug("Enter key skipped (--no-auto-enter)")
	} else if err := r.press(ctx, keysEnter); err != nil {
		r.problem(ctx, fmt.Sprintf("ydotool Enter key simulation failed (exit code: %d)", exitCodeOf(err)))
	}
	if !r.saveAfter {
		return
	}
	if err := r.press(ctx, keysCtrlS); err != nil {
		r.problem(ctx, fmt.Sprintf("The text was pasted but the save (Ctrl+S) failed (exit code: %d)", exitCodeOf(err)))
		return
	}
	r.log.info("Save simulated via Ctrl+S")
}
