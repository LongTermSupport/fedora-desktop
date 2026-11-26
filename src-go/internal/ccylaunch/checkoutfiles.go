package ccylaunch

import (
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/checkoutpin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// F25, the checkout files the bash launcher writes and binds that no person approves (Plan 00266
// review findings 1 and 6), which it reaches only through these questions and so only through an
// os.Root of the checkout (internal/checkoutpin C1-C3): a link a container put at one of them, or a
// .claude/ccy linked out of the checkout, is exit 1 naming the file, never followed.
//
//   - `--env-local-copy CHECKOUT DIR` copies the checkout's .claude/ccy/ccy.env.local into a new
//     private file of DIR (the launch's own temporary directory) and prints the copy's path, which
//     the launcher binds read-only over the workspace's copy: the bytes judged are the bytes bound,
//     and a link swapped in after the check binds nothing. No file is exit 0 and nothing.
//   - `--checkout-files CHECKOUT gitignore` writes .claude/ccy/.gitignore as F12's prelude does
//     (rewritten without its ignore-everything line, each missing exception added), its words on
//     stdout; `--checkout-files CHECKOUT dist` is the prelude's ccy.env.local.dist and its
//     ccy.env.local version check, its words on stderr. Each write replaces the file whole.
//
// CHECKOUT and DIR are absolute; anything else is exit 64. Answered under CCY_LEGACY too: the bash
// launcher asks them.
const (
	envLocalCopyFlag  = "--env-local-copy"
	checkoutFilesFlag = "--checkout-files"
	envLocalCopyUsage = "usage: claude-yolo " + envLocalCopyFlag + " CHECKOUT DIR\n"
	filesUsage        = "usage: claude-yolo " + checkoutFilesFlag + " CHECKOUT gitignore|dist\n"
)

// envLocalCopy is F25's copy.
func (f Front) envLocalCopy() term.ExitCode {
	if len(f.Args) != 3 || !filepath.IsAbs(f.Args[1]) || !filepath.IsAbs(f.Args[2]) {
		return f.usage(envLocalCopyUsage)
	}
	checkout, err := checkoutpin.RealCheckout(f.Args[1])
	if err != nil {
		return f.fail("ccy: " + err.Error())
	}
	copied, present, err := checkoutpin.CopyOut(checkout, localRel, f.Args[2])
	if err != nil {
		return f.fail("ccy: not binding this checkout's own settings: " + err.Error())
	}
	if !present {
		return term.ExitOK
	}
	var p term.Printer
	p.Print(f.Stdout, copied+"\n")
	if err := p.Err(); err != nil {
		return f.fail("ccy: could not write the copy's path: " + err.Error())
	}
	return term.ExitOK
}

// checkoutFiles is F25's writes.
func (f Front) checkoutFiles() term.ExitCode {
	if len(f.Args) != 3 || !filepath.IsAbs(f.Args[1]) || (f.Args[2] != "gitignore" && f.Args[2] != "dist") {
		return f.usage(filesUsage)
	}
	checkout, err := checkoutpin.RealCheckout(f.Args[1])
	if err != nil {
		return f.fail("ccy: " + err.Error())
	}
	f.Dir = checkout
	repo, why := f.readCheckoutFiles()
	if why != "" {
		return f.fail("ccy: " + why)
	}
	var p term.Printer
	if f.Args[2] == "gitignore" {
		err = repo.writeGitignore(&p)
	} else {
		err = repo.syncDist(&p)
	}
	if err != nil {
		return f.fail("ccy: " + err.Error())
	}
	if err := p.Err(); err != nil {
		return f.fail("ccy: could not write the answer: " + err.Error())
	}
	return term.ExitOK
}
