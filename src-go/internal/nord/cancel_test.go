package nord

import (
	"os"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E24: Ctrl-C while the import runs waits for it, runs the configuration no further, removes the
// half-made profile, says Cancelled and exits 130.
func TestCtrlCDuringImport(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	importLine := "nmcli connection import type openvpn file " + w.home + "/" + configsDir + "uk1.ovpn"
	w.during[importLine] = w.interrupt
	wantCode(t, w.run("connect", "uk1"), term.ExitCancelled)
	wantChanges(t, w, importLine, "nmcli connection delete uuid 00000000-0000-4000-8000-000000000001")
	if len(w.cancelledUnder) != 0 {
		t.Errorf("commands abandoned mid-run: %q", w.cancelledUnder)
	}
	w.wantErr("\nCancelled.\n")
	w.wantNoErr("Connected")
}

// E24: Ctrl-C while bringing the old connection down runs nothing after it.
func TestCtrlCDuringDown(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn", "de2.ovpn")
	de := w.imported("nordvpn-de2")
	de.active = "activated"
	w.imported("nordvpn-uk1")
	w.during["nmcli connection down uuid "+de.uuid] = w.interrupt
	wantCode(t, w.run("connect", "uk1"), term.ExitCancelled)
	wantChanges(t, w, "nmcli connection down uuid "+de.uuid)
	if len(w.cancelledUnder) != 0 {
		t.Errorf("commands abandoned mid-run: %q", w.cancelledUnder)
	}
}

// E24: Ctrl-C at the chooser's menu ends the wait at once.
func TestCtrlCAtTheMenu(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	w.ttyIn = &blockingTTY{onRead: w.interrupt, done: done}
	wantCode(t, w.run(), term.ExitCancelled)
	w.wantErr("Choose a server [1, q]: ", "\nCancelled.\n")
	wantChanges(t, w)
}

// blockingTTY is a terminal on which nothing is ever typed: its first read calls onRead (the
// Ctrl-C), then every read waits until the test ends.
type blockingTTY struct {
	onRead func()
	done   chan struct{}
}

func (b *blockingTTY) Read([]byte) (int, error) {
	if b.onRead != nil {
		f := b.onRead
		b.onRead = nil
		f()
	}
	<-b.done
	return 0, os.ErrClosed
}

// E24: a listing (read-only) runs under the run's context, so a cancel stops it.
func TestCtrlCDuringAListing(t *testing.T) {
	w := newWorld(t)
	w.during["nmcli -t -f NAME,UUID,TYPE connection show"] = w.interrupt
	wantCode(t, w.run("cleanup"), term.ExitCancelled)
	if len(w.cancelledUnder) != 1 {
		t.Errorf("cancelled under %q", w.cancelledUnder)
	}
	wantChanges(t, w)
}

// E24, B16: every command that changes something runs in a session of its own, outside the
// terminal's foreground group, so a Ctrl-C at the terminal reaches nord and not nmcli; the
// listings do not.
func TestChangingCommandsRunInTheirOwnSession(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn", "de2.ovpn")
	w.imported("nordvpn-de2").active = "activated"
	wantCode(t, w.run("connect", "uk1"), term.ExitOK)
	changing := 0
	for _, c := range w.fake.Calls() {
		isChange := c.Name == "nmcli" && c.Args[0] == "connection" && c.Args[1] != "show"
		if isChange {
			changing++
		}
		if c.NewSession != isChange {
			t.Errorf("%s: NewSession %v", c.Line(), c.NewSession)
		}
	}
	if changing != 5 {
		t.Errorf("%d changing commands", changing)
	}
}

// U5: a signal after the connect has finished does not turn its success into a cancel.
func TestLateSignalKeepsTheSuccess(t *testing.T) {
	w := newWorld(t)
	w.config("uk1.ovpn")
	p := w.imported("nordvpn-uk1")
	w.during["nmcli connection up uuid "+p.uuid] = w.interrupt
	wantCode(t, w.run("connect", "uk1"), term.ExitOK)
	w.wantErr("Connected to uk1.\n")
	w.wantNoErr("Cancelled.")
}
