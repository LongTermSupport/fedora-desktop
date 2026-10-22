package forge

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/clock"
)

const (
	// tokenLife is how long a credential read from gh is held (B 2.6 step 11).
	tokenLife     = 10 * time.Minute
	maxTokenBytes = 1024
	redacted      = "forge.Token(redacted)"
)

// ghToken is the one command that reads the credential (R44).
var ghToken = execx.Cmd{Name: "gh", Args: []string{"auth", "token", "--hostname", "github.com"}}

// Token is the gh credential. It prints as redacted under every verb; its value reaches only
// a request's Authorization header (a source test holds this).
type Token struct{ credential string }

// String is the token as it may be shown: never its value.
func (Token) String() string { return redacted }

// GoString is String, so %#v cannot show the value either.
func (Token) GoString() string { return redacted }

// Format prints the token redacted whatever the verb, so %x or %d cannot reach the field.
func (Token) Format(f fmt.State, _ rune) {
	if _, err := io.WriteString(f, redacted); err != nil {
		return
	}
}

func (t Token) header() string { return "Bearer " + t.credential }

// Login reads the gh credential through execx when a check needs it, holds it at most
// tokenLife on the boot clock, and forgets it at once on Drop (after a 401). Safe for
// concurrent use: concurrent readers share one gh run.
type Login struct {
	run   execx.Runner
	clock clock.Clock
	mu    sync.Mutex
	held  bool
	token Token
	at    time.Duration
}

// NewLogin is a login that runs gh through r; a nil c is the machine's clock.
func NewLogin(r execx.Runner, c clock.Clock) *Login {
	if c == nil {
		c = clock.System()
	}
	return &Login{run: r, clock: c}
}

// Token is the held credential, read from gh when none is held or it is tokenLife old.
// A missing gh, a gh that exits non-zero or prints a token off its grammar (1-1024 bytes of
// \x21-\x7e after one trailing newline is trimmed) is ErrLogin; a context that ends while gh
// runs is the context's error.
func (l *Login) Token(ctx context.Context) (Token, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held && l.clock.Boot()-l.at < tokenLife {
		return l.token, nil
	}
	l.forget()
	res, err := l.run.Run(ctx, ghToken)
	switch {
	case ctx.Err() != nil:
		return Token{}, fmt.Errorf("forge: reading the gh login: %w", ctx.Err())
	case execx.IsNotInstalled(err):
		return Token{}, fmt.Errorf("%w: gh is not installed", ErrLogin)
	case err != nil || res.ExitCode != 0:
		return Token{}, fmt.Errorf("%w: gh auth token failed (exit %d)", ErrLogin, res.ExitCode)
	}
	value := strings.TrimSuffix(string(res.Stdout), "\n")
	if !tokenShaped(value) {
		return Token{}, fmt.Errorf("%w: gh printed no token", ErrLogin)
	}
	l.held, l.token, l.at = true, Token{credential: value}, l.clock.Boot()
	return l.token, nil
}

// Drop forgets the held credential, so the next request reads it again.
func (l *Login) Drop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.forget()
}

func (l *Login) forget() { l.held, l.token = false, Token{} }

func tokenShaped(s string) bool {
	if s == "" || len(s) > maxTokenBytes {
		return false
	}
	for i := range len(s) {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}
