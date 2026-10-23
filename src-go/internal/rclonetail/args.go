package rclonetail

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	defaultInterval = 5 * time.Second
	maxIntervalS    = 86400
)

// options is the command line read whole (E1).
type options struct {
	interval time.Duration
	once     bool
	filter   string
	help     bool
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
	o := options{interval: defaultInterval}
	var names []string
	for i := 0; i < len(flags); i++ {
		a := flags[i]
		switch {
		case a == "-i" || a == "--interval":
			if i+1 >= len(flags) || flags[i+1] == "" {
				return options{}, errors.New("--interval needs a value")
			}
			i++
			d, err := interval(flags[i])
			if err != nil {
				return options{}, err
			}
			o.interval = d
		case a == "-o" || a == "--once":
			o.once = true
		case strings.HasPrefix(a, "-") && a != "-":
			return options{}, fmt.Errorf("unknown option '%s'", a)
		default:
			names = append(names, a)
		}
	}
	filter, err := mountName(append(names, operands...))
	if err != nil {
		return options{}, err
	}
	o.filter = filter
	return o, nil
}

// mountName is the one MOUNT_NAME, or "" for none.
func mountName(names []string) (string, error) {
	switch len(names) {
	case 0:
		return "", nil
	case 1:
		return names[0], nil
	default:
		return "", fmt.Errorf("only one mount name can be given (got '%s' and '%s')", names[0], names[1])
	}
}

// interval is a whole number of seconds from 1 to a day, written without a sign or a leading 0.
func interval(s string) (time.Duration, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > maxIntervalS || strconv.Itoa(n) != s {
		return 0, fmt.Errorf("--interval must be a whole number of seconds from 1 to %d (got '%s')", maxIntervalS, s)
	}
	return time.Duration(n) * time.Second, nil
}

func isHelp(a string) bool { return a == "-h" || a == "--help" }
