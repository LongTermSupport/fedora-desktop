// Package checkoutpin holds the checkout files the host launcher applies to the approval a person
// gave them (Plan 00266 Phase A, Defence Before Fix; the class is "host launcher applies
// container-writable checkout config", held by project_checkout_config_pin and the semgrep rule
// ccy-host-applies-checkout-config).
//
// A ccy container writes its checkout, so it writes the files the host launcher reads there:
// .claude/ccy/mounts, whose lines the launcher binds into the session's next container, and the
// compose files, which it runs `up -d` on as the host user. Applied unasked, either lets a
// container widen its own next launch, and a restore after a reboot applies them with nobody
// there. So each is applied only when its content is content a person approved:
//
//   - K1. The approvals live in the user's state directory beside the session registry
//     ($CCY_STATE_DIR/approved, else ${XDG_STATE_HOME:-~/.local/state}/ccy/approved; Locate),
//     one record per checkout and kind (0700 directories, 0600 files), never in a checkout and
//     never under a path a mounts line may bind (ParseMounts refuses ~/.local/state and the
//     relocated base, and every directory above them).
//   - K2. A record holds the checkout's real path and the SHA-256 of the content approved. It is
//     read strictly: another format, another checkout or a malformed digest is an error, never a
//     pass.
//   - K3. Verify passes only the recorded digest. Nothing recorded, or another digest, is a
//     RefusalError naming the file and the exact verb that approves it (ApproveVerb).
//   - K4. The mounts digest is the bytes of .claude/ccy/mounts; the compose digest covers every
//     compose file and override present, the .env they are interpolated from and each file they
//     include or extend, by name and content, so a file appearing changes it. A link in a pinned
//     file's place, a referenced file outside the checkout and a .env setting COMPOSE_FILE are
//     refused: no pin could cover what they make the engine read. Every read goes through one
//     os.Root of the checkout.
package checkoutpin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
)

// Mounts is a project's mounts file, in its checkout.
const Mounts = ".claude/ccy/mounts"

// ComposeFiles are the compose files and overrides podman-compose and docker compose read from a
// project's directory.
var ComposeFiles = []string{
	"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml", "podman-compose.yaml", "podman-compose.yml",
	"compose.override.yaml", "compose.override.yml", "docker-compose.override.yaml", "docker-compose.override.yml",
	"podman-compose.override.yaml", "podman-compose.override.yml",
}

// composeEnv is the file compose interpolates its files from.
const composeEnv = ".env"

// Kind is what a record approves.
type Kind string

// The kinds; KindLastLaunch is beside its file (lastlaunch.go).
const (
	KindMounts  Kind = "mounts"
	KindCompose Kind = "compose"
)

// File is the checkout file a kind names, as a refusal says it.
func (k Kind) File() string {
	switch k {
	case KindMounts:
		return Mounts
	case KindLastLaunch:
		return LastLaunch
	case KindCompose:
	}
	return "compose files"
}

// ApproveVerb is the command a person runs to approve a kind's content in a checkout.
func ApproveVerb(kind Kind, checkout string) string {
	return "ccy-teams approve " + string(kind) + " " + shellword.Quote(checkout)
}

// Store is the approvals, under Dir.
type Store struct{ Dir string }

// Locate is the store beside the session registry (K1).
func Locate(getenv func(string) string) (Store, error) {
	sessions, err := registry.Dir(getenv)
	if err != nil {
		return Store{}, err
	}
	return Store{Dir: filepath.Join(filepath.Dir(sessions), "approved")}, nil
}

// Base is the launcher's state directory the store is in, which no mounts line may bind.
func (s Store) Base() string { return filepath.Dir(s.Dir) }

// RealCheckout is a checkout directory's real path, which records are keyed by.
func RealCheckout(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", resolved)
	}
	return resolved, nil
}

// path is the record of kind for checkout: a directory named for the checkout's path.
func (s Store) path(checkout string, kind Kind) string {
	sum := sha256.Sum256([]byte(checkout))
	return filepath.Join(s.Dir, hex.EncodeToString(sum[:16]), string(kind))
}

const header = "ccy-checkout-pin 1"

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// RefusalError is content no person approved (K3).
type RefusalError struct {
	Kind     Kind
	Checkout string
	Why      string
}

