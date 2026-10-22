// Package ctl is the teamchat-ctl child's own files and its reach to a team (Plan 00209
// SPEC-n2f.md OP1-OP6, DESIGN-v2-C-host.md 3.2.2 and 3.6.2 step 1): the admin token under
// ctl/<team>/, bootstrap step 1 (the admin account), and the production admin.Opener.
//
// ctl/<team>/ is opened once, without following a link, and must be a directory the child's
// euid owns with mode 0700; every file in it is opened from that directory, without following
// a link and without blocking, refused unless regular, and capped. A file is written to a
// fresh temporary file (0600, exclusive), synced, linked to its name (an existing name fails,
// never replaced), the temporary unlinked and the directory synced (OP4). No error holds the
// password, the token or the registration key (OP5).
package ctl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/admin"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

const (
	// AdminDevice is the admin account's device (n2f-D2).
	AdminDevice = "TCADMIN"
	// StepAdmin is the step a refusal of bootstrap step 1 names.
	StepAdmin = "admin"

	adminLocalpart = "admin"
	tokenName      = "admin.token"
	passwordName   = "admin.password"
	// maxTokenBytes is the most a token file may hold (OP2).
	maxTokenBytes = 4096
	// passwordBytes is the random bytes of a fresh password, written as twice as many hex.
	passwordBytes = 32
	tempBytes     = 8
	dirMode       = 0o700
	fileMode      = 0o600
	loopback      = "127.0.0.1"
)

// geteuid is the child's effective uid; a variable so a test can stand in another owner.
var geteuid = os.Geteuid

// Config is what Open reads and writes with: the state root's prefix (paths.CtlStateRoot under
// it; "" on a host), the clock, the random source, and the admin client maker.
type Config struct {
	Root  string
	Now   func() time.Time
	Rand  io.Reader
	Admin func(baseURL string) (*matrix.Admin, error)
}

// Opener is Open as the admin.Opener the child serves through (OP6).
func (cfg Config) Opener() admin.Opener {
	return func(ctx context.Context, team paths.TeamID, port int, regKey string) (admin.Opened, error) {
		return Open(ctx, cfg, team, port, regKey)
	}
}

func (cfg Config) withDefaults() Config {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	if cfg.Admin == nil {
		cfg.Admin = func(base string) (*matrix.Admin, error) { return matrix.NewAdmin(matrix.Config{BaseURL: base}) }
	}
	return cfg
}

// Open is one team as the child reaches it (OP1): the admin client on 127.0.0.1:port with the
// team's admin token, apply's paths under ctl/<team>/, and a member-client maker. With a
// registration key (team apply) it runs bootstrap step 1 first (OP3); without one, the token
// must be there (OP2). A refusal is an *apply.Error of team apply's admin step with Drift set
// (exit 70); any other failure is the files' or the homeserver's.
func Open(ctx context.Context, cfg Config, team paths.TeamID, port int, regKey string) (_ admin.Opened, err error) {
	cfg = cfg.withDefaults()
	base := "http://" + loopback + ":" + strconv.Itoa(port)
	anon, err := cfg.Admin(base)
	if err != nil {
		return admin.Opened{}, fmt.Errorf("the admin client: %w", err)
	}
	d, err := openDir(cfg.Root + paths.CtlState(team))
	if err != nil {
		return admin.Opened{}, err
	}
	defer func() { err = errors.Join(err, d.close()) }()
	if err = d.sweep(passwordName, tokenName); err != nil {
		return admin.Opened{}, err
	}
	b := bootstrap{dir: d, anon: anon, team: team, rand: cfg.Rand}
	token, err := b.token(ctx, regKey)
	if err != nil {
		return admin.Opened{}, err
	}
	client, err := anon.WithToken(token)
	if err != nil {
		return admin.Opened{}, fmt.Errorf("%s: %w", d.at(tokenName), err)
	}
	return admin.Opened{
		Deps: apply.Deps{
			Admin:      client,
			RosterPath: cfg.Root + paths.RosterFile(team),
			RoomsPath:  cfg.Root + paths.RoomsFile(team),
			GuestDir:   cfg.Root + paths.GuestTokenDir(team),
			Now:        cfg.Now,
		},
		Member: func(token string) (*matrix.Client, error) {
			return matrix.NewMember(matrix.Config{BaseURL: base, PlainHTTPHosts: []string{loopback}, Token: token})
		},
	}, nil
}

// refused is a refusal of bootstrap step 1 (exit 70).
func refused(format string, args ...any) error {
	return &apply.Error{Op: apply.OpTeamApply, Step: StepAdmin, Drift: true, Err: fmt.Errorf(format, args...)}
}

