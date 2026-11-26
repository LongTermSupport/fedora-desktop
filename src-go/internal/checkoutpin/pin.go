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
//   - K4. The mounts digest is the bytes of .claude/ccy/mounts and where each source resolves
//     (K8); the compose digest covers every
//     compose file and override present, the .env they are interpolated from and each file they
//     include or extend, by name and content, so a file appearing changes it. A link in a pinned
//     file's place, a referenced file outside the checkout and a .env setting COMPOSE_FILE are
//     refused: no pin could cover what they make the engine read. Every read goes through one
//     os.Root of the checkout.
//   - K6. An approval screen shows what is approved, not what a terminal would draw from it:
//     every line Command prints goes through Shown, which escapes each control and format
//     character (a bidi override would reorder a path or value on screen), and the values ccy
//     writes or binds (the saved launch choices, the mounts paths) refuse them outright.
//   - K7. The compose digest covers everything compose reads from the checkout, not only the
//     compose files: each build's Dockerfile, each env_file and label_file (a service's or an
//     include's), and each config's and secret's file, by name, presence and content. A path
//     compose hands the engine by name (a bind source, a build context or additional context,
//     a config or secret file, a bind volume's device) is refused when any part of it in the
//     checkout is a link, since a container could point it anywhere without changing a byte
//     hashed. A build context's other files are not hashed: they are the project's source,
//     which a session edits by design, and what the image's code can reach on this machine is
//     fixed by the compose text approved. What the walk cannot judge is refused, by name: a key
//     it does not know (but an x- extension), a path interpolated from any variable but
//     ${PWD}, a credential_spec file, a file read from outside the checkout. Every path is
//     checked again at each use, so a link made after the approval is refused too; a link made
//     between the check and the engine's own read is not (a live container of the same
//     checkout could race it).
//   - K8. A mounts approval covers where each source resolves, not only the file's text: the
//     digest holds each source's real path (every link on the way followed; one not there yet
//     through its nearest existing directory), taken again at each launch, so a link retargeted
//     after the approval is refused like an edit. Device and inode are not held: an rclone
//     FUSE mount gets a new device number at each mount, so a restore after a reboot would
//     refuse every such source. A source a container of the launch could write a part of is
//     refused outright, since it could make that part a link between the check and the bind:
//     one inside the checkout, or at or inside another read-write source (the file's own lines
//     and CCY_EXTRA_MOUNTS). The deny list holds the shared temporary directories (/tmp,
//     /var/tmp; /dev is denied whole), the user's runtime directory and tmux's socket
//     directory (XDG_RUNTIME_DIR, TMUX_TMPDIR), where tmux's and podman's sockets live.
package checkoutpin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
)

// Mounts is a project's mounts file, in its checkout.
const Mounts = ".claude/ccy/mounts"

// ComposeFiles are the compose files and overrides podman-compose and docker compose read from a
// project's directory; container-compose is podman-compose's own name too.
var ComposeFiles = []string{
	"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml", "podman-compose.yaml", "podman-compose.yml",
	"container-compose.yaml", "container-compose.yml",
	"compose.override.yaml", "compose.override.yml", "docker-compose.override.yaml", "docker-compose.override.yml",
	"podman-compose.override.yaml", "podman-compose.override.yml", "container-compose.override.yaml", "container-compose.override.yml",
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
	case recorded != digest && kind == KindMounts:
		return &RefusalError{Kind: kind, Checkout: checkout, Why: "it, or where one of its sources resolves, has changed since it was approved (" + at + ")"}
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
	read, digest, present, err := ComposeRead(checkout)
	for _, f := range read {
		if f.Name != composeEnv {
			files = append(files, f.Name)
		}
	}
	return files, digest, present, err
}

// PinnedFile is one file a digest covers, by its name in the checkout, and the bytes hashed.
type PinnedFile struct {
	Name    string
	Content []byte
}

