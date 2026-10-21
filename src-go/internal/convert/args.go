package convert

import (
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// options is the command line (SPEC-convert.md section 3).
type options struct {
	file        string
	to          []string
	force, help bool
}

func usageError(format string, args ...any) error {
	return &failure{code: term.ExitUsage, msg: fmt.Sprintf(format, args...) + " — see 'convert --help'."}
}

// parser is the state of one parse: the first error is kept, so -h later on still wins (E1).
type parser struct {
	o        options
	files    []string
	toSet    bool
	firstErr error
}

func (p *parser) fail(err error) {
	if p.firstErr == nil {
		p.firstErr = err
	}
}

// parseArgs is E1, E2 and E6.
func parseArgs(args []string) (options, error) {
	var p parser
	for i := 0; i < len(args); i++ {
		i = p.arg(args, i)
	}
	if p.o.help {
		return p.o, nil
	}
	switch {
	case p.firstErr != nil:
	case len(p.files) == 0:
		p.fail(usageError("no file given"))
	case len(p.files) > 1:
		p.fail(usageError("one file at a time, got %d", len(p.files)))
	default:
		p.o.file = p.files[0]
	}
	return p.o, p.firstErr
}

// arg reads args[i] and returns the index of the last argument it consumed.
func (p *parser) arg(args []string, i int) int {
	a := args[i]
	switch {
	case a == "--":
		p.files = append(p.files, args[i+1:]...)
		return len(args)
	case a == "-h" || a == "--help":
		p.o.help = true
	case a == "-f" || a == "--force":
		p.o.force = true
	case a == "--to":
		if i+1 == len(args) {
			p.fail(usageError("--to needs a list of formats"))
			return i
		}
		p.setTo(args[i+1])
		return i + 1
	case strings.HasPrefix(a, "--to="):
		p.setTo(strings.TrimPrefix(a, "--to="))
	case strings.HasPrefix(a, "-") && a != "-":
		p.fail(usageError("unknown option '%s'", a))
	default:
		p.files = append(p.files, a)
	}
	return i
}

// setTo is E6: the comma-separated list, trimmed, lower-cased and without repeats.
func (p *parser) setTo(list string) {
	if p.toSet {
		p.fail(usageError("--to is given twice"))
		return
	}
	p.toSet = true
	for item := range strings.SplitSeq(list, ",") {
		f := strings.ToLower(strings.TrimSpace(item))
		if f == "" {
			p.fail(usageError("empty format in --to '%s'", list))
			return
		}
		if !slices.Contains(p.o.to, f) {
			p.o.to = append(p.o.to, f)
		}
	}
}
