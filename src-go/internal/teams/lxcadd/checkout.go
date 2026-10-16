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
)

// The Checkout step. The checkouts the home search found are offered as choices, ccy projects
// first, beside "Type a path…", which opens the container's directories: a list walked with the
// arrows and Enter, with Tab completing what is typed, as a shell does. Every listing is read
// from the host's side, as root, by the same reader as the checkout (never by entering the
// container), and bounded in time.

const (
	// typePath is the choice that opens the directory list.
	typePath = "Type a path…"

	// actComplete completes what is typed, or opens the highlighted directory as Enter does: a
	// row of the list, with Tab its shortcut, as a shell has it.
	actComplete = "complete"

	// maxBrowse is how many moves the directory list takes before the run gives up.
	maxBrowse = 200

	// The tags of the rows that are not directories.
	tagUse = "use"
	tagUp  = "up"
	tagDir = "dir:"

	// maxNameBytes is the longest directory name a file system holds.
	maxNameBytes = 255
)

// errBack is the person leaving the directory list.
var errBack = errors.New("back to the checkouts found")

func (a *Adder) askCheckout(ctx context.Context, ans *answers) error {
	found := a.findCheckouts(ctx, ans)
	hint := "Where the agent's repository is checked out, inside the container: its top directory. " +
		"The handle's repo, host and seat come from it as ccy derives them: the repo from its origin, the host from " +
		".claude/ccy/ccy.env.local (else local), the seat the next free one. " +
		"Offered: the checkouts in its home with a GitHub remote, ccy projects first; " + typePath +
		" walks the container's directories, with Tab completing a name."
	if err := a.step(ctx, ans, "Checkout", hint); err != nil {
		return err
	}
	start := "/home/" + ans.user + "/"
	for range maxTries {
		picked, err := a.pickCheckout(ctx, ans, found, start)
		if err != nil {
			return err
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
		start = path.Dir(picked) + "/"
	}
	a.term.Printf("Giving up after %d tries; nothing was changed.\n", maxTries)
	return &stop{term.ExitFailure}
}

// findCheckouts is the checkouts in the user's home with a GitHub remote, ccy projects first. A
// search that cannot be read, or that takes too long, is said, and the path is typed instead.
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
		a.term.Printf("The search of the home of %s took too long and was stopped (after %s); type the checkout's path.\n",
			ans.user, a.ReadTimeout)
		return nil
	case err != nil:
		a.term.Printf("The checkouts in the home of %s could not be listed: %s\nType the checkout's path.\n", ans.user, err.Error())
		return nil
	case found.TimedOut:
		a.term.Printf("The search of the home of %s stopped at its time limit, so it may have missed some; "+
			"those it found are offered, or type the path.\n", ans.user)
	case len(found.Found) == 0:
		a.term.Printf("No checkout with a GitHub remote was found in the home of %s; type its path.\n", ans.user)
	}
	return found.Found
}

// checkoutRow is how a found checkout is offered.
func checkoutRow(c declare.Checkout) string {
	if c.Ccy {
		return c.Path + "   (" + c.Repo + ", a ccy project)"
	}
	return c.Path + "   (" + c.Repo + ")"
}

