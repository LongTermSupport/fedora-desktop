package badsemgreptest

import "context"

// Scan proves a rule by a scan whose limit the gate sets.
func Scan(ctx context.Context) Result {
	return Run(ctx, "semgrep", "scan", "--timeout=120", "--json", "--config", "rules.yml", "fixture.py")
}

// OtherTool is another program's --test, which is not semgrep's runner.
func OtherTool(ctx context.Context) Result {
	return Run(ctx, "pytest", "--test", "tests/")
}

// LaterTest is a target that happens to be named test, not the subcommand.
func LaterTest(ctx context.Context) Result {
	return Run(ctx, "semgrep", "scan", "--config", "rules.yml", "test")
}
