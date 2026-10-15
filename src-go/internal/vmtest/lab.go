package vmtest

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// The lab tree's files, relative to VMTEST_HOME. Every one is deployed by play-vm-test-lab.yml.
const (
	labEnvFile      = "lab.env"
	manifestFile    = "scenarios.json"
	sshKeyFile      = "ssh/lab-key"
	guestCleanup    = "guest-cleanup.bash"
	allowlistFile   = "scenarios.allowlist"
	hostOnlyFile    = "scenarios.host-only"
	runsDir         = "runs"
	imagesDir       = "images"
	basesDir        = "bases"
	membersDir      = "members"
	ksServer        = "ks-vm-server.cfg"
	ksDesktop       = "ks-vm-desktop.cfg"
	sessionRunner   = "vmtest-session-provision.bash"
	statusFile      = "freshness-status.txt"
	retentionLog    = "retention.log"
	scrubLibrary    = "lib/run-log-scrub.bash"
	scrubFront      = "lib/run-log-scrub-cli.bash"
	defaultHelpers  = "/usr/local/lib/ccy-helpers"
	playName        = "play-vm-test-lab.yml"
	fedoraVersionRE = `^[0-9]+$`
)

// The programs and the arguments more than one step spells.
const (
	virshBin          = "virsh"
	virtInstallBin    = "virt-install"
	sshBin            = "ssh"
	qemuImgBin        = "qemu-img"
	formatQcow2       = "qcow2"
	virtioModel       = "virtio"
	displayNone       = "none"
	flagConnect       = "--connect"
	flagCPU           = "--cpu"
	flagGraphics      = "--graphics"
	flagImport        = "--import"
	flagDisk          = "--disk"
	flagName          = "--name"
	flagLog           = "--log"
	flagFedoraVersion = "--fedora-version"
	flagBaseJSON      = "--base-json"
	fieldProfile      = "profile"
	envAccounts       = "RUN_BASH_" + "GITHUB_ACCOUNTS"
)

// paths is where the lab and the helpers are (E6). It needs no lab.env, so the member verbs
// that read nothing else can use it alone.
type paths struct {
	home       string
	helpersLib string
	userHome   string
}

func newPaths(env Env) paths {
	home := env.Getenv("VMTEST_HOME")
	if home == "" {
		home = filepath.Join(env.Home, ".local", "share", "vmtest")
	}
	lib := env.Getenv("VMTEST_HELPERS_LIB")
	if lib == "" {
		lib = defaultHelpers
	}
	return paths{home: home, helpersLib: lib, userHome: env.Home}
}

// at is a path under the lab tree.
func (p paths) at(rel ...string) string {
	return filepath.Join(append([]string{p.home}, rel...)...)
}

// lab is a loaded lab: the paths and what lab.env says (E7).
type lab struct {
	paths

	fedora   string
	repoURL  string
	branch   string
	checkout string
}

var (
	labLineRE = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
	digitsRE  = regexp.MustCompile(fedoraVersionRE)
	// shellish is a value bash would have expanded or split; lab.env holds plain values only.
	shellish = regexp.MustCompile("[\\s$`\"'\\\\;&|<>()]")
)

// parseLabEnv reads lab.env as KEY=VALUE data, never as a script (B1).
func parseLabEnv(path string, body []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(body))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := labLineRE.FindStringSubmatch(line)
		if m == nil || shellish.MatchString(m[2]) {
			return nil, failf("%s:%d: not a KEY=VALUE line of plain text: %s", path, n, line)
		}
		if _, dup := out[m[1]]; dup {
			return nil, failf("%s:%d: %s is set twice", path, n, m[1])
		}
		out[m[1]] = m[2]
	}
	if err := sc.Err(); err != nil {
		return nil, failf("cannot read %s: %v", path, err)
	}
	return out, nil
}

