// Package rootargs reads the command line the repository-root gates share: -h/--help, --root DIR
// and --scripts DIR. Two grammars exist and both are kept, because the gates' messages differ:
// Lenient takes the next word as a value whatever it is and reports an unknown argument as a
// plain error; Strict refuses an empty or flag-like value and reports an unknown argument as an
// *UnknownArgError, which the gate answers with its usage text.
package rootargs

import (
	"errors"
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

func (e *UnknownArgError) Error() string { return unknownMessage(e.Arg) }

func unknownMessage(arg string) string { return fmt.Sprintf("unknown argument '%s'", arg) }

const (
	flagRoot    = "--root"
	flagScripts = "--scripts"
)

// Lenient reads the flags; the word after --root or --scripts is its value, whatever it is.
func Lenient(args []string) (Args, error) {
	var opts Args
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		switch arg {
		case "-h", "--help":
			opts.Help = true
		case flagRoot, flagScripts:
			if len(rest) == 0 {
				return opts, fmt.Errorf("%s needs a value", arg)
			}
			if arg == flagRoot {
				opts.Root = rest[0]
			} else {
				opts.Scripts = rest[0]
			}
			rest = rest[1:]
		default:
			return opts, errors.New(unknownMessage(arg))
		}
	}
	return opts, nil
}

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
