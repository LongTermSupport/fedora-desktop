package ttyecho

import (
	"context"
	"os"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// FakeModes is a terminal's mode for tests: the mode a hold set, every descriptor it asked
// about, and a failure to read or set it.
type FakeModes struct {
	Mode   unix.Termios
	Sets   int
	FDs    []int
	GetErr error
	SetErr error
}

// Get is the mode, or GetErr.
func (f *FakeModes) Get(fd int) (*unix.Termios, error) {
	f.FDs = append(f.FDs, fd)
	if f.GetErr != nil {
		return nil, f.GetErr
	}
	m := f.Mode
	return &m, nil
}

// Set keeps t as the mode, or fails with SetErr.
func (f *FakeModes) Set(fd int, t *unix.Termios) error {
	f.FDs = append(f.FDs, fd)
	if f.SetErr != nil {
		return f.SetErr
	}
	f.Mode = *t
	f.Sets++
	return nil
}

// Echo is whether the terminal echoes what is typed.
func (f *FakeModes) Echo() bool { return f.Mode.Lflag&unix.ECHO != 0 }

// Cooked is a terminal as a shell leaves it: echoing, line by line, signals on.
const Cooked = unix.ECHO | unix.ECHOCTL | unix.ICANON | unix.ISIG

// Fake is a hold over tty, taken for a terminal in Cooked mode, and the mode a test reads; it
// watches no signal.
func Fake(tty *os.File) (*Hold, *FakeModes) {
	modes := &FakeModes{Mode: unix.Termios{Lflag: Cooked}}
	h := &Hold{
		TTY: tty, IsTerminal: func(v any) bool { return v == tty }, Modes: modes,
		Notify: func(chan<- os.Signal, ...os.Signal) {}, StopNotify: func(chan<- os.Signal) {},
		Ignored: func(os.Signal) bool { return false }, Raise: func(os.Signal) error { return nil },
		Stderr: os.Stderr,
	}
	return h, modes
}

// Screen is what a ui screen does first under h: the hold taken, as a test of a command needs
// before it checks that the run puts the mode back.
func Screen(h *Hold) error {
	_, err := h.Runner(noScreen{}).Run(context.Background(), execx.Cmd{Name: "a-screen", EchoHeld: true})
	return err
}

// noScreen runs nothing.
type noScreen struct{}

func (noScreen) Run(context.Context, execx.Cmd) (execx.Result, error) { return execx.Result{}, nil }
