package ccylaunch

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	altScreenFlag   = "--secret-screen"
	altScreenUsage  = "usage: claude-yolo " + altScreenFlag + " < TEXT\n"
	altScreenPrompt = "\nCopy it, then press Enter to clear it from the screen.\n"
)

// secretScreen is F24: stdin, which holds a secret, copied to stdout through term.SecretScreen.
// On a terminal it is shown on the alternate screen as it arrives (a child's live output, such
// as claude setup-token's sign-in, stays usable), then left once Enter is read from the
// terminal; however the run ends, the screen is left. Not a terminal, it is the payload, copied
// byte for byte.
func (f Front) secretScreen(context.Context) term.ExitCode {
	if len(f.Args) != 1 {
		return f.usage(altScreenUsage)
	}
	isTerminal := f.IsTerminal
	if isTerminal == nil {
		isTerminal = term.FileIsTerminal
	}
	screen := &term.SecretScreen{W: f.Stdout, Terminal: isTerminal(f.Stdout)}
	screen.Open()
	err := f.showSecret(screen)
	screen.Close()
	if err == nil {
		err = screen.Err()
	}
	if err != nil {
		return f.fail("ccy: " + err.Error())
	}
	return term.ExitOK
}

func (f Front) showSecret(screen *term.SecretScreen) error {
	if f.Stdin != nil {
		if _, err := io.Copy(screen, f.Stdin); err != nil {
			return errors.New("could not show the text:" + err.Error())
		}
	}
	if !screen.Showing() {
		return nil
	}
	screen.Show(altScreenPrompt)
	open := f.OpenTTY
	if open == nil {
		open = func() (io.ReadCloser, error) { return os.Open("/dev/tty") }
	}
	tty, err := open()
	if err != nil {
		return errors.New("could not open the terminal to read the Enter: " + err.Error())
	}
	_, err = bufio.NewReader(tty).ReadString('\n')
	if cerr := tty.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return errors.New("no Enter was read (" + err.Error() + "); the screen is cleared")
	}
	return nil
}
