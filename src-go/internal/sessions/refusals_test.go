package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// checkHost is a host whose restore is on, with its records to be judged by restore --check.
func checkHost(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	restoreHost(h)
	restoreOn(h, true)
	return h
}

// ccyRecord records a ccy session in dir launched with args.
func ccyRecord(t *testing.T, h *harness, name, dir string, args ...string) {
	t.Helper()
	record(t, h, registry.Record{Name: name, Dir: dir, Launcher: "/launch/ccy", Prefix: "ccy", Restore: true, Args: args})
}

// ccRecord records a cc session in dir.
func ccRecord(t *testing.T, h *harness, name, dir string) {
	t.Helper()
	record(t, h, registry.Record{Name: name, Dir: dir, Launcher: "/launch/cc", Prefix: "cc", Restore: true, Args: []string{"--bus"}})
}

// checkSays runs restore --check and holds its exit code and stdout.
func checkSays(t *testing.T, h *harness, code term.ExitCode, want string) {
	t.Helper()
	exitCodeIs(t, h.run("restore", "--check"), code, h)
	if h.stdout() != want {
		t.Fatalf("stdout %q, want %q\nstderr: %s", h.stdout(), want, h.stderr())
	}
}

// fedora-desktop#284 item 1: a recorded --ssh-key that is no longer a file stops the launch's
// argument parser ("SSH key not found"), so restore --check calls it UNRESTORABLE
// ssh-key-missing. A relative key is looked for in the session's folder, where its launch runs.
// The restore itself still starts it.
func TestRestoreCheckRefusesARecordedSSHKeyThatIsGone(t *testing.T) {
	h := checkHost(t)
	dir := realDir(t, h, "p")
	key := h.write("keys/id_here", "k")
	h.write("p/keys/id_rel", "k")
	realDir(t, h, "keys/a-directory")
	ccyRecord(t, h, "ccy-abs", dir, "--ssh-key", key)
	ccyRecord(t, h, "ccy-dir", dir, "--ssh-key", filepath.Join(h.root, "keys/a-directory"))
	ccyRecord(t, h, "ccy-gone", dir, "--ssh-key", key, "--ssh-key", filepath.Join(h.root, "keys/id_gone"))
	ccyRecord(t, h, "ccy-rel", dir, "--ssh-key", "keys/id_rel")
	ccyRecord(t, h, "ccy-after", dir, "--no-ssh", "--", "--ssh-key", "/nowhere")
	checkSays(t, h, 1, "ccy-abs RESTORABLE\nccy-after RESTORABLE\nccy-dir UNRESTORABLE ssh-key-missing\n"+
		"ccy-gone UNRESTORABLE ssh-key-missing\nccy-rel RESTORABLE\n")
	contains(t, "why", h.stderr(), "ERROR: ccy-gone cannot be restored: its --ssh-key "+filepath.Join(h.root, "keys/id_gone")+
		" is not a file, so its launch would stop at once.")

	h.errOut.Reset()
	exitCodeIs(t, h.run("restore"), 0, h)
	if len(startedSessions(h)) != 5 {
		t.Fatalf("the restore starts every record: %v", h.ran())
	}
}

