// Package ansible is qa-ansible: the static Ansible gate. It scans the Ansible source trees with
// text patterns and runs six checks that ansible-playbook --syntax-check cannot: error-hiding
// directives, playbook shebang and execute bit, self-default variables, provisioning scope and
// guard, deprecated top-level fact variables and guessed runtime-directory uids. It starts no
// process (the bash it replaced ran grep, find, awk, head and jq); the list of gitignored files,
// which it leaves unread, is injected through Env.Ignored. SPEC-qa-ansible.md is its specification.
package ansible

import (
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
)

// isYAML is whether a base name is one the checks read: the extensions of --include='*.yml' and
// --include='*.yaml' (and find's -name), hidden names included.
func isYAML(name string) bool {
	return strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")
}

// dirExists is whether dir is a directory, a symlink to one included (the bash's [[ -d ]]).
func dirExists(fsys fs.FS, dir string) bool {
	info, err := fs.Stat(fsys, dir)
	return err == nil && info.IsDir()
}

// yamlFiles lists the YAML files under dir, as paths relative to the root, directories read in
// name order and depth first. Like grep -r and find -type f it neither follows nor lists a
// regular file only; dir itself may be a symlink. A symlink met below dir that names a YAML file
// is read as that file; one to a directory, or a dangling one, is an error (the bash skipped
// them without a word). skipVendor leaves out roles/vendor, the ansible-galaxy install target
// (the bash left out every directory named vendor, at any depth).
func yamlFiles(fsys fs.FS, dir string, skipVendor bool) ([]string, error) {
	var found []string
	if err := walk(fsys, dir, skipVendor, &found); err != nil {
		return nil, err
	}
	return found, nil
}

func walk(fsys fs.FS, dir string, skipVendor bool, found *[]string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("cannot read the directory %s: %w", dir, err)
	}
	for _, entry := range entries {
		name := path.Join(dir, entry.Name())
		switch {
		case entry.Type()&fs.ModeSymlink != 0:
			if err := followLink(fsys, name, found); err != nil {
				return err
			}
		case entry.IsDir():
			if skipVendor && name == "roles/vendor" {
				continue
			}
			if err := walk(fsys, name, skipVendor, found); err != nil {
				return err
			}
		case entry.Type().IsRegular() && isYAML(entry.Name()):
			*found = append(*found, name)
		}
	}
	return nil
}

// followLink lists a symlink to a YAML file and refuses one it cannot read as a file.
func followLink(fsys fs.FS, name string, found *[]string) error {
	info, err := fs.Stat(fsys, name)
	if err != nil {
		return fmt.Errorf("the symlink %s cannot be read: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("the symlink %s is not a regular file and is not followed", name)
	}
	if isYAML(name) {
		*found = append(*found, name)
	}
	return nil
}

// publishedFS is a file system without the entries git ignores: they are not part of what the
// repository publishes, so the gate neither reads them nor trips over them.
type publishedFS struct {
	fs.FS

	ignored map[string]bool
}

// ReadDir implements fs.ReadDirFS.
func (p publishedFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(p.FS, name)
	if err != nil {
		return nil, err
	}
	kept := entries[:0]
	for _, entry := range entries {
		if !p.ignored[path.Join(name, entry.Name())] {
			kept = append(kept, entry)
		}
	}
	return kept, nil
}

// published is fsys without the ignored paths.
func published(fsys fs.FS, ignored []string) fs.FS {
	set := make(map[string]bool, len(ignored))
	for _, p := range ignored {
		set[p] = true
	}
	return publishedFS{FS: fsys, ignored: set}
}

// splitLines is grep's view of a file: lines end at a newline, the last may lack one, and a
// carriage return is part of the line.
func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	text := strings.TrimSuffix(string(data), "\n")
	return strings.Split(text, "\n")
}

// hit is one matching line of a file.
type hit struct {
	rel    string
	number int
	text   string
}

// String is the hit as grep -n printed it, relative to the root.
func (h hit) String() string {
	return h.rel + ":" + strconv.Itoa(h.number) + ":" + h.text
}

// grepLines is grep -rn over the YAML files under dirs (roles/vendor skipped): every line keep
// accepts, in the order of dirs then the walk.
func grepLines(fsys fs.FS, dirs []string, keep func(text string) bool) ([]hit, error) {
	var hits []hit
	for _, dir := range dirs {
		files, err := yamlFiles(fsys, dir, true)
		if err != nil {
			return nil, err
		}
		for _, rel := range files {
			data, err := fs.ReadFile(fsys, rel)
			if err != nil {
				return nil, fmt.Errorf("a file could not be read: %w", err)
			}
			for number, text := range splitLines(data) {
				if keep(text) {
					hits = append(hits, hit{rel: rel, number: number + 1, text: text})
				}
			}
		}
	}
	return hits, nil
}
