package wsi

import (
	"errors"
	"fmt"
	"strings"
)

// netapiAddress is the local whisper.cpp server -n names.
const netapiAddress = "127.0.0.1:58080"

// options is one command line (W1).
type options struct {
	clipboard, autoPaste, skipEnter, wrap, noNotify, claude, debug, verbose, help bool

	// pasteWithShift is "0" (Ctrl+V) or "1" (Ctrl+Shift+V) as the panel chose at Insert, or ""
	// for the legacy PRIMARY and Shift+Insert paste.
	pasteWithShift string

	language, claudeModel, claudeStyle, token, server string
}

// parseArgs reads the whole command line (W1-W3). An argument error is returned with the
// options read from every other argument, so the report carries the session token and the
// debug setting wherever they stand (B3). -h ends the reading, unless an error came first.
func parseArgs(args []string) (options, error) {
	o := options{claudeModel: "sonnet", claudeStyle: "corporate"}
	values := map[string]*string{
		"--paste-with-shift": &o.pasteWithShift,
		"-l":                 &o.language, "--language": &o.language,
		"--session-token": &o.token,
		"--claude-model":  &o.claudeModel,
		"--claude-style":  &o.claudeStyle,
	}
	flags := map[string]*bool{
		"-c": &o.clipboard, "--clipboard": &o.clipboard,
		"-a": &o.autoPaste, "--auto-paste": &o.autoPaste,
		"--no-auto-enter": &o.skipEnter,
		"-w":              &o.wrap, "--wrap-marker": &o.wrap,
		"--no-notify":      &o.noNotify,
		"--claude-process": &o.claude,
		"-v":               &o.verbose, "--verbose": &o.verbose,
	}
	debug := func() { o.debug, o.verbose = true, true }
	netapi := func() { o.server = netapiAddress }
	actions := map[string]func(){"-d": debug, "--debug": debug, "-n": netapi, "--netapi": netapi}
	var first error
	note := func(err error) {
		if first == nil {
			first = err
		}
	}
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		switch {
		case arg == "-h" || arg == "--help":
			if first == nil {
				o.help = true
				return o, nil
			}
		case actions[arg] != nil:
			actions[arg]()
		case values[arg] != nil:
			if len(rest) == 0 {
				note(fmt.Errorf("%s requires a value", arg))
				continue
			}
			*values[arg] = rest[0]
			rest = rest[1:]
		case flags[arg] != nil:
			*flags[arg] = true
		case strings.HasPrefix(arg, "-"):
			note(fmt.Errorf("unknown option: %s (try --help)", arg))
		default:
			o.server = arg
		}
	}
	if o.pasteWithShift != "" && o.pasteWithShift != "0" && o.pasteWithShift != "1" {
		note(errors.New("--paste-with-shift requires 0 (Ctrl+V) or 1 (Ctrl+Shift+V), got: " + o.pasteWithShift))
	}
	return o, first
}
