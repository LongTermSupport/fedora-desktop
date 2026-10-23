package ghaccount

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type mode int

const (
	modeNone mode = iota
	modeAdd
	modeSetupAll
	modeCheck
)

// options is the command line read whole (E1).
type options struct {
	mode            mode
	alias, username string
	help            bool
}

var (
	aliasPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	loginPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
)

// validAlias is E3's alias: a file name, an SSH host suffix and a shell function name.
func validAlias(s string) bool { return aliasPattern.MatchString(s) }

// validLogin is E3's GitHub login.
func validLogin(s string) bool { return loginPattern.MatchString(s) }

// parseArgs reads the command line (E1, E2). No argument, or -h/--help anywhere, is the usage.
func parseArgs(args []string) (options, error) {
	if len(args) == 0 || slices.ContainsFunc(args, func(a string) bool { return a == "-h" || a == "--help" }) {
		return options{help: true}, nil
	}
	var o options
	for _, a := range args {
		next := options{}
		switch {
		case a == "--setup-all":
			next.mode = modeSetupAll
		case a == "--check":
			next.mode = modeCheck
		case strings.HasPrefix(a, "--add="):
			var err error
			if next, err = parseAdd(strings.TrimPrefix(a, "--add=")); err != nil {
				return options{}, err
			}
		default:
			return options{}, fmt.Errorf("unknown argument '%s'", a)
		}
		if o.mode != modeNone {
			return options{}, errors.New("give one of --add, --setup-all and --check")
		}
		o = next
	}
	return o, nil
}

func parseAdd(pair string) (options, error) {
	alias, username, found := strings.Cut(pair, ":")
	switch {
	case !found:
		return options{}, errors.New("use --add=ALIAS:USERNAME")
	case alias == "" || username == "":
		return options{}, errors.New("both an alias and a username are needed: --add=ALIAS:USERNAME")
	case !validAlias(alias):
		return options{}, fmt.Errorf("'%s' is not an alias: letters, digits, '_' and '-', starting with a letter or digit", alias)
	case !validLogin(username):
		return options{}, fmt.Errorf("'%s' is not a GitHub username", username)
	}
	return options{mode: modeAdd, alias: alias, username: username}, nil
}
