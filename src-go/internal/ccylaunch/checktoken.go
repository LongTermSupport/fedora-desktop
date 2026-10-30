package ccylaunch

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// F17, --check-token ENGINE IMAGE: whether the API accepts the token in CLAUDE_CODE_OAUTH_TOKEN,
// asked from inside the image the session will run in. The engine runs the image with the
// token by name only (engine.RunWithSecret: never in argv, the image never pulled), and in it
// curl sends the usage reader's one small request with the token fed on its stdin by the
// shell's printf, so no argv in the container holds it either. Three answers:
//
//   - exit 0, silent: the API took the token (HTTP 200, or 429: a token it rate-limits is one
//     it knows; whether its usage is left is ccy-token-usable's question, F15);
//   - ExitTokenRejected (3): the API refused it (HTTP 401 or 403), or it is not a token at
//     all (a character outside the token grammar). This alone is the case for token recovery;
//   - exit 1: the check could not be made, its cause on stderr: the engine did not start or
//     could not run the image, the image is not built, curl could not reach the API, or the API
//     answered something that says nothing about the token (5xx, 400, no answer). This is
//     never a verdict on the token.
//
// Anything but ENGINE and IMAGE is exit 64. Answered under CCY_LEGACY too: the bash asks it.

const (
	checkTokenFlag  = "--check-token"
	checkEnvName    = "CLAUDE_CODE_OAUTH_TOKEN"
	checkTokenUsage = "usage: " + checkEnvName + "=TOKEN claude-yolo " + checkTokenFlag + " ENGINE IMAGE\n"
	checkUnchecked  = "ccy: the token could not be checked: "
	// checkScript runs in the image: the token goes to curl in its config on stdin, by the
	// shell's printf builtin, and curl's own arguments follow as the script's arguments.
	checkScript = `printf 'header = "Authorization: Bearer %s"\n' "$` + checkEnvName + `" | curl --config - "$@"`
)

// ExitTokenRejected is --check-token's exit for a token the API refused.
const ExitTokenRejected term.ExitCode = 3

// tokenGrammar is every character a Claude OAuth token is made of.
var tokenGrammar = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// checkToken is F17.
func (f Front) checkToken(ctx context.Context) term.ExitCode {
	if len(f.Args) != 3 {
		var p term.Printer
		p.Print(f.Stderr, checkTokenUsage)
		if p.Err() != nil {
			return term.ExitFailure
		}
		return term.ExitUsage
	}
	kind, image := engine.Kind(f.Args[1]), f.Args[2]
	value := f.getenv(checkEnvName)
	if value == "" {
		return f.fail(checkUnchecked + checkEnvName + " holds no token")
	}
	if !tokenGrammar.MatchString(value) {
		return f.failWith(ExitTokenRejected, "ccy: this is not a token: it holds a character no token has")
	}
	settings, why := f.usageSettings()
	if why != "" {
		return f.fail(checkUnchecked + why)
	}
	eng, err := engine.New(kind, f.Runner)
	if err != nil {
		return f.fail(checkUnchecked + err.Error())
	}
	secret, err := execx.NewSecret(checkEnvName, value)
	if err != nil {
		return f.fail(checkUnchecked + err.Error())
	}
	res, err := eng.RunWithSecret(ctx, image, secret, "sh", checkArgs(settings)...)
	switch {
	case errors.Is(err, engine.ErrNoImage):
		return f.fail(fmt.Sprintf("%sthe image %s is not built (%s has no such image)", checkUnchecked, image, kind))
	case err != nil:
		// The engine's own failure or curl's, with the words each gave.
		return f.fail(checkUnchecked + strings.TrimSpace(err.Error()))
	}
	switch code := strings.TrimSpace(string(res.Stdout)); code {
	case "200", "429":
		return term.ExitOK
	case "401", "403":
		return f.failWith(ExitTokenRejected, "ccy: the API refused the token (HTTP "+code+")")
	default:
		return f.fail(checkUnchecked + "the API answered HTTP " + code + ", which says nothing about the token")
	}
}

// checkArgs is the shell's argv in the image: the script, its $0, and curl's arguments, which
// are the usage reader's request with its body and headers discarded.
func checkArgs(s usageSettings) []string {
	return []string{
		"-c", checkScript, "ccy-check-token",
		"--silent", "--show-error", "--request", "POST",
		curlHeader, "content-type: application/json",
		curlHeader, "anthropic-version: 2023-06-01",
		curlHeader, "anthropic-beta: oauth-2025-04-20",
		"--data", usageRequest(s.model),
		"--connect-timeout", s.connect, "--max-time", s.timeout,
		"--output", "/dev/null", "--write-out", "%{http_code}", s.endpoint,
	}
}
