package tokens

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// ExitNoUsableToken is a pool with files in it of which none may be used, or none at all.
const ExitNoUsableToken term.ExitCode = 3

// ErrNoTokens is a listing of a pool with no token file in it.
var ErrNoTokens = errors.New("no token files")

// Pool is a token directory as read: whether it exists, and its *.token file names, sorted.
type Pool struct {
	Dir    string
	Exists bool
	Files  []string
}

// ReadPool reads the token directory. A directory that does not exist is a pool that does not
// exist; one that cannot be read is an error. A *.token entry counts when it is, or links to, a
// regular file.
func ReadPool(dir string) (Pool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return Pool{Dir: dir}, nil
	}
	if err != nil {
		return Pool{}, fmt.Errorf("read the token directory: %w", err)
	}
	p := Pool{Dir: dir, Exists: true}
	for _, e := range entries {
		if _, ok := ParseFileName(e.Name()); !ok {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, e.Name()))
		if err == nil && info.Mode().IsRegular() {
			p.Files = append(p.Files, e.Name())
		}
	}
	slices.Sort(p.Files)
	return p, nil
}

// List writes the pool listing (--list-tokens) to w; ErrNoTokens, after saying so, when the
// pool has no token file.
func List(w io.Writer, p Pool, tool string, today time.Time, colour bool) error {
	rule := strings.Repeat("═", 80)
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s\nAvailable Claude Code Tokens for %s\n%s\n\n", rule, tool, rule)
	if len(p.Files) == 0 {
		fmt.Fprintf(&b, "No tokens found in: %s\n\nCreate a token with: ccy --create-token\n\n", p.Dir)
		if _, err := io.WriteString(w, b.String()); err != nil {
			return fmt.Errorf("write the token list: %w", err)
		}
		return ErrNoTokens
	}
	fmt.Fprintf(&b, "Token storage: %s\n\n", p.Dir)
	for _, name := range p.Files {
		path := filepath.Join(p.Dir, name)
		if f, ok := ParseFileName(name); ok && f.Dated {
			fmt.Fprintf(&b, "  • %s\n    File: %s\n    Expires: %s (%s)\n\n",
				f.Name, path, ColourExpiry(f.Expiry.Format(dateLayout), today, colour), f.Status(today))
			continue
		}
		fmt.Fprintf(&b, "  • %s\n    File: %s\n    Status: %s\n\n", name, path, StatusUndated)
	}
	fmt.Fprintf(&b, "%s\n\n", rule)
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write the token list: %w", err)
	}
	return nil
}

// Mode is which launcher asks for a token: ccy's container, or cc on the host.
type Mode int

// The two launchers.
const (
	ModeContainer Mode = iota
	ModeHost
)

// ParseMode reads a mode's name.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "container":
		return ModeContainer, nil
	case "host":
		return ModeHost, nil
	default:
		return 0, fmt.Errorf("invalid token mode %q (expected container or host)", s)
	}
}

// Outcome is what the launcher does with the pool before any question.
type Outcome int

// The outcomes.
const (
	// OutcomeChoose offers the valid tokens.
	OutcomeChoose Outcome = iota
	// OutcomeDesktop uses the host's own (Desktop) account: cc with nothing in the pool.
	OutcomeDesktop
	// OutcomeRefuse stops the launch with Exit.
	OutcomeRefuse
)

// Decision is the launcher's answer to the pool: the valid and expired files, and what it does.
type Decision struct {
	Outcome Outcome
	Exit    term.ExitCode
	Valid   []string
	Expired []string
}

// Decide is the launcher's answer to the pool. ccy never launches without a named token; cc
// falls through to the Desktop account only when the pool holds nothing, and refuses a pool
// whose tokens are all past their stamp, which would otherwise sign in as another account.
func Decide(mode Mode, p Pool, today time.Time) Decision {
	var d Decision
	for _, name := range p.Files {
		if IsValid(name, today) {
			d.Valid = append(d.Valid, name)
		} else {
			d.Expired = append(d.Expired, name)
		}
	}
	switch {
	case len(d.Valid) > 0:
		d.Outcome = OutcomeChoose
	case mode == ModeHost && len(p.Files) == 0:
		d.Outcome = OutcomeDesktop
	case !p.Exists:
		d.Outcome, d.Exit = OutcomeRefuse, term.ExitFailure
	default:
		d.Outcome, d.Exit = OutcomeRefuse, ExitNoUsableToken
	}
	return d
}

// RemoveOthers is what the import does with the other files of the imported name.
type RemoveOthers int

// The three answers of CCY_TOKEN_IMPORT_REMOVE_OTHERS.
const (
	RemoveAsk RemoveOthers = iota
	RemoveNo
	RemoveYes
)

// RemoveOthersEnv is the import's switch.
const RemoveOthersEnv = "CCY_TOKEN_IMPORT_REMOVE_OTHERS"

// usageError is a value the import refuses with exit 64.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

// ParseRemoveOthers reads CCY_TOKEN_IMPORT_REMOVE_OTHERS ("" when unset).
func ParseRemoveOthers(v string) (RemoveOthers, error) {
	switch v {
	case "":
		return RemoveAsk, nil
	case "0":
		return RemoveNo, nil
	case "1":
		return RemoveYes, nil
	default:
		return 0, &usageError{msg: RemoveOthersEnv + " must be 1 (remove), 0 (keep) or unset (ask). Nothing imported."}
	}
}

// ExitOf is the exit code an error from this package ends with.
func ExitOf(err error) term.ExitCode {
	if err == nil {
		return term.ExitOK
	}
	if usage, ok := errors.AsType[*usageError](err); ok && usage != nil {
		return term.ExitUsage
	}
	return term.ExitFailure
}
