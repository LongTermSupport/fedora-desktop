package checkoutpin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Command is the three verbs over the store (Plan 00266 Tasks A.2 and A.3):
//
//   - V1. `ccy-teams internal project-mounts`, the launcher's, run in the checkout before a
//     container exists: no .claude/ccy/mounts prints nothing; CCY_NO_PROJECT_MOUNTS=1 ignores
//     it, saying so on stderr; otherwise every line is validated (ParseMounts, with the
//     launcher's state directory denied wherever it lives), each source must exist and the
//     path it resolves to is judged again (a link cannot reach a denied path), and the file's
//     content must be the content a person approved (Store.Verify). Any failure is exit 1 with
//     every finding on stderr and nothing on stdout, attended or not: the launch never half
//     applies the file and never asks. Passed, stdout is `-v` and `<src>:<dst>:<opt>` per
//     mount, one per line, the source resolved, for the launcher's run.
//   - V2. `ccy-teams internal checkout-pin compose <unattended>`, which the launcher asks before
//     a compose up: with true (a restore, a restart's relaunch, a launch with nobody to ask) the
//     compose files' digest must be the one recorded, else exit 1 naming the files and the verb;
//     with false (a person is there and chose to start them) the digest is recorded. No compose
//     file is exit 0 either way.
//   - V3. `ccy-teams approve mounts|compose|last-launch [CHECKOUT]` (the checkout defaults to the
//     working directory), a person's: shown what it approves (each mount resolved, every line of
//     every file the compose digest covers, or each saved launch choice), a [y/N] question at a terminal, and only a yes records the digest. No terminal is
//     exit 1, so neither a session nor a pipe can approve; a mounts file that breaks a rule cannot
//     be approved, nor a saved launch file the launcher would not read.
//
// Wrong arguments are exit 64.
type Command struct {
	Getenv func(string) string
	Getwd  func() (string, error)
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
	// Confirm asks a person at a terminal; nil when stdin or stderr is not one.
	Confirm func(ctx context.Context, question string) (bool, error)
}

const (
	noMountsEnv  = "CCY_NO_PROJECT_MOUNTS"
	usageApprove = "usage: ccy-teams approve mounts|compose|last-launch [CHECKOUT]"
	usageCompose = "usage: ccy-teams internal checkout-pin compose true|false"
)

// ProjectMounts is V1.
func (c Command) ProjectMounts(args []string) term.ExitCode {
	if len(args) != 0 {
		return c.usage("usage: ccy-teams internal project-mounts")
	}
	if c.Getenv(noMountsEnv) == "1" {
		c.say("⚠  " + noMountsEnv + "=1: ignoring " + Mounts)
		return term.ExitOK
	}
	dir, err := c.checkout("")
	if err != nil {
		return c.fail(err)
	}
	content, digest, present, err := MountsDigest(dir)
	if err != nil {
		return c.fail(fmt.Errorf("%s/%s cannot be read, so its project mounts cannot be checked: %w", dir, Mounts, err))
	}
	if !present {
		return term.ExitOK
	}
	mounts, store, ok := c.validMounts(dir, content, true)
	if !ok {
		c.say("  Fix the lines above, comment them out, or set " + noMountsEnv + "=1 to launch without this project's declared mounts.")
		return term.ExitFailure
	}
	if err := store.Verify(dir, KindMounts, digest); err != nil {
		c.say("✗ " + err.Error())
		c.say("  Or launch without them: " + noMountsEnv + "=1.")
		return term.ExitFailure
	}
	var out strings.Builder
	for _, m := range mounts {
		out.WriteString("-v\n" + m.Src + ":" + m.Dst + ":" + m.Opt + "\n")
		c.say("✓ Project mount: " + m.Src + " → " + m.Dst + " (" + m.Opt + ")")
	}
	if _, err := io.WriteString(c.Stdout, out.String()); err != nil {
		return term.ExitFailure
	}
	return term.ExitOK
}

// validMounts is the file's mounts, their sources resolved and judged again, or false with
// every problem said. A launch (mustExist) needs every source there; an approval does not, since
// an rclone mount may not be up while a person approves.
func (c Command) validMounts(dir string, content []byte, mustExist bool) ([]Mount, Store, bool) {
	home := c.Getenv("HOME")
	store, err := Locate(c.Getenv)
	if err != nil || !filepath.IsAbs(home) {
		c.say(fmt.Sprintf("✗ the project mounts cannot be judged: HOME is %q (%v)", home, err))
		return nil, Store{}, false
	}
	extra := []string{store.Base()}
	denied := Denied(home, extra)
	mounts, problems := ParseMounts(content, home, dir, extra)
	for i, m := range mounts {
		resolved, err := filepath.EvalSymlinks(m.Src)
		if err != nil {
			if mustExist {
				problems = append(problems, Problem{Line: m.Line, Why: "host path does not exist: " + m.Src +
					". If it is a mount (an rclone FUSE mount), start it first: an absent mount binds as nothing."})
			}
			continue
		}
		for _, why := range HostProblems(resolved, home, dir, denied) {
			problems = append(problems, Problem{Line: m.Line, Why: why + ", which " + m.Src + " leads to"})
		}
		mounts[i].Src = resolved
	}
	for _, p := range problems {
		c.say("✗ " + dir + "/" + Mounts + " line " + strconv.Itoa(p.Line) + ":\n  " + p.Why)
	}
	if len(problems) > 0 {
		c.say(fmt.Sprintf("%d problem(s) in %s/%s — not applied.", len(problems), dir, Mounts))
		return nil, store, false
	}
	return mounts, store, true
}

