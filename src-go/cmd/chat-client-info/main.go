// Command chat-client-info prints, for each chat client it was built with, `flatpak info`, its
// permissions and its running instances, for a cc --teams session whose deny rules refuse any
// command naming a client's app id (internal/chatclientinfo). It takes no arguments.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/chatclientinfo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/matrixclients"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(execx.OS{}, user.Current, os.Args[1:], os.Stdout, os.Stderr)))
}

// run builds the app from the embedded list and the account's password-database entry, never
// from the caller's environment.
func run(runner execx.Runner, account func() (*user.User, error), args []string, stdout, stderr io.Writer) term.ExitCode {
	list, err := matrixclients.Load()
	if err != nil {
		return fail(stderr, term.ExitConfig, "the client list", err)
	}
	me, err := account()
	if err != nil {
		return fail(stderr, term.ExitFailure, "this account", err)
	}
	uid, err := strconv.Atoi(me.Uid)
	if err != nil {
		return fail(stderr, term.ExitFailure, "this account's uid", err)
	}
	app := &chatclientinfo.App{
		Runner: runner, Clients: list.Clients, Home: me.HomeDir, UID: uid, Stdout: stdout, Stderr: stderr,
	}
	return app.Run(context.Background(), args)
}

func fail(stderr io.Writer, code term.ExitCode, what string, err error) term.ExitCode {
	if _, werr := fmt.Fprintf(stderr, "chat-client-info: %s: %v\n", what, err); werr != nil {
		return term.ExitFailure
	}
	return code
}
