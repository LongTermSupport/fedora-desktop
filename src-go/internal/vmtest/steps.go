package vmtest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const (
	guestRunState     = "/var/lib/vmtest"
	guestUserEmail    = "vmtest@example.com" // RFC 2606 reserved: the guest's git identity is a placeholder
	provisionLimit    = 5400 * time.Second
	prepareLimit      = 1800 * time.Second
	sessionPoll       = 15 * time.Second
	sessionReport     = 300 * time.Second
	rebootSettle      = 10 * time.Second
	rebootLimit       = 180 * time.Second
	sessionSettle     = 15 * time.Second
	prepareDoneMarker = "VMTEST-GUEST-PREPARE-DONE"
	prepareErrPrefix  = "ERROR: prepare: "
	rebootCommand     = "sudo systemd-run --quiet --on-active=2 systemctl reboot"
	bootIDCommand     = "cat /proc/sys/kernel/random/boot_id"
)

var seat0RE = regexp.MustCompile(`(?m)^\s*[0-9]+\s+[0-9]+\s+` + cloudUser + `\s+seat0`)

// running is one `vmtest run` in progress: what its steps share and its teardown undoes.
type running struct {
	c          *cli
	l          *lab
	s          scenarioFacts
	b          baseFacts
	g          *guest
	id         string // the run id
	dir        string // runs/<run-id>
	baseDir    string
	commit     string
	cr         credentials
	verdict    verdict // the freshness verdict the run started under
	startedAt  int64
	transcript *os.File
	follower   *follower
	// ok is a passing verdict: only then does the teardown remove the overlay.
	ok bool
}

// note writes a harness line to the transcript and to stderr.
func (r *running) note(text string) {
	r.c.p.Print(io.MultiWriter(r.transcript, r.c.env.Stderr), text)
}

// tee is where a guest command's output goes: the transcript and stderr, and extra if given.
func (r *running) tee(extra ...io.Writer) io.Writer {
	return io.MultiWriter(append([]io.Writer{r.transcript, r.c.env.Stderr}, extra...)...)
}

// githubEnv is the run.bash identity: none, or for a host-only run the throwaway account
// and the guest paths of its delivered secrets (E36).
func (r *running) githubEnv() []envPair {
	if !r.s.HostOnly {
		return []envPair{{envAccounts, displayNone}}
	}
	return []envPair{
		{envAccounts, r.cr.account},
		{"RUN_BASH_GITHUB_TOKEN_FILE", guestDropDir + "/gh-token"},
		{"RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE", guestDropDir + "/ssh-passphrase"},
	}
}

// overrides is the scenario's run_env as ` KEY='value'` words, for the log and the ssh line.
func (r *running) overrides() string {
	var b strings.Builder
	for _, p := range r.s.RunEnv {
		b.WriteString(" " + p.Key + "='" + p.Value + "'")
	}
	return b.String()
}

// provision runs run.bash at the pinned commit and returns its status (E36, E37).
func (r *running) provision(ctx context.Context) (int, error) {
	if r.b.Profile == profileDesktop {
		return r.sessionProvision(ctx)
	}
	var env strings.Builder
	env.WriteString("env RUN_BASH_HEADLESS=1 RUN_BASH_USER_EMAIL='" + guestUserEmail + "'")
	for _, p := range r.githubEnv() {
		v := p.Value
		if p.Key == envAccounts && r.s.HostOnly {
			v = "'" + v + "'"
		}
		env.WriteString(" " + p.Key + "=" + v)
	}
	env.WriteString(" RUN_BASH_VAULT_PASSWORD_FILE=$HOME/.vmtest/throwaway-pw RUN_BASH_GIT_REF='" + r.commit + "'")
	env.WriteString(" RUN_BASH_PROVISIONING_PROFILE='" + r.b.Profile + "'" + r.overrides() + " TERM=dumb bash ~/run.bash")
	return r.g.stream(ctx, provisionLimit, env.String(), r.tee())
}

