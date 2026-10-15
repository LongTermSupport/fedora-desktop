package vmtestmanifest

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/internal/proc"
)

const (
	validatorModule = "helpers.vmtest.validate_manifest"
	okPrefix        = "VMTEST-MANIFEST-OK "
	invalidMarker   = "VMTEST-MANIFEST-INVALID"
)

// okLine is the validator's one success line: its counts, in this order, as decimal integers.
var okLine = regexp.MustCompile(`^` + okPrefix + `scenarios=\d+ runnable=\d+ bridge=\d+ host_only=\d+ bases=\d+$`)

// verdict is what the validator said: the counts of a valid manifest, or the reasons it is not.
type verdict struct {
	counts  string
	invalid []string
}

// validate hands the manifest, as JSON, to the validator the host runs. Only a clear answer is
// one: valid with its marker, or invalid with its marker. Everything else means the validator did
// not judge, which is an error for the gate.
func (g *gate) validate(ctx context.Context, version int, manifest []byte) (verdict, error) {
	limit := g.env.Timeout
	if limit == 0 {
		limit = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	res, err := g.env.Runner.Run(ctx, proc.Cmd{
		Argv:  []string{"python3", "-m", validatorModule, "--fedora-version", strconv.Itoa(version)},
		Dir:   g.root,
		Env:   []string{"PYTHONDONTWRITEBYTECODE=1"},
		Stdin: bytes.NewReader(manifest),
	})
	switch {
	case err != nil:
		return verdict{}, fmt.Errorf("%s could not run (python3 is required): %w", validatorModule, err)
	case res.TimedOut:
		return verdict{}, fmt.Errorf("%s did not finish within %s", validatorModule, limit)
	case res.Leftover:
		return verdict{}, fmt.Errorf("%s left a process holding its output open", validatorModule)
	}
	lines := strings.Split(strings.TrimSuffix(res.Stdout, "\n"), "\n")
	switch {
	case res.Code == 0 && len(lines) == 1 && okLine.MatchString(lines[0]):
		return verdict{counts: strings.TrimPrefix(lines[0], okPrefix)}, nil
	case res.Code == 1 && lines[0] == invalidMarker && len(lines) == 1:
		return verdict{invalid: invalidReasons(res.Stderr)}, nil
	}
	return verdict{}, fmt.Errorf("%s exited %d without a single %sline or a %s line (stdout %q)\n%s",
		validatorModule, res.Code, okPrefix, invalidMarker, res.Stdout, strings.TrimSpace(res.Stderr))
}

// invalidReasons is the validator's complaint, one finding per line of its stderr.
func invalidReasons(stderr string) []string {
	var reasons []string
	for line := range strings.SplitSeq(strings.TrimSpace(stderr), "\n") {
		if reason := strings.TrimSpace(strings.TrimPrefix(line, "ERROR: ")); reason != "" {
			reasons = append(reasons, manifestFile+": "+reason)
		}
	}
	if len(reasons) == 0 {
		reasons = []string{manifestFile + ": the validator rejected the manifest without saying why"}
	}
	return reasons
}
