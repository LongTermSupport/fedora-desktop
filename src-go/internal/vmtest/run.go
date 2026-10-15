package vmtest

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// runArgs is the parsed `vmtest run` command line (E2, E28).
type runArgs struct {
	id     string
	commit string
	cr     credentials
}

func parseRunArgs(args []string) (runArgs, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return runArgs{}, usagef("usage: vmtest run <scenario-id> [--commit SHA]")
	}
	r := runArgs{id: args[0]}
	slots := map[string]*string{
		"--commit": &r.commit, "--github-account": &r.cr.account,
		"--github-token-file": &r.cr.tokenFile, "--github-ssh-passphrase-file": &r.cr.ppFile,
	}
	seen := map[string]bool{}
	for rest := args[1:]; len(rest) > 0; rest = rest[2:] {
		slot, ok := slots[rest[0]]
		switch {
		case !ok:
			return runArgs{}, usagef("unknown option: %s", rest[0])
		case seen[rest[0]]:
			return runArgs{}, usagef("%s given twice", rest[0])
		case len(rest) < 2 || rest[1] == "":
			return runArgs{}, usagef("%s needs a value", rest[0])
		}
		seen[rest[0]] = true
		*slot = rest[1]
	}
	return r, nil
}

func (c *cli) cmdRun(ctx context.Context, args []string) error {
	ra, err := parseRunArgs(args)
	if err != nil {
		return err
	}
	l, err := c.loadLab()
	if err != nil {
		return err
	}
	return c.runScenario(ctx, l, ra)
}

// admit is every gate a scenario passes before anything about the run is prepared (E28-E30).
func (c *cli) admit(ctx context.Context, l *lab, ra runArgs) (scenarioFacts, error) {
	s, err := c.scenarioFacts(ctx, l, ra.id)
	if err != nil {
		return scenarioFacts{}, err
	}
	if !s.Runnable {
		return scenarioFacts{}, failf("%s is not runnable: its guest script has not declared a planned check count", ra.id)
	}
	if s.HostOnly {
		return s, c.hostOnlyPreflight(l, ra.id, ra.cr)
	}
	// A credential option on an ordinary scenario is refused, not ignored: the caller believes
	// a secret was delivered, and dropping it would let them think the token path ran.
	if ra.cr.any() {
		return scenarioFacts{}, failf("the --github-* options apply only to a host_only scenario, and %s is not one", ra.id)
	}
	// The deployed allowlist is the authority (§2.3): the manifest alone admits nothing.
	if !listed(l.at(allowlistFile), ra.id) {
		return scenarioFacts{}, failf("%s is not in the deployed allowlist %s; re-run %s on the host", ra.id, l.at(allowlistFile), playName)
	}
	return s, nil
}

// checker is the scenario's own checker when it ships one, else its profile's: resolution
// order, never a fallback on error (E31).
func checker(l *lab, s scenarioFacts, profile string) (string, error) {
	for _, name := range []string{"guest-acceptance-" + s.ID + ".bash", "guest-acceptance-" + profile + ".bash"} {
		if readable(l.at(name)) {
			return l.at(name), nil
		}
	}
	return "", failf("no checker for %s: neither guest-acceptance-%s.bash nor guest-acceptance-%s.bash is readable under %s; deploy the lab first", s.ID, s.ID, profile, l.home)
}

// resolveCommit is the commit to provision from: the one given, or the branch tip on the
// REMOTE, never a local checkout (§3.4, E32).
func (c *cli) resolveCommit(ctx context.Context, l *lab, given string) (string, error) {
	if given != "" {
		if !commitRE.MatchString(given) {
			return "", failf("--commit must be a 40-hex commit, got %s", given)
		}
		return given, nil
	}
	tip, err := c.git.RemoteBranchTip(ctx, l.repoURL, l.branch)
	if err != nil {
		return "", ctxErr(ctx, failf("could not read the tip of %s from %s", l.branch, l.repoURL))
	}
	return tip, nil
}

// rawURL is the raw-file URL for a path at a commit of the public repo.
func rawURL(repoURL, commit, path string) string {
	repo := strings.TrimPrefix(strings.TrimSuffix(repoURL, ".git"), "https://github.com/")
	return "https://raw.githubusercontent.com/" + repo + "/" + commit + "/" + path
}