// sessionProvision runs run.bash INSIDE a desktop guest's autologin session (§5.3 item 3): the
// session runner gets its run.env over SSH, the host waits for run.exit, then pulls the run
// log into the transcript (E37). It fails only when the guest cannot be driven at all.
func (r *running) sessionProvision(ctx context.Context) (int, error) {
	r.c.logf("handing the run to the session runner (waits for %s/run.exit, up to %ds)", guestRunState, int(provisionLimit.Seconds()))
	if err := r.g.doWith(ctx, "umask 077 && cat >"+guestRunState+"/run.env.tmp && mv -f "+guestRunState+"/run.env.tmp "+guestRunState+"/run.env",
		strings.NewReader(strings.Join(r.sessionEnv(), "\n")+"\n")); err != nil {
		return 0, err
	}
	finished, err := r.waitRunExit(ctx)
	if err != nil || !finished {
		return timedOut, err
	}
	out, err := r.g.output(ctx, "cat "+guestRunState+"/run.log")
	if err != nil {
		return 0, err
	}
	r.c.p.Print(r.tee(), out)
	raw, err := r.g.output(ctx, "cat "+guestRunState+"/run.exit")
	if err != nil {
		return 0, err
	}
	rc, convErr := strconv.Atoi(strings.TrimSpace(raw))
	if convErr != nil || rc < 0 {
		return 0, failf("the session runner wrote a malformed run.exit: %s", strings.TrimSpace(raw))
	}
	return rc, nil
}

// sessionEnv is the run.env the session runner reads: KEY=value lines, no quoting.
func (r *running) sessionEnv() []string {
	ids := r.githubEnv()
	lines := make([]string, 0, 6+len(ids)+len(r.s.RunEnv))
	lines = append(lines, "RUN_BASH_HEADLESS=1", "RUN_BASH_USER_EMAIL="+guestUserEmail)
	for _, p := range ids {
		lines = append(lines, p.Key+"="+p.Value)
	}
	lines = append(lines, "RUN_BASH_VAULT_PASSWORD_FILE=/home/"+cloudUser+"/.vmtest/throwaway-pw",
		"RUN_BASH_GIT_REF="+r.commit, "RUN_BASH_PROVISIONING_PROFILE="+r.b.Profile, "TERM=dumb")
	for _, p := range r.s.RunEnv {
		lines = append(lines, p.Key+"="+p.Value)
	}
	return lines
}

// waitRunExit waits for the session runner's run.exit, reporting progress; false is the
// provisioning limit passed, with what the run log holds copied to the transcript.
func (r *running) waitRunExit(ctx context.Context) (bool, error) {
	start, report := r.c.env.Now(), sessionReport
	for {
		if err := r.g.mustBeUp(ctx, "run.bash in the session"); err != nil {
			return false, err
		}
		if r.g.succeeds(ctx, "test -r "+guestRunState+"/run.exit") {
			return true, nil
		}
		if err := r.c.env.Sleep(ctx, sessionPoll); err != nil {
			return false, ctxErr(ctx, err)
		}
		elapsed := r.c.env.Now().Sub(start)
		if elapsed >= provisionLimit {
			out, err := r.g.output(ctx, "cat "+guestRunState+"/run.log")
			r.c.p.Print(r.transcript, out)
			if err != nil {
				r.c.logf("could not read the run log from the guest")
			}
			r.note(fmt.Sprintf("ERROR: the session runner did not finish run.bash within %ds\n", int(provisionLimit.Seconds())))
			return false, nil
		}
		if elapsed >= report {
			r.c.logf("still provisioning in the session after %ds", int(report.Seconds()))
			report += sessionReport
		}
	}
}

