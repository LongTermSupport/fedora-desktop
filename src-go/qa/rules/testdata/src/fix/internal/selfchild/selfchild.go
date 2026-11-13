// Package selfchild is project_self_child_stderr's fixture: its tests run their own test binary
// as a child and judge the child's stderr.
package selfchild

// Result is a child's outcome, as execx and proc give it.
type Result struct {
	Code   int
	Stderr []byte
}

// Run is what the child said.
type Run struct {
	Exit   int
	Stderr string
}

// Clean is a child's stderr without the coverage runtime's line, as the shared helper reads it.
func Clean(stderr string) string { return stderr }