// fedora-desktop#284 item 2: a recorded --network the engine does not list stops the launch
// ("Network not found"), so restore --check asks the record's engine (its --engine, else
// CCY_CONTAINER_ENGINE, else podman) for its networks, read-only, as the launcher does, and
// calls it UNRESTORABLE network-missing. A list that cannot be read is network-check-failed.
func TestRestoreCheckRefusesARecordedNetworkThatIsGone(t *testing.T) {
	h := checkHost(t)
	dir := realDir(t, h, "p")
	h.fake.On([]string{"podman", "network", "ls", "--format", "{{.Name}}"}, execx.Result{Stdout: []byte("podman\nproj_default\n")})
	h.fake.On([]string{"docker", "network", "ls", "--format", "{{.Name}}"}, execx.Result{ExitCode: 1, Stderr: []byte("cannot reach the daemon")})
	ccyRecord(t, h, "ccy-docker", dir, "--engine", "docker", "--network", "proj_default")
	ccyRecord(t, h, "ccy-gone", dir, "--network", "proj")
	ccyRecord(t, h, "ccy-here", dir, "--network", "proj_default")
	ccyRecord(t, h, "ccy-nonet", dir, "--no-network")
	checkSays(t, h, 1, "ccy-docker UNRESTORABLE network-check-failed\nccy-gone UNRESTORABLE network-missing\n"+
		"ccy-here RESTORABLE\nccy-nonet RESTORABLE\n")
	contains(t, "why", h.stderr(),
		"ERROR: ccy-gone cannot be restored: its --network proj is not a podman network, so its launch would stop at once.",
		"ERROR: ccy-docker cannot be restored: the docker networks could not be listed to check its --network proj_default (",
		"cannot reach the daemon")
	if n := h.count("podman network ls"); n != 2 {
		t.Fatalf("the engine is asked once per record with a --network (%d): %v", n, h.ran())
	}

	h = checkHost(t)
	dir = realDir(t, h, "p")
	h.env["CCY_CONTAINER_ENGINE"] = "docker"
	h.fake.On([]string{"docker", "network", "ls", "--format", "{{.Name}}"}, execx.Result{Stdout: []byte("bridge\nnet\n")})
	ccyRecord(t, h, "ccy-env", dir, "--network", "net")
	ccyRecord(t, h, "ccy-junk", dir, "--engine", "lxc", "--network", "net")
	checkSays(t, h, 1, "ccy-env RESTORABLE\nccy-junk UNRESTORABLE network-check-failed\n")

	h = checkHost(t)
	dir = realDir(t, h, "p")
	h.app.LookPath = func(string) (string, error) { return "", errors.New("not on PATH") }
	ccyRecord(t, h, "ccy-noengine", dir, "--network", "net")
	checkSays(t, h, 1, "ccy-noengine UNRESTORABLE network-check-failed\n")
}

// fedora-desktop#284 item 4: allowed-hostnames that rules this host out (never, or no pattern
// matching it) stops the launch (check_allowed_hostname), so restore --check calls it
// UNRESTORABLE host-not-allowed; a file that cannot be read is host-check-failed. cc does not
// read the rule.
func TestRestoreCheckRefusesAProjectThatRulesThisHostOut(t *testing.T) {
	h := checkHost(t)
	rules := map[string]string{"never": "never\n", "other": "other-host\n", "mine": "# this host\nbo?\n", "loop": ""}
	for name, content := range rules {
		dir := realDir(t, h, name)
		if name == "loop" {
			path := filepath.Join(dir, ".claude/ccy/allowed-hostnames")
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path, path); err != nil {
				t.Fatal(err)
			}
		} else {
			h.write(name+"/.claude/ccy/allowed-hostnames", content)
		}
		ccyRecord(t, h, "ccy-"+name, dir)
	}
	ccRecord(t, h, "cc-never", filepath.Join(h.root, "never"))
	checkSays(t, h, 1, "cc-never RESTORABLE\nccy-loop UNRESTORABLE host-check-failed\nccy-mine RESTORABLE\n"+
		"ccy-never UNRESTORABLE host-not-allowed\nccy-other UNRESTORABLE host-not-allowed\n")
	contains(t, "why", h.stderr(),
		"ERROR: ccy-never cannot be restored: "+filepath.Join(h.root, "never")+"/.claude/ccy/allowed-hostnames says never, so ccy will not run there on any host.",
		"ERROR: ccy-other cannot be restored: "+filepath.Join(h.root, "other")+"/.claude/ccy/allowed-hostnames allows other-host, not this host (box).",
		"ERROR: ccy-loop cannot be restored: whether ccy may run on this host could not be read: ")

	h = checkHost(t)
	h.app.Hostname = func() (string, error) { return "", errors.New("no uname") }
	ccyRecord(t, h, "ccy-mine", realDir(t, h, "mine"))
	h.write("mine/.claude/ccy/allowed-hostnames", "box\n")
	checkSays(t, h, 1, "ccy-mine UNRESTORABLE host-check-failed\n")
}

// passphraseCheck is a host with restore on and a ccy and a cc record, for the passphrase file.
func passphraseCheck(t *testing.T) *harness {
	t.Helper()
	h := checkHost(t)
	dir := realDir(t, h, "p")
	ccyRecord(t, h, "ccy-p", dir)
	ccRecord(t, h, "cc-p", dir)
	return h
}

// dropIn writes the restore unit's ssh-unlock drop-in naming value.
func dropIn(h *harness, value string) {
	h.write("home/.config/systemd/user/ccy-sessions-restore.service.d/ssh-unlock.conf",
		"# deployed by play-claude-yolo.yml\n[Service]\nEnvironment="+unlockFileEnv+"="+value+"\n")
}