func readable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	return f.Close() == nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// loadLab checks the deployed lab and reads lab.env (E7, E8).
func (c *cli) loadLab() (*lab, error) {
	p := c.paths
	envPath := p.at(labEnvFile)
	body, err := os.ReadFile(envPath)
	if err != nil {
		return nil, failf("no %s; deploy the lab with %s first", envPath, playName)
	}
	vars, err := parseLabEnv(envPath, body)
	if err != nil {
		return nil, err
	}
	l := &lab{
		paths: p, fedora: vars["VMTEST_FEDORA_VERSION"], repoURL: vars["VMTEST_REPO_URL"],
		branch: vars["VMTEST_REPO_BRANCH"], checkout: vars["VMTEST_CHECKOUT"],
	}
	switch {
	case !digitsRE.MatchString(l.fedora):
		return nil, failf("%s does not set VMTEST_FEDORA_VERSION", envPath)
	case !strings.HasPrefix(l.repoURL, "https://"):
		return nil, failf("%s does not set an https VMTEST_REPO_URL", envPath)
	case l.branch == "":
		return nil, failf("%s does not set VMTEST_REPO_BRANCH", envPath)
	case !readable(p.at(manifestFile)):
		return nil, failf("no %s; deploy the lab with %s first", p.at(manifestFile), playName)
	case !readable(p.at(sshKeyFile)) || !readable(p.at(sshKeyFile+".pub")):
		return nil, failf("no lab SSH key at %s; deploy the lab first", p.at(sshKeyFile))
	case !readable(p.at(guestCleanup)):
		return nil, failf("no %s; deploy the lab first", p.at(guestCleanup))
	case !isDir(c.env.GPGKeyDir):
		return nil, failf("%s is missing; distribution-gpg-keys is installed by %s", c.env.GPGKeyDir, playName)
	}
	for _, dir := range []string{imagesDir, basesDir} {
		if err := os.MkdirAll(p.at(dir), 0o755); err != nil {
			return nil, failf("cannot create %s: %v", p.at(dir), err)
		}
	}
	return l, nil
}

// needCheckout is VMTEST_CHECKOUT, which verify and sweep need as an existing directory (E7).
func (l *lab) needCheckout() (string, error) {
	if l.checkout == "" || !isDir(l.checkout) {
		return "", failf("%s does not name an existing VMTEST_CHECKOUT; re-run %s", l.at(labEnvFile), playName)
	}
	return l.checkout, nil
}

// helperCall is one run of a deployed Python helper module.
type helperCall struct {
	module string
	args   []string
	stdin  io.Reader
	// stdout, when set, receives the helper's stdout; otherwise it is captured into the Result.
	stdout io.Writer
	// stderr, when set, receives the helper's stderr; otherwise it is the CLI's.
	stderr io.Writer
}

// helper runs `python3 -P -m helpers.vmtest.<module>` against the deployed library: the one
// seam to the Python (D1). -P keeps the working directory off sys.path, so a checkout the user
// happens to stand in cannot shadow the deployed copies (B2). The helper's stderr is the CLI's.
// A helper that exits non-zero returns its Result with an *execx.ExitError.
func (c *cli) helper(ctx context.Context, h helperCall) (execx.Result, error) {
	stderr := h.stderr
	if stderr == nil {
		stderr = c.env.Stderr
	}
	res, err := c.env.Runner.Run(ctx, execx.Cmd{
		Name:   "python3",
		Args:   append([]string{"-P", "-m", "helpers.vmtest." + h.module}, h.args...),
		Env:    []string{"PYTHONPATH=" + c.paths.helpersLib},
		Stdin:  h.stdin,
		Stdout: h.stdout,
		Stderr: stderr,
	})
	if err != nil {
		if exit, ok := errors.AsType[*execx.ExitError](err); ok {
			return res, exit
		}
		return res, fmt.Errorf("run helpers.vmtest.%s: %w", h.module, err)
	}
	return res, nil
}

// helperExitCode is the helper's exit status, or -1 when it did not run to an exit.
func helperExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exit, ok := errors.AsType[*execx.ExitError](err); ok {
		return exit.Code
	}
	return -1
}

// manifest is the deployed manifest, read for a helper's stdin.
func (l *lab) manifest() ([]byte, error) {
	body, err := os.ReadFile(l.at(manifestFile))
	if err != nil {
		return nil, failf("cannot read %s: %v", l.at(manifestFile), err)
	}
	return body, nil
}

// ctxErr reports a cancelled context as itself, so a signal is never worded as a step failure.
func ctxErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%w", context.Cause(ctx))
	}
	return err
}
