package wsi

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func graceKit(t *testing.T, seconds string) *kit {
	t.Helper()
	k := newKit(t)
	k.run.On([]string{filepath.Join(k.bin, "wsi-stop-grace")}, execx.Result{Stdout: []byte(seconds + "\n")})
	return k
}

func TestW17TheFirstTermStartsTheGraceAndItsEndStops(t *testing.T) {
	k := graceKit(t, "2")
	k.autoFire[2*time.Second] = true
	if code := k.exec("--session-token", "tok"); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if !slices.Equal(k.states(), []string{"PREPARING", "RECORDING", "STOPPING", "TRANSCRIBING", "IDLE"}) {
		t.Fatalf("states %q", k.states())
	}
	want := "'Stopping in 2s - finish your sentence.\\nEscape: discard. Insert waits until the text is pasted.'|'audio-input-microphone'|2000"
	if !slices.Contains(k.notes(), want) {
		t.Fatalf("notes %q", k.notes())
	}
	if k.out.String() != "Hello world\n" {
		t.Fatalf("stdout %q", k.out.String())
	}
}

func TestW16ASecondTermDuringTheGraceStopsAtOnce(t *testing.T) {
	k := graceKit(t, "10")
	k.onState["STOPPING"] = func() { k.signals <- syscall.SIGTERM }
	if code := k.exec(); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if !slices.Contains(k.states(), "STOPPING") || !k.rec.current.stopped {
		t.Fatalf("states %q", k.states())
	}
}

func TestW16AnInterruptStopsAtOnceEvenWithAGrace(t *testing.T) {
	k := graceKit(t, "10")
	k.onState["RECORDING"] = func() { k.signals <- syscall.SIGINT }
	if code := k.exec(); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if slices.Contains(k.states(), "STOPPING") {
		t.Fatalf("states %q", k.states())
	}
}

func TestW16TheLimitStopsTheRecording(t *testing.T) {
	k := newKit(t)
	delete(k.onState, "RECORDING")
	k.autoFire[maxRecording] = true
	if code := k.exec("-v"); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	k.wantStderr("[DEBUG] 30-second limit reached, auto-stopping recording")
}

func TestW18AStopBeforeTheRecorderStartsIsActedOnWhenItStarts(t *testing.T) {
	k := graceKit(t, "2")
	k.autoFire[2*time.Second] = true
	delete(k.onState, "RECORDING")
	k.signals <- syscall.SIGTERM
	delete(k.autoFire, prepareDelay)
	k.onState["STOPPING"] = func() { k.autoFire[prepareDelay] = true }
	if code := k.exec(); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if !slices.Equal(k.states(), []string{"PREPARING", "STOPPING", "TRANSCRIBING", "IDLE"}) {
		t.Fatalf("a held TERM starts the grace and RECORDING never follows STOPPING: %q", k.states())
	}
}

func TestW15ARecorderThatEndsWhilePreparingSendsNoRecording(t *testing.T) {
	k := newKit(t)
	k.recorderEndsAtOnce = true
	delete(k.autoFire, prepareDelay)
	if code := k.exec("-v"); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if slices.Contains(k.states(), "RECORDING") {
		t.Fatalf("states %q", k.states())
	}
	k.wantStderr("The recorder stopped while preparing: RECORDING is not sent")
}

func TestW15APIDFileThatCannotBeWrittenStopsTheRecorder(t *testing.T) {
	k := newKit(t)
	if err := os.Mkdir(k.pidFile(), 0o750); err != nil {
		t.Fatal(err)
	}
	if code := k.exec(); code != 1 {
		t.Fatalf("exit %d", code)
	}
	k.wantStderr("[ERROR] Cannot write the PID file " + k.pidFile())
	if !k.rec.current.stopped {
		t.Fatal("the recorder was left running")
	}
}

func TestW16TheRealRecorderIsPwRecordStoppedWithSIGINT(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"pw-record"}, execx.Result{})
	var sink bytes.Buffer
	rec := ExecRecorder{Runner: f}.Start(context.Background(), "/dev/shm/x.wav", &sink)
	<-rec.Done()
	if rec.Err() != nil {
		t.Fatal(rec.Err())
	}
	rec.Stop()
	calls := f.Calls()
	if len(calls) != 1 || calls[0].Line() != "pw-record --rate 44100 --channels 2 --latency 50ms /dev/shm/x.wav" {
		t.Fatalf("calls %v", calls)
	}
	if calls[0].Interrupt != syscall.SIGINT || calls[0].Stdout != &sink || calls[0].Stderr != &sink {
		t.Fatalf("cmd %+v", calls[0])
	}
}

// A stand-in pw-record on PATH that says which signal stopped it.
const stubRecorder = `#!/bin/sh
trap 'echo INT > "$STUB_SAID"; exit 0' INT
trap 'echo TERM > "$STUB_SAID"; exit 0' TERM
: > "$STUB_SAID.started"
while :; do sleep 0.05; done
`

func TestW16TheRealRecorderIsInterruptedWhenStopped(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "pw-record")
	if err := os.WriteFile(stub, []byte(stubRecorder), 0o700); err != nil {
		t.Fatal(err)
	}
	said := filepath.Join(dir, "said")
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	t.Setenv("STUB_SAID", said)
	rec := ExecRecorder{Runner: execx.OS{}}.Start(context.Background(), filepath.Join(dir, "x.wav"), nil)
	for i := 0; ; i++ {
		if _, err := os.Stat(said + ".started"); err == nil {
			break
		}
		if i > 400 {
			t.Fatal("the stub recorder never started")
		}
		time.Sleep(25 * time.Millisecond)
	}
	rec.Stop()
	select {
	case <-rec.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the recorder did not stop")
	}
	if rec.Err() == nil || !strings.Contains(rec.Err().Error(), "canceled") {
		t.Fatalf("err %v", rec.Err())
	}
	got, err := os.ReadFile(said)
	if err != nil || string(got) != "INT\n" {
		t.Fatalf("the recorder got %q (%v), want INT", got, err)
	}
}
