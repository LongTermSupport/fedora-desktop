// Package rootargs reads the command line the repository-root gates share: -h/--help, --root DIR
// and --scripts DIR. A value that is missing, empty or starts with '-' is refused, because an
// empty root would be read as "not given" and the gate would judge another tree; an unknown
// argument is an *UnknownArgError, which the gate answers with its usage text.
package rootargs

import (
	"fmt"
)

// Args is what the shared flags said.
type Args struct {
	Help    bool
	Root    string
	Scripts string
}

// UnknownArgError is an argument the gate does not know; the usage text follows it.
type UnknownArgError struct{ Arg string }

func (e *UnknownArgError) Error() string { return fmt.Sprintf("unknown argument '%s'", e.Arg) }

const (
	flagRoot    = "--root"
	flagScripts = "--scripts"
)

// Strict reads the flags; a value that is missing, empty or starts with '-' is refused.
func Strict(args []string) (Args, error) {
	var opts Args
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		switch arg {
		case "-h", "--help":
			opts.Help = true
		case flagRoot, flagScripts:
			if len(rest) == 0 || rest[0] == "" || rest[0][0] == '-' {
				return Args{}, fmt.Errorf("%s needs a directory", arg)
			}
			if arg == flagRoot {
				opts.Root = rest[0]
			} else {
				opts.Scripts = rest[0]
			}
			rest = rest[1:]
		default:
			return Args{}, &UnknownArgError{Arg: arg}
		}
	}
	return opts, nil
}