// Compose is V2.
func (c Command) Compose(args []string) term.ExitCode {
	if len(args) != 1 || (args[0] != "true" && args[0] != "false") {
		return c.usage(usageCompose)
	}
	dir, err := c.checkout("")
	if err != nil {
		return c.fail(err)
	}
	files, digest, present, err := ComposeDigest(dir)
	if err != nil {
		return c.fail(err)
	}
	if !present {
		return term.ExitOK
	}
	store, err := Locate(c.Getenv)
	if err != nil {
		return c.fail(err)
	}
	if args[0] == "true" {
		if err := store.Verify(dir, KindCompose, digest); err != nil {
			c.say("✗ " + err.Error() + " (read: " + strings.Join(files, ", ") + ")")
			return term.ExitFailure
		}
		return term.ExitOK
	}
	if err := store.Record(dir, KindCompose, digest, c.Now()); err != nil {
		return c.fail(err)
	}
	c.say("✓ Recorded the compose files a restore may start (" + strings.Join(files, ", ") + ").")
	return term.ExitOK
}

// Approve is V3.
func (c Command) Approve(ctx context.Context, args []string) term.ExitCode {
	if len(args) < 1 || len(args) > 2 || !slices.Contains([]Kind{KindMounts, KindCompose, KindLastLaunch}, Kind(args[0])) {
		return c.usage(usageApprove)
	}
	kind, given := Kind(args[0]), ""
	if len(args) == 2 {
		given = args[1]
	}
	if c.Confirm == nil {
		return c.fail(errors.New("approving asks a person at a terminal; run " + usageApprove[len("usage: "):] + " in a terminal on this machine, not from a session or a pipe"))
	}
	dir, err := c.checkout(given)
	if err != nil {
		return c.fail(err)
	}
	digest, ok := c.shown(dir, kind)
	if !ok {
		return term.ExitFailure
	}
	store, err := Locate(c.Getenv)
	if err != nil {
		return c.fail(err)
	}
	yes, err := c.Confirm(ctx, "Approve this content of "+dir+" for its launches and restores?")
	if err != nil {
		return c.fail(err)
	}
	if !yes {
		c.say("Not approved; nothing recorded.")
		return term.ExitFailure
	}
	if err := store.Record(dir, kind, digest, c.Now()); err != nil {
		return c.fail(err)
	}
	c.say("✓ Approved. A change to it needs approving again.")
	return term.ExitOK
}

// shown says what an approval of kind in dir would cover and returns its digest, or false when
// there is nothing to approve or it cannot be.
func (c Command) shown(dir string, kind Kind) (string, bool) {
	if kind == KindLastLaunch {
		return c.shownLastLaunch(dir)
	}
	if kind == KindCompose {
		files, digest, present, err := ComposeRead(dir)
		if err != nil || !present {
			c.say(fmt.Sprintf("✗ no compose file to approve in %s (%v)", dir, err))
			return "", false
		}
		c.say("The compose files " + dir + " starts with `up -d`, as you, on this machine (under --engine docker, as root):")
		c.showFiles(files)
		c.say("Read them: a volume they name is bound from this machine, and a build runs its Dockerfile.")
		return digest, true
	}
	content, digest, present, err := MountsDigest(dir)
	if err != nil || !present {
		c.say(fmt.Sprintf("✗ no %s to approve in %s (%v)", Mounts, dir, err))
		return "", false
	}
	mounts, _, ok := c.validMounts(dir, content, false)
	if !ok {
		return "", false
	}
	c.say("The host paths " + dir + "/" + Mounts + " binds into its sessions' containers:")
	for _, m := range mounts {
		c.say("  " + m.Src + " → " + m.Dst + " (" + m.Opt + ")")
	}
	return digest, true
}

// showFiles prints each file under its name, line by line (through say, so escaped: K6).
func (c Command) showFiles(files []PinnedFile) {
	for _, f := range files {
		c.say("── " + f.Name + " ──")
		text := strings.TrimSuffix(string(f.Content), "\n")
		for i, line := range strings.Split(text, "\n") {
			c.say(fmt.Sprintf("%4d  %s", i+1, line))
		}
	}
	c.say("── end ──")
}

// shownLastLaunch says the saved launch choices an approval would let a launch nobody attends
// take, and returns their digest; a file the launcher would not read cannot be approved.
func (c Command) shownLastLaunch(dir string) (string, bool) {
	content, digest, present, err := LastLaunchDigest(dir)
	if err != nil || !present {
		c.say(fmt.Sprintf("✗ no %s to approve in %s (%v)", LastLaunch, dir, err))
		return "", false
	}
	settings, err := ParseLastLaunch(content)
	if err != nil {
		c.say(fmt.Sprintf("✗ %s/%s is not as ccy writes it, so it cannot be approved: %v", dir, LastLaunch, err))
		return "", false
	}
	c.say("The launch choices " + dir + "/" + LastLaunch + " gives a launch with nobody to ask (a restore, a headless launch):")
	for _, s := range settings {
		c.say("  " + s.Key + "=" + strconv.Quote(s.Value))
	}
	c.say("Read them before you approve: LAST_SSH_KEYS are forwarded into the container, LAST_NETWORK is joined.")
	return digest, true
}

// checkout is the real path of the named checkout, or of the working directory.
func (c Command) checkout(given string) (string, error) {
	if given == "" {
		wd, err := c.Getwd()
		if err != nil {
			return "", err
		}
		given = wd
	}
	return RealCheckout(given)
}

// say prints one message line, through Shown: a message may quote checkout text (K6).
func (c Command) say(s string) {
	if _, err := io.WriteString(c.Stderr, Shown(s)+"\n"); err != nil {
		return
	}
}

func (c Command) fail(err error) term.ExitCode {
	c.say("✗ " + err.Error())
	return term.ExitFailure
}

func (c Command) usage(text string) term.ExitCode {
	c.say(text)
	return term.ExitUsage
}
