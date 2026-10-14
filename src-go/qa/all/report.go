package all

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// counts is a stage's summary, and the totals of the merged report.
type counts struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// stageReport is one stage's JSON report: the numbers and failures the merge reads, and the
// whole document, which the merged report carries under the stage's name.
type stageReport struct {
	counts   counts
	failures []json.RawMessage
	raw      json.RawMessage
}

// namedReport is a stage's slot in the merged report; a nil report is a stage whose report
// could not be used, which the merge writes as null in that stage's own slot (the bash merged
// positionally, so an empty report shifted every later stage down one).
type namedReport struct {
	key    string
	report *stageReport
}

// parseStageReport reads a stage's report, refusing an empty file, anything that is not a JSON
// object, and an object without the summary counts and the failures list the merge needs.
func parseStageReport(body []byte) (stageReport, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return stageReport{}, errors.New("the report file is empty: the stage did not write it")
	}
	var probe struct {
		Summary *struct {
			Total  *int `json:"total"`
			Passed *int `json:"passed"`
			Failed *int `json:"failed"`
		} `json:"summary"`
		Failures *[]json.RawMessage `json:"failures"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return stageReport{}, fmt.Errorf("the report is not a JSON report object: %w", err)
	}
	if probe.Summary == nil {
		return stageReport{}, errors.New("the report has no summary")
	}
	c, err := readCounts(probe.Summary.Total, probe.Summary.Passed, probe.Summary.Failed)
	if err != nil {
		return stageReport{}, err
	}
	if probe.Failures == nil {
		return stageReport{}, errors.New("the report has no failures list")
	}
	return stageReport{counts: c, failures: *probe.Failures, raw: bytes.TrimSpace(body)}, nil
}

// readCounts requires every count to be present and not negative.
func readCounts(total, passed, failed *int) (counts, error) {
	var out counts
	for _, f := range []struct {
		name string
		in   *int
		out  *int
	}{{"total", total, &out.Total}, {"passed", passed, &out.Passed}, {"failed", failed, &out.Failed}} {
		if f.in == nil || *f.in < 0 {
			return counts{}, fmt.Errorf("the report's summary.%s is missing or negative", f.name)
		}
		*f.out = *f.in
	}
	return out, nil
}

// mergeReports builds the machine report: the status, the summed counts, every stage's
// failures in stage order, and each stage's document under its name. It is indented two spaces
// and keyed in the order jq produced, with no HTML escaping of a stage's own text.
func mergeReports(status string, reports []namedReport) ([]byte, counts, error) {
	var total counts
	failures := []json.RawMessage{}
	var checks bytes.Buffer
	checks.WriteByte('{')
	for i, nr := range reports {
		key, err := json.Marshal(nr.key)
		if err != nil {
			return nil, counts{}, fmt.Errorf("stage name %q: %w", nr.key, err)
		}
		if i > 0 {
			checks.WriteByte(',')
		}
		checks.Write(key)
		checks.WriteByte(':')
		if nr.report == nil {
			checks.WriteString("null")
			continue
		}
		checks.Write(nr.report.raw)
		total.Total += nr.report.counts.Total
		total.Passed += nr.report.counts.Passed
		total.Failed += nr.report.counts.Failed
		failures = append(failures, nr.report.failures...)
	}
	checks.WriteByte('}')
	doc := struct {
		Status   string            `json:"status"`
		Summary  counts            `json:"summary"`
		Failures []json.RawMessage `json:"failures"`
		Checks   json.RawMessage   `json:"checks"`
	}{status, total, failures, checks.Bytes()}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, counts{}, fmt.Errorf("cannot encode the report: %w", err)
	}
	return out.Bytes(), total, nil
}

// reportMode is the permission of the machine report: readable by the CI artifact upload and by
// the next user, as the bash's redirection made it.
const reportMode = 0o644

// writeReport replaces the report at path in one step: written beside it and renamed, so a
// reader never sees half of one, and a failure leaves nothing behind.
func writeReport(path string, body []byte) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".qa-results-*")
	if err != nil {
		return fmt.Errorf("cannot create the report beside %s: %w", path, err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(f.Name()))
		}
	}()
	if _, err = f.Write(body); err != nil {
		return errors.Join(fmt.Errorf("cannot write %s: %w", f.Name(), err), f.Close())
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("cannot close %s: %w", f.Name(), err)
	}
	if err = os.Chmod(f.Name(), reportMode); err != nil {
		return fmt.Errorf("cannot set the mode of %s: %w", f.Name(), err)
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("cannot move the report into place at %s: %w", path, err)
	}
	return nil
}
