// Command ccc runs Claude Code in a safe container in ~/CCC (internal/ccc).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ccc"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// RequiredVersion is ccy's REQUIRED_CONTAINER_VERSION, set by the play with -ldflags -X.
var RequiredVersion string

func main() {
	os.Exit(int(run(context.Background(), os.Args[1:])))
}

func run(ctx context.Context, args []string) term.ExitCode {
	home, homeErr := os.UserHomeDir()
	who, userErr := identityOf(user.Current())
	runner := execx.OS{}
	app := &ccc.App{
		Runner:          runner,
		Foreground:      runner,
		Guard:           &ccc.PtyGuard{Foreground: runner, In: os.Stdin, Out: os.Stdout, InFd: terminalFd(os.Stdin)},
		Stdin:           os.Stdin,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		IsTerminal:      term.FileIsTerminal,
		Home:            home,
		Now:             time.Now,
		User:            who,
		TimeZone:        zoneName(os.Readlink("/etc/localtime")),
		Term:            os.Getenv("TERM"),
		Pid:             os.Getpid(),
		RequiredVersion: RequiredVersion,
		SetupErr:        errors.Join(homeErr, userErr),
	}
	return app.Run(ctx, args)
}

// identityOf is the account the folder's snapshots are committed as.
func identityOf(me *user.User, err error) (ccc.Identity, error) {
	if err != nil {
		return ccc.Identity{}, fmt.Errorf("find the account: %w", err)
	}
	return ccc.Identity{Login: me.Username, FullName: fullName(me)}, nil
}

// terminalFd is f's descriptor when f is a terminal, for the guard's raw mode; -1 otherwise.
func terminalFd(f *os.File) int {
	if term.FileIsTerminal(f) {
		return int(f.Fd())
	}
	return -1
}

// zoneName is the zone /etc/localtime links to ("Europe/London"); empty, and the container
// left on UTC, when it is not a link into a zoneinfo tree.
func zoneName(link string, err error) string {
	if err != nil {
		return ""
	}
	if _, zone, ok := strings.Cut(link, "zoneinfo/"); ok {
		return zone
	}
	return ""
}

// fullName is the account's full name: the first field of its GECOS entry.
func fullName(u *user.User) string {
	return strings.Split(u.Name, ",")[0]
}