// bootstrap is step 1 over one team's directory and an admin client with no token.
type bootstrap struct {
	dir  *dir
	anon *matrix.Admin
	team paths.TeamID
	rand io.Reader
}

func (b bootstrap) adminID() string { return "@" + adminLocalpart + ":" + paths.ServerName(b.team) }

// token is the team's admin token: the stored one, checked by whoami when regKey is given, or
// with regKey and none stored, a fresh one from the account (OP2, OP3).
func (b bootstrap) token(ctx context.Context, regKey string) (string, error) {
	data, found, err := b.dir.read(tokenName, maxTokenBytes)
	if err != nil {
		return "", err
	}
	if found && !isToken(data) {
		return "", fmt.Errorf("%s is not one token of printable ASCII", b.dir.at(tokenName))
	}
	switch {
	case !found && regKey == "":
		return "", fmt.Errorf("team %s has no admin token: run team apply", b.team)
	case regKey == "":
		return string(data), nil
	case found:
		return string(data), b.verify(ctx, string(data))
	}
	return b.signIn(ctx, regKey)
}

// verify checks a stored token is the admin's, and finishes a bootstrap that stored it but
// died before the password was deleted.
func (b bootstrap) verify(ctx context.Context, token string) error {
	client, err := b.anon.WithToken(token)
	if err != nil {
		return fmt.Errorf("%s: %w", b.dir.at(tokenName), err)
	}
	id, err := client.Whoami(ctx)
	if errors.Is(err, matrix.ErrAuthRefused) {
		return refused("the homeserver refuses the stored admin token; the account can only be recovered by restore")
	}
	if err != nil {
		return fmt.Errorf("whoami with the admin token: %w", err)
	}
	if id.UserID != b.adminID() {
		return refused("the stored admin token is %s's, not %s", id.UserID, b.adminID())
	}
	return b.dir.remove(passwordName)
}

// signIn registers the admin account if it is not there yet, logs in on AdminDevice, stores the
// token and deletes the password. A password left by an earlier run is the account's; with
// none, a fresh one is written before the account is registered, so a crash at any point
// leaves what the next run needs (OP3).
func (b bootstrap) signIn(ctx context.Context, regKey string) (string, error) {
	password, found, err := b.password()
	if err != nil {
		return "", err
	}
	if !found {
		if password, err = b.freshPassword(); err != nil {
			return "", err
		}
	}
	reg := matrix.Registration{Username: adminLocalpart, Password: password, Admin: true}
	_, err = b.anon.Register(ctx, regKey, reg)
	inUse := isUserInUse(err)
	switch {
	case inUse && !found:
		return "", errors.Join(
			refused("%s exists but its token is lost; the account can only be recovered by restore", b.adminID()),
			b.dir.remove(passwordName))
	case err != nil && !inUse:
		return "", fmt.Errorf("registering %s: %w", b.adminID(), err)
	}
	sess, err := b.anon.LoginDevice(ctx, adminLocalpart, password, AdminDevice)
	if errors.Is(err, matrix.ErrForbidden) {
		return "", refused("%s does not take the stored admin password; the account can only be recovered by restore", b.adminID())
	}
	if err != nil {
		return "", fmt.Errorf("signing in as %s: %w", b.adminID(), err)
	}
	if sess.UserID != b.adminID() {
		return "", refused("signing in as %s answered %s", b.adminID(), sess.UserID)
	}
	token := sess.AccessToken.Reveal()
	if err := b.dir.create(tokenName, []byte(token), b.rand); err != nil {
		return "", err
	}
	return token, b.dir.remove(passwordName)
}

// password is the stored admin password, if there is one.
func (b bootstrap) password() (string, bool, error) {
	data, found, err := b.dir.read(passwordName, 2*passwordBytes)
	if err != nil || !found {
		return "", false, err
	}
	if !isPassword(data) {
		return "", false, fmt.Errorf("%s is not %d lowercase hex digits", b.dir.at(passwordName), 2*passwordBytes)
	}
	return string(data), true, nil
}

func (b bootstrap) freshPassword() (string, error) {
	raw := make([]byte, passwordBytes)
	if _, err := io.ReadFull(b.rand, raw); err != nil {
		return "", fmt.Errorf("drawing the admin password: %w", err)
	}
	password := hex.EncodeToString(raw)
	return password, b.dir.create(passwordName, []byte(password), b.rand)
}

func isUserInUse(err error) bool {
	e, ok := errors.AsType[*matrix.Error](err)
	return ok && e.Errcode == "M_USER_IN_USE"
}

