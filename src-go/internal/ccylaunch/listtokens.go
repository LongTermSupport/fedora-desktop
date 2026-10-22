package ccylaunch

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tokens"
)

// tokenTool is the launcher the token listing names.
const tokenTool = "ccy"

// tokenDir is the bash launcher's TOKEN_DIR, "$HOME/.claude-tokens/ccy/tokens".
func (f Front) tokenDir() string {
	return f.getenv("HOME") + "/.claude-tokens/ccy/tokens"
}

// poolWhy is why the bash could read the token pool at dir otherwise than the front, or "": the
// path is not clean (the bash prints it as built), the zone is read otherwise, a file is not a
// regular file, the glob would sort otherwise, a name is read otherwise (T1), or an expiry would
// be coloured otherwise (a day the clocks change). It also returns the pool's file names.
func (f Front) poolWhy(dir string, now time.Time) ([]string, string) {
	if filepath.Clean(dir) != dir {
		return nil, "the token directory's path is not clean: " + dir
	}
	if !zoneReadsAlike(f.getenv) {
		return nil, "the local zone could be read otherwise"
	}
	names, _, why := tokenGlob(dir, isTokenFile)
	if why != "" {
		return nil, why
	}
	if !orderAlike(names, collationIsBytes(f.getenv)) {
		return nil, "the collation could list the pool in another order"
	}
	for _, name := range names {
		if !readsAlike(name) {
			return nil, "the bash reads the token file name otherwise: " + name
		}
		if m := bashDate.FindStringSubmatch(name); m != nil && !colourAlike(m[1], now) {
			return nil, "the bash colours the expiry otherwise today: " + name
		}
	}
	return names, ""
}

// listTokens is F10: --list-tokens alone, the bash launcher's list_tokens.
func (f Front) listTokens() (term.ExitCode, string) {
	dir := f.tokenDir()
	now := f.clock().Now()
	if _, why := f.poolWhy(dir, now); why != "" {
		return term.ExitFailure, why
	}
	return f.printPool(dir, now), ""
}

// printPool writes the pool listing to stdout: exit 1 when the pool is empty, as the bash's
// list_tokens returns 1 then, or when the listing could not be written.
func (f Front) printPool(dir string, now time.Time) term.ExitCode {
	pool, err := tokens.ReadPool(dir)
	if err == nil {
		// Coloured whatever stdout is, as the bash colours it (T3's terminal-only colour waits
		// for the bash to be retired, so both launchers print the same bytes meanwhile).
		err = tokens.List(f.Stdout, pool, tokenTool, now, true)
	}
	switch {
	case errors.Is(err, tokens.ErrNoTokens):
		return term.ExitFailure
	case err != nil:
		return f.fail("ccy: " + err.Error())
	}
	return term.ExitOK
}