// pickCheckout is a found checkout chosen, or a path from the directory list. Leaving the list
// goes back to the checkouts found; with none found, it cancels.
func (a *Adder) pickCheckout(ctx context.Context, ans *answers, found []declare.Checkout, start string) (string, error) {
	if len(found) == 0 {
		chosen, err := a.browse(ctx, ans, start, "Cancel")
		if errors.Is(err, errBack) {
			return "", a.cancelled()
		}
		return chosen, err
	}
	rows := make([]string, 0, len(found)+1)
	for _, c := range found {
		rows = append(rows, checkoutRow(c))
	}
	rows = append(rows, typePath)
	for {
		choice, err := a.Gum.Choose(ctx, a.header(ans, "Checkout", "Checkout in "+ans.container), rows[0], rows)
		if err != nil {
			return "", a.gumErr(err)
		}
		for i, c := range found {
			if choice == rows[i] {
				return c.Path, nil
			}
		}
		chosen, err := a.browse(ctx, ans, start, "Back to the checkouts found")
		if !errors.Is(err, errBack) {
			return chosen, err
		}
	}
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

// browser is the directory list's state: the directory shown and what is typed in it.
type browser struct {
	dir, query string
	shown      *listing
}

// browse walks the container's directories from start and is the path chosen, or errBack when
// the person leaves. A directory that cannot be listed is said and the list stays where it was;
// when the first cannot be listed, the container cannot be read and the run ends saying why.
func (a *Adder) browse(ctx context.Context, ans *answers, start, back string) (string, error) {
	b := &browser{dir: start}
	for range maxBrowse {
		if err := a.show(ctx, ans, b); err != nil {
			return "", err
		}
		picked, err := a.Picker.Pick(ctx, a.browseSpec(ans, b, back))
		if errors.Is(err, ui.ErrLeave) {
			return "", errBack
		}
		if err != nil {
			return "", err
		}
		if chosen, done := a.move(ctx, ans, b, picked); done {
			return chosen, nil
		}
	}
	a.term.Printf("Giving up after %d moves in the directory list; nothing was changed.\n", maxBrowse)
	return "", &stop{term.ExitFailure}
}

// show reads the directory to show, unless it is the one shown. A directory that does not exist
// is said and its parent shown.
func (a *Adder) show(ctx context.Context, ans *answers, b *browser) error {
	for b.shown == nil || b.shown.Path != listed(b.dir) {
		l, err := a.listDir(ctx, ans, b.dir)
		if err != nil && b.shown == nil {
			return a.failed("the directories of " + ans.label() + " could not be read: " + err.Error())
		}
		if err != nil {
			a.problem(err.Error(), false)
			b.dir, b.query = withSlash(b.shown.Path), ""
			continue
		}
		if !l.Exists && b.dir != "/" {
			a.term.Printf("  %s does not exist in %s.\n", b.dir, ans.container)
			b.dir = withSlash(path.Dir(strings.TrimSuffix(b.dir, "/")))
			continue
		}
		b.shown = &l
	}
	return nil
}

// browseSpec is the directory list: the directories (hidden ones are reached by typing), using
// this directory (first when it is a checkout), and up.
func (a *Adder) browseSpec(ans *answers, b *browser, back string) ui.PickSpec {
	var rows, tags []string
	add := func(row, tag string) {
		rows = append(rows, row)
		tags = append(tags, tag)
	}
	use := "Use " + listed(b.dir) + " as the checkout"
	if b.shown.Git {
		add(use+"   (a git checkout)", tagUse)
	}
	for _, e := range b.shown.Dirs {
		if strings.HasPrefix(e.Name, ".") {
			continue
		}
		row := e.Name + "/"
		if e.Git {
			row += "   (a git checkout)"
		}
		add(row, tagDir+e.Name)
	}
	if !b.shown.Git {
		add(use, tagUse)
	}
	if b.dir != "/" {
		add("../   (up to "+listed(path.Dir(listed(b.dir)))+")", tagUp)
	}
	notes := []string{
		a.context(ans, "Checkout"),
		"Type to filter. Text that matches no row: Tab completes it as a path, Enter takes it as typed (end it with / to open it).",
	}
	if b.shown.Truncated {
		notes = append(notes, "Only the first directories are listed; type a name to reach another.")
	}
	if b.shown.unshown > 0 {
		notes = append(notes, fmt.Sprintf("%d name that cannot be shown safely is left out.", b.shown.unshown))
	}
	return ui.PickSpec{
		Title:   a.context(ans, "Checkout"),
		Header:  ui.Header("Enter open or choose   Tab complete   Esc back", notes...),
		Actions: []ui.Action{{ID: actComplete, Label: "Complete what is typed as a path", Key: "tab"}},
		Rows:    rows, Tags: tags, ExitLabel: back,
		Typed: true, Prompt: b.dir, Query: b.query,
	}
}

// move is where a choice in the directory list leads: the path chosen (done), or the next
// directory and text to show.
func (a *Adder) move(ctx context.Context, ans *answers, b *browser, picked ui.Picked) (string, bool) {
	b.query = ""
	switch {
	case picked.Action == actComplete && picked.Query != "":
		// Tab completes what is typed, whichever row fzf highlights for it.
		a.complete(ctx, ans, b, picked.Query)
	case picked.Tag == tagUse:
		return listed(b.dir), true
	case picked.Tag == tagUp:
		b.dir = withSlash(path.Dir(listed(b.dir)))
	case strings.HasPrefix(picked.Tag, tagDir):
		b.dir += strings.TrimPrefix(picked.Tag, tagDir) + "/"
	case picked.Action == actComplete && picked.Tag == "":
		// Tab with nothing typed and no row: nothing to complete.
	default:
		typed := resolve(b.dir, picked.Query)
		if !strings.HasSuffix(picked.Query, "/") {
			return listed(typed), true
		}
		b.dir = withSlash(typed)
	}
	return "", false
}

// complete is Tab on typed text: the one directory it starts, or the text the several it starts
// share; nothing to complete is said.
func (a *Adder) complete(ctx context.Context, ans *answers, b *browser, typed string) {
	full := resolve(b.dir, typed)
	if strings.HasSuffix(typed, "/") {
		b.dir = withSlash(full)
		return
	}
	parent, frag := withSlash(path.Dir(full)), path.Base(full)
	if parent != b.dir {
		b.dir = parent
		if err := a.show(ctx, ans, b); err != nil {
			a.problem(err.Error(), false)
			return
		}
		if b.dir != parent {
			return
		}
	}
	var matches []string
	for _, e := range b.shown.Dirs {
		if strings.HasPrefix(e.Name, frag) {
			matches = append(matches, e.Name)
		}
	}
	switch len(matches) {
	case 0:
		a.term.Printf("  Nothing in %s starts with %s.\n", b.dir, frag)
	case 1:
		b.dir += matches[0] + "/"
	default:
		b.query = sharedPrefix(matches)
	}
}

// resolve is typed text as an absolute path in the container: from the root when it starts with a
// slash, else from dir.
func resolve(dir, typed string) string {
	if strings.HasPrefix(typed, "/") {
		return path.Clean(typed)
	}
	return path.Clean(dir + typed)
}

// listed is a directory as the reader names it: no trailing slash, except the root.
func listed(dir string) string {
	if dir == "/" {
		return dir
	}
	return strings.TrimSuffix(dir, "/")
}

// withSlash is a directory as the prompt shows it, ending in a slash.
func withSlash(dir string) string {
	if strings.HasSuffix(dir, "/") {
		return dir
	}
	return dir + "/"
}

// sharedPrefix is the longest text every name starts with, whole characters only.
func sharedPrefix(names []string) string {
	prefix := []rune(names[0])
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, string(prefix)) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return string(prefix)
}
