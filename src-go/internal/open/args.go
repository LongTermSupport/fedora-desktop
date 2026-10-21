package open

import (
	"fmt"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// options is the command line (E1).
type options struct {
	ask, terminal, setDefault, list, dryRun, help bool
	targets                                       []string
}

// flagSetters is every option, short and long; -d implies --ask.
var flagSetters = map[string]func(*options){
	"-h": func(o *options) { o.help = true }, "--help": func(o *options) { o.help = true },
	"-a": func(o *options) { o.ask = true }, "--ask": func(o *options) { o.ask = true },
	"-t": func(o *options) { o.terminal = true }, "--terminal": func(o *options) { o.terminal = true },
	"-d": setDefault, "--set-default": setDefault,
	"-l": func(o *options) { o.list = true }, "--list": func(o *options) { o.list = true },
	"-n": func(o *options) { o.dryRun = true }, "--dry-run": func(o *options) { o.dryRun = true },
}

func setDefault(o *options) { o.setDefault, o.ask = true, true }

func usageError(format string, args ...any) error {
	return &failure{code: term.ExitUsage, msg: fmt.Sprintf(format, args...)}
}

// parseArgs is E1-E2: the whole command line is read before anything runs, so -h anywhere
// wins over an error (B14).
func parseArgs(args []string) (options, error) {
	var o options
	var firstErr error
	for i, a := range args {
		if a == "--" {
			o.targets = append(o.targets, args[i+1:]...)
			break
		}
		if set, ok := flagSetters[a]; ok {
			set(&o)
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			if firstErr == nil {
				firstErr = usageError("unknown option '%s' — see 'open --help'.", a)
			}
			continue
		}
		o.targets = append(o.targets, a)
	}
	if o.help {
		return o, nil
	}
	if firstErr == nil && len(o.targets) == 0 {
		firstErr = usageError("nothing to open — see 'open --help'.")
	}
	return o, firstErr
}
