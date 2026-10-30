package ccylaunch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// F18, --check-network ENGINE NETWORK: whether a container on NETWORK reaches the internet,
// asked as the launcher always asked it (alpine's wget of http://google.com, the image pulled
// when missing). Three answers:
//
//   - exit 0, silent: wget fetched the page;
//   - ExitNoInternet (4): the probe ran and wget failed, wget's words on stderr (a name it
//     could not resolve, a connection refused or timed out). Only this is the network's fault;
//   - exit 1: the probe could not be run, the cause on stderr: the engine did not start or is
//     not one ccy drives, alpine could not be pulled, or the engine could not run wget. This is
//     never a verdict on the network.
//
// F19, --claude-versions ENGINE IMAGE: the Claude Code version in IMAGE (`claude --version`)
// and the newest the npm registry publishes, printed "INSTALLED LATEST" on stdout, exit 0. Exit 1
// when either could not be read, saying which and why: the image's run and its words, or curl's
// failure (the registry not reached, or its error answer), or an answer naming no version.
//
// Anything but the two arguments is exit 64. Both are answered under CCY_LEGACY too: the bash
// asks them.

const (
	checkNetworkFlag   = "--check-network"
	claudeVersionsFlag = "--claude-versions"
	checkNetworkUsage  = "usage: claude-yolo " + checkNetworkFlag + " ENGINE NETWORK\n"
	versionsUsage      = "usage: claude-yolo " + claudeVersionsFlag + " ENGINE IMAGE\n"
	versionsUnread     = "ccy: the Claude Code versions could not be read: "
	npmLatestURL       = "https://registry.npmjs.org/@anthropic-ai/claude-code/latest"
	// registryWait is how long the registry has to answer: the update check is a launch-time
	// convenience and must not hold a launch up.
	registryWait = 5 * time.Second
	// curlHTTPError is curl's exit for an HTTP error answer under --fail.
	curlHTTPError = 22
)

// ExitNoInternet is --check-network's exit for a probe that ran and reached nothing.
const ExitNoInternet term.ExitCode = 4

// networkProbe is the probe's image and command.
var networkProbe = []string{"alpine", "wget", "-q", "-O-", "--timeout=10", "http://google.com"}

var (
	versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+`)
	versionWord    = regexp.MustCompile(`^[0-9A-Za-z.+-]+$`)
)

// usage answers a wrong argument count with the usage text.
func (f Front) usage(text string) term.ExitCode {
	var p term.Printer
	p.Print(f.Stderr, text)
	if p.Err() != nil {
		return term.ExitFailure
	}
	return term.ExitUsage
}

// checkNetwork is F18.
func (f Front) checkNetwork(ctx context.Context) term.ExitCode {
	if len(f.Args) != 3 {
		return f.usage(checkNetworkUsage)
	}
	network := f.Args[2]
	unchecked := "ccy: the network " + network + " could not be checked: "
	eng, err := engine.New(engine.Kind(f.Args[1]), f.Runner)
	if err != nil {
		return f.fail(unchecked + err.Error())
	}
	res, err := eng.RunOnNetwork(ctx, network, networkProbe[0], networkProbe[1:]...)
	switch {
	case err == nil:
		return term.ExitOK
	case engine.ContainerFailed(err):
		words := strings.TrimSpace(string(res.Stderr))
		if words == "" {
			words = fmt.Sprintf("wget exited %d", res.ExitCode)
		}
		return f.failWith(ExitNoInternet, "ccy: the network "+network+" reaches no internet: "+words)
	default:
		return f.fail(unchecked + eng.Kind().String() + " could not run the probe: " + strings.TrimSpace(err.Error()))
	}
}

// claudeVersions is F19.
func (f Front) claudeVersions(ctx context.Context) term.ExitCode {
	if len(f.Args) != 3 {
		return f.usage(versionsUsage)
	}
	eng, err := engine.New(engine.Kind(f.Args[1]), f.Runner)
	if err != nil {
		return f.fail(versionsUnread + err.Error())
	}
	installed, why := installedVersion(ctx, eng, f.Args[2])
	if why != "" {
		return f.fail(versionsUnread + why)
	}
	latest, why := f.latestVersion(ctx)
	if why != "" {
		return f.fail(versionsUnread + why)
	}
	var p term.Printer
	p.Print(f.Stdout, installed+" "+latest+"\n")
	if err := p.Err(); err != nil {
		return f.fail("ccy: could not write the answer: " + err.Error())
	}
	return term.ExitOK
}

// installedVersion is the version `claude --version` prints in the image, or why there is none.
func installedVersion(ctx context.Context, eng engine.Engine, image string) (string, string) {
	out, err := eng.ClaudeVersion(ctx, image)
	if err != nil {
		return "", strings.TrimSpace(err.Error())
	}
	if v := versionPattern.FindString(out); v != "" {
		return v, ""
	}
	return "", fmt.Sprintf("claude --version in %s printed no version: %q", image, strings.TrimSpace(out))
}

// latestVersion is the version the npm registry names as latest, or why there is none.
func (f Front) latestVersion(ctx context.Context) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, registryWait)
	defer cancel()
	res, err := f.Runner.Run(ctx, execx.Cmd{Name: "curl", Args: []string{"-fsSL", npmLatestURL}})
	exit, exited := errors.AsType[*execx.ExitError](err)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "", fmt.Sprintf("the npm registry did not answer within %s", registryWait)
	case exited && exit.Code == curlHTTPError:
		return "", "the npm registry answered with an error: " + strings.TrimSpace(exit.Stderr)
	case err != nil:
		return "", "the npm registry could not be reached: " + strings.TrimSpace(err.Error())
	}
	var answer struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(res.Stdout, &answer) != nil || !versionWord.MatchString(answer.Version) {
		return "", fmt.Sprintf("the npm registry's answer names no version: %q", strings.TrimSpace(string(res.Stdout)))
	}
	return answer.Version, ""
}