func (e *RefusalError) Error() string {
	file := e.Checkout + "/" + e.Kind.File()
	if e.Kind == KindCompose {
		file = "the compose files in " + e.Checkout
	}
	return fmt.Sprintf("%s: %s, so it is not applied (a container session can write it). If the change is yours, approve it at a terminal: %s",
		file, e.Why, ApproveVerb(e.Kind, e.Checkout))
}

// Verify passes when digest is the one recorded for kind in checkout (K3).
func (s Store) Verify(checkout string, kind Kind, digest string) error {
	recorded, at, found, err := s.read(checkout, kind)
	switch {
	case err != nil:
		return err
	case !found:
		return &RefusalError{Kind: kind, Checkout: checkout, Why: "no approval is recorded for its content"}
	case recorded != digest:
		return &RefusalError{Kind: kind, Checkout: checkout, Why: "it has changed since it was approved (" + at + ")"}
	}
	return nil
}

// Record approves digest as kind's content in checkout.
func (s Store) Record(checkout string, kind Kind, digest string, at time.Time) error {
	if !digestPattern.MatchString(digest) {
		return fmt.Errorf("refusing to record %q: not a SHA-256", digest)
	}
	file := s.path(checkout, kind)
	if err := os.MkdirAll(s.Base(), 0o700); err != nil {
		return err
	}
	for _, dir := range []string{s.Dir, filepath.Dir(file)} {
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	text := header + "\ncheckout=" + checkout + "\nsha256=" + digest + "\napproved=" + at.UTC().Format(time.RFC3339) + "\n"
	return fsx.WriteAtomic(file, "the approval of "+string(kind)+" in "+checkout, []byte(text), 0o600)
}

// read is the digest recorded for kind in checkout and when (K2).
func (s Store) read(checkout string, kind Kind) (digest, at string, found bool, err error) {
	file := s.path(checkout, kind)
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) != 5 || lines[0] != header || lines[4] != "" ||
		lines[1] != "checkout="+checkout || !strings.HasPrefix(lines[2], "sha256=") || !strings.HasPrefix(lines[3], "approved=") {
		return "", "", false, fmt.Errorf("%s is not an approval record of %s; remove it and approve again: %s", file, checkout, ApproveVerb(kind, checkout))
	}
	return strings.TrimPrefix(lines[2], "sha256="), strings.TrimPrefix(lines[3], "approved="), true, nil
}

// MountsDigest is the mounts file's content and digest, and whether there is one (K4).
func MountsDigest(checkout string) (content []byte, digest string, present bool, err error) {
	return fileDigest(checkout, Mounts)
}

// maxComposeFiles bounds the files a compose digest follows.
const maxComposeFiles = 64

// ComposeDigest is the files a compose up in checkout reads, their digest, and whether there is
// any compose file at all (K4).
func ComposeDigest(checkout string) (files []string, digest string, present bool, err error) {
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return nil, "", false, err
	}
	defer closeRoot(root, &err)
	w := composeWalk{root: root, checkout: checkout, seen: map[string]bool{}}
	for _, name := range ComposeFiles {
		if err := w.add(name, false); err != nil {
			return nil, "", false, err
		}
	}
	if len(w.files) == 0 {
		return nil, "", false, nil
	}
	if err := w.env(); err != nil {
		return nil, "", false, err
	}
	for i := 0; i < len(w.files); i++ {
		if err := w.follow(w.files[i]); err != nil {
			return nil, "", false, err
		}
	}
	sum := sha256.Sum256(w.text.Bytes())
	return w.files, hex.EncodeToString(sum[:]), true, nil
}

// composeWalk is one ComposeDigest: the files found so far, in order, and the text hashed.
type composeWalk struct {
	root     *os.Root
	checkout string
	seen     map[string]bool
	files    []string
	text     bytes.Buffer
}

// add puts one file into the digest, present or not; a referenced one must be present.
func (w *composeWalk) add(name string, referenced bool) error {
	content, here, err := readPinned(w.root, name)
	if err != nil {
		return err
	}
	if referenced && !here {
		return fmt.Errorf("%s/%s is included by a compose file and is not there", w.checkout, name)
	}
	w.entry(name, here, content)
	if here {
		w.seen[name] = true
		w.files = append(w.files, name)
	}
	return nil
}

