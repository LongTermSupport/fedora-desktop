package convert

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// cliToolsPlay installs convert and the engines it calls (SPEC-convert.md E10).
const cliToolsPlay = "playbooks/imports/optional/common/play-cli-tools.yml"

// Engine is a proven converter that does one or more pairs of the table.
type Engine interface {
	// Program is the executable looked up on PATH before anything runs (E10).
	Program() string
	// Play is the playbook that installs Program.
	Play() string
	// Convert writes the conversion of the file at in to the path out; both are absolute.
	Convert(ctx context.Context, r execx.Runner, in, out string) error
}

// Output is one format an input type can become.
type Output struct {
	Format string // the word --to takes and the output file's extension
	Desc   string // the listing's description
	Engine Engine
}

// InputType is one kind of file convert reads, recognised by its extensions.
type InputType struct {
	Name    string
	Exts    []string
	Outputs []Output
}

// Table is every conversion convert knows (SPEC-convert.md section 2). A new pair is a row.
var Table = []InputType{
	{
		Name: "markdown",
		Exts: []string{".md", ".markdown"},
		Outputs: []Output{
			{Format: "docx", Desc: "Word document", Engine: Pandoc{From: "markdown", To: "docx"}},
		},
	},
}

// Pandoc converts between document formats with pandoc's reader From and writer To.
type Pandoc struct {
	From, To string
}

// Program implements Engine.
func (Pandoc) Program() string { return "pandoc" }

// Play implements Engine.
func (Pandoc) Play() string { return cliToolsPlay }

// Convert implements Engine.
func (p Pandoc) Convert(ctx context.Context, r execx.Runner, in, out string) error {
	_, err := r.Run(ctx, execx.Cmd{
		Name: "pandoc",
		Args: []string{"--from=" + p.From, "--to=" + p.To, "--output=" + out, in},
	})
	return err
}

// typeOf is E4: the input type of the file named path, by its extension in any case.
func typeOf(path string, table []InputType) (InputType, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, in := range table {
		if slices.Contains(in.Exts, ext) {
			return in, true
		}
	}
	return InputType{}, false
}

// output is the row for format, if this input type has one.
func (in InputType) output(format string) (Output, bool) {
	for _, o := range in.Outputs {
		if o.Format == format {
			return o, true
		}
	}
	return Output{}, false
}

// formats is the --to words this input type takes, in table order.
func (in InputType) formats() []string {
	names := make([]string, 0, len(in.Outputs))
	for _, o := range in.Outputs {
		names = append(names, o.Format)
	}
	return names
}
