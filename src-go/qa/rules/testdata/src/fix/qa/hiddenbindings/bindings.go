// Package hiddenbindings is the shell gate of project_hidden_bindings: it names the fzf flags
// it looks for in shell scripts, and builds no argv.
package hiddenbindings

const (
	bindFlag   = "--bind"
	expectFlag = "--expect"
)

func flags() []string { return []string{bindFlag, expectFlag, "--bind=" + "x"} }
