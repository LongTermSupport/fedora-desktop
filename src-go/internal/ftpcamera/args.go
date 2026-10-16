package ftpcamera

import (
	"context"
	"errors"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// The viewer classes (E54).
const (
	viewRAW = "RAW"
	viewJPG = "JPG"
)

// options is the command line: at most one primary mode and the modifiers.
type options struct {
	sort, pass, push, copy, preflight, prune bool
	async, asyncCopy                         bool
	viewer                                   string
	hotspot, debugFTP                        bool
}

// server is whether the mode runs the FTP server: default, --async, --async-copy.
func (o options) server() bool {
	return !o.sort && !o.pass && !o.push && !o.copy && !o.preflight && !o.prune
}

var flagSetters = map[string]func(*options){
	"--sort":           func(o *options) { o.sort = true },
	"--pass":           func(o *options) { o.pass = true },
	"--push":           func(o *options) { o.push = true },
	"--copy":           func(o *options) { o.copy = true },
	"--copy-preflight": func(o *options) { o.preflight = true },
	"--prune":          func(o *options) { o.prune = true },
	"--async":          func(o *options) { o.async = true },
	"--async-copy":     func(o *options) { o.async, o.asyncCopy = true, true },
	"--hotspot":        func(o *options) { o.hotspot = true },
	"--debug-ftp":      func(o *options) { o.debugFTP = true },
	"--no-tui":         func(*options) {},
}

var viewerFlags = map[string]string{"--view": viewRAW, "--view-jpg": viewJPG}

// parse reads args left to right (E1-E5): the options, whether help was asked for, and the
// lines of a usage error.
func parse(args []string) (options, bool, []string) {
	var o options
	for _, a := range args {
		if set, ok := flagSetters[a]; ok {
			set(&o)
			continue
		}
		if class, ok := viewerFlags[a]; ok {
			if o.viewer != "" {
				return o, false, []string{"ERROR: --view and --view-jpg are mutually exclusive"}
			}
			o.viewer = class
			continue
		}
		if a == "-h" || a == "--help" {
			return o, true, nil
		}
		return o, false, []string{"ERROR: unknown argument: " + a, "Run 'ftp-camera --help' for usage"}
	}
	return o, false, o.check()
}

// check refuses two primary modes and a server modifier without a server mode (E4, E5).
func (o options) check() []string {
	modes := 0
	for _, on := range []bool{o.sort, o.pass, o.push, o.copy, o.prune, o.preflight, o.async} {
		if on {
			modes++
		}
	}
	if modes > 1 {
		return []string{
			"ERROR: only one primary mode at a time",
			"  Pick one of: --sort --pass --push --copy --copy-preflight --prune --async --async-copy",
		}
	}
	if o.server() {
		return nil
	}
	refusals := []struct {
		on   bool
		what string
		why  string
	}{
		{o.viewer != "", "--view / --view-jpg", "exits before any uploads arrive, so there's nothing to view."},
		{o.hotspot, "--hotspot", "exits without running the FTP server."},
		{o.debugFTP, "--debug-ftp", "never starts vsftpd, so there is no protocol to trace."},
	}
	for _, r := range refusals {
		if r.on {
			return []string{
				"ERROR: " + r.what + " requires an FTP-server mode",
				"  (default, --async, or --async-copy). The mode you picked",
				"  " + r.why,
			}
		}
	}
	return nil
}

// The launcher's questions (E8).
var (
	modeLines = []string{
		"default      [+viewer]  Start FTP server, sort at end on Ctrl+C",
		"async        [+viewer]  Sort each upload as it arrives",
		"async-copy   [+viewer]  Sort + per-file rclone copy as uploads arrive",
		"sort                    Sort already-uploaded files and exit",
		"push                    Direct rclone push of the sort tree and exit",
		"copy                    Mount-write-through copy with verify and exit",
		"prune                   Delete orphan RAW files (interactive) and exit",
		"pass                    Print FTP password and exit",
	}
	viewerLines  = []string{"no viewer", "view RAW", "view JPG"}
	hotspotLines = []string{"no hotspot", "use hotspot"}
)

const (
	modeHeader    = "Pick a primary mode  (viewer + hotspot offered next for server modes)"
	viewerHeader  = "Viewer?  (rare; usually leave 'no viewer')"
	hotspotHeader = "Hotspot?  (skips public WiFi; rare unless on an untrusted network)"
)

// modeDefault is the launcher's word for the default FTP-server mode.
const modeDefault = "default"

func serverKeyword(word string) bool {
	return word == modeDefault || word == "async" || word == "async-copy"
}

// firstWord is the mode keyword of a launcher row.
func firstWord(row string) string {
	if f := strings.Fields(row); len(f) > 0 {
		return f[0]
	}
	return ""
}

// launcherFlags turns the three answers into flags.
func launcherFlags(mode, viewer, hotspot string) []string {
	var flags []string
	if word := firstWord(mode); word != modeDefault {
		flags = append(flags, "--"+word)
	}
	switch viewer {
	case "view RAW":
		flags = append(flags, "--view")
	case "view JPG":
		flags = append(flags, "--view-jpg")
	}
	if hotspot == "use hotspot" {
		flags = append(flags, "--hotspot")
	}
	return flags
}

// ask is one launcher question under a heading: a header, the row to start on, the rows, the fzf
// height.
type ask func(ctx context.Context, h ui.Heading, header, selected string, rows []string, height string) (string, error)

// launchCrumb is the crumb of the launcher's questions, and launchSteps how many it asks at most.
const (
	launchCrumb = "ftp-camera"
	launchSteps = 3
)

// launch asks the launcher's questions (E8). ok false ends the run with code.
func (c *cli) launch(ctx context.Context) ([]string, term.ExitCode, bool) {
	q := c.chooser()
	if q == nil {
		c.err.f("Note: neither 'gum' nor 'fzf' is installed — TUI unavailable.\n")
		c.err.f("  Starting default FTP-server mode. Run 'ftp-camera --help'\n")
		c.err.f("  for the full flag list, or install gum/fzf to get the picker.\n")
		return nil, term.ExitOK, true
	}
	mode, err := q(ctx, ui.Here(launchCrumb).AtStep(1, launchSteps, "Mode"), modeHeader, modeLines[0], modeLines, "40%")
	viewer, hotspot := "", ""
	if err == nil && serverKeyword(firstWord(mode)) {
		viewer, err = q(ctx, ui.Here(launchCrumb).AtStep(2, launchSteps, "Viewer").About("mode "+firstWord(mode)),
			viewerHeader, viewerLines[0], viewerLines, "20%")
		if err == nil {
			hotspot, err = q(ctx, ui.Here(launchCrumb).AtStep(3, launchSteps, "Hotspot").About("mode "+firstWord(mode)+" · "+viewer),
				hotspotHeader, hotspotLines[0], hotspotLines, "20%")
		}
	}
	switch {
	case errors.Is(err, ui.ErrCancelled), errors.Is(err, ui.ErrLeave):
		c.err.f("Cancelled.\n")
		return nil, term.ExitCancelled, false
	case err != nil:
		return nil, c.fail("the launcher could not ask: %v", err), false
	}
	return launcherFlags(mode, viewer, hotspot), term.ExitOK, true
}

// chooser is gum when installed, else fzf, else nil (E8, D4).
func (c *cli) chooser() ask {
	if c.onPath("gum") {
		g := &ui.Gum{Runner: c.env.Runner, Stdin: c.env.Stdin, Stderr: c.env.Stderr, Plain: !c.t.ColourOn(c.env.Stderr)}
		return func(ctx context.Context, h ui.Heading, header, selected string, rows []string, _ string) (string, error) {
			return g.Choose(ctx, h, header, selected, rows)
		}
	}
	if c.onPath("fzf") {
		p := &ui.Picker{Runner: c.env.Runner, Stderr: c.env.Stderr}
		return func(ctx context.Context, h ui.Heading, header, _ string, rows []string, height string) (string, error) {
			got, err := p.Choose(ctx, h, ui.ChooseSpec{Prompt: "> ", Header: header, Height: height, Rows: rows})
			if err != nil {
				return "", err
			}
			if len(got) == 0 || strings.TrimSpace(got[0]) == "" {
				return "", ui.ErrLeave
			}
			return got[0], nil
		}
	}
	return nil
}
