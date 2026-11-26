// Command agent-bus-lightning writes one Lightning instance and launcher per team the desktop's
// human watches in Lightning, and removes the launchers of teams that are not (Plan 00254 Task
// 5.2); its watch verb is the diagnostics watchdog the agent-bus-lightning-watch user service
// runs (Plan 00268 Task 1.7). The flow is internal/lightning; play-agent-bus-element.yml runs it.
package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/lightning"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(int(code))
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) term.ExitCode {
	return lightning.App{
		Args: args, Stdin: stdin, Stdout: stdout, Stderr: stderr,
		Euid: os.Geteuid(), UID: os.Getuid(), Home: passwdHome,
		Watch: &lightning.WatchEnv{
			Ctx: ctx, Proc: os.DirFS("/proc"), Run: execx.OS{}, Now: time.Now, After: time.After,
		},
	}.Run()
}

// passwdHome is a uid's home in the passwd database, as element.py's pwd.getpwuid.
func passwdHome(uid int) (string, error) {
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}
