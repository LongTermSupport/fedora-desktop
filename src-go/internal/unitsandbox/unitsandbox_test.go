package unitsandbox

import (
	"strings"
	"testing"
)

// kinds is a source classifier from a table: what is not in it is Unknown.
func kinds(table map[string]SourceKind) func(string) SourceKind {
	return func(src string) SourceKind { return table[src] }
}

// The originating instance (the v2 homeserver unit as first run): two file binds into a read-only
// TemporaryFileSystem. Both are findings.
func TestAFileBoundInsideATemporaryFileSystemIsAFinding(t *testing.T) {
	const unit = "[Service]\n" +
		"TemporaryFileSystem=/run/systemd/resolve:ro\n" +
		"BindReadOnlyPaths=/usr/local/share/x/resolv.conf:/run/systemd/resolve/stub-resolv.conf\n" +
		"BindReadOnlyPaths=/usr/local/share/x/resolv.conf:/run/systemd/resolve/resolv.conf\n"
	got := Check(unit, kinds(map[string]SourceKind{"/usr/local/share/x/resolv.conf": File}))
	if len(got) != 2 || got[0].Line != 3 || got[1].Line != 4 || got[0].Tmpfs != "/run/systemd/resolve" {
		t.Fatalf("findings %+v, want lines 3 and 4 inside /run/systemd/resolve", got)
	}
	if msg := got[0].String(); !strings.HasPrefix(msg, ID+": ") || !strings.Contains(msg, "stub-resolv.conf") {
		t.Fatalf("message %q does not carry the identifier and the bind", msg)
	}
}

// A source nobody can say is a directory is a finding too: the rule fails closed.
func TestASourceOfUnknownKindIsAFinding(t *testing.T) {
	got := Check("TemporaryFileSystem=/a:ro\nBindPaths=/src:/a/b\n", kinds(nil))
	if len(got) != 1 || got[0].Kind != Unknown || !strings.Contains(got[0].String(), "cannot tell") {
		t.Fatalf("findings %+v", got)
	}
}

// What does not carry the hazard: a directory bound inside, a directory bound over the whole
// tmpfs path (the fix), a bind outside any tmpfs, a bind on the tmpfs path itself.
func TestWhatIsNotAFinding(t *testing.T) {
	table := map[string]SourceKind{"/dir": Directory, "/f": File}
	for name, unit := range map[string]string{
		"directory inside":       "TemporaryFileSystem=/a:ro\nBindPaths=/dir:/a/b\n",
		"directory over the dir": "BindReadOnlyPaths=/dir:/run/systemd/resolve\n",
		"file outside":           "TemporaryFileSystem=/a:ro\nBindReadOnlyPaths=/f:/ab/c\n",
		"file on the path":       "TemporaryFileSystem=/a\nBindReadOnlyPaths=/f:/a\n",
		"comment":                "TemporaryFileSystem=/a\n# BindReadOnlyPaths=/f:/a/b\n",
		"reset":                  "TemporaryFileSystem=/a\nTemporaryFileSystem=\nBindReadOnlyPaths=/f:/a/b\n",
	} {
		if got := Check(unit, kinds(table)); len(got) != 0 {
			t.Errorf("%s: findings %+v", name, got)
		}
	}
}

// A property list in code (Python f-strings, a systemd-run --property= argument) is read too:
// the originating class as Plan 00161's probe wrote it, its sources unknown placeholders.
func TestQuotedAndPropertyLines(t *testing.T) {
	unit := `        f"TemporaryFileSystem={UNIT_INPUT_DIR}:ro",` + "\n" +
		`        f"BindReadOnlyPaths={binary}:{UNIT_INPUT_DIR}/tuwunel",` + "\n" +
		`    '--property=TemporaryFileSystem=/run/x:ro' \` + "\n" +
		`    "--property=BindReadOnlyPaths=/f:/run/x/y"` + "\n"
	got := Check(unit, kinds(map[string]SourceKind{"/f": File}))
	if len(got) != 2 || got[0].Bind.Source != "{binary}" || got[1].Bind.Dest != "/run/x/y" {
		t.Fatalf("findings %+v", got)
	}
}

// systemd's list syntax: several entries on a line, the - and + prefixes, a source bound onto
// itself, options after the destination, a tmpfs given after the bind, indented (YAML) lines.
func TestTheListSyntax(t *testing.T) {
	table := map[string]SourceKind{"/f": File, "/a/g": File, "/dir": Directory}
	unit := "  BindPaths=-/dir:/a/d +/f:/a/x:rbind /a/g\n" +
		"  TemporaryFileSystem=/z /a:ro,mode=0755\n"
	got := Check(unit, kinds(table))
	if len(got) != 2 || got[0].Bind.Source != "/f" || got[0].Bind.Dest != "/a/x" || got[1].Bind.Dest != "/a/g" {
		t.Fatalf("findings %+v", got)
	}
}