// isToken is OP2's grammar: 1 to maxTokenBytes bytes of printable ASCII without a space.
func isToken(data []byte) bool {
	if len(data) == 0 || len(data) > maxTokenBytes {
		return false
	}
	for _, c := range data {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

func isPassword(data []byte) bool {
	if len(data) != 2*passwordBytes {
		return false
	}
	for _, c := range data {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// dir is ctl/<team>/ held open by fd; every file is reached from it.
type dir struct {
	fd   int
	path string
}

// openDir opens path, which must be a directory the child's euid owns, mode 0700, not a link.
func openDir(path string) (*dir, error) {
	fd, err := unix.Open(path, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("the team's control directory %s: %w", path, err)
	}
	d := &dir{fd: fd, path: path}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, errors.Join(fmt.Errorf("%s: %w", path, err), d.close())
	}
	if int64(st.Uid) != int64(geteuid()) || st.Mode&0o7777 != dirMode {
		return nil, errors.Join(fmt.Errorf("%s is not a directory of uid %d with mode 0700", path, geteuid()), d.close())
	}
	return d, nil
}

func (d *dir) close() error {
	if err := unix.Close(d.fd); err != nil {
		return fmt.Errorf("closing %s: %w", d.path, err)
	}
	return nil
}

func (d *dir) at(name string) string { return filepath.Join(d.path, name) }

// read is name's content; found is false when there is none. A link, a file that is not
// regular, and one over limit bytes are refused.
func (d *dir) read(name string, limit int) ([]byte, bool, error) {
	fd, err := unix.Openat(d.fd, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cannot open %s: %w", d.at(name), err)
	}
	f := os.NewFile(uintptr(fd), d.at(name))
	data, err := readRegular(f, limit)
	if err = errors.Join(err, f.Close()); err != nil {
		return nil, false, fmt.Errorf("%s: %w", d.at(name), err)
	}
	return data, true, nil
}

func readRegular(f *os.File, limit int) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("over %d bytes", limit)
	}
	return data, nil
}

// tempName is a fresh temporary name beside name.
func tempName(name string, rnd io.Reader) (string, error) {
	raw := make([]byte, tempBytes)
	if _, err := io.ReadFull(rnd, raw); err != nil {
		return "", fmt.Errorf("drawing a temporary name for %s: %w", name, err)
	}
	return "." + name + "." + hex.EncodeToString(raw) + ".tmp", nil
}

// create writes name, 0600, linked from a synced temporary file: an existing name fails and
// is never replaced (OP4).
func (d *dir) create(name string, data []byte, rnd io.Reader) error {
	temp, err := tempName(name, rnd)
	if err != nil {
		return err
	}
	fd, err := unix.Openat(d.fd, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, fileMode)
	if err != nil {
		return fmt.Errorf("cannot create a temporary file for %s: %w", d.at(name), err)
	}
	f := os.NewFile(uintptr(fd), d.at(temp))
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err == nil {
		err = unix.Linkat(d.fd, temp, d.fd, name, 0)
	}
	if err = errors.Join(err, unix.Unlinkat(d.fd, temp, 0)); err != nil {
		return fmt.Errorf("cannot put %s in place: %w", d.at(name), err)
	}
	return d.sync()
}

// isTempOf is a temporary name tempName made for name: `.<name>.<anything>.tmp`.
func isTempOf(entry, name string) bool {
	rest, ok := strings.CutPrefix(entry, "."+name+".")
	if !ok {
		return false
	}
	middle, ok := strings.CutSuffix(rest, ".tmp")
	return ok && middle != ""
}

// sweep removes every temporary of names a crash left between create's open and its unlink,
// so no copy of a secret outlives the run that wrote it (OP4). The directory is listed through
// the held fd; the team lock keeps any other writer out.
func (d *dir) sweep(names ...string) error {
	fd, err := unix.Openat(d.fd, ".", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("cannot list %s: %w", d.path, err)
	}
	f := os.NewFile(uintptr(fd), d.path)
	entries, err := f.Readdirnames(-1)
	if err = errors.Join(err, f.Close()); err != nil {
		return fmt.Errorf("cannot list %s: %w", d.path, err)
	}
	for _, entry := range entries {
		if !slices.ContainsFunc(names, func(name string) bool { return isTempOf(entry, name) }) {
			continue
		}
		if err := d.remove(entry); err != nil {
			return err
		}
	}
	return nil
}

// remove deletes name if it is there, and syncs the directory.
func (d *dir) remove(name string) error {
	err := unix.Unlinkat(d.fd, name, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot delete %s: %w", d.at(name), err)
	}
	return d.sync()
}

func (d *dir) sync() error {
	if err := unix.Fsync(d.fd); err != nil {
		return fmt.Errorf("cannot sync %s: %w", d.path, err)
	}
	return nil
}