// fedora-desktop#284 item 7: the restore starts nothing when the SSH key passphrase file the
// unit gives it cannot be used, so restore --check makes the same check of the same file (the
// variable when it is set, as under the unit, else the file the unit's drop-in names, %h being
// HOME) and calls every record UNRESTORABLE passphrase-file-unusable. With no drop-in there is
// no file to check.
func TestRestoreCheckMakesTheRestoresPassphraseCheck(t *testing.T) {
	h := passphraseCheck(t)
	checkSays(t, h, 0, "cc-p RESTORABLE\nccy-p RESTORABLE\n")

	h = passphraseCheck(t)
	dropIn(h, "%h/.claude-tokens/ccy/restore-ssh-passphrase")
	checkSays(t, h, 1, "cc-p UNRESTORABLE passphrase-file-unusable\nccy-p UNRESTORABLE passphrase-file-unusable\n")
	contains(t, "why", h.stderr(), "ERROR: ccy-p cannot be restored: the SSH key passphrase file for session restore is missing: "+
		filepath.Join(h.env["HOME"], ".claude-tokens/ccy/restore-ssh-passphrase")+". "+unlockFileFix)

	h = passphraseCheck(t)
	dropIn(h, "%h/pp")
	h.write("home/pp", "fixture phrase")
	checkSays(t, h, 0, "cc-p RESTORABLE\nccy-p RESTORABLE\n")
	if strings.Contains(h.stderr(), "fixture phrase") || strings.Contains(h.stdout(), "fixture phrase") {
		t.Fatal("the passphrase is never printed")
	}

	h = passphraseCheck(t)
	dropIn(h, "%h/pp")
	chmod(t, h.write("home/pp", "fixture phrase"), 0o644)
	h.env[unlockFileEnv] = h.write("good-pp", "fixture phrase")
	checkSays(t, h, 0, "cc-p RESTORABLE\nccy-p RESTORABLE\n")

	h = passphraseCheck(t)
	dropIn(h, "/pp/%u")
	checkSays(t, h, 1, "cc-p UNRESTORABLE passphrase-file-unusable\nccy-p UNRESTORABLE passphrase-file-unusable\n")
	contains(t, "specifier", h.stderr(), "names its passphrase file with a systemd specifier other than %h")
}

// fedora-desktop#284 item 9: cc refuses a token pool whose every token is past its stamp ("cc
// will not pick for you"), so restore --check calls a cc record UNRESTORABLE token-pool-expired
// then; a pool with a valid token, an empty one (cc uses the Desktop account) and no pool are
// restorable. A ccy record is not judged by the pool.
func TestRestoreCheckRefusesCcWhenItsWholeTokenPoolHasExpired(t *testing.T) {
	pool := "home/.claude-tokens/ccy/tokens/"
	h := checkHost(t)
	dir := realDir(t, h, "p")
	ccRecord(t, h, "cc-p", dir)
	ccyRecord(t, h, "ccy-p", dir)
	checkSays(t, h, 0, "cc-p RESTORABLE\nccy-p RESTORABLE\n")

	realDir(t, h, pool)
	h.out.Reset()
	checkSays(t, h, 0, "cc-p RESTORABLE\nccy-p RESTORABLE\n")

	h.write(pool+"old.1969-01-01.token", "x")
	h.write(pool+"undated.token", "x")
	h.out.Reset()
	checkSays(t, h, 1, "cc-p UNRESTORABLE token-pool-expired\nccy-p RESTORABLE\n")
	contains(t, "why", h.stderr(), "ERROR: cc-p cannot be restored: every token in "+filepath.Join(h.root, pool)+
		" is past its expiry stamp (old.1969-01-01.token, undated.token), and cc will not pick one for you. Mint a fresh one: ccy --create-token")

	h.write(pool+"new.2099-01-01.token", "x")
	h.out.Reset()
	checkSays(t, h, 0, "cc-p RESTORABLE\nccy-p RESTORABLE\n")

	h = checkHost(t)
	h.write("home/.claude-tokens/ccy/tokens", "not a directory")
	ccRecord(t, h, "cc-p", realDir(t, h, "p"))
	checkSays(t, h, 1, "cc-p UNRESTORABLE token-check-failed\n")
}
