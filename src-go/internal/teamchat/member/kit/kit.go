// Package kit assembles a member kit directory (Plan 00209 SPEC-n3 KT1, n3-D4, B 2.12): the
// static binary, the plugin whose hooks.json names `<kit>/bin/teamchat hook <event>` by
// absolute path for Claude Code's four events, and the settings with the status line. The
// commands embed the kit's own path, so a kit is assembled for the path it will be placed at
// (the host kit's is valid unchanged in a guest, whose path is the same).
package kit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// Hook is one Claude Code hook event and the argument `teamchat hook` takes for it.
type Hook struct {
	Event string
	Arg   string
}

// Hooks are the plugin's hooks (B 2.8), and nothing else.
var Hooks = []Hook{
	{"SessionStart", "session-start"},
	{"UserPromptSubmit", "user-prompt-submit"},
	{"Stop", "stop"},
	{"SessionEnd", "session-end"},
}

// StatusLineRefresh is the status line's refreshInterval in seconds (B 2.9): below the 30 s
// heartbeat, so a stale segment is seen within one beat.
const StatusLineRefresh = 25

// maxBinary caps the binary read into the kit.
var maxBinary int64 = 256 << 20

const (
	dirMode  = 0o755
	fileMode = 0o644
	binMode  = 0o755
)

// shellSafeGrammar is the characters a kit path may hold: Claude Code runs a hook's and the
// status line's command through a shell, so the path must need no quoting.
var shellSafeGrammar = regexp.MustCompile(`^[A-Za-z0-9/._-]+$`)

func shellSafe(p string) bool {
	return shellSafeGrammar.MatchString(p) && filepath.IsAbs(p) && filepath.Clean(p) == p && p != "/"
}

var errInto = errors.New("the kit is assembled into an existing, empty directory of this user's that no one else can write")

// Assemble writes the kit for kit's path into the directory into, which must exist, be empty,
// be this user's and not group- or other-writable. Every directory and the binary are 0755 and
// every other file 0644, set by fchmod whatever the umask; nothing is opened through a link.
func Assemble(into string, kit paths.Kit, binary io.Reader) error {
	if !shellSafe(kit.String()) {
		return errors.New("the kit path must be absolute, clean and hold only A-Z a-z 0-9 / . _ -")
	}
	if !filepath.IsAbs(into) || filepath.Clean(into) != into {
		return errInto
	}
	root, err := openRoot(into)
	if err != nil {
		return err
	}
	return errors.Join(assemble(root, kit, binary), root.close())
}

// entry is one file or directory of the kit; a nil content is a directory.
type entry struct {
	path    string
	mode    uint32
	content io.Reader
	limit   int64
}

func entries(kit paths.Kit, binary io.Reader) []entry {
	plugin := "plugin/" + paths.PluginName
	doc := func(path string, v proto.Value) entry { return entry{path, fileMode, document(v), maxDocument} }
	return []entry{
		{"bin", dirMode, nil, 0},
		{"bin/" + paths.MemberBinary, binMode, binary, maxBinary},
		doc("settings.json", settings(kit)),
		{"plugin", dirMode, nil, 0},
		{plugin, dirMode, nil, 0},
		{plugin + "/.claude-plugin", dirMode, nil, 0},
		doc(plugin+"/.claude-plugin/plugin.json", manifest()),
		{plugin + "/hooks", dirMode, nil, 0},
		doc(plugin+"/hooks/hooks.json", hooks(kit)),
	}
}

func assemble(root *dir, kit paths.Kit, binary io.Reader) error {
	return build(root, entries(kit, binary))
}

// build makes each entry below its parent, which an earlier entry made; every directory it
// opened is synced and closed, last made first.
func build(root *dir, list []entry) (err error) {
	open := map[string]*dir{".": root}
	var made []*dir
	defer func() {
		for _, d := range slices.Backward(made) {
			err = errors.Join(err, d.close())
		}
	}()
	for _, e := range list {
		parent, name := open[filepath.Dir(e.path)], filepath.Base(e.path)
		if e.content != nil {
			if err := parent.create(name, e.mode, e.content, e.limit); err != nil {
				return err
			}
			continue
		}
		d, err := parent.mkdir(name)
		if err != nil {
			return err
		}
		open[e.path] = d
		made = append(made, d)
	}
	return nil
}

