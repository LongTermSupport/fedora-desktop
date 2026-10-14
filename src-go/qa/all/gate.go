package all

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/gates"
	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

// exec runs a command through the process seam under the per-gate time limit. The result of a
// command killed at the limit has TimedOut set; one killed because the run was interrupted does
// not, and the caller finds that out from the context.
func (s *session) exec(ctx context.Context, cmd proc.Cmd) (proc.Result, error) {
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}
	res, err := s.env.Runner.Run(ctx, cmd)
	if err != nil {
		return proc.Result{}, fmt.Errorf("cannot run %s: %w", cmd.Argv[0], err)
	}
	return res, nil
}

// bad is a command that was meant to pass and did not: the reason for its stage line and the
// capture for stderr.
type bad struct {
	reason, capture string
}

// failure says whether a command failed. A command that could not start, one killed at the
// limit and one that exited non-zero are all failures; status 2 is not told apart (SPEC K2).
func (s *session) failure(g gates.Gate, res proc.Result, err error) *bad {
	switch {
	case err != nil:
		return &bad{g.Text + " (" + err.Error() + ")", err.Error()}
	case res.TimedOut:
		return &bad{g.Text + " (timed out after " + s.timeout.String() + ")", res.Stdout + res.Stderr}
	case res.Code != 0:
		return &bad{g.Text, res.Stdout + res.Stderr}
	}
	return nil
}

// gateFailed records a failed gate: its capture on stderr first (nothing if it is empty), then
// the stage line on stdout, which is what the census of a run reads.
func (s *session) gateFailed(name, reason, capture string) {
	if trimmed := strings.TrimRight(capture, "\n"); trimmed != "" {
		s.errf("%s\n", trimmed)
	}
	s.outf("✗ %s: %s\n", name, reason)
	s.failedGates = append(s.failedGates, name)
}

// summarise answers the text for a passing gate's stage line, or the reason it cannot give
// one. Every reader must find its line: a gate whose summary cannot be read has proved nothing.
func summarise(g gates.Gate, capture string) (string, string) {
	parts := make([]string, 0, len(g.Readers))
	for _, r := range g.Readers {
		if r.Detail == nil {
			got, ok := gates.CaseCount(capture)
			if !ok {
				return "", "exited 0 but printed no 'passed: <n>' line, so it proved nothing"
			}
			parts = append(parts, got)
			continue
		}
		got, ok := gates.Detail(capture, r.Detail)
		if !ok {
			return "", "ran, but its summary line was not found: " + r.Detail.String()
		}
		parts = append(parts, got)
	}
	return strings.Join(parts, "; "), ""
}

// runHard is a gate: run it, require status 0, read its summary.
func (s *session) runHard(ctx context.Context, g gates.Gate) {
	res, err := s.exec(ctx, proc.Cmd{Argv: g.Command(s.root), Dir: s.root, Merge: true})
	if ctx.Err() != nil {
		return
	}
	if b := s.failure(g, res, err); b != nil {
		s.gateFailed(g.Name, b.reason, b.capture)
		return
	}
	summary, unreadable := summarise(g, res.Stdout)
	if unreadable != "" {
		s.gateFailed(g.Name, unreadable, res.Stdout)
		return
	}
	s.outf("✓ %s: %s\n", g.Name, summary)
}

// runDrift is the deployed-drift gate: its own output is the stage line, so a pass that printed
// none is not a pass.
func (s *session) runDrift(ctx context.Context, g gates.Gate) {
	res, err := s.exec(ctx, proc.Cmd{Argv: g.Command(s.root), Dir: s.root, Merge: true})
	if ctx.Err() != nil {
		return
	}
	if b := s.failure(g, res, err); b != nil {
		s.gateFailed(g.Name, b.reason, b.capture)
		return
	}
	line := regexp.MustCompile(`(?m)^[✓⚠] ` + regexp.QuoteMeta(g.Name) + `: `)
	if !line.MatchString(res.Stdout) {
		s.gateFailed(g.Name, "ran, but printed no "+g.Name+" stage line", res.Stdout)
		return
	}
	s.outf("%s\n", strings.TrimRight(res.Stdout, "\n"))
}

