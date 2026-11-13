package term

import "io"

// AltScreenOn enters the terminal's alternate screen, clears it and homes the cursor;
// AltScreenOff leaves it, putting back the normal screen as it was. The alternate screen keeps
// no scrollback, and tmux keeps it out of a pane's history.
const (
	AltScreenOn  = "\x1b[?1049h\x1b[H\x1b[2J"
	AltScreenOff = "\x1b[?1049l"
)

// SecretScreen is the one way a program writes a secret (a password, a token) to a stream a
// person may read (rule project_secret_scrollback). On a terminal it is the terminal's
// alternate screen: entered for the secret and left, by Close, once the person is done with
// it, so the secret leaves no copy in the scrollback. A stream that is not a terminal (a pipe,
// a file) has no screen and no scrollback: the secret is written to it plainly, as the payload
// its reader asked for. Terminal says which W is.
type SecretScreen struct {
	W        io.Writer
	Terminal bool

	open bool
	err  error
}

// Open enters the alternate screen, once; on a stream that is not a terminal it does nothing.
// A child whose output holds a secret runs between Open and Close.
func (s *SecretScreen) Open() {
	if s.open || !s.Terminal {
		return
	}
	s.open = true
	s.write(AltScreenOn)
}

// Show writes text, which holds a secret, on the alternate screen, entering it first.
func (s *SecretScreen) Show(text string) {
	s.Open()
	s.write(text)
}

// Write is Show as an io.Writer, for a child's output that holds a secret.
func (s *SecretScreen) Write(p []byte) (int, error) {
	s.Show(string(p))
	if s.err != nil {
		return 0, s.err
	}
	return len(p), nil
}

// Showing is whether the alternate screen is open.
func (s *SecretScreen) Showing() bool { return s.open }

// Close leaves the alternate screen if it is open, taking every line written on it away.
func (s *SecretScreen) Close() {
	if !s.open {
		return
	}
	s.open = false
	s.write(AltScreenOff)
}

// Err is the first write that failed, nil if every one landed.
func (s *SecretScreen) Err() error { return s.err }

func (s *SecretScreen) write(text string) {
	if _, err := io.WriteString(s.W, text); err != nil && s.err == nil {
		s.err = err
	}
}