const maxDocument = 64 << 10

func document(v proto.Value) io.Reader { return bytes.NewReader(append(v.Compact(), '\n')) }

func command(kit paths.Kit, args string) proto.Value {
	return proto.Obj(proto.M("type", proto.Str("command")), proto.M("command", proto.Str(kit.Binary()+" "+args)))
}

func settings(kit paths.Kit) proto.Value {
	line := command(kit, "status-line")
	members := append(line.Members(), proto.M("refreshInterval", proto.Int(StatusLineRefresh)))
	return proto.Obj(proto.M("statusLine", proto.Obj(members...)))
}

func manifest() proto.Value {
	return proto.Obj(
		proto.M("name", proto.Str(paths.PluginName)),
		proto.M("description", proto.Str("teamchat's hooks for the member session this kit launches")),
	)
}

func hooks(kit paths.Kit) proto.Value {
	events := make([]proto.Member, len(Hooks))
	for i, h := range Hooks {
		events[i] = proto.M(h.Event, proto.Arr(proto.Obj(proto.M("hooks", proto.Arr(command(kit, "hook "+h.Arg))))))
	}
	return proto.Obj(proto.M("hooks", proto.Obj(events...)))
}

// dir is an open directory the kit is written below, by its fd only.
type dir struct {
	fd   int
	path string
}

func openRoot(path string) (*dir, error) {
	fd, err := unix.Open(path, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInto, err)
	}
	d := &dir{fd: fd, path: path}
	if err := d.checkRoot(); err != nil {
		return nil, errors.Join(err, unix.Close(fd))
	}
	return d, nil
}

func (d *dir) checkRoot() error {
	var st unix.Stat_t
	if err := unix.Fstat(d.fd, &st); err != nil {
		return err
	}
	if int(st.Uid) != os.Geteuid() || st.Mode&0o022 != 0 {
		return errInto
	}
	dup, err := unix.Dup(d.fd)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(dup), d.path)
	names, err := f.Readdirnames(1)
	err = errors.Join(ignoreEOF(err), f.Close())
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return errInto
	}
	return unix.Fchmod(d.fd, dirMode)
}

func ignoreEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// mkdir makes a new directory below d, 0755, and opens it.
func (d *dir) mkdir(name string) (*dir, error) {
	if err := unix.Mkdirat(d.fd, name, dirMode); err != nil {
		return nil, fmt.Errorf("making %s/%s: %w", d.path, name, err)
	}
	fd, err := unix.Openat(d.fd, name, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s/%s: %w", d.path, name, err)
	}
	if err := unix.Fchmod(fd, dirMode); err != nil {
		return nil, errors.Join(err, unix.Close(fd))
	}
	return &dir{fd: fd, path: d.path + "/" + name}, nil
}

// create writes a new file below d from r, at most limit bytes and at least one, then syncs it.
func (d *dir) create(name string, mode uint32, r io.Reader, limit int64) (err error) {
	fd, err := unix.Openat(d.fd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, mode)
	if err != nil {
		return fmt.Errorf("creating %s/%s: %w", d.path, name, err)
	}
	f := os.NewFile(uintptr(fd), d.path+"/"+name)
	defer func() { err = errors.Join(err, f.Close()) }()
	if err = f.Chmod(os.FileMode(mode)); err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, limit+1))
	switch {
	case err != nil:
		return fmt.Errorf("writing %s/%s: %w", d.path, name, err)
	case n == 0 || n > limit:
		return fmt.Errorf("%s/%s: the content is empty or over %d bytes", d.path, name, limit)
	}
	return f.Sync()
}

// close syncs d, so its new entries are durable, and closes it.
func (d *dir) close() error {
	return errors.Join(unix.Fsync(d.fd), unix.Close(d.fd))
}
