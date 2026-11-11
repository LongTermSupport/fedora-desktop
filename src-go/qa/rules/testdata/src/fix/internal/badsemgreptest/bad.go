// Package badsemgreptest proves rules with semgrep's own test runner, whose limit on a rule cannot
// be set, so a loaded host fails a rule that is sound.
package badsemgreptest

import "context"

// Result is what a run returned.
type Result struct{ Code int }

// Run stands in for the gates' process runner.
func Run(_ context.Context, _ ...string) Result { return Result{} }

// TestFlag names the runner through a constant, which is still the runner.
const TestFlag = "--test"

// Direct is the shape the gates had: the runner in the argument list of the call.
func Direct(ctx context.Context) Result {
	return Run(ctx, "semgrep", "--test", "--timeout=120", "--config", "rules.yml", "fixture.py") // want `semgrep's test runner`
}

// Literal is the argv as a slice.
func Literal() []string {
	return []string{"semgrep", "--test", "--config", "rules.yml"} // want `semgrep's test runner`
}

// Named is the flag through a constant.
func Named(ctx context.Context) Result {
	return Run(ctx, "semgrep", TestFlag, "fixture.py") // want `semgrep's test runner`
}

// Appended adds the flag to an argv built apart from it.
func Appended() []string {
	argv := make([]string, 0, 3)
	argv = append(argv, "semgrep", "--metrics=off")
	return append(argv, "--test") // want `semgrep's test runner`
}

// Subcommand is osemgrep's test subcommand, which takes no limit either.
func Subcommand(ctx context.Context) Result {
	return Run(ctx, "semgrep", "test", "--config", "rules.yml") // want `semgrep's test runner`
}
