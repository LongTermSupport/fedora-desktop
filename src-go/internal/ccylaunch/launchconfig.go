package ccylaunch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/checkoutpin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// F16, the bash launcher's saved launch configuration (checkoutpin.LastLaunch), which only the
// front reads and writes, always through internal/checkoutpin (Plan 00266 K5):
//
//   - `--read-launch-config CHECKOUT attended|unattended` hands the file back one
//     `KEY<TAB>value` line per key, in the file's order, for the bash to take with `read` (no
//     source, no eval); no file is exit 0 and nothing. The file is read strictly
//     (checkoutpin.ParseLastLaunch): a file that cannot be read or is malformed is exit 1, one
//     stderr line naming the path and the cause, nothing on stdout, and the file is never
//     changed. unattended (a restore, a restart, a headless launch, no terminal) takes only
//     content whose digest is the one recorded: the launcher's own last write, or a person's
//     approval (ccy-teams approve last-launch); anything else is exit 1 naming the file and that
//     verb, since a container session can write it and its keys choose the SSH keys forwarded
//     and the network joined. attended hands the recorded content back unasked. Other content
//     is shown at a terminal (stdin and stderr), every value through checkoutpin.Shown, and
//     asked about with a default of no (Plan 00266 review finding 8): a typed yes hands it back
//     and records its digest as a person's approval; Enter or no hands nothing back (exit 0, so
//     the launcher asks each choice) and records nothing; with no terminal it is refused as an
//     unattended read is.
//   - `--write-launch-config CHECKOUT` writes the text on stdin as the file, refusing text the
//     reader would refuse, and records its digest.
//   - `--launch-network CHECKOUT` prints the network the file joins (nothing for none or no
//     file); with `--forget NETWORK` it blanks LAST_NETWORK when it names NETWORK, pinned again
//     when the file was the recorded content, removed when it was not (a forget never approves
//     the rest of a file nobody approved).
//
// CHECKOUT is an absolute path. Which keys a format needs, and what a format other than this
// ccy's means, is the launcher's to decide. Answered under CCY_LEGACY too: the bash launcher asks
// them, so handing them over would ask the bash itself.

const (
	launchConfigFlag   = "--read-launch-config"
	writeConfigFlag    = "--write-launch-config"
	launchNetworkFlag  = "--launch-network"
	launchConfigUsage  = "usage: claude-yolo " + launchConfigFlag + " CHECKOUT attended|unattended\n"
	writeConfigUsage   = "usage: claude-yolo " + writeConfigFlag + " CHECKOUT < TEXT\n"
	launchNetworkUsage = "usage: claude-yolo " + launchNetworkFlag + " CHECKOUT [--forget NETWORK]\n"
	maxLaunchConfig    = 64 << 10
)

// launchConfig is F16's read.
func (f Front) launchConfig(ctx context.Context) term.ExitCode {
	if len(f.Args) != 3 || !filepath.IsAbs(f.Args[1]) || (f.Args[2] != "attended" && f.Args[2] != "unattended") {
		return f.usage(launchConfigUsage)
	}
	saved, err := f.savedChoices(f.Args[1])
	if err != nil {
		return f.fail("ccy: cannot read the saved launch configuration " + err.Error())
	}
	settings, verified := saved.settings, saved.refused
	if saved.content == nil {
		return term.ExitOK
	}
	if verified != nil {
		if f.Args[2] == "unattended" || !f.atTerminal() {
			return f.fail("ccy: " + verified.Error())
		}
		approved, code := f.approveChoices(ctx, saved)
		if !approved {
			return code
		}
	}
	var out strings.Builder
	for _, s := range settings {
		out.WriteString(s.Key + "\t" + s.Value + "\n")
	}
	var p term.Printer
	p.Print(f.Stdout, out.String())
	if err := p.Err(); err != nil {
		return f.fail("ccy: could not write the saved launch configuration: " + err.Error())
	}
	return term.ExitOK
}

