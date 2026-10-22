package ccylaunch

import (
	"bytes"
	"context"
	_ "embed" // the import script's body
	"errors"
	"os"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tokens"
)

// F13, `--export-token NAME`: the import script of the name's latest file, printed after the
// in-repo prelude. Printing the token is this flag's purpose; it goes to stdout and nowhere
// else, never to an argv, the environment or a log.

//go:embed export-body.txt
var exportBody string

// generatedLayout is the bash's date '+%Y-%m-%d %H:%M:%S'.
const generatedLayout = "2006-01-02 15:04:05"

// export is what --export-token NAME will print, read before anything is written.
type export struct {
	name, file, expires string
	found, valid        bool
	token               string
}

// readExport reads the pool for name: the export, or why the bash could read it otherwise.
func (f Front) readExport(name string) (export, string) {
	e := export{name: name}
	if tokens.ValidName(name) != nil {
		return e, "the export's name is not a token name"
	}
	files, why := poolWhy(f.tokenDir())
	if why != "" {
		return e, why
	}
	// The bash's glob "$dir/NAME".*.token; each match must be a dated file of the name, so the
	// last, in byte order, is the latest (D1).
	for _, fn := range files {
		rest, ok := strings.CutPrefix(fn, name+".")
		if !ok || !strings.HasSuffix(rest, tokens.Suffix) {
			continue
		}
		parsed, ok := tokens.ParseFileName(fn)
		if !ok || !parsed.Dated || parsed.Name != name {
			return e, "the bash's glob of the name matches another file: " + fn
		}
		e.file, e.expires, e.found = fn, parsed.Expiry.Format(time.DateOnly), true
	}
	if !e.found {
		return e, ""
	}
	e.valid = tokens.IsValid(e.file, f.clock().Now())
	if !e.valid {
		return e, ""
	}
	raw, err := os.ReadFile(f.tokenDir() + "/" + e.file)
	if err != nil {
		return e, "the token file cannot be read: " + e.file
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return e, "the token file holds a NUL: " + e.file
	}
	e.token = strings.TrimRight(string(raw), "\n")
	return e, ""
}

// exportToken is F13.
func (f Front) exportToken(ctx context.Context) (term.ExitCode, string) {
	var e export
	check := func() string {
		var why string
		e, why = f.readExport(f.Args[1])
		return why
	}
	return f.inRepoFlag(ctx, check, func(p *term.Printer) (term.ExitCode, error) { return f.printExport(p, e) })
}

func (f Front) printExport(p *term.Printer, e export) (term.ExitCode, error) {
	switch {
	case !e.found:
		f.printError(p, "No token found with name: "+e.name)
		p.Print(f.Stdout, "\nAvailable tokens:\n")
		pool, err := tokens.ReadPool(f.tokenDir())
		if err == nil {
			err = tokens.List(f.Stdout, pool, tokenTool, f.clock().Now(), f.colourOn())
		}
		if errors.Is(err, tokens.ErrNoTokens) {
			err = nil
		}
		return term.ExitFailure, err
	case !e.valid:
		f.printError(p, "Token '"+e.name+"' is expired ("+e.expires+")")
		p.Print(f.Stdout, "Create a new token first: ccy --create-token\n")
		return term.ExitFailure, nil
	case e.token == "":
		f.printError(p, "Token file is empty: "+f.tokenDir()+"/"+e.file)
		return term.ExitFailure, nil
	case strings.Contains(e.token, "'"):
		f.printError(p, "Token contains unexpected characters")
		return term.ExitFailure, nil
	}
	p.Print(f.Stdout, "# CCY Token: "+e.name+" | Expires: "+e.expires+" | Generated: "+f.clock().Now().Format(generatedLayout)+"\n"+
		"# Paste into terminal on target machine, or save in LastPass/1Password.\n"+
		"# Other files of this name: asks before removing them; CCY_TOKEN_IMPORT_REMOVE_OTHERS=1 removes, =0 keeps.\n"+
		"bash <<'CCYTOKEN'\nset -eo pipefail\nname='"+e.name+"'\ndate='"+e.expires+"'\ntoken='"+e.token+"'\n"+exportBody)
	return term.ExitOK, nil
}