// divergences is every known difference between this run and a real install of its profile
// (§6.6 rule 09), for judge_run.
func divergences(profile string) string {
	list := "throwaway-vault-password,lab-ssh-key-baked"
	if profile == profileDesktop {
		list += ",luks-unlock-automated,plymouth-disabled,gdm-autologin-enabled-by-harness,session-runner-autostart"
	}
	return list
}

// runScenario is the full lifecycle of one scenario (E28-E47).
func (c *cli) runScenario(ctx context.Context, l *lab, ra runArgs) (err error) {
	s, err := c.admit(ctx, l, ra)
	if err != nil {
		return err
	}
	b, err := c.baseFacts(ctx, l, s.BaseKey)
	if err != nil {
		return err
	}
	baseDir := l.at(basesDir, b.Name)
	if !readable(filepath.Join(baseDir, "base.qcow2")) || !readable(filepath.Join(baseDir, "base.json")) {
		return failf("base %s is absent (stage: base); build it with: vmtest build-base %s", b.Name, s.BaseKey)
	}
	check, err := checker(l, s, b.Profile)
	if err != nil {
		return err
	}
	commit, err := c.resolveCommit(ctx, l, ra.commit)
	if err != nil {
		return err
	}
	p, err := c.probeUpstream(ctx, l)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, p.remove()) }()
	probeRev, err := p.single("VMTEST-FRESHNESS-REVISION")
	if err != nil {
		return err
	}
	v, err := c.freshness(ctx, l, p, baseDir, false)
	if err != nil {
		return err
	}
	c.logf("freshness: %s (degraded=%s)", v.decision, v.degraded)
	if v.refused {
		c.p.Print(c.env.Stderr, v.line+"\n")
		return failf("base %s may not be used (freshness: %s); rebuild it with: vmtest build-base %s", b.Name, v.decision, s.BaseKey)
	}
	if err := c.floor(ctx, l.at(runsDir), v.size, "run", b.RAMMiB); err != nil {
		return ctxErr(ctx, failf("the host cannot carry a run on %s (stage: clone); see the VMTEST-DISK/VMTEST-RAM lines above", b.Name))
	}
	r := &running{c: c, l: l, s: s, b: b, baseDir: baseDir, commit: commit, cr: ra.cr, verdict: v}
	r.id = c.env.Now().UTC().Format("20060102T150405Z") + "-" + s.ID
	r.dir = l.at(runsDir, r.id)
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return failf("cannot create %s: %v", r.dir, err)
	}
	defer r.teardown(context.WithoutCancel(ctx))
	if err := r.boot(ctx); err != nil {
		return err
	}
	if err := r.provisionAndCheck(ctx, check); err != nil {
		return err
	}
	return r.judge(ctx, v, probeRev)
}

// boot creates the overlay and boots the guest until it answers SSH (E34).
func (r *running) boot(ctx context.Context) error {
	c := r.c
	port, err := c.env.FreePort()
	if err != nil {
		return failf("%v", err)
	}
	r.g = &guest{c: c, key: r.l.at(sshKeyFile), domain: "vmtest-run-" + r.id, port: port, dir: r.dir}
	overlay := filepath.Join(r.dir, "overlay.qcow2")
	if err := c.overlay(ctx, filepath.Join(r.baseDir, "base.qcow2"), overlay); err != nil {
		return err
	}
	r.startedAt = c.env.Now().Unix()
	c.logf("run %s: booting %s on %s (SSH forwarded to 127.0.0.1:%d)", r.id, r.g.domain, r.b.Name, port)
	// A desktop needs a display for GNOME (VNC, local only, which `virsh screenshot` reads)
	// and a writable serial port for the LUKS unlock; a server needs neither.
	display := []string{flagGraphics, displayNone}
	if r.b.Profile == profileDesktop {
		display = []string{flagGraphics, "vnc,listen=127.0.0.1", "--video", virtioModel}
	}
	if err := c.bootOverlay(ctx, r.g, r.b, overlay, display, serialSpec(r.b.Profile, r.dir)); err != nil {
		return err
	}
	if r.b.Profile == profileDesktop {
		pp := filepath.Join(r.baseDir, luksFile)
		if !readable(pp) {
			return failf("no %s beside %s; rebuild it (stage: boot)", luksFile, r.b.Name)
		}
		f, err := r.g.unlock(ctx, r.dir, pp)
		if err != nil {
			return err
		}
		r.follower = f
	}
	return r.g.waitSSH(ctx)
}

