package sessions

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccylaunch"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tokens"
)

// TokenReader is how a ccy record's --token is judged (fedora-desktop#283): the file a launch
// would start on, found as the launcher finds it, and that file's usage, read with the launch's
// own reader (ccylaunch.Front.TokenUsability, which ccy-token-usable runs).
type TokenReader interface {
	// LaunchFile is the token file a launch given --token token starts on, and false when there
	// is none.
	LaunchFile(token string) (string, bool)
	// Usability is the usage left on the token in file, asked of the API unless the cache holds
	// a fresh answer, as the launch asks it.
	Usability(ctx context.Context, file string) ccylaunch.Usability
}

// launchTokens is the TokenReader over ccy's own token pool and usage reader.
type launchTokens struct {
	front ccylaunch.Front
}

// LaunchFile is the launcher's choice for --token (claude-yolo, "Token name provided"): a value
// with a slash is that file; a name is the first <pool>/<name>.*.token in byte order. Either
// must be a regular file.
func (l launchTokens) LaunchFile(token string) (string, bool) {
	file := token
	if !strings.Contains(token, "/") {
		entries, err := os.ReadDir(l.front.TokenDir())
		if err != nil {
			return "", false
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if n := e.Name(); strings.HasPrefix(n, token+".") && strings.HasSuffix(n, tokens.Suffix) && len(n) > len(token)+len(tokens.Suffix) {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			return "", false
		}
		slices.Sort(names)
		file = filepath.Join(l.front.TokenDir(), names[0])
	}
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return file, true
}

// Usability reads the usage as the launch does: fetched unless the cache holds a fresh answer.
func (l launchTokens) Usability(ctx context.Context, file string) ccylaunch.Usability {
	return l.front.TokenUsability(ctx, file, true)
}

// The words restore --check and the manifest give a record whose --token would stop its launch.
const (
	whyTokenMissing     = "token-missing"
	whyTokenExpired     = "token-expired"
	whyTokenUsedUp      = "token-used-up"
	whyTokenCheckFailed = "token-check-failed"
)

// recordedToken is the --token value of a recorded launch: the last one among the launcher's
// own arguments, whatever it is; a --token with nothing after it is none (as in the launcher).
func recordedToken(args []string) (string, bool) {
	own := ownArgs(args)
	token, found := "", false
	for i, word := range own {
		if word == "--token" && i+1 < len(own) {
			token, found = own[i+1], true
		}
	}
	return token, found
}

// tokenRefusal judges a ccy record's --token as its unattended launch would: a token that is
// gone, has expired, has no usage left, or whose usage check fails stops the launch before the
// session starts, and the plan says why. Usage that cannot be read does not stop the launch, so
// the plan is restorable and note says so. A record with no --token is not judged here.
func (a *App) tokenRefusal(ctx context.Context, rec registry.Record, file string) restorePlan {
	token, found := recordedToken(rec.Args)
	if rec.Prefix != prefixCcy || !found {
		return restorePlan{}
	}
	kept := " (the record is kept at " + file + ")."
	launch, found := a.Tokens.LaunchFile(token)
	if !found {
		return restorePlan{why: whyTokenMissing, sentence: "its --token " + token +
			" names no token file in the pool, so its launch would stop at once. Declare it with a token that exists" + kept}
	}
	if base := filepath.Base(launch); !tokens.IsValid(base, a.Now()) {
		return restorePlan{why: whyTokenExpired, sentence: "its token file " + base +
			" has expired or expires today, and renewing it needs a person at a browser: ccy --update-token=" + token + kept}
	}
	u := a.Tokens.Usability(ctx, launch)
	switch u.State {
	case ccylaunch.UsageUsedUp:
		return restorePlan{why: whyTokenUsedUp, sentence: "its token " + token + " has no usage left (" + u.Explain(a.Now()) +
			"), and with nobody to choose another its launch would stop. Check again once it clears, or declare it with another token" + kept}
	case ccylaunch.UsageFault:
		return restorePlan{why: whyTokenCheckFailed, sentence: "the usage left on its token " + token + " could not be checked (" +
			u.Reason + "), and the launch stops when its usage check fails" + kept}
	case ccylaunch.UsageUnknown:
		return restorePlan{note: "the usage left on its token " + token + " could not be read (" + u.Reason +
			"); the launch starts on it anyway, so it counts as restorable."}
	case ccylaunch.UsageLeft:
	}
	return restorePlan{}
}

// newLaunchTokens is the production TokenReader: ccy's launch front over this process's
// environment and the App's runner.
func (a *App) newLaunchTokens() TokenReader {
	return launchTokens{front: ccylaunch.Front{
		Environ: os.Environ(), LookPath: exec.LookPath, Runner: a.Runner, Stdout: a.Stderr, Stderr: a.Stderr,
	}}
}
