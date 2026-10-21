package open

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// urlScheme is E4: the scheme of a URL, lower-cased, by RFC 3986's grammar (a letter, then
// letters, digits, +, - and .) followed by ://, or mailto:.
func urlScheme(target string) (string, bool) {
	scheme, rest, found := strings.Cut(target, ":")
	if !found || scheme == "" || !validScheme(scheme) {
		return "", false
	}
	scheme = strings.ToLower(scheme)
	if strings.HasPrefix(rest, "//") || scheme == "mailto" {
		return scheme, true
	}
	return "", false
}

func validScheme(s string) bool {
	for i, c := range s {
		letter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		other := (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.'
		if !letter && (i == 0 || !other) {
			return false
		}
	}
	return true
}

// resolve is E4-E6: the target as programs will be given it, and its type.
func (r *run) resolve(ctx context.Context, target string) (string, string, error) {
	if scheme, ok := urlScheme(target); ok {
		return target, "x-scheme-handler/" + scheme, nil
	}
	if _, err := os.Stat(target); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", fail("no such file or directory: '%s'", target)
		}
		return "", "", fail("cannot read '%s': %v", target, err)
	}
	if err := r.env.Readable(target); err != nil {
		return "", "", fail("not readable: '%s'", target)
	}
	if strings.HasPrefix(target, "-") {
		target = "./" + target
	}
	mime, err := r.typeOf(ctx, target)
	return target, mime, err
}

// playHint is where the type detectors come from.
const playHint = "Deploy them with: ansible-playbook playbooks/imports/optional/common/play-cli-tools.yml --tags open-command"

// typeOf is E6: inode/directory, else the first detector that answers.
func (r *run) typeOf(ctx context.Context, path string) (string, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return "inode/directory", nil
	}
	detectors := []execx.Cmd{
		{Name: "mimetype", Args: []string{"--brief", "--", path}},
		{Name: "xdg-mime", Args: []string{"query", "filetype", path}},
		{Name: "file", Args: []string{"--brief", "--mime-type", "--", path}},
	}
	var reasons []string
	for _, d := range detectors {
		if _, err := r.env.LookPath(d.Name); err != nil {
			reasons = append(reasons, d.Name+": not installed")
			continue
		}
		mime, err := r.detect(ctx, d)
		if err == nil {
			return mime, nil
		}
		if errors.Is(err, errCancelled) {
			return "", err
		}
		reasons = append(reasons, d.Name+": "+err.Error())
		r.say(fmt.Sprintf("%s could not type '%s': %v; trying the next detector", d.Name, path, err))
	}
	return "", fail("could not determine the file type of '%s'.\n  %s\n  This needs one of: mimetype (perl-File-MimeInfo), xdg-mime (xdg-utils), or file.\n  %s",
		path, strings.Join(reasons, "\n  "), playHint)
}

// detect runs one detector and accepts only one type/subtype word.
func (r *run) detect(ctx context.Context, d execx.Cmd) (string, error) {
	var stdout bytes.Buffer
	d.Stdout = &stdout
	if err := r.step(ctx, d); err != nil {
		return "", err
	}
	answer := strings.TrimSpace(stdout.String())
	if !isMimeType(answer) {
		return "", fmt.Errorf("it answered %q", answer)
	}
	return answer, nil
}

// isMimeType is one type/subtype word: no white space, one slash with text on both sides.
func isMimeType(s string) bool {
	typ, sub, found := strings.Cut(s, "/")
	return found && typ != "" && sub != "" && !strings.ContainsAny(s, " \t\n") && !strings.Contains(sub, "/")
}
