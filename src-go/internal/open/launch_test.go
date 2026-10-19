package open

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E20: the list format, on stdout.
func TestListFormat(t *testing.T) {
	w, p := pngWorld(t, gioWithDefault)
	if code := w.run("-l", p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	want := p + " (image/png)\n" +
		"   1) Image Viewer [GUI]                         gui\n" +
		"   2) GIMP [GUI]                                 gui\n" +
		"   3) chafa (terminal image)                     term\n" +
		"   4) file (identify only)                       term\n"
	if got := w.stdout.String(); got != want {
		t.Fatalf("stdout\n%q\nwant\n%q", got, want)
	}
	if w.called("gio launch") {
		t.Fatal("--list launched")
	}
}

// E21, B13: a dry run prints the command, shell-quoted, and runs nothing.
func TestDryRun(t *testing.T) {
	w, p := pngWorld(t, gioWithDefault)
	if code := w.run("-n", p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	entry := w.dataDirs + "/applications/org.gnome.Loupe.desktop"
	if got := w.stdout.String(); got != "gio launch "+entry+" "+p+"\n" {
		t.Fatalf("stdout %q", got)
	}
	if w.called("gio launch "+entry+" "+p) || strings.Contains(w.stderr.String(), "→") {
		t.Fatal("a dry run launched")
	}

	w = newWorld(t)
	p = w.file("my file.pdf", "x")
	w.types("application/pdf")
	w.install("pdftotext", "less")
	if code := w.run("--dry-run", p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if got := w.stdout.String(); got != "pdftotext -layout -- '"+p+"' - | less\n" {
		t.Fatalf("stdout %q", got)
	}
}

// E21: a dry run with --set-default on a desktop app shows the registration first.
func TestDryRunWithSetDefault(t *testing.T) {
	w, p := pngWorld(t, gioWithDefault)
	w.picker.chooseRow("1")
	if code := w.run("-n", "-d", p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	entry := w.dataDirs + "/applications/org.gimp.GIMP.desktop"
	if got := w.stdout.String(); got != "gio mime image/png org.gimp.GIMP.desktop\ngio launch "+entry+" "+p+"\n" {
		t.Fatalf("stdout %q", got)
	}
	if w.called("gio mime image/png org.gimp.GIMP.desktop") {
		t.Fatal("a dry run registered a default")
	}
}

// E22, E23, B6: --set-default registers through gio, then launches with stdin from /dev/null.
func TestSetDefaultRegistersThenLaunches(t *testing.T) {
	w, p := pngWorld(t, gioWithDefault)
	w.runner.On([]string{"gio", "mime", "image/png", "org.gimp.GIMP.desktop"}, execx.Result{})
	w.picker.chooseRow("1")
	if code := w.run("-d", p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	entry := w.dataDirs + "/applications/org.gimp.GIMP.desktop"
	ran := w.ran()
	reg, launch := indexOf(ran, "gio mime image/png org.gimp.GIMP.desktop"), indexOf(ran, "gio launch "+entry+" "+p)
	if reg < 0 || launch < reg {
		t.Fatalf("ran %q", ran)
	}
	w.wantStderr("→ " + p + " — GIMP [GUI]\n")
	w.wantStderr("  Registered org.gimp.GIMP.desktop as the default for image/png.\n")
	if c := w.cmd("gio launch " + entry + " " + p); c.Stdin != nil {
		t.Fatalf("gio launch stdin %v", c.Stdin)
	}
}

// E23: a failed registration starts nothing; a failed launch is exit 1.
func TestGioFailures(t *testing.T) {
	w, p := pngWorld(t, gioWithDefault)
	w.runner.On([]string{"gio", "mime", "image/png", "org.gimp.GIMP.desktop"}, execx.Result{ExitCode: 4, Stderr: []byte("denied")})
	w.picker.chooseRow("1")
	if code := w.run("-d", p); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("open: could not set org.gimp.GIMP.desktop as the default for image/png: gio exited 4: denied\n")
	if w.called("gio launch") {
		t.Fatal("launched after a failed registration")
	}

	w, p = pngWorld(t, gioWithDefault)
	w.runner.On([]string{"gio", "launch"}, execx.Result{ExitCode: 1, Stderr: []byte("no display")})
	if code := w.run(p); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("open: gio launch failed: gio exited 1: no display\n")
}

// E24: --set-default on a terminal handler is a note; the handler gets this process's streams.
func TestTerminalHandlerStreamsAndSetDefaultNote(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.install("chafa")
	w.runner.On([]string{"chafa"}, execx.Result{Stdout: []byte("pixels\n")})
	w.picker.chooseRow("0")
	if code := w.run("-d", p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	w.wantStderr("  Note: --set-default applies to GUI apps only; 'chafa (terminal image)' is a terminal handler.\n")
	if w.stdout.String() != "pixels\n" {
		t.Fatalf("the handler's stdout %q", w.stdout.String())
	}
	c := w.cmd("chafa -- " + p)
	if c.Stdin == nil || c.Stderr == nil {
		t.Fatalf("streams %+v", c)
	}
}

// E24: a pipeline feeds the producer's stdout to the pager; the producer reads /dev/null.
func TestPipelineFeedsThePager(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.pdf", "x")
	w.types("application/pdf")
	w.install("pdftotext", "less")
	w.runner.On([]string{"pdftotext"}, execx.Result{Stdout: []byte("page one\n")})
	var paged string
	var mu sync.Mutex
	w.runner.OnFunc([]string{"less"}, func(c execx.Cmd) (execx.Result, error) {
		data, err := io.ReadAll(c.Stdin)
		if err != nil {
			return execx.Result{}, err
		}
		mu.Lock()
		paged = string(data)
		mu.Unlock()
		return execx.Result{}, nil
	})
	if code := w.run(p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if paged != "page one\n" {
		t.Fatalf("the pager read %q", paged)
	}
	if c := w.cmd("pdftotext -layout -- " + p + " -"); c.Stdin != nil {
		t.Fatalf("the producer's stdin %v", c.Stdin)
	}
	if c := w.cmd("less"); c.Stdout == nil {
		t.Fatal("the pager has no terminal")
	}
}

// E25, B10: a handler's non-zero exit is named, exit 1.
func TestHandlerFailureIsNamed(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.install("chafa")
	w.runner.On([]string{"chafa"}, execx.Result{ExitCode: 3})
	if code := w.run(p); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("open: chafa (terminal image) exited 3\n")
}

// E25: in a pipeline the pager's failure counts, and the producer's when the pager had not ended
// first.
func TestPipelineFailures(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.pdf", "x")
	w.types("application/pdf")
	w.install("pdftotext", "less")
	w.runner.On([]string{"pdftotext"}, execx.Result{ExitCode: 1, Stderr: []byte("corrupt")})
	w.runner.OnFunc([]string{"less"}, drainStdin)
	if code := w.run(p); code != term.ExitFailure {
		t.Fatalf("producer failing: exit %d", code)
	}
	w.wantStderr("open: pdftotext (text) exited 1\n")

	w = newWorld(t)
	p = w.file("a.pdf", "x")
	w.types("application/pdf")
	w.install("pdftotext", "less")
	w.runner.On([]string{"pdftotext"}, execx.Result{Stdout: []byte("x\n")})
	w.runner.On([]string{"less"}, execx.Result{ExitCode: 2})
	if code := w.run(p); code != term.ExitFailure {
		t.Fatalf("pager failing: exit %d", code)
	}
	// The pager failed, so the pager is named, not the producer.
	w.wantStderr("open: less exited 2\n")
}

// E29: a handler ended by the person's Ctrl-C ends open quietly, exit 130.
func TestHandlerEndedByCtrlCIsAQuietCancel(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.install("chafa")
	w.runner.OnError([]string{"chafa"}, &execx.ExitError{Name: "chafa", Code: -1, Signal: syscall.SIGINT})
	if code := w.run(p); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
	if got := w.stderr.String(); got != "→ "+p+" — chafa (terminal image)\n" {
		t.Fatalf("stderr %q", got)
	}
}

// E29: a handler that exits non-zero after the person pressed Ctrl-C while it ran (mpv exits
// 4) ends open quietly, exit 130.
func TestHandlerFailingAfterCtrlCIsAQuietCancel(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.mp4", "x")
	w.types("video/mp4")
	w.install("mpv")
	w.runner.OnFunc([]string{"mpv"}, func(execx.Cmd) (execx.Result, error) {
		w.signals <- os.Interrupt
		return execx.Result{ExitCode: 4}, nil
	})
	if code := w.run(p); code != term.ExitCancelled {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(w.stderr.String(), "open: ") {
		t.Fatalf("stderr %q", w.stderr.String())
	}
}

// E29: a producer ended by Ctrl-C while the pager survived it is the same quiet cancel.
func TestPipelineProducerEndedByCtrlCIsAQuietCancel(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.pdf", "x")
	w.types("application/pdf")
	w.install("pdftotext", "less")
	w.runner.OnError([]string{"pdftotext"}, &execx.ExitError{Name: "pdftotext", Code: -1, Signal: syscall.SIGINT})
	w.runner.OnFunc([]string{"less"}, drainStdin)
	if code := w.run(p); code != term.ExitCancelled {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if strings.Contains(w.stderr.String(), "open: ") {
		t.Fatalf("stderr %q", w.stderr.String())
	}
}

// E30: any other signal that ends a handler (or its pager) is named, exit 128+n.
func TestHandlerKilledByAnotherSignalIsNamed(t *testing.T) {
	for _, tc := range []struct {
		sig  syscall.Signal
		code term.ExitCode
		name string
	}{{syscall.SIGTERM, 143, "SIGTERM"}, {syscall.SIGQUIT, 131, "SIGQUIT"}, {syscall.SIGKILL, 137, "SIGKILL"}} {
		w := newWorld(t)
		p := w.file("a.png", "x")
		w.types("image/png")
		w.install("chafa")
		w.runner.OnError([]string{"chafa"}, &execx.ExitError{Name: "chafa", Code: -1, Signal: tc.sig})
		if code := w.run(p); code != tc.code {
			t.Fatalf("%s: exit %d", tc.name, code)
		}
		w.wantStderr("open: chafa (terminal image) killed by " + tc.name + "\n")
	}

	w := newWorld(t)
	p := w.file("a.pdf", "x")
	w.types("application/pdf")
	w.install("pdftotext", "less")
	w.runner.On([]string{"pdftotext"}, execx.Result{Stdout: []byte("x\n")})
	w.runner.OnError([]string{"less"}, &execx.ExitError{Name: "less", Code: -1, Signal: syscall.SIGTERM})
	if code := w.run(p); code != 143 {
		t.Fatalf("pager: exit %d", code)
	}
	w.wantStderr("open: less killed by SIGTERM\n")
}

// E31: an interrupt while a type detector runs cancels; the next detector is not asked, and
// nothing opens. A detector killed by it cancels even when open had not yet seen the signal.
func TestInterruptWhileTypingCancels(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.install("mimetype", "chafa")
	w.runner.OnFunc([]string{"mimetype"}, func(execx.Cmd) (execx.Result, error) {
		w.signals <- os.Interrupt
		return execx.Result{Stdout: []byte("image/png\n")}, nil
	})
	w.runner.On([]string{"chafa"}, execx.Result{})
	if code := w.run(p); code != term.ExitCancelled {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	w.wantStderr("open: cancelled — nothing opened.\n")
	if w.runner.Called("file") || w.runner.Called("chafa") {
		t.Fatalf("carried on: %q", w.ran())
	}

	w = newWorld(t)
	p = w.file("a.png", "x")
	w.types("image/png")
	w.install("mimetype", "chafa")
	w.runner.OnError([]string{"mimetype"}, &execx.ExitError{Name: "mimetype", Code: -1, Signal: syscall.SIGINT})
	if code := w.run(p); code != term.ExitCancelled {
		t.Fatalf("killed detector: exit %d: %s", code, w.stderr.String())
	}
	if w.runner.Called("file") || strings.Contains(w.stderr.String(), "trying the next detector") {
		t.Fatalf("carried on: %q\n%s", w.ran(), w.stderr.String())
	}
}

// E31: an interrupt while gio mime lists the apps cancels.
func TestInterruptDuringGioMimeCancels(t *testing.T) {
	w, p := pngWorld(t, gioWithDefault)
	w.runner.OnFunc([]string{"gio", "mime", "image/png"}, func(execx.Cmd) (execx.Result, error) {
		w.signals <- os.Interrupt
		return execx.Result{Stdout: []byte(gioWithDefault)}, nil
	})
	if code := w.run(p); code != term.ExitCancelled {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if w.called("gio launch") || w.runner.Called("gio launch") {
		t.Fatalf("launched: %q", w.ran())
	}
}

// E31: gio launch failing because of an interrupt (killed by it, or ending after it) is a
// cancel, not a failed launch.
func TestInterruptDuringGioLaunchCancels(t *testing.T) {
	w, p := pngWorld(t, gioWithDefault)
	w.runner.OnError([]string{"gio", "launch"}, &execx.ExitError{Name: "gio", Code: -1, Signal: syscall.SIGINT})
	if code := w.run(p); code != term.ExitCancelled {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	w.wantStderr("open: cancelled — nothing opened.\n")

	w, p = pngWorld(t, gioWithDefault)
	w.runner.OnFunc([]string{"gio", "launch"}, func(execx.Cmd) (execx.Result, error) {
		w.signals <- os.Interrupt
		return execx.Result{ExitCode: 1}, nil
	})
	if code := w.run(p); code != term.ExitCancelled {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if strings.Contains(w.stderr.String(), "gio launch failed") {
		t.Fatalf("stderr %q", w.stderr.String())
	}
}

// E31: an interrupt that arrives after the choice but before the handler runs cancels it.
func TestInterruptBeforeTheHandlerRunsCancels(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.install("chafa")
	w.runner.On([]string{"chafa"}, execx.Result{})
	w.picker.chooseRow("0")
	w.picker.during = func() { w.signals <- os.Interrupt }
	if code := w.run("-a", p); code != term.ExitCancelled {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
	if w.runner.Called("chafa") {
		t.Fatal("the handler ran")
	}
}

// E25: the person quitting the pager early stops the producer, which is not a failure.
func TestQuittingThePagerEarlyIsNotAFailure(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.pdf", "x")
	w.types("application/pdf")
	w.install("pdftotext", "less")
	pagerGone := make(chan struct{})
	w.runner.OnFunc([]string{"pdftotext"}, func(c execx.Cmd) (execx.Result, error) {
		<-pagerGone
		// The pager has ended and its end of the pipe is closed: the write breaks, as SIGPIPE
		// ends a real producer.
		if _, err := c.Stdout.Write([]byte("more\n")); err == nil {
			return execx.Result{}, errors.New("the write after the pager ended should break")
		}
		return execx.Result{ExitCode: 141}, nil
	})
	w.runner.OnFunc([]string{"less"}, func(execx.Cmd) (execx.Result, error) {
		defer close(pagerGone)
		return execx.Result{}, nil
	})
	if code := w.run(p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
}

// E27: a handler gone from PATH by the time it runs is named.
func TestHandlerNotInstalledAtRunTime(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.install("chafa")
	w.runner.OnError([]string{"chafa"}, &execx.NotInstalledError{Name: "chafa"})
	if code := w.run(p); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("open: chafa is not installed\n")
}

// E26: a signal that arrives while a handler runs does not end open; it waits for the handler.
func TestSignalWhileAHandlerRunsIsTheHandlers(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.install("chafa")
	w.runner.OnFunc([]string{"chafa"}, func(execx.Cmd) (execx.Result, error) {
		w.signals <- os.Interrupt
		return execx.Result{}, nil
	})
	if code := w.run(p); code != term.ExitOK {
		t.Fatalf("exit %d: %s", code, w.stderr.String())
	}
}

// E28: a stdout that cannot be written is exit 1.
func TestUnwritableStdoutFails(t *testing.T) {
	w := newWorld(t)
	p := w.file("a.png", "x")
	w.types("image/png")
	w.install("chafa")
	app := w.app()
	app.Env.Stdout = brokenWriter{}
	if code := app.Run(t.Context(), []string{"--list", p}); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	w.wantStderr("open: could not write the output: ")
}

// E28: an ordinary open writes nothing of its own to stdout.
func TestOpeningWritesNothingToStdout(t *testing.T) {
	w, p := pngWorld(t, gioWithDefault)
	if code := w.run(p); code != term.ExitOK || w.stdout.Len() != 0 {
		t.Fatalf("exit %d, stdout %q", code, w.stdout.String())
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func drainStdin(c execx.Cmd) (execx.Result, error) {
	if _, err := io.Copy(io.Discard, c.Stdin); err != nil {
		return execx.Result{}, err
	}
	return execx.Result{}, nil
}

func indexOf(lines []string, line string) int {
	for i, l := range lines {
		if l == line {
			return i
		}
	}
	return -1
}
