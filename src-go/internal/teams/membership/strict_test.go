package membership

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validDesired = `{"v":1,"session":"s","kind":"ccy","checkout":"/p","generation":1,"written_ms":1,"seats":[{"team":"alpha","seat":"1"}],"farewell":[{"team":"beta","seat":"2","reason":"removed"}]}`

const validReport = `{"v":1,"generation":2,"seats":[{"team":"alpha","seat":"1","handle":"h","state":"held"}],"refused":[{"team":"beta","seat":"2","why":"w","holder":"x"}]}`

func writeDesiredFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, File), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeReportFile(t *testing.T, checkout, body string) {
	t.Helper()
	path := ReportPath(checkout, "s")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadRefusesAKeyInTheWrongCase(t *testing.T) {
	for name, body := range map[string]string{
		"top level": strings.Replace(validDesired, `"session"`, `"Session"`, 1),
		"nested":    strings.Replace(validDesired, `"team":"alpha"`, `"Team":"alpha"`, 1),
	} {
		if _, err := Read(writeDesiredFile(t, body)); err == nil {
			t.Errorf("%s: a key in the wrong case was accepted", name)
		}
	}
}

func TestReadRefusesADuplicateKey(t *testing.T) {
	for name, body := range map[string]string{
		"top level": strings.Replace(validDesired, `"v":1,`, `"v":1,"v":1,`, 1),
		"nested":    strings.Replace(validDesired, `"seat":"1"`, `"seat":"1","seat":"1"`, 1),
	} {
		if _, err := Read(writeDesiredFile(t, body)); err == nil {
			t.Errorf("%s: a duplicate key was accepted", name)
		}
	}
}

func TestReadStillDecodesAValidFile(t *testing.T) {
	got, err := Read(writeDesiredFile(t, validDesired))
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 1 || got.Seats[0].Team != "alpha" || got.Farewell[0].Reason != ReasonRemoved {
		t.Fatalf("read %+v", got)
	}
}

func TestReadReportRefusesAKeyInTheWrongCase(t *testing.T) {
	for name, body := range map[string]string{
		"top level": strings.Replace(validReport, `"generation"`, `"Generation"`, 1),
		"nested":    strings.Replace(validReport, `"handle"`, `"HANDLE"`, 1),
	} {
		checkout := t.TempDir()
		writeReportFile(t, checkout, body)
		if _, err := ReadReport(checkout, "s"); err == nil {
			t.Errorf("%s: a key in the wrong case was accepted", name)
		}
	}
}

func TestReadReportRefusesADuplicateKey(t *testing.T) {
	for name, body := range map[string]string{
		"top level": strings.Replace(validReport, `"v":1,`, `"v":1,"v":1,`, 1),
		"nested":    strings.Replace(validReport, `"state":"held"`, `"state":"held","state":"held"`, 1),
	} {
		checkout := t.TempDir()
		writeReportFile(t, checkout, body)
		if _, err := ReadReport(checkout, "s"); err == nil {
			t.Errorf("%s: a duplicate key was accepted", name)
		}
	}
}

func TestReadRefusesAnOversizedOrMalformedFile(t *testing.T) {
	for name, body := range map[string]string{
		"oversized":      validDesired + strings.Repeat(" ", maxDesiredBytes),
		"kind":           strings.Replace(validDesired, `"kind":"ccy"`, `"kind":"lxc"`, 1),
		"relative":       strings.Replace(validDesired, `"checkout":"/p"`, `"checkout":"p"`, 1),
		"missing seats":  strings.Replace(validDesired, `"seats":[{"team":"alpha","seat":"1"}],`, ``, 1),
		"negative stamp": strings.Replace(validDesired, `"written_ms":1`, `"written_ms":-1`, 1),
		"wrong type":     strings.Replace(validDesired, `"v":1`, `"v":"1"`, 1),
	} {
		if _, err := Read(writeDesiredFile(t, body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestReadReportRefusesAWrongTypeOrAnOversizedFile(t *testing.T) {
	for name, body := range map[string]string{
		"oversized":  validReport + strings.Repeat(" ", maxDesiredBytes),
		"wrong type": strings.Replace(validReport, `"v":1`, `"v":"1"`, 1),
		"version":    strings.Replace(validReport, `"v":1`, `"v":2`, 1),
	} {
		checkout := t.TempDir()
		writeReportFile(t, checkout, body)
		if _, err := ReadReport(checkout, "s"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestReadReportStillDecodesAValidFileWithTold(t *testing.T) {
	body := strings.Replace(validReport, `"refused"`, `"told":{"Roles":[],"x":1},"refused"`, 1)
	checkout := t.TempDir()
	writeReportFile(t, checkout, body)
	got, err := ReadReport(checkout, "s")
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != 2 || !got.Holds("alpha", "1") || got.Why("beta", "2") != "w (held by a x lock)" || len(got.Told) == 0 {
		t.Fatalf("read %+v", got)
	}
}
