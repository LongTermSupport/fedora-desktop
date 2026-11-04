// Command release-plan is what the release workflow asks about a push to a release branch:
// what it calls for (decide), the changelog entry to commit (prepare), and the notes of a
// published release (notes). It changes nothing but the changelog file prepare writes.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/release"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr)))
}

// run builds the app over the checkout in the working directory and the GitHub repository named
// by GITHUB_REPOSITORY, which the workflow sets.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) term.ExitCode {
	dir, err := os.Getwd()
	if err != nil {
		return fail(stderr, err)
	}
	repo := getenv("GITHUB_REPOSITORY")
	runner := execx.OS{}
	return release.App{
		Out: stdout,
		Err: stderr,
		Git: gitx.New(runner),
		Pulls: func(branch string) release.Pulls {
			return release.GitHubPulls{Runner: runner, Repo: repo, Branch: branch}
		},
		Dir:    dir,
		Remote: "origin",
		Now:    time.Now,
		Files:  release.OSFiles{Root: dir},
	}.Run(context.Background(), args)
}

func fail(stderr io.Writer, err error) term.ExitCode {
	if _, werr := fmt.Fprintf(stderr, "release-plan: %v\n", err); werr != nil {
		return term.ExitFailure
	}
	return term.ExitFailure
}
