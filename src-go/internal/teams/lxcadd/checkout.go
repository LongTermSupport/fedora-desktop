package lxcadd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui/pathinput"
)

// The Checkout step: a line the checkout's path is typed in, with Tab completing a directory
// name from the container's tree as a shell does. The checkouts the search found (the user's
// home and the common web and service roots) are suggested under it, ccy projects first, and
// the line starts with the first. Every listing is read from the host's side, as root, by the
// same reader as the checkout (never by entering the container), and bounded in time.

// searched says where the checkout search looks, for what is said about it.
const searched = "/var/www, /srv, /opt"

// maxNameBytes is the longest directory name a file system holds.
const maxNameBytes = 255

// PathAsker asks for a path on a line with Tab completion; *pathinput.Asker is the real one.
type PathAsker interface {
	Ask(ctx context.Context, s pathinput.Spec) (string, error)
}

func (a *Adder) askCheckout(ctx context.Context, ans *answers) error {
	found := a.findCheckouts(ctx, ans)
	hint := "Where the agent's repository is checked out, inside the container: its top directory. " +
		"The handle's repo, host and seat come from it as ccy derives them: the repo from its origin, the host from " +
		".claude/ccy/ccy.env.local (else local), the seat the next free one. " +
		"Type its path; Tab completes a directory name from the container's tree, as a shell does. " +
		"The checkouts with a GitHub remote found in the home of " + ans.user + ", " + searched +
		" are suggested under the line, ccy projects first; the arrows put one in it."
	if err := a.step(ctx, ans, "Checkout", hint); err != nil {
		return err
	}
	value := "/"
	if len(found) > 0 {
		value = found[0].Path
	}
	for range maxTries {
		picked, err := a.Paths.Ask(ctx, a.pathSpec(ans, found, value))
		if errors.Is(err, ui.ErrLeave) || errors.Is(err, ui.ErrCancelled) {
			return a.cancelled()
		}
		if err != nil {
			return a.failed("the checkout's path could not be asked: " + err.Error())
		}
		good, err := a.checkoutOK(ctx, ans, picked)
		if err != nil {
			return err
		}
		if good {
			ans.checkout = picked
			a.term.Answered(picked)
			return nil
		}
		value = withSlash(path.Dir(picked))
	}
	a.term.Printf("Giving up after %d tries; nothing was changed.\n", maxTries)
	return &stop{term.ExitFailure}
}

// findCheckouts is the checkouts with a GitHub remote the search found, ccy projects first. A
// search that cannot be read, takes too long, or stopped at a limit is said; the path is typed.
func (a *Adder) findCheckouts(ctx context.Context, ans *answers) []declare.Checkout {
	bounded, cancel := context.WithTimeout(ctx, a.ReadTimeout)
	defer cancel()
	scan, err := a.Lister.ReadCheckout(bounded, a.script, "checkouts", ans.container, ans.user, "-")
	var found declare.Checkouts
	if err == nil {
		found, err = a.Declare.LxcCheckouts(ctx, scan)
	}
	switch {
	case err != nil && errors.Is(bounded.Err(), context.DeadlineExceeded):
		a.term.Printf("The search for checkouts in %s took too long and was stopped (after %s); type the checkout's path.\n",
			ans.container, a.ReadTimeout)
		return nil
	case err != nil:
		a.term.Printf("The checkouts in %s could not be listed: %s\nType the checkout's path.\n", ans.container, err.Error())
		return nil
	case found.Partial:
		a.term.Printf("The search for checkouts in %s stopped at its time or count limit, so it may have missed some; "+
			"those it found are suggested, or type the path.\n", ans.container)
	case len(found.Found) == 0:
		a.term.Printf("No checkout with a GitHub remote was found in %s (the home of %s, %s); type its path.\n",
			ans.container, ans.user, searched)
	}
	return found.Found
}

