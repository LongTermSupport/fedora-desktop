package convert

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The invariants of SPEC-convert.md section 2, over the real table.
func TestTableInvariants(t *testing.T) {
	seenExt := map[string]string{}
	for _, in := range Table {
		if in.Name == "" || len(in.Exts) == 0 || len(in.Outputs) == 0 {
			t.Fatalf("input type %+v needs a name, extensions and outputs", in)
		}
		for _, ext := range in.Exts {
			if !strings.HasPrefix(ext, ".") || ext != strings.ToLower(ext) {
				t.Errorf("%s: extension %q must start with . and be lower case", in.Name, ext)
			}
			if owner, dup := seenExt[ext]; dup {
				t.Errorf("extension %q belongs to %s and %s", ext, owner, in.Name)
			}
			seenExt[ext] = in.Name
		}
		checkOutputs(t, in)
	}
}

func checkOutputs(t *testing.T, in InputType) {
	t.Helper()
	seenFormat := map[string]bool{}
	for _, out := range in.Outputs {
		if out.Format == "" || out.Format != strings.ToLower(out.Format) || seenFormat[out.Format] {
			t.Errorf("%s: format %q must be lower case and unique", in.Name, out.Format)
		}
		seenFormat[out.Format] = true
		if out.Desc == "" || out.Engine == nil {
			t.Errorf("%s -> %s needs a description and an engine", in.Name, out.Format)
			continue
		}
		if out.Engine.Program() == "" || !strings.HasPrefix(out.Engine.Play(), "playbooks/") {
			t.Errorf("%s -> %s: engine must name its program and play, got %q %q", in.Name, out.Format, out.Engine.Program(), out.Engine.Play())
		}
	}
}

// The first row: Markdown becomes DOCX through pandoc (issue #102).
func TestTableHasMarkdownToDocxThroughPandoc(t *testing.T) {
	in, ok := typeOf("notes.md", Table)
	if !ok || in.Name != "markdown" {
		t.Fatalf("notes.md: %+v %v", in, ok)
	}
	out, ok := in.output("docx")
	if !ok || out.Engine.Program() != "pandoc" {
		t.Fatalf("docx: %+v %v", out, ok)
	}
}

func TestPandocRunsTheReaderAndWriterOnTheGivenPaths(t *testing.T) {
	f := &execx.Fake{}
	f.On([]string{"pandoc"}, execx.Result{})
	p := Pandoc{From: "markdown", To: "docx"}
	if err := p.Convert(context.Background(), f, "/in/notes.md", "/out/notes.docx.tmp.7"); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	want := []string{"--from=markdown", "--to=docx", "--output=/out/notes.docx.tmp.7", "/in/notes.md"}
	if len(calls) != 1 || calls[0].Name != "pandoc" || !slices.Equal(calls[0].Args, want) {
		t.Fatalf("calls %+v", calls)
	}
	if p.Play() != cliToolsPlay {
		t.Fatalf("play %q", p.Play())
	}
}

func TestTypeOfIsCaseInsensitiveAndRefusesTheUnknown(t *testing.T) {
	for _, name := range []string{"a.MD", "dir.x/b.Markdown"} {
		if in, ok := typeOf(name, Table); !ok || in.Name != "markdown" {
			t.Errorf("%s: %+v %v", name, in, ok)
		}
	}
	for _, name := range []string{"a.txt", "README", "md"} {
		if _, ok := typeOf(name, Table); ok {
			t.Errorf("%s matched", name)
		}
	}
}