// atTerminal is whether a person is there to ask: stdin and stderr are terminals (stdout is the
// launcher's capture of the answer).
func (f Front) atTerminal() bool {
	isTerminal := f.IsTerminal
	if isTerminal == nil {
		isTerminal = term.FileIsTerminal
	}
	return f.Stdin != nil && isTerminal(f.Stdin) && isTerminal(f.Stderr)
}

// approveChoices shows saved choices ccy did not write, every value through checkoutpin.Shown,
// and asks whether to use them, defaulting to no (Plan 00266 review finding 8). A yes records
// their digest as a person's approval; a no records nothing and hands nothing back, so the
// launcher asks each choice. approved is false with the code to exit with.
func (f Front) approveChoices(ctx context.Context, s saved) (approved bool, code term.ExitCode) {
	path := checkoutpin.LastLaunchPath(s.checkout)
	var shown strings.Builder
	shown.WriteString("\n" + checkoutpin.Shown(path) + ": " + checkoutpin.Shown(s.refused.Why) + ".\n")
	shown.WriteString("ccy did not write these Quick Launch choices; a container session can. They choose the SSH keys forwarded and the network joined:\n\n")
	for _, setting := range s.settings {
		shown.WriteString("  " + checkoutpin.Shown(setting.Key) + "=" + checkoutpin.Shown(setting.Value) + "\n")
	}
	var p term.Printer
	p.Print(f.Stderr, shown.String()+"\n")
	if err := p.Err(); err != nil {
		return false, term.ExitFailure
	}
	h := ui.Here("ccy", "Quick Launch").About(checkoutpin.Shown(path))
	yes, err := f.lines(launchConfigFlag).YesNo(ctx, h, "Use these choices, and approve them for launches nobody attends?", false)
	switch {
	case errors.Is(err, ui.ErrCancelled):
		return false, term.ExitCancelled
	case err != nil:
		return false, f.fail("ccy: " + err.Error())
	case !yes:
		p.Print(f.Stderr, "Not used, and not approved: the launch asks each choice.\n")
		if p.Err() != nil {
			return false, term.ExitFailure
		}
		return false, term.ExitOK
	}
	store, err := checkoutpin.Locate(f.getenv)
	if err == nil {
		err = store.Record(s.checkout, checkoutpin.KindLastLaunch, s.digest, f.clock().Now())
	}
	if err != nil {
		return false, f.fail("ccy: could not record the approval of " + path + ": " + err.Error())
	}
	return true, term.ExitOK
}

// saved is a checkout's saved choices as read: its real path, the file's bytes (nil for no
// file), its settings, and the refusal a launch nobody attends would meet (nil when the content
// is the recorded one).
type saved struct {
	checkout string
	content  []byte
	digest   string
	settings []checkoutpin.Setting
	refused  *checkoutpin.RefusalError
}

// savedChoices reads the file in the checkout at dir, read once, so what is judged is what is
// handed back. err is a file that does not read, its path first.
func (f Front) savedChoices(dir string) (saved, error) {
	checkout, err := checkoutpin.RealCheckout(dir)
	if err != nil {
		return saved{}, fmt.Errorf("in %s: %w", dir, err)
	}
	s := saved{checkout: checkout}
	path := checkoutpin.LastLaunchPath(checkout)
	content, digest, present, err := checkoutpin.LastLaunchDigest(checkout)
	if err != nil || !present {
		return s, wrapPath(path, err)
	}
	if s.settings, err = checkoutpin.ParseLastLaunch(content); err != nil {
		return s, wrapPath(path, err)
	}
	store, err := checkoutpin.Locate(f.getenv)
	if err != nil {
		return s, wrapPath(path, err)
	}
	s.content, s.digest = content, digest
	err = store.Verify(checkout, checkoutpin.KindLastLaunch, digest)
	if refused, ok := errors.AsType[*checkoutpin.RefusalError](err); ok {
		s.refused = refused
		return s, nil
	}
	return s, wrapPath(path, err)
}