// ComposeRead is ComposeDigest with each file's bytes, the .env among them when there is one,
// in the order hashed: what an approval of the compose files shows (V3).
func ComposeRead(checkout string) (files []PinnedFile, digest string, present bool, err error) {
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return nil, "", false, err
	}
	defer closeRoot(root, &err)
	w := composeWalk{root: root, checkout: checkout, seen: map[string]bool{}}
	for _, name := range ComposeFiles {
		if err := w.add(name, true); err != nil {
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
	return w.read, hex.EncodeToString(sum[:]), true, nil
}

// composeWalk is one ComposeDigest: the files found so far, in order, and the text hashed.
type composeWalk struct {
	root     *os.Root
	checkout string
	seen     map[string]bool
	files    []string
	read     []PinnedFile
	text     bytes.Buffer
}

// add puts one file into the digest, present or not. A compose file a compose file includes
// must be present, and is walked in turn (files); another is hashed only.
func (w *composeWalk) add(name string, compose bool) error {
	content, here, err := readPinned(w.root, name)
	if err != nil {
		return err
	}
	w.seen[name] = true
	w.entry(name, here, content)
	if !compose || !here {
		return nil
	}
	w.files = append(w.files, name)
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
	if here {
		w.read = append(w.read, PinnedFile{Name: name, Content: content})
	}
}

// follow adds what one compose file makes compose read (K7): the compose files it includes or
// extends, and the other files it reads, each hashed; and it refuses a link in any checkout path
// compose hands the engine by name.
func (w *composeWalk) follow(name string) error {
	content, _, err := readPinned(w.root, name)
	if err != nil {
		return err
	}
	use, err := composeUses(content)
	if err != nil {
		return fmt.Errorf("%s/%s: %w", w.checkout, name, err)
	}
	for _, ref := range use.linkFree {
		if err := w.linkFree(name, ref, use.outsideOK[ref]); err != nil {
			return err
		}
	}
	if err := w.take(name, use.follow, true); err != nil {
		return err
	}
	return w.take(name, use.pinned, false)
}

// take adds each file name makes compose read, none of them outside the checkout; an included
// compose file must be there.
func (w *composeWalk) take(name string, refs []string, compose bool) error {
	for _, ref := range refs {
		rel, outside, err := w.resolve(name, ref)
		if err != nil {
			return err
		}
		if outside {
			return fmt.Errorf("%s/%s makes compose read %s, outside the checkout, which no approval can cover", w.checkout, name, ref)
		}
		if err := w.addOnce(rel, compose); err != nil {
			return err
		}
		if compose && !slices.Contains(w.files, rel) {
			return fmt.Errorf("%s/%s is included by a compose file and is not there", w.checkout, rel)
		}
	}
	return nil
}

// addOnce adds a file the walk has not seen: a compose file must be there, another file is
// hashed as there or not.
func (w *composeWalk) addOnce(rel string, compose bool) error {
	if w.seen[rel] {
		return nil
	}
	if len(w.seen) >= maxComposeFiles {
		return fmt.Errorf("%s: the compose files reach more than %d files", w.checkout, maxComposeFiles)
	}
	return w.add(rel, compose)
}

// resolve is a path compose reads, as name writes it, relative to the checkout, or outside it.
// Only ${PWD} (the checkout, where the launcher runs compose) and a leading ~ are expanded: any
// other interpolation is refused, since the walk cannot tell what compose would make of it.
func (w *composeWalk) resolve(name, ref string) (rel string, outside bool, err error) {
	p := strings.NewReplacer("${PWD}", w.checkout, "$PWD", w.checkout).Replace(ref)
	if strings.Contains(p, "$") {
		return "", false, fmt.Errorf("%s/%s names %s, interpolated from a variable, so ccy cannot tell which path compose reads; write the path out (only ${PWD}, the checkout, is read)", w.checkout, name, ref)
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false, fmt.Errorf("%s/%s names %s, and the home directory is not known: %w", w.checkout, name, ref, err)
		}
		p = home + p[1:]
	}
	if path.IsAbs(p) {
		p = path.Clean(p)
		if p != w.checkout && !strings.HasPrefix(p, w.checkout+"/") {
			return "", true, nil
		}
		return strings.TrimPrefix(strings.TrimPrefix(p, w.checkout), "/"), false, nil
	}
	rel = path.Clean(path.Join(path.Dir(name), p))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", true, nil
	}
	return rel, false, nil
}

// linkFree refuses a link in any part of a checkout path compose hands the engine by name. A
// part not there yet is no link; a path outside the checkout is refused unless outsideOK.
func (w *composeWalk) linkFree(name, ref string, outsideOK bool) error {
	rel, outside, err := w.resolve(name, ref)
	if err != nil {
		return err
	}
	if outside {
		if outsideOK {
			return nil
		}
		return fmt.Errorf("%s/%s builds from %s, outside the checkout, which no approval can cover", w.checkout, name, ref)
	}
	if rel == "" || rel == "." {
		return nil
	}
	parts := strings.Split(rel, "/")
	for i := range parts {
		part := path.Join(parts[:i+1]...)
		info, err := w.root.Lstat(part)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s/%s hands the engine %s, and %s/%s is a link: a container session could point it anywhere on this machine without changing a byte approved; put the directory or file itself there", w.checkout, name, ref, w.checkout, part)
		}
	}
	return nil
}

var composeFileSet = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?COMPOSE_FILE[ \t]*=`)

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