// pathSpec is the checkout line: its heading, the checkouts found as suggestions, and Tab's
// listing of the container's directories.
func (a *Adder) pathSpec(ans *answers, found []declare.Checkout, value string) pathinput.Spec {
	suggestions := make([]pathinput.Suggestion, 0, len(found))
	for _, c := range found {
		note := c.Repo
		if c.Ccy {
			note += ", a ccy project"
		}
		suggestions = append(suggestions, pathinput.Suggestion{Path: c.Path, Note: note})
	}
	return pathinput.Spec{
		Heading: a.head, Prompt: "Checkout in " + ans.container + ": ",
		Value: value, Suggestions: suggestions,
		List: func(ctx context.Context, dir string) (pathinput.Listing, error) { return a.listing(ctx, ans, dir) },
	}
}

// listing is one directory of the container for Tab: its subdirectories, a git checkout noted,
// and what was left out said.
func (a *Adder) listing(ctx context.Context, ans *answers, dir string) (pathinput.Listing, error) {
	l, err := a.listDir(ctx, ans, dir)
	if err != nil {
		return pathinput.Listing{}, err
	}
	if !l.Exists {
		return pathinput.Listing{}, fmt.Errorf("%s does not exist in %s", dir, ans.container)
	}
	out := pathinput.Listing{Entries: make([]pathinput.Entry, 0, len(l.Dirs))}
	for _, e := range l.Dirs {
		entry := pathinput.Entry{Name: e.Name}
		if e.Git {
			entry.Note = "a git checkout"
		}
		out.Entries = append(out.Entries, entry)
	}
	var notes []string
	if l.Truncated {
		notes = append(notes, "Only the first directories are listed; type more of a name.")
	}
	if l.unshown > 0 {
		notes = append(notes, fmt.Sprintf("%d name that cannot be shown safely is left out.", l.unshown))
	}
	out.Note = strings.Join(notes, " ")
	return out, nil
}

// listing is one directory as the reader lists it.
type listing struct {
	Path      string  `json:"path"`
	Exists    bool    `json:"exists"`
	Git       bool    `json:"git"`
	Truncated bool    `json:"truncated"`
	Dirs      []entry `json:"dirs"`
	// unshown is how many names were left out because they could steer the terminal.
	unshown int
}

type entry struct {
	Name string `json:"name"`
	Git  bool   `json:"git"`
}

// listDir reads one directory of the container, bounded in time. dir ends in a slash.
func (a *Adder) listDir(ctx context.Context, ans *answers, dir string) (listing, error) {
	bounded, cancel := context.WithTimeout(ctx, a.ReadTimeout)
	defer cancel()
	asked := dir
	if asked != "/" {
		asked = strings.TrimSuffix(dir, "/")
	}
	out, err := a.Lister.ReadCheckout(bounded, a.script, "dirs", ans.container, ans.user, asked)
	if err != nil {
		if errors.Is(bounded.Err(), context.DeadlineExceeded) {
			return listing{}, fmt.Errorf("listing %s in %s took longer than %s", dir, ans.container, a.ReadTimeout)
		}
		return listing{}, err
	}
	return parseListing(out)
}

// parseListing judges what the reader printed for a directory: the container controls the names.
func parseListing(out string) (listing, error) {
	dec := json.NewDecoder(bytes.NewReader([]byte(out)))
	dec.DisallowUnknownFields()
	var l listing
	if err := dec.Decode(&l); err != nil {
		return listing{}, fmt.Errorf("the directory listing could not be read: %w", err)
	}
	if !strings.HasPrefix(l.Path, "/") || l.Dirs == nil {
		return listing{}, errors.New("the directory listing is not one the reader prints")
	}
	kept := l.Dirs[:0]
	for _, e := range l.Dirs {
		if e.Name == "" || e.Name == "." || e.Name == ".." || strings.Contains(e.Name, "/") || len(e.Name) > maxNameBytes {
			return listing{}, fmt.Errorf("the directory listing of %s names %q, which is not a directory name", l.Path, e.Name)
		}
		if strings.ContainsFunc(e.Name, func(r rune) bool { return !unicode.IsPrint(r) }) {
			l.unshown++
			continue
		}
		kept = append(kept, e)
	}
	l.Dirs = kept
	return l, nil
}

// withSlash is a directory as the line starts with it, ending in a slash.
func withSlash(dir string) string {
	if strings.HasSuffix(dir, "/") {
		return dir
	}
	return dir + "/"
}