// prepare runs the scenario's own fixture in the guest, between run.bash and the reboot
// (E40). A scenario with nothing to set up ships no script, and that is said out loud.
func (r *running) prepare(ctx context.Context) error {
	name := "guest-prepare-" + r.s.ID + ".bash"
	script := r.l.at(name)
	if !readable(script) {
		r.note(fmt.Sprintf("# harness: no %s under %s; this scenario prepares nothing\n", name, r.l.home))
		return nil
	}
	r.note(fmt.Sprintf("# harness: running the %s fixture in the guest, before the reboot\n", r.s.ID))
	if err := r.g.scp(ctx, script, "/tmp/guest-prepare.bash"); err != nil {
		return err
	}
	var own bytes.Buffer
	rc, err := r.g.stream(ctx, prepareLimit, "env TERM=dumb bash /tmp/guest-prepare.bash", r.tee(&own))
	if err != nil {
		return err
	}
	if rc != 0 {
		return failf("the %s fixture exited %d (stage: prepare): %s; nothing after it would be judging the scenario it names", r.s.ID, rc, prepareFailureReason(own.String()))
	}
	// Exit 0 is not "ran to the end": a fixture that returned early exits 0 having prepared
	// part of the scenario. The marker is its final line, read from its own output (B8).
	if !hasLine(own.String(), prepareDoneMarker) {
		return failf("the %s fixture exited 0 without printing %s (stage: prepare); it did not reach its last step", r.s.ID, prepareDoneMarker)
	}
	return nil
}

// prepareFailureReason is the fixture's own last `ERROR: prepare:` line, for the verdict: the
// transcript stays on the host, so a sandboxed caller would learn only "exited 1".
func prepareFailureReason(out string) string {
	reason := ""
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, prepareErrPrefix) {
			reason = line
		}
	}
	if reason == "" {
		return "the fixture printed no ERROR: prepare: line"
	}
	return reason
}

// reboot brings the guest through a reboot by its profile's mechanics (E41). Whether to reboot
// is the scenario's answer; an unknown profile refuses, because not rebooting would judge the
// wrong boot and look exactly like judging the right one.
func (r *running) reboot(ctx context.Context) error {
	switch r.b.Profile {
	case profileDesktop:
		return r.desktopReboot(ctx)
	case profileServer:
		return r.serverReboot(ctx)
	}
	return failf("no reboot mechanics for profile %s (stage: boot)", r.b.Profile)
}

func (r *running) bootID(ctx context.Context) (string, error) {
	out, err := r.g.output(ctx, bootIDCommand)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(out)
	if id == "" {
		return "", failf("the guest gave no boot id (stage: boot)")
	}
	return id, nil
}

// serverReboot requires the boot id to change: SSH answering proves only that SSH answers,
// which it also does when the reboot never happened.
func (r *running) serverReboot(ctx context.Context) error {
	before, err := r.bootID(ctx)
	if err != nil {
		return err
	}
	r.note("# harness: rebooting the guest; the checks judge the boot AFTER this one\n")
	if err := r.g.do(ctx, rebootCommand); err != nil {
		return err
	}
	if err := r.c.env.Sleep(ctx, rebootSettle); err != nil {
		return ctxErr(ctx, err)
	}
	if err := r.g.waitSSH(ctx); err != nil {
		return err
	}
	start := r.c.env.Now()
	for {
		after, err := r.bootID(ctx)
		if err != nil {
			return err
		}
		if after != before {
			r.note(fmt.Sprintf("# harness: boot id %s -> %s\n", before, after))
			return nil
		}
		if err := r.c.env.Sleep(ctx, poll); err != nil {
			return ctxErr(ctx, err)
		}
		if r.c.env.Now().Sub(start) >= rebootLimit {
			return failf("the guest still reports boot id %s %ds after the reboot request; it never rebooted (stage: boot)", before, int(rebootLimit.Seconds()))
		}
	}
}

