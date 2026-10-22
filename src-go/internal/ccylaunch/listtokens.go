package ccylaunch

import (
	"errors"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tokens"
)

// tokenTool is the launcher the token listing names.
const tokenTool = "ccy"

// tokenDir is the bash launcher's TOKEN_DIR, "$HOME/.claude-tokens/ccy/tokens".
func (f Front) tokenDir() string {
	return f.getenv("HOME") + "/.claude-tokens/ccy/tokens"
}

// poolWhy is why the bash could read the token pool at dir otherwise than the front, or "": a
// file is not a regular file, or a name is read otherwise (T1). It also returns the pool's file
// names, in byte order (D1).
func poolWhy(dir string) ([]string, string) {
	names, _, why := tokenGlob(dir, isTokenFile)
	if why != "" {
		return nil, why
	}
	for _, name := range names {
		if !readsAlike(name) {
			return nil, "the bash reads the token file name otherwise: " + name
		}
	}
	return names, ""
}

// listTokens is F10: --list-tokens alone, the bash launcher's list_tokens.
func (f Front) listTokens() (term.ExitCode, string) {
	dir := f.tokenDir()
	if _, why := poolWhy(dir); why != "" {
		return term.ExitFailure, why
	}
	pool, err := tokens.ReadPool(dir)
	if err == nil {
		err = tokens.List(f.Stdout, pool, tokenTool, f.clock().Now(), f.colourOn())
	}
	switch {
	case errors.Is(err, tokens.ErrNoTokens):
		return term.ExitFailure, ""
	case err != nil:
		return f.fail("ccy: " + err.Error()), ""
	}
	return term.ExitOK, ""
}

// colourOn is T3: the expiry is coloured only on a terminal, and never with NO_COLOR (D2).
func (f Front) colourOn() bool {
	return f.colourOnFor(f.Stdout)
}
