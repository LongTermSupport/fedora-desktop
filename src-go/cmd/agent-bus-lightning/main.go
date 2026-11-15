// Command agent-bus-lightning writes one Lightning instance and launcher per team the desktop's
// human watches in Lightning, and removes the launchers of teams that are not (Plan 00254 Task
// 5.2). The flow is internal/lightning; play-agent-bus-element.yml runs it.
package main

import (
	"io"
	"os"
	"os/user"
	"strconv"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/lightning"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) term.ExitCode {
	return lightning.App{
		Args: args, Stdin: stdin, Stdout: stdout, Stderr: stderr,
		Euid: os.Geteuid(), UID: os.Getuid(), Home: passwdHome,
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