// desktopReboot is the reboot a real user does when run.bash finishes: reboot, answer the LUKS
// prompt again, wait for SSH and then for the autologin session on seat0, so the checks judge
// the session GNOME starts AFTER provisioning.
func (r *running) desktopReboot(ctx context.Context) error {
	r.note("# harness: rebooting the guest after run.bash, as run.bash asks; checks run in the new session\n")
	if err := r.g.do(ctx, rebootCommand); err != nil {
		return err
	}
	if err := r.c.env.Sleep(ctx, rebootSettle); err != nil {
		return ctxErr(ctx, err)
	}
	r.follower.stop()
	r.follower = nil
	f, err := r.g.unlock(ctx, r.dir, filepath.Join(r.baseDir, luksFile))
	if err != nil {
		return err
	}
	r.follower = f
	if err := r.g.waitSSH(ctx); err != nil {
		return err
	}
	r.c.logf("waiting for the autologin session on seat0 (up to %ds)", int(rebootLimit.Seconds()))
	start := r.c.env.Now()
	for {
		if err := r.g.mustBeUp(ctx, "the autologin session"); err != nil {
			return err
		}
		if out, err := r.g.merged(ctx, "loginctl list-sessions --no-legend"); err == nil && seat0RE.MatchString(out) {
			break
		}
		if err := r.c.env.Sleep(ctx, poll); err != nil {
			return ctxErr(ctx, err)
		}
		if r.c.env.Now().Sub(start) >= rebootLimit {
			return failf("no autologin session on seat0 within %ds after the reboot (stage: boot)", int(rebootLimit.Seconds()))
		}
	}
	// gnome-shell needs a moment after the session appears before its D-Bus interface answers.
	return ctxErr(ctx, r.c.env.Sleep(ctx, sessionSettle))
}

// envName is the name of an `env` output line: everything before its first '='.
func envName(line string) string {
	return strings.Split(line, "=")[0]
}

// evidence collects a desktop's evidence-only items (E43): the session environment the runner
// inherited against a transient user unit's, and a screenshot. A missing item is logged.
func (r *running) evidence(ctx context.Context) {
	session := r.capture(ctx, "cat "+guestRunState+"/session.env", "session.env", "no session.env in the guest")
	unit := r.capture(ctx, "systemd-run --user --pipe --quiet --wait env | sort", "transient-unit.env", "could not probe the transient-unit environment")
	if session != "" && unit != "" {
		inUnit := map[string]bool{}
		for line := range strings.SplitSeq(strings.TrimRight(unit, "\n"), "\n") {
			inUnit[envName(line)] = true
		}
		var only []string
		for line := range strings.SplitSeq(strings.TrimRight(session, "\n"), "\n") {
			name := envName(line)
			if !inUnit[name] && !slices.Contains(only, name) {
				only = append(only, name)
			}
		}
		slices.Sort(only)
		var list strings.Builder
		for _, n := range only {
			list.WriteString(n + ",")
		}
		r.c.p.Print(r.transcript, "VMTEST-EVIDENCE session_only_vars="+list.String()+"\n")
	}
	shot := filepath.Join(r.dir, "screenshot.png")
	if err := r.c.virshShow(ctx, "screenshot", r.g.domain, shot); err != nil {
		r.c.logf("screenshot failed (recorded as absent)")
		return
	}
	r.c.p.Print(r.transcript, "VMTEST-EVIDENCE screenshot=runs/"+r.id+"/screenshot.png (evidence only: proves the compositor produced a frame, not that the frame is correct)\n")
}

// capture runs remote and keeps its output in the run directory; it returns the output, or
// "" (logging why) when there was none.
func (r *running) capture(ctx context.Context, remote, file, absent string) string {
	out, err := r.g.output(ctx, remote)
	if werr := os.WriteFile(filepath.Join(r.dir, file), []byte(out), 0o644); werr != nil {
		r.c.logf("could not save %s: %v", file, werr)
	}
	if err != nil {
		r.c.logf("%s", absent)
		return ""
	}
	return out
}

// succeeds reports whether remote exits 0 in the guest; its output is dropped, as a probe's is.
func (g *guest) succeeds(ctx context.Context, remote string) bool {
	cmd := g.sshCmd(remote)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	_, err := g.c.env.Runner.Run(ctx, cmd)
	return err == nil
}

// merged is remote's stdout and stderr together, as the session probe reads them.
func (g *guest) merged(ctx context.Context, remote string) (string, error) {
	out, err := execx.Combined(ctx, g.c.env.Runner, g.sshCmd(remote))
	return out, ctxErr(ctx, err)
}