func wrapPath(path string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", path, err)
}

// writeLaunchConfig is F16's write.
func (f Front) writeLaunchConfig() term.ExitCode {
	if len(f.Args) != 2 || !filepath.IsAbs(f.Args[1]) {
		return f.usage(writeConfigUsage)
	}
	if f.Stdin == nil {
		return f.fail("ccy: " + writeConfigFlag + " writes its standard input, and there is none")
	}
	text, err := stdinread.Read(f.Stdin, stdinread.Options{
		Prog: "claude-yolo " + writeConfigFlag, Err: f.Stderr, Limit: maxLaunchConfig + 1,
		GiveUpAfter: stdinread.PipedGiveUp, RefuseTerminal: true, Clock: f.InputClock, IsTerminal: f.IsTerminal,
	})
	if err != nil {
		return f.fail("ccy: could not read the launch configuration to save: " + err.Error())
	}
	if len(text) > maxLaunchConfig {
		return f.fail("ccy: the launch configuration to save is longer than any ccy writes")
	}
	return f.saveChoices(f.Args[1], text, "")
}

// saveChoices writes text as the checkout's saved choices and pins it, saying said on stdout.
func (f Front) saveChoices(dir string, text []byte, said string) term.ExitCode {
	checkout, err := checkoutpin.RealCheckout(dir)
	if err != nil {
		return f.fail("ccy: cannot save the launch configuration in " + dir + ": " + err.Error())
	}
	store, err := checkoutpin.Locate(f.getenv)
	if err == nil {
		err = checkoutpin.WriteLastLaunch(store, checkout, text, f.clock().Now())
	}
	if err != nil {
		return f.fail("ccy: " + err.Error())
	}
	return f.say(said)
}

// launchNetwork is F16's network question and forget.
func (f Front) launchNetwork() term.ExitCode {
	forget := len(f.Args) == 4 && f.Args[2] == "--forget" && f.Args[3] != ""
	if (len(f.Args) != 2 && !forget) || !filepath.IsAbs(f.Args[1]) {
		return f.usage(launchNetworkUsage)
	}
	s, err := f.savedChoices(f.Args[1])
	if err != nil {
		return f.fail("ccy: cannot read the saved launch configuration " + err.Error())
	}
	var named string
	for _, setting := range s.settings {
		if setting.Key == "LAST_NETWORK" {
			named = setting.Value
		}
	}
	if !forget {
		if named == "" {
			return term.ExitOK
		}
		return f.say(named + "\n")
	}
	network, path := f.Args[3], checkoutpin.LastLaunchPath(s.checkout)
	if s.content == nil || named != network {
		return term.ExitOK
	}
	if s.refused != nil {
		if err := checkoutpin.RemoveLastLaunch(s.checkout); err != nil {
			return f.fail("ccy: could not remove " + path + ", so it still names " + network + ": " + err.Error())
		}
		return f.say("Removed the Quick Launch configuration (" + path + "): it names " + network + " and " + s.refused.Why + ", so it is not kept; the next launch asks again.\n")
	}
	// The file parsed strictly, so the network is on exactly this line, once.
	text := strings.Replace("\n"+string(s.content), "\nLAST_NETWORK=\""+network+"\"\n", "\nLAST_NETWORK=\"\"\n", 1)[1:]
	return f.saveChoices(s.checkout, []byte(text), "Cleared "+network+" from the Quick Launch configuration ("+path+").\n")
}

// say prints text on stdout; nothing is exit 0 too.
func (f Front) say(text string) term.ExitCode {
	var p term.Printer
	p.Print(f.Stdout, text)
	if err := p.Err(); err != nil {
		return f.fail("ccy: could not write the answer: " + err.Error())
	}
	return term.ExitOK
}
