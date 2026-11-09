package term

// ExitCode is a tool's exit status.
type ExitCode int

// The codes the tools document.
const (
	ExitOK      ExitCode = 0
	ExitFailure ExitCode = 1
	ExitConfig  ExitCode = 78
)

// FromChild is the owner's translation of another program's exit code into a documented one:
// its own conversion is not judged.
func FromChild(code int) ExitCode {
	if code == int(ExitConfig) {
		return ExitCode(code)
	}
	return ExitFailure
}