// stdoutExplanation is shown, above what the helper suite printed, when it wrote to the stream
// the verdicts are parsed from.
var stdoutExplanation = strings.Join([]string{
	"  A test printing here can forge or split a stage line. If it is a test's own",
	"  print, wrap it in contextlib.redirect_stdout; if it is a subprocess a test",
	"  spawned, capture that subprocess rather than letting it inherit.",
}, "\n")

// runHelpers is the helper unit suite. Its counts arrive in a file, written from unittest's
// TestResult, because nothing a test prints may share a channel with them; stdout and stderr
// are captured apart so that anything on stdout can be refused. Three ways to fail, one gate:
// each stands down for the one before it, so the gate is recorded once.
func (s *session) runHelpers(ctx context.Context, g gates.Gate) {
	counts, err := s.tempFile()
	if err != nil {
		s.gateFailed(g.Name, "cannot make its counts file", err.Error())
		return
	}
	token := s.env.NewToken()
	argv := append(g.Command(s.root), "--counts-file", counts, "--counts-token", token)
	res, err := s.exec(ctx, proc.Cmd{Argv: argv, Dir: s.root})
	if ctx.Err() != nil {
		return
	}
	if b := s.failure(g, res, err); b != nil {
		s.gateFailed(g.Name, b.reason, b.capture)
		return
	}
	if res.Stdout != "" {
		s.gateFailed(g.Name, "wrote to stdout, which is this suite's verdict stream", stdoutExplanation+"\n"+strings.TrimRight(res.Stdout, "\n"))
		return
	}
	summary, err := gates.HelperCounts(counts, token)
	if err != nil {
		s.errf("%s counts: %v\n", g.Name, err)
		s.gateFailed(g.Name, "ran, but its counts could not be read", "")
		return
	}
	s.outf("✓ %s: %s\n", g.Name, summary)
}

// runStage is one of the jq-merged report stages. Its output goes straight to the terminal,
// live; the stage prints its own stage line. Status 2 is the one abort of the run: a stage that
// cannot run its tools has nothing to accumulate.
func (s *session) runStage(ctx context.Context, g gates.Gate) (ExitCode, bool) {
	path, err := s.tempFile()
	if err != nil {
		s.errf("ERROR: %v\n", err)
		return ExitCannotRun, true
	}
	res, err := s.exec(ctx, proc.Cmd{
		Argv: g.Command(s.root), Dir: s.root, Env: []string{"QA_JSON_OUT=" + path},
		StdoutTo: s.env.Stdout, StderrTo: s.env.Stderr,
	})
	if ctx.Err() != nil {
		return ExitOK, false
	}
	clean := err == nil && !res.TimedOut && res.Code == 0
	switch {
	case err != nil:
		s.stageFailed(g.Name, "could not start: "+err.Error())
	case res.TimedOut:
		s.stageFailed(g.Name, "timed out after "+s.timeout.String())
	case res.Code == int(ExitCannotRun):
		s.errf("%s\n", g.Text)
		return ExitCannotRun, true
	case res.Code != 0:
		s.noteStageFailed(g.Name)
	}
	s.collectReport(g.Name, path, clean)
	return ExitOK, false
}

// stageFailed prints a stage line of our own for a stage that could not print one itself.
func (s *session) stageFailed(name, why string) {
	s.outf("✗ %s: %s\n", name, why)
	s.noteStageFailed(name)
}

// noteStageFailed records a failed stage once, however many ways it failed.
func (s *session) noteStageFailed(name string) {
	if !slices.Contains(s.failedStages, name) {
		s.failedStages = append(s.failedStages, name)
	}
}

// collectReport reads the stage's JSON report into its own slot. A report that cannot be used
// is a failure of the stage (when the stage otherwise passed), and so is a clean exit beside a
// report that counts failures.
func (s *session) collectReport(name, path string, clean bool) {
	slot := namedReport{key: strings.ReplaceAll(name, "-", "_")}
	body, err := os.ReadFile(path)
	if err == nil {
		var report stageReport
		if report, err = parseStageReport(body); err == nil {
			slot.report = &report
		}
	}
	s.reports = append(s.reports, slot)
	switch {
	case err != nil && clean:
		s.stageFailed(name, "its report is unusable: "+err.Error())
	case slot.report != nil && clean && slot.report.counts.Failed > 0:
		s.stageFailed(name, fmt.Sprintf("exited 0 but its report counts %d failed", slot.report.counts.Failed))
	}
}
