package open

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// candidate is one way to open a target: a desktop application (desktopFile set) or a terminal
// handler (argv set, with an optional pager the argv's stdout feeds).
type candidate struct {
	label string
	// A desktop application: its ID and the entry gio launches.
	id, desktopFile string
	// A terminal handler: the program and its arguments, the target among them.
	argv, pager []string
}

func (c candidate) gui() bool { return c.desktopFile != "" }

func (c candidate) kind() string {
	if c.gui() {
		return "gui"
	}
	return "term"
}

// gioProg lists a type's applications, sets a default and starts an application (D2).
const gioProg = "gio"

// gioMissing is E8's refusal in a graphical session without gio.
const gioMissing = "gio (glib2) is not installed; it lists and starts desktop apps. 'open -t' offers terminal handlers only."

// desktopCandidates is E8-E10: the applications gio knows for mime, the default first, and
// whether there is a default.
func (r *run) desktopCandidates(ctx context.Context, mime string) ([]candidate, bool, error) {
	if _, err := r.env.LookPath(gioProg); err != nil {
		return nil, false, fail("%s", gioMissing)
	}
	var stdout bytes.Buffer
	_, err := r.env.Runner.Run(ctx, execx.Cmd{
		Name: gioProg, Args: []string{"mime", mime}, Env: []string{"LC_ALL=C.UTF-8"}, Stdout: &stdout,
	})
	if err != nil {
		return nil, false, fail("gio mime %s failed: %v", mime, err)
	}
	def, ids := parseGioMime(stdout.String())
	index, err := r.desktopIndex()
	if err != nil {
		return nil, false, err
	}
	var cands []candidate
	hasDefault := false
	for _, id := range ids {
		path, found := index[id]
		if !found {
			continue
		}
		name, err := desktopName(path, id)
		if err != nil {
			return nil, false, err
		}
		if id == def && len(cands) == 0 {
			hasDefault = true
		}
		cands = append(cands, candidate{label: name + " [GUI]", id: id, desktopFile: path})
	}
	return cands, hasDefault, nil
}

// parseGioMime reads `gio mime <type>` in the C locale (E8): the default's ID, and every ID it
// names, the default first, each once.
func parseGioMime(out string) (string, []string) {
	const defaultPrefix = "Default application for "
	var def string
	var ids []string
	for line := range strings.Lines(out) {
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, defaultPrefix):
			if i := strings.LastIndex(line, ": "); i >= 0 {
				def = strings.TrimSpace(line[i+2:])
			}
		case strings.HasPrefix(line, "\t"):
			if id := strings.TrimSpace(line); strings.HasSuffix(id, ".desktop") && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	if def != "" {
		ids = append([]string{def}, slices.DeleteFunc(ids, func(id string) bool { return id == def })...)
	}
	return def, ids
}

// applicationDirs is E9's search order.
func (r *run) applicationDirs() []string {
	dataHome := r.env.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(r.env.Getenv("HOME"), ".local", "share")
	}
	dataDirs := r.env.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	dirs := []string{filepath.Join(dataHome, "applications")}
	for d := range strings.SplitSeq(dataDirs, ":") {
		if d != "" {
			dirs = append(dirs, filepath.Join(d, "applications"))
		}
	}
	return dirs
}

// desktopIndex is E9: every desktop entry's ID and path, the first directory that has an ID
// winning. Built once per run.
func (r *run) desktopIndex() (map[string]string, error) {
	if r.apps != nil {
		return r.apps, nil
	}
	index := map[string]string{}
	for _, dir := range r.applicationDirs() {
		if err := indexDir(dir, index); err != nil {
			return nil, err
		}
	}
	r.apps = index
	return index, nil
}

// indexDir adds dir's entries to index, keeping IDs already there. A missing directory adds
// nothing; one that cannot be read is an error.
func indexDir(dir string, index map[string]string) error {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".desktop") {
			return nil
		}
		entry, err := isEntryFile(path)
		if err != nil || !entry {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		id := strings.ReplaceAll(filepath.ToSlash(rel), "/", "-")
		if _, taken := index[id]; !taken {
			index[id] = path
		}
		return nil
	})
	if err != nil {
		return fail("cannot read %s: %v", dir, err)
	}
	return nil
}

// isEntryFile is whether path, followed through a symlink, is a regular file (B2). A dangling
// link is not an entry; any other failure to look is an error.
func isEntryFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

// desktopName is E10: the [Desktop Entry] group's unlocalised Name, else the ID without
// .desktop.
func desktopName(path, id string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fail("cannot read %s: %v", path, err)
	}
	inEntry := false
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if inEntry && found && strings.TrimSpace(key) == "Name" {
			if name := strings.TrimSpace(value); name != "" {
				return name, nil
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", fail("cannot read %s: %v", path, err)
	}
	return strings.TrimSuffix(id, ".desktop"), nil
}
