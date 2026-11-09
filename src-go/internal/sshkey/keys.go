package sshkey

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// A private key file's first line is beginMark, the key's kind, then keyMark (OpenSSH, PEM and
// PKCS#8 alike). The two halves are kept apart so no source line reads as a key block.
const (
	beginMark = "-----BEGIN "
	keyMark   = "PRIVATE KEY-----"
)

// headLimit is how much of a file is read to find its first line.
const headLimit = 128

// headingWidth is what a heading wraps to when the terminal does not say its width.
const headingWidth = 80

// cancelKey is the menus' Cancel line (E7, E15).
const cancelKey = "q"

var errNotPrivateKey = errors.New("its first line is not a private key's")

// isPrivateKey reports whether path's first line is a private-key header.
func isPrivateKey(path string) (bool, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return false, err
	}
	head := make([]byte, headLimit)
	n, readErr := io.ReadFull(f, head)
	closeErr := f.Close()
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
		return false, readErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	line := string(head[:n])
	if end := strings.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	line = strings.TrimSuffix(line, "\r")
	return strings.HasPrefix(line, beginMark) && strings.HasSuffix(line, keyMark), nil
}

// checkKey is E5: a readable regular file that is a private key.
func checkKey(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return reason(err)
	}
	if info.IsDir() {
		return errors.New("it is a directory")
	}
	if !info.Mode().IsRegular() {
		return errors.New("it is not a regular file")
	}
	ok, err := isPrivateKey(path)
	if err != nil {
		return reason(err)
	}
	if !ok {
		return errNotPrivateKey
	}
	return nil
}

// reason is a file error without its path, which the message already names.
func reason(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	return err
}

// privateKeys are the private keys in dir by name (E6): regular files, symlinks followed, whose
// first line is a private-key header. A file this account cannot read is not a key it can use.
func privateKeys(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if strings.HasSuffix(e.Name(), ".pub") {
			continue
		}
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		ok, err := isPrivateKey(path)
		if errors.Is(err, fs.ErrPermission) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if ok {
			keys = append(keys, path)
		}
	}
	return keys, nil
}

// loginKey is the key to log in with: the one given, checked (E5), or the one chosen (E6-E9).
func (r *run) loginKey(ctx context.Context, given string) (string, term.ExitCode) {
	if given != "" {
		if !r.keyUsable(given) {
			return "", term.ExitFailure
		}
		return given, term.ExitOK
	}
	keys, err := privateKeys(r.sshDir())
	if err != nil {
		r.failf("could not list the keys in %s: %v.", r.show(r.sshDir()), err)
		return "", term.ExitFailure
	}
	if len(keys) == 0 {
		r.failf("no private keys in %s; give one with %s KEY.", r.show(r.sshDir()), r.tool.KeyFlag)
		return "", term.ExitFailure
	}
	lines, closeTTY, ok := r.terminal()
	if !ok {
		r.failf("no key given and no terminal to choose one on; give it with %s KEY.", r.tool.KeyFlag)
		return "", term.ExitUsage
	}
	key, code := r.pick(ctx, lines, keys)
	if err := closeTTY(); err != nil && code == term.ExitOK {
		r.failf("could not close the terminal: %v.", err)
		return "", term.ExitFailure
	}
	return key, code
}

// keyUsable is E5's check, said when it fails.
func (r *run) keyUsable(path string) bool {
	err := checkKey(path)
	if err == nil {
		return true
	}
	if private, ok := strings.CutSuffix(path, ".pub"); ok {
		r.failf("%s is a public key; give its private key %s.", r.show(path), r.show(private))
		return false
	}
	r.failf("%s is not a readable private key: %v.", r.show(path), err)
	return false
}

// pick is the login key menu (E6, E7).
func (r *run) pick(ctx context.Context, lines *ui.Lines, keys []string) (string, term.ExitCode) {
	choices := make([]ui.Choice, 0, len(keys)+1)
	for i, k := range keys {
		choices = append(choices, ui.Choice{Key: strconv.Itoa(i + 1), Label: filepath.Base(k)})
	}
	choices = append(choices, ui.Choice{Key: cancelKey, Label: "Cancel"})
	about := plural(len(keys), "private key") + " in " + r.show(r.sshDir())
	if r.tool.kind == kindCopyID {
		about += " · the key to log in with, not the key to install"
	}
	answer, err := lines.Choose(ctx, ui.Here(r.tool.Name, "Login key").About(about), "Key number", choices)
	if err != nil {
		return "", r.menuFailed(ctx, err, "no key chosen")
	}
	if answer == cancelKey {
		return "", r.cancelled(ctx)
	}
	n, err := strconv.Atoi(answer)
	if err != nil {
		r.failf("the menu answered %q: %v.", answer, err)
		return "", term.ExitFailure
	}
	return keys[n-1], term.ExitOK
}

// menuFailed is a menu that ended without an answer: left (E7, E16), given up after its tries,
// or broken.
func (r *run) menuFailed(ctx context.Context, err error, what string) term.ExitCode {
	if errors.Is(err, ui.ErrCancelled) {
		return r.cancelled(ctx)
	}
	if gave, ok := errors.AsType[*ui.GaveUpError](err); ok {
		r.failf("%s after %d tries; nothing was run.", what, gave.Tries)
		return term.ExitFailure
	}
	r.failf("%v.", err)
	return term.ExitFailure
}

// terminal is the menus' terminal: the controlling terminal for input, stderr for the screens.
func (r *run) terminal() (*ui.Lines, func() error, bool) {
	tty, err := r.env.OpenTTY()
	if err != nil {
		return nil, nil, false
	}
	width := headingWidth
	if n := r.env.Columns(); n > 0 {
		width = n
	}
	colour := r.env.IsTerminal(r.env.Stderr) && r.env.Getenv("NO_COLOR") == ""
	return &ui.Lines{In: tty, Out: r.env.Stderr, Colour: colour, Width: width}, tty.Close, true
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
