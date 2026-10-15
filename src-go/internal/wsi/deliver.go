package wsi

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The two selections.
const (
	selectionClipboard = "CLIPBOARD"
	selectionPrimary   = "PRIMARY"
)

// copyText puts text on a selection with the session's tool (W27): xsel on X11, wl-copy on
// anything else. exact leaves out the trailing newline.
func (r *run) copyText(ctx context.Context, selection, text string, exact bool) error {
	if !exact {
		text += "\n"
	}
	cmd := execx.Cmd{Name: wlCopyBin}
	if selection == selectionPrimary {
		cmd.Args = []string{"-p"}
	}
	if r.session == sessionX11 {
		cmd = execx.Cmd{Name: xselBin, Args: []string{"-ib"}}
		if selection == selectionPrimary {
			cmd.Args = []string{"-ip"}
		}
	}
	cmd.Stdin = strings.NewReader(text)
	_, err := r.env.Runner.Run(ctx, cmd)
	return err
}

// copyOrFail is copyText with W27's failure.
func (r *run) copyOrFail(ctx context.Context, selection, text string, exact bool) error {
	if err := r.copyText(ctx, selection, text, exact); err != nil {
		return r.fail(ctx, "Copying the text to the "+selection+" selection failed: "+err.Error())
	}
	r.log.debug("Text copied to the " + selection + " selection (" + r.session + ")")
	return nil
}

// knownSession is a session type the clipboard tools serve.
func (r *run) knownSession() bool { return r.session == sessionWayland || r.session == sessionX11 }

// deliver is W25-W35.
func (r *run) deliver(ctx context.Context, text string) error {
	r.save(ctx, text)
	if r.env.Getenv("DISPLAY") == "" && r.env.Getenv("WAYLAND_DISPLAY") == "" && r.env.Getenv("DESKTOP_SESSION") == "" {
		r.out.Print(r.env.Stdout, text+"\n")
		return nil
	}
	if r.o.autoPaste {
		return r.autoPaste(ctx, text)
	}
	selection, hint := selectionPrimary, "Middle-click to paste"
	if r.o.clipboard {
		selection, hint = selectionClipboard, "Ctrl+V to paste"
	}
	r.log.debug("Sending to clipboard (window manager: " + r.session + ")")
	if r.knownSession() {
		if err := r.copyOrFail(ctx, selection, text, false); err != nil {
			return err
		}
	} else {
		r.problem(ctx, "Unknown window manager: "+r.session)
		r.out.Print(r.env.Stdout, text+"\n")
	}
	r.panel.notifyState(ctx, stateSuccess, preview(text)+"\n\n"+hint, "")
	return nil
}

// save is W25.
func (r *run) save(ctx context.Context, text string) {
	path := filepath.Join(r.home, ".cache/speech-to-text/last-transcription.txt")
	err := os.MkdirAll(filepath.Dir(path), 0o750)
	if err == nil {
		err = os.WriteFile(path, []byte(text+"\n"), 0o600)
	}
	if err != nil {
		r.problem(ctx, "Could not save the text to "+path+": "+err.Error())
		return
	}
	r.log.debug("Transcription saved to: " + path)
}