// provisionAndCheck runs run.bash, the fixture and reboot the scenario asks for, and the
// checker, all into the transcript; then a desktop's evidence and a host-only run's scrub.
func (r *running) provisionAndCheck(ctx context.Context, check string) error {
	c := r.c
	f, err := os.OpenFile(filepath.Join(r.dir, "transcript.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return failf("cannot create the transcript: %v", err)
	}
	r.transcript = f
	c.p.Print(f, "# vmtest run "+r.id+"\n# scenario "+r.s.ID+" on base "+r.b.Name+" ("+r.b.Kind+", "+r.b.Profile+") freshness="+r.decision()+" commit="+r.commit+"\n")
	c.logf("fetching run.bash at %s into the guest", r.commit[:12])
	if err = r.g.do(ctx, "curl -fsSL -o ~/run.bash '"+rawURL(r.l.repoURL, r.commit, "run.bash")+"' && chmod 0755 ~/run.bash"); err != nil {
		return err
	}
	// The guest's only secret: a per-run random vault password that decrypts nothing (§10).
	if err = r.g.do(ctx, "umask 077 && mkdir -p ~/.vmtest && head -c 32 /dev/urandom | base64 > ~/.vmtest/throwaway-pw"); err != nil {
		return err
	}
	if r.s.HostOnly {
		if err = r.g.pushSecrets(ctx, r.cr); err != nil {
			return err
		}
	}
	overrides := ""
	if o := r.overrides(); o != "" {
		overrides = "; overrides:" + o
	}
	c.logf("provisioning (headless run.bash; this is the long part — up to %ds)%s", int(provisionLimit.Seconds()), overrides)
	rc, err := r.provision(ctx)
	if err != nil {
		return err
	}
	r.note("RUN-BASH-EXIT " + strconv.Itoa(rc) + "\n")
	// The fixture and the reboot are the scenario's to ask for, together: what the fixture
	// sets up is what the boot has to pick up (E39).
	if r.s.Reboot {
		if err := r.prepare(ctx); err != nil {
			return err
		}
		if err := r.reboot(ctx); err != nil {
			return err
		}
	}
	if err := r.accept(ctx, check); err != nil {
		return err
	}
	if r.b.Profile == profileDesktop {
		r.evidence(ctx)
	}
	if r.s.HostOnly {
		// BEFORE the run is judged or reported: run.bash echoes plenty and the serial console
		// captures everything the guest printed.
		c.logf("scrubbing the run's artefacts and verifying no supplied secret survives")
		if err := c.scrub(ctx, r.l, r.cr, r.dir, r.transcript.Name(), filepath.Join(r.dir, "console.log")); err != nil {
			return err
		}
	}
	return nil
}

// accept runs the checker in the guest, its output into the transcript (E42).
func (r *running) accept(ctx context.Context, check string) error {
	if err := r.g.scp(ctx, check, "/tmp/guest-acceptance.bash"); err != nil {
		return err
	}
	env := []string{"env"}
	if r.b.Profile == profileDesktop {
		// The checks must reach the session's GNOME Shell: the bus of the autologin session.
		env = append(env, "XDG_RUNTIME_DIR=/run/user/$(id -u) DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$(id -u)/bus")
	}
	if r.s.HostOnly {
		// "No secret bytes survived" needs the bytes, so the needles go back in, briefly; the
		// provisioning secrets go first, as they are what the checker must not find.
		if err := r.g.dropSecrets(ctx); err != nil {
			return err
		}
		if err := r.g.pushNeedles(ctx, r.cr); err != nil {
			return err
		}
		env = append(env, "VMTEST_GITHUB_USER='"+r.cr.account+"' VMTEST_NEEDLES_FILE="+guestDropDir+"/needles")
	}
	env = append(env, "VMTEST_COMMIT='"+r.commit+"' VMTEST_USER_EMAIL='"+guestUserEmail+"' bash /tmp/guest-acceptance.bash")
	cmd := r.g.sshCmd(strings.Join(env, " "))
	w := r.tee()
	cmd.Stdout, cmd.Stderr = w, w
	_, runErr := r.c.env.Runner.Run(ctx, cmd)
	code, err := statusOf(ctx, ctx, runErr)
	if err != nil {
		return err
	}
	if code != 0 {
		r.c.logf("guest acceptance script exited %d (recorded; the transcript decides)", code)
	}
	return nil
}

// decision is the freshness decision the run started under, for the transcript header.
func (r *running) decision() string { return r.verdict.decision }

// judge destroys the guest, has judge_run write the response, applies the refresh outcome and
// reports the verdict (E46). Exit 0 only for pass.
func (r *running) judge(ctx context.Context, v verdict, probeRev string) error {
	c := r.c
	r.follower.stop()
	r.follower = nil
	if err := c.virshShowFail(ctx, "destroy", r.g.domain); err != nil {
		return err
	}
	if err := c.virshShowFail(ctx, "undefine", "--nvram", r.g.domain); err != nil {
		return err
	}
	r.g.domain = ""
	response := filepath.Join(r.dir, "response.json")
	baseJSON := filepath.Join(r.baseDir, "base.json")
	verdictText, err := r.runJudge(ctx, v, probeRev, response, baseJSON)
	if err != nil {
		return err
	}
	r.ok = verdictText == "pass"
	// §4.4a: the run was the refresh probe; a passing run can certify the base current.
	res, err := c.helper(ctx, helperCall{module: "refresh", args: []string{"certify", "--response", response, flagBaseJSON, baseJSON}})
	if err != nil {
		return ctxErr(ctx, failf("could not apply the run's refresh outcome to %s", baseJSON))
	}
	certified := strings.TrimSpace(string(res.Stdout))
	c.logf("%s", certified)
	if strings.Contains(certified, "reason=refresh outcome is 'stale'") {
		c.logf("base %s is stale: refresh it with: vmtest refresh-base %s", r.b.Name, r.s.BaseKey)
	}
	c.logf("verdict: %s; response %s; transcript %s", verdictText, response, r.transcript.Name())
	c.sayf("VMTEST-RUN %s verdict=%s response=%s", r.id, verdictText, response)
	if !r.ok {
		return &exitStatusError{code: term.ExitFailure}
	}
	return nil
}

// runJudge writes response.json with judge_run and reads its verdict as JSON (B11).
func (r *running) runJudge(ctx context.Context, v verdict, probeRev, response, baseJSON string) (string, error) {
	c := r.c
	out, err := os.OpenFile(response, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", failf("cannot create %s: %v", response, err)
	}
	_, runErr := c.helper(ctx, helperCall{module: "judge_run", stdout: out, args: []string{
		"--transcript", r.transcript.Name(), "--transcript-path", "runs/" + r.id + "/transcript.log",
		flagBaseJSON, baseJSON, "--scenario", r.s.ID, "--planned", r.s.Planned, "--max-skipped", r.s.MaxSkipped,
		"--run-id", r.id, "--commit", r.commit, "--branch", r.l.branch, "--freshness-decision", v.decision,
		"--freshness-degraded", v.degraded, "--freshness-divergences", v.divergences, "--divergences", divergences(r.b.Profile),
		"--started-at", strconv.FormatInt(r.startedAt, 10), "--finished-at", strconv.FormatInt(c.env.Now().Unix(), 10),
		"--probe-revision", probeRev,
	}})
	cerr := out.Close()
	if ctx.Err() != nil {
		return "", ctxErr(ctx, runErr)
	}
	code := helperExitCode(runErr)
	body, rerr := os.ReadFile(response)
	var doc struct {
		Verdict *string `json:"verdict"`
	}
	if code < 0 || code == 2 || cerr != nil || rerr != nil || json.Unmarshal(body, &doc) != nil || doc.Verdict == nil || *doc.Verdict == "" {
		return "", failf("could not judge the run; see %s", r.dir)
	}
	return *doc.Verdict, nil
}

// teardown runs on every exit path of a run (E47).
func (r *running) teardown(ctx context.Context) {
	r.follower.stop()
	if r.g != nil {
		r.c.dropDomain(ctx, r.g.domain)
	}
	if r.transcript != nil {
		if err := r.transcript.Close(); err != nil {
			r.c.logf("could not close the transcript: %v", err)
		}
	}
	if r.ok {
		if err := removeIfPresent(filepath.Join(r.dir, "overlay.qcow2")); err != nil {
			r.c.logf("%v", err)
		}
		return
	}
	r.c.logf("overlay and console kept for diagnosis: %s", r.dir)
}
