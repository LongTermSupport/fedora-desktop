package rclonecache

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// options is the command line read whole (E1).
type options struct {
	label     string
	noDir     bool
	target    string
	hasTarget bool
	help      bool
}

// parseArgs reads the command line. -h anywhere before `--` is the usage, whatever else the
// line holds.
func parseArgs(args []string) (options, error) {
	flags, operands := args, []string(nil)
	if end := slices.Index(args, "--"); end >= 0 {
		flags, operands = args[:end], args[end+1:]
	}
	if slices.ContainsFunc(flags, isHelp) {
		return options{help: true}, nil
	}
	var o options
	for i := 0; i < len(flags); i++ {
		a := flags[i]
		switch {
		case a == "-l" || a == "--label":
			if i+1 >= len(flags) || flags[i+1] == "" {
				return options{}, errors.New("--label needs a value")
			}
			i++
			o.label = flags[i]
		case a == "--no-dir":
			o.noDir = true
		case strings.HasPrefix(a, "-") && a != "-":
			return options{}, fmt.Errorf("unknown option '%s'", a)
		default:
			if err := o.setTarget([]string{a}); err != nil {
				return options{}, err
			}
		}
	}
	return o, o.setTarget(operands)
}

// setTarget takes the first of targets; any second one is a usage error.
func (o *options) setTarget(targets []string) error {
	for _, t := range targets {
		if o.hasTarget {
			return fmt.Errorf("only one target can be given (got '%s' and '%s')", o.target, t)
		}
		o.target, o.hasTarget = t, true
	}
	return nil
}

func isHelp(a string) bool { return a == "-h" || a == "--help" }
