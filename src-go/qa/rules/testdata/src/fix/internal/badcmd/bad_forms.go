package badcmd

import (
	"slices"

	"fix/internal/execx"
)

func concatenated(rest []string) execx.Cmd {
	return execx.Cmd{Name: "env", Args: slices.Concat([]string{"A=1", "git"}, rest)} // want `"git" is owned by internal/gitx`
}

func concatenatedHidden(head, rest []string) execx.Cmd {
	return execx.Cmd{Name: "env", Args: slices.Concat(head, rest)} // want `cannot see which program "env" runs: its arguments are not a literal`
}

func spreadAfterLiteral(rest []string) execx.Cmd {
	return execx.Cmd{Name: "env", Args: append([]string{"A=1"}, rest...)} // want `cannot see which program "env" runs: its arguments are not a literal`
}

func spreadLiteral() execx.Cmd {
	return execx.Cmd{Name: "env", Args: append([]string{"A=1"}, []string{"sudo"}...)} // want `"sudo" is owned by internal/sudo`
}

func appended() execx.Cmd {
	return execx.Cmd{Name: "env", Args: append([]string{"A=1"}, "gum", "style")} // want `"gum" is owned by internal/ui`
}

func nilArgs() execx.Cmd {
	return execx.Cmd{Name: "env", Args: nil} // want `cannot see which program "env" runs: its arguments end`
}

func keyedArgs() execx.Cmd {
	return execx.Cmd{Name: "env", Args: []string{2: "git"}} // want `cannot see which program "env" runs: its arguments are not a literal`
}

func hiddenProgram(p string) execx.Cmd {
	return execx.Cmd{Name: "env", Args: []string{"A=1", p}} // want `cannot see which program "env" runs: its arguments are not a literal`
}

func scopedNoSeparator() execx.Cmd {
	return execx.Cmd{Name: "systemd-run", Args: []string{"--user", "tmux"}} // want `cannot see which program "systemd-run" runs`
}

func doubleDash() execx.Cmd {
	return execx.Cmd{Name: "sudo", Args: []string{"--", "git"}} // want `"sudo" is owned by internal/sudo`
}

func timeoutOnly() execx.Cmd {
	return execx.Cmd{Name: "timeout", Args: []string{"5"}} // want `cannot see which program "timeout" runs`
}

func nameOnly() execx.Cmd {
	return execx.Cmd{Name: "env"} // want `cannot see which program "env" runs`
}

func shellFlagsOnly() execx.Cmd {
	return execx.Cmd{Name: "dash", Args: []string{"-e", "-c", "x"}} // want `a shell string`
}

func shellInteractive() execx.Cmd {
	return execx.Cmd{Name: "zsh", Args: []string{"--login"}}
}

func pointerLiteral() *execx.Cmd {
	return &execx.Cmd{Name: "fzf"} // want `"fzf" is owned by internal/ui`
}

func otherStruct() struct{ Name string } {
	return struct{ Name string }{Name: "git"}
}

func assignOther() string {
	var s struct{ Name string }
	s.Name = "git"
	return s.Name
}

func assignHiddenWrapper(v []string) execx.Cmd {
	var c execx.Cmd
	c.Name = "env" // want `cannot see which program "env" runs: its arguments are not a literal`
	c.Args = v
	return c
}

func assignFirstArg() execx.Cmd {
	var c execx.Cmd
	c, c.Name = execx.Cmd{}, "git" // want `"git" is owned by internal/gitx`
	return c
}
