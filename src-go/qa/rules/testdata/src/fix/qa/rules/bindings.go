package rules

// The rules package names the fzf flags it looks for; it builds no argv.
const (
	bindFlag   = "--bind"
	expectFlag = "--expect"
)

func flags() []string { return []string{bindFlag, expectFlag, "--bind=ctrl-a:accept"} }