// env puts the .env into the digest, refusing one that names other compose files.
func (w *composeWalk) env() error {
	content, here, err := readPinned(w.root, composeEnv)
	if err != nil {
		return err
	}
	if here && composeFileSet.Match(content) {
		return fmt.Errorf("%s/%s sets COMPOSE_FILE, which makes compose read files no approval covers; remove it", w.checkout, composeEnv)
	}
	w.entry(composeEnv, here, content)
	return nil
}

// entry is one file's part of the hashed text: its name, whether it is there, its length and
// its bytes, so no two sets of files hash alike.
func (w *composeWalk) entry(name string, here bool, content []byte) {
	w.text.WriteString(name + "\x00" + strconv.FormatBool(here) + "\x00" + strconv.Itoa(len(content)) + "\x00")
	w.text.Write(content)
}

// follow adds the files one compose file includes or extends.
func (w *composeWalk) follow(name string) error {
	content, _, err := readPinned(w.root, name)
	if err != nil {
		return err
	}
	refs, err := composeRefs(content)
	if err != nil {
		return fmt.Errorf("%s/%s: %w", w.checkout, name, err)
	}
	for _, ref := range refs {
		rel := path.Clean(path.Join(path.Dir(name), ref))
		if path.IsAbs(ref) || rel == ".." || strings.HasPrefix(rel, "../") {
			return fmt.Errorf("%s/%s includes %s, outside the checkout, which no approval can cover", w.checkout, name, ref)
		}
		if w.seen[rel] {
			continue
		}
		if len(w.seen) >= maxComposeFiles {
			return fmt.Errorf("%s: the compose files reach more than %d files", w.checkout, maxComposeFiles)
		}
		if err := w.add(rel, true); err != nil {
			return err
		}
	}
	return nil
}

var composeFileSet = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?COMPOSE_FILE[ \t]*=`)

// composeRefs are the other files a compose file reads: each include (a path, or a map's path,
// one or a list) and each service's extends file.
func composeRefs(content []byte) ([]string, error) {
	var doc struct {
		Include  []yaml.Node `yaml:"include"`
		Services map[string]struct {
			Extends yaml.Node `yaml:"extends"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("not readable as compose YAML: %w", err)
	}
	var refs []string
	for _, n := range doc.Include {
		switch n.Kind {
		case yaml.ScalarNode:
			refs = append(refs, n.Value)
		case yaml.MappingNode:
			var entry struct {
				Path yaml.Node `yaml:"path"`
			}
			if err := n.Decode(&entry); err != nil {
				return nil, err
			}
			paths, err := scalars(entry.Path)
			if err != nil {
				return nil, err
			}
			refs = append(refs, paths...)
		case yaml.DocumentNode, yaml.SequenceNode, yaml.AliasNode:
			return nil, errors.New("an include entry is neither a path nor a map")
		}
	}
	for _, name := range slices.Sorted(maps.Keys(doc.Services)) {
		svc := doc.Services[name]
		if svc.Extends.Kind != yaml.MappingNode {
			continue
		}
		var ext struct {
			File string `yaml:"file"`
		}
		if err := svc.Extends.Decode(&ext); err != nil {
			return nil, err
		}
		if ext.File != "" {
			refs = append(refs, ext.File)
		}
	}
	return refs, nil
}

// scalars is a node that is one string or a list of strings.
func scalars(n yaml.Node) ([]string, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		return []string{n.Value}, nil
	case yaml.SequenceNode:
		var out []string
		if err := n.Decode(&out); err != nil {
			return nil, err
		}
		return out, nil
	case yaml.DocumentNode, yaml.MappingNode, yaml.AliasNode:
	}
	return nil, errors.New("an include path is neither a string nor a list")
}

// readPinned reads a regular file of the checkout; absent is not present, and a link or anything
// else in its place is refused.
func readPinned(root *os.Root, name string) ([]byte, bool, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%s/%s is not a regular file (%s), so no approval can cover what it reads", root.Name(), name, info.Mode().Type())
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return nil, false, err
	}
	return bytes.Clone(data), true, nil
}

func closeRoot(root *os.Root, err *error) {
	if closeErr := root.Close(); closeErr != nil && *err == nil {
		*err = closeErr
	}
}
