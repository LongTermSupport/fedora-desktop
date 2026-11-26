// Package chatclientinfo is chat-client-info: a read-only look at the chat clients' flatpaks
// for a cc --teams session, whose deny rules refuse any command that names a client's app id
// (R82, Plan 00265).
//
// It is the one hole in those rules, and it is fail-closed: it takes no argument at all, and it
// runs only the fixed argv table below, `flatpak info` and `flatpak info --show-permissions` for
// each client of the list it was built with (internal/matrixclients) and one `flatpak ps`. It
// never runs another flatpak verb, never enters a sandbox and never reads a client's data.
package chatclientinfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/matrixclients"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// InstallPath is where play-claude-yolo.yml installs the command.
const InstallPath = "/var/local/claude-code/chat-client-info"

// CallBound is the longest one flatpak call may run.
const CallBound = 30 * time.Second

const (
	name = "chat-client-info"
	// flatpak is run by its full path in a clean environment, so neither the caller's PATH nor
	// anything else it exports (LD_PRELOAD, FLATPAK_USER_DIR) chooses what runs or what it reads.
	flatpak = "/usr/bin/flatpak"
	// psColumns are the columns of a running instance shown; the application column is what a
	// row is matched to its client by.
	psColumns = "--columns=instance,pid,application"
	// notInstalled is flatpak's word for an app id it has no install of, under LC_ALL=C.
	notInstalled = "not installed"
	// envSection is where flatpak info --show-permissions lists the variables an override
	// sets; their values are not printed, since an override could carry a token.
	envSection = "[Environment]"
)

// App is one run of the command.
type App struct {
	Runner  execx.Runner
	Clients []matrixclients.Client
	// Home and UID are the account's, from the password database, never the caller's
	// environment: they place flatpak's per-user installation and runtime directory.
	Home   string
	UID    int
	Stdout io.Writer
	Stderr io.Writer
}

// Table is every argv the command runs, in order: the running instances once, then each
// client's install and its permissions.
func Table(clients []matrixclients.Client) [][]string {
	table := [][]string{psArgv()}
	for _, c := range clients {
		table = append(table, infoArgv(c.AppID), permissionsArgv(c.AppID))
	}
	return table
}

func psArgv() []string                   { return []string{flatpak, "ps", psColumns} }
func infoArgv(id string) []string        { return []string{flatpak, "info", id} }
func permissionsArgv(id string) []string { return []string{flatpak, "info", "--show-permissions", id} }

// Run is the command. Any argument is refused: what it reads is fixed when it is built, so a
// caller can never point it at another app or another verb.
func (a *App) Run(ctx context.Context, args []string) term.ExitCode {
	if len(args) > 0 {
		a.fail("takes no arguments; it prints flatpak info, the permissions and the running " +
			"instances of each chat client it was built with")
		return term.ExitUsage
	}
	report, err := a.report(ctx)
	if err != nil {
		a.fail(err.Error())
		return term.ExitFailure
	}
	if _, err := a.Stdout.Write(report); err != nil {
		a.fail(fmt.Sprintf("write the report: %v", err))
		return term.ExitFailure
	}
	return term.ExitOK
}

func (a *App) fail(msg string) {
	if _, err := fmt.Fprintf(a.Stderr, "%s: %s\n", name, msg); err != nil {
		return
	}
}

// report is the whole output, built before any of it is printed so a failure prints none.
func (a *App) report(ctx context.Context) ([]byte, error) {
	ps, err := a.run(ctx, psArgv())
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	for _, c := range a.Clients {
		fmt.Fprintf(&out, "== %s (%s) ==\n", c.Name, c.AppID)
		info, err := a.run(ctx, infoArgv(c.AppID))
		if isNotInstalled(err) {
			out.WriteString(notInstalled + "\n\n")
			continue
		}
		if err != nil {
			return nil, err
		}
		perms, err := a.run(ctx, permissionsArgv(c.AppID))
		if err != nil {
			return nil, err
		}
		out.Write(withNewline(info))
		out.WriteString("-- permissions --\n")
		out.Write(withNewline(hideEnvironment(perms)))
		out.WriteString("-- running --\n")
		out.WriteString(running(ps, c.AppID))
		out.WriteString("\n")
	}
	return out.Bytes(), nil
}

// run runs one argv of the table under CallBound.
func (a *App) run(ctx context.Context, argv []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, CallBound)
	defer cancel()
	res, err := a.Runner.Run(ctx, execx.Cmd{Name: flatpak, Args: argv[1:], CleanEnv: true, Env: a.env()})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}
	return res.Stdout, nil
}

// env is flatpak's whole environment: a fixed PATH, the C locale (so its "not installed" is
// the English words), and the account's own home and runtime directory.
func (a *App) env() []string {
	return []string{
		"PATH=/usr/bin:/bin", "LC_ALL=C", "HOME=" + a.Home,
		"XDG_RUNTIME_DIR=/run/user/" + strconv.Itoa(a.UID),
	}
}

// hideEnvironment replaces each value in the permissions' [Environment] section with <hidden>.
func hideEnvironment(perms []byte) []byte {
	var out bytes.Buffer
	inEnv := false
	for line := range strings.SplitAfterSeq(string(perms), "\n") {
		body := strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(body, "[") {
			inEnv = body == envSection
		}
		if key, _, ok := strings.Cut(body, "="); inEnv && ok {
			line = strings.Replace(line, body, key+"=<hidden>", 1)
		}
		out.WriteString(line)
	}
	return out.Bytes()
}

// isNotInstalled is flatpak info's answer for an app id with no install: it ran, exited
// non-zero and said so. Any other failure is a fault.
func isNotInstalled(err error) bool {
	exit, ok := errors.AsType[*execx.ExitError](err)
	return ok && execx.ExitedNonZero(err) && strings.Contains(exit.Stderr, notInstalled)
}

// running is the rows of flatpak ps whose application column is id, or "not running".
func running(ps []byte, id string) string {
	var rows strings.Builder
	for line := range strings.SplitSeq(string(ps), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == id {
			rows.WriteString(line + "\n")
		}
	}
	if rows.Len() == 0 {
		return "not running\n"
	}
	return rows.String()
}

func withNewline(b []byte) []byte {
	if len(b) > 0 && !bytes.HasSuffix(b, []byte("\n")) {
		return append(b, '\n')
	}
	return b
}
