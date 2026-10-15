package wsi

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// prepareDelay lets the audio pipeline start before the panel shows RECORDING, so the first
// words are not clipped (W15).
const prepareDelay = 300 * time.Millisecond

// Recorder starts the microphone recording into path; out takes the recorder's own output.
type Recorder interface {
	Start(ctx context.Context, path string, out io.Writer) Recording
}

// Recording is one running recorder.
type Recording interface {
	// Stop asks the recorder to end (SIGINT, so it flushes its buffers); it may be called
	// more than once.
	Stop()
	// Done is closed once the recorder has ended.
	Done() <-chan struct{}
	// Err is how it ended, read after Done.
	Err() error
}

// ExecRecorder is the real recorder: pw-record through the one process seam, stopped with
// SIGINT and killed if it has not ended execx.DefaultGrace later (W16, B6).
type ExecRecorder struct {
	Runner execx.Runner
}

type execRecording struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// Start implements Recorder.
func (e ExecRecorder) Start(ctx context.Context, path string, out io.Writer) Recording {
	rctx, cancel := context.WithCancel(ctx)
	rec := &execRecording{cancel: cancel, done: make(chan struct{})}
	cmd := execx.Cmd{
		Name:      "pw-record",
		Args:      []string{"--rate", "44100", "--channels", "2", "--latency", "50ms", path},
		Stdout:    out,
		Stderr:    out,
		Interrupt: syscall.SIGINT,
	}
	go func() {
		defer close(rec.done)
		_, rec.err = e.Runner.Run(rctx, cmd)
	}()
	return rec
}

func (r *execRecording) Stop()                 { r.cancel() }
func (r *execRecording) Done() <-chan struct{} { return r.done }
func (r *execRecording) Err() error            { return r.err }

// recordingState is the one loop's view of a recording (W15-W18).
type recordingState struct {
	rec       Recording
	prepare   <-chan time.Time
	limit     <-chan time.Time
	grace     <-chan time.Time
	pending   bool
	preparing bool
}

// record is W15-W18: the recorder runs until a stop, the grace or the limit ends it. Every
// state change is sent from this one loop, so the panel gets them in order. A stop that came
// before the recorder started waits in the signal channel and is read here (B5).
func (r *run) record(ctx context.Context) error {
	r.panel.notifyState(ctx, "PREPARING", "Preparing to record...", "")
	out := io.Discard
	if r.o.debug {
		out = r.env.Stderr
	}
	rec := r.env.Recorder.Start(ctx, r.wav, out)
	r.recording = rec
	r.log.debug("pw-record started")
	if err := os.WriteFile(r.pidFile, []byte(strconv.Itoa(r.env.PID)+"\n"), 0o600); err != nil {
		r.stopRecorder()
		return r.fail(ctx, fmt.Sprintf("Cannot write the PID file %s: %v", r.pidFile, err))
	}
	r.log.debug(fmt.Sprintf("PID file written: %s (%d)", r.pidFile, r.env.PID))
	s := &recordingState{rec: rec, prepare: r.env.After(prepareDelay), preparing: true}
	for ended := false; !ended; {
		ended = r.recordStep(ctx, s)
	}
	r.log.debug(fmt.Sprintf("Recorder exited: %v", errText(rec.Err())))
	return nil
}

// recordStep handles one event; true once the recorder has ended.
func (r *run) recordStep(ctx context.Context, s *recordingState) bool {
	select {
	case <-s.prepare:
		s.prepare, s.preparing = nil, false
		r.prepared(ctx, s)
	case sig := <-r.env.Signals:
		r.stopRequested(ctx, s, sig)
	case <-s.grace:
		s.grace = nil
		r.log.debug("Stop grace over - stopping the recorder")
		s.rec.Stop()
	case <-s.limit:
		s.limit = nil
		r.log.debug(fmt.Sprintf("%d-second limit reached, auto-stopping recording", maxRecordingSeconds))
		s.rec.Stop()
	case <-s.rec.Done():
		if s.preparing {
			r.log.debug("The recorder stopped while preparing: RECORDING is not sent")
		}
		return true
	}
	return false
}

// prepared is the end of the preparing delay: RECORDING, unless a stop is under way.
func (r *run) prepared(ctx context.Context, s *recordingState) {
	select {
	case <-s.rec.Done():
		r.log.debug("The recorder stopped while preparing: RECORDING is not sent")
		return
	default:
	}
	if s.pending {
		r.log.debug("Stop requested while preparing: the grace runs, RECORDING is not sent")
		return
	}
	r.panel.notifyState(ctx, "RECORDING", "Recording... (Insert to stop)", "")
	s.limit = r.env.After(maxRecording)
	r.log.debug(fmt.Sprintf("%d-second timer started", maxRecordingSeconds))
}

// stopRequested is a TERM (the panel's Insert) or an INT (W16, W17).
func (r *run) stopRequested(ctx context.Context, s *recordingState, sig os.Signal) {
	if sig != syscall.SIGTERM || s.pending || r.grace == 0 {
		r.log.debug("Stop requested - sending SIGINT to the recorder")
		s.rec.Stop()
		return
	}
	s.pending = true
	r.log.debug(fmt.Sprintf("Stop requested - recording for %ds more (TERM again stops now)", r.grace))
	s.grace = r.env.After(time.Duration(r.grace) * time.Second)
	r.panel.state(ctx, "STOPPING")
	r.panel.notify(ctx, fmt.Sprintf("Stopping in %ds - finish your sentence.\nEscape: discard. Insert waits until the text is pasted.", r.grace),
		micIcon, r.grace*1000)
}

// stopRecorder ends a recorder that is still running and waits for it.
func (r *run) stopRecorder() {
	if r.recording == nil {
		return
	}
	r.recording.Stop()
	<-r.recording.Done()
}

func errText(err error) string {
	if err == nil {
		return "status 0"
	}
	return err.Error()
}
