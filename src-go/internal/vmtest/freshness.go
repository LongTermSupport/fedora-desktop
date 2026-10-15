package vmtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// buildRecipeVersion is the base build recipe's version. Bump it whenever the way a base is
// BUILT changes (disk size, cloud-init options, the upgrade step, what is cleaned): the recipe
// digest is this number plus guest-cleanup.bash, so a recipe change reinstalls every base
// (§4.4) while an unrelated change to this CLI does not.
const buildRecipeVersion = 1

const (
	kindFast       = "fast"
	kindFull       = "full"
	profileServer  = "server"
	profileDesktop = "desktop"
	zeroSHA256     = "0000000000000000000000000000000000000000000000000000000000000000"
)

// kickstartFor is the deployed VM kickstart a full base of profile installs from.
func kickstartFor(l *lab, profile string) (string, error) {
	var ks string
	switch profile {
	case profileServer:
		ks = l.at(ksServer)
	case profileDesktop:
		ks = l.at(ksDesktop)
	default:
		return "", failf("no VM kickstart for profile %s", profile)
	}
	if !readable(ks) {
		return "", failf("no %s; deploy the lab first", ks)
	}
	return ks, nil
}

// recipeDigest is the build recipe's digest (E49): a change to how a base is built
// invalidates it (§3.2).
func recipeDigest(l *lab, kind, profile string) (string, error) {
	files := []string{l.at(guestCleanup)}
	if kind == kindFull {
		ks, err := kickstartFor(l, profile)
		if err != nil {
			return "", err
		}
		files = append(files, ks)
		if profile == profileDesktop {
			files = append(files, l.at(sessionRunner))
		}
	}
	h := sha256.New()
	if _, err := fmt.Fprintf(h, "build-recipe-version=%d\n", buildRecipeVersion); err != nil {
		return "", fmt.Errorf("hash the recipe: %w", err)
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return "", failf("no %s; deploy the lab first", f)
		}
		if _, err := h.Write(body); err != nil {
			return "", fmt.Errorf("hash the recipe: %w", err)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fileSHA256 hashes a file whole.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", failf("cannot read %s: %v", path, err)
	}
	h := sha256.New()
	_, cerr := io.Copy(h, f)
	if err := errors.Join(cerr, f.Close()); err != nil {
		return "", failf("cannot hash %s: %v", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verdict is the freshness gate's answer for one base (E48).
type verdict struct {
	line, decision, degraded, divergences, integrity string
	// refused is the gate's exit 1: the base may not be used. It is data, not a failure.
	refused bool
	size    int64
}

// tokens reads `key=value` words of a marker line.
func tokens(line string, sep func(rune) bool) map[string]string {
	out := map[string]string{}
	for _, w := range strings.FieldsFunc(line, sep) {
		if k, v, ok := strings.Cut(w, "="); ok {
			if _, seen := out[k]; !seen {
				out[k] = v
			}
		}
	}
	return out
}

// oneLine is the single line of out that starts with marker.
func oneLine(out, marker string) (string, bool) {
	var found []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, marker+" ") {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		return "", false
	}
	return found[0], true
}

func (c *cli) gateArgs(l *lab, p *probe, baseJSON, digest, sha string) []string {
	return []string{
		flagFedoraVersion, l.fedora, "--manifest", l.at(manifestFile), flagBaseJSON, baseJSON,
		"--probe", p.path, "--recipe-digest", digest, "--actual-base-sha256", sha,
	}
}

// freshness is the §4.4 verdict for the base in dir, from the probe already taken (E48). It
// fails only when the record or the disk cannot be read as one base; a refusal is data.
func (c *cli) freshness(ctx context.Context, l *lab, p *probe, dir string, deep bool) (verdict, error) {
	baseJSON, disk := filepath.Join(dir, "base.json"), filepath.Join(dir, "base.qcow2")
	rec, err := c.baseRecord(ctx, l, p, baseJSON)
	if err != nil {
		return verdict{}, err
	}
	info, err := os.Stat(disk)
	if err != nil {
		return verdict{}, failf("cannot read %s: %v", disk, err)
	}
	if strconv.FormatInt(info.Size(), 10) != rec["base_size"] || strconv.FormatInt(info.ModTime().Unix(), 10) != rec["base_mtime"] {
		return verdict{}, failf("base.qcow2 for %s differs from its record (size/mtime); refusing a base of unknown provenance", rec["name"])
	}
	v := verdict{integrity: "quick", size: info.Size()}
	sha := rec["base_sha256"]
	if deep {
		v.integrity = "deep"
		c.logf("hashing %s (deep check; this reads the whole base)", disk)
		if sha, err = fileSHA256(disk); err != nil {
			return verdict{}, err
		}
	}
	digest, err := recipeDigest(l, rec["kind"], rec[fieldProfile])
	if err != nil {
		return verdict{}, err
	}
	res, err := c.helper(ctx, helperCall{module: "freshness_gate", args: c.gateArgs(l, p, baseJSON, digest, sha)})
	code := helperExitCode(err)
	if code < 0 {
		return verdict{}, ctxErr(ctx, err)
	}
	return v.judged(string(res.Stdout), code)
}

// judged is v with the gate's verdict line (exit 0 current, 1 refused) filled in.
func (v verdict) judged(out string, code int) (verdict, error) {
	line, ok := oneLine(out, "VMTEST-FRESHNESS-VERDICT")
	if (code != 0 && code != 1) || !ok {
		return verdict{}, failf("the freshness gate printed no verdict (exit %d)", code)
	}
	fields := tokens(strings.TrimPrefix(line, "VMTEST-FRESHNESS-VERDICT "), func(r rune) bool { return r == '\t' })
	for _, k := range []string{"decision", "degraded", "divergences"} {
		if _, ok := fields[k]; !ok || (k == "decision" && fields[k] == "") {
			return verdict{}, failf("freshness verdict has no %s: %s", k, line)
		}
	}
	v.line, v.decision, v.degraded, v.divergences, v.refused = line, fields["decision"], fields["degraded"], fields["divergences"], code == 1
	return v, nil
}

// baseRecord is what the base's record says of it (name, kind, profile, size, mtime, sha),
// read by the gate in --record-only mode.
func (c *cli) baseRecord(ctx context.Context, l *lab, p *probe, baseJSON string) (map[string]string, error) {
	fast, err := recipeDigest(l, kindFast, profileServer)
	if err != nil {
		return nil, err
	}
	res, err := c.helper(ctx, helperCall{
		module: "freshness_gate",
		args:   append(c.gateArgs(l, p, baseJSON, fast, zeroSHA256), "--record-only"),
	})
	if err != nil {
		return nil, ctxErr(ctx, failf("could not read %s", baseJSON))
	}
	line, ok := oneLine(string(res.Stdout), "VMTEST-BASE-RECORD")
	if !ok {
		return nil, failf("unexpected gate output: %s", strings.TrimSpace(string(res.Stdout)))
	}
	rec := tokens(line, func(r rune) bool { return r == ' ' })
	for _, k := range []string{"name", "kind", fieldProfile, "base_size", "base_mtime", "base_sha256"} {
		if rec[k] == "" {
			return nil, failf("marker has no %s: %s", k, line)
		}
	}
	return rec, nil
}

// builtBases is the directory of every base that has a readable record, in name order.
func builtBases(l *lab) ([]string, error) {
	matches, err := filepath.Glob(l.at(basesDir, "*", "base.json"))
	if err != nil {
		return nil, fmt.Errorf("list the bases: %w", err)
	}
	slices.Sort(matches)
	var dirs []string
	for _, m := range matches {
		if readable(m) {
			dirs = append(dirs, filepath.Dir(m))
		}
	}
	return dirs, nil
}

func (c *cli) cmdFreshnessStatus(ctx context.Context, args []string) error {
	deep, err := deepFlag(args)
	if err != nil {
		return err
	}
	l, err := c.loadLab()
	if err != nil {
		return err
	}
	return c.freshnessStatus(ctx, l, deep)
}

func deepFlag(args []string) (bool, error) {
	switch {
	case len(args) == 0:
		return false, nil
	case len(args) == 1 && args[0] == "--deep":
		return true, nil
	}
	return false, usagef("usage: vmtest freshness-status [--deep]")
}

// freshnessStatus reports every built base's freshness; it never builds anything (E50).
func (c *cli) freshnessStatus(ctx context.Context, l *lab, deep bool) (err error) {
	p, err := c.probeUpstream(ctx, l)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, p.remove()) }()
	rev, err := p.single("VMTEST-FRESHNESS-REVISION")
	if err != nil {
		return err
	}
	compose, err := p.single("VMTEST-FRESHNESS-COMPOSE-ID")
	if err != nil {
		return err
	}
	integrity := "quick"
	if deep {
		integrity = "deep"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "written_at=%s\nintegrity=%s\nprobe_revision=%s\nprobe_compose_id=%s\n",
		c.env.Now().UTC().Format("2006-01-02T15:04:05Z"), integrity, rev, compose)
	dirs, err := builtBases(l)
	if err != nil {
		return err
	}
	worst := false
	for _, dir := range dirs {
		name := filepath.Base(dir)
		if !readable(filepath.Join(dir, "base.qcow2")) {
			return failf("%s has a record but no base.qcow2", name)
		}
		v, err := c.freshness(ctx, l, p, dir, deep)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "base=%s\tdecision=%s\tdegraded=%s\tintegrity=%s\tdivergences=%s\n", name, v.decision, v.degraded, v.integrity, v.divergences)
		c.sayf("VMTEST-FRESHNESS-STATUS %s decision=%s degraded=%s integrity=%s", name, v.decision, v.degraded, v.integrity)
		worst = worst || v.refused
	}
	if err := fsx.WriteAtomic(l.at(statusFile), "freshness status", []byte(b.String()), 0o644); err != nil {
		return failf("%v", err)
	}
	c.logf("freshness status written: %s", l.at(statusFile))
	if worst {
		return &exitStatusError{code: term.ExitFailure}
	}
	return nil
}

func noArgs(args []string, usage string) error {
	if len(args) != 0 {
		return usagef("usage: %s", usage)
	}
	return nil
}

func (c *cli) cmdSweep(ctx context.Context, args []string) error {
	if err := noArgs(args, "vmtest sweep"); err != nil {
		return err
	}
	l, err := c.loadLab()
	if err != nil {
		return err
	}
	return c.sweep(ctx, l)
}

// sweep applies the retention policy; the evictions it prints are its payload (E51).
func (c *cli) sweep(ctx context.Context, l *lab) error {
	checkout, err := l.needCheckout()
	if err != nil {
		return err
	}
	if _, err := c.helper(ctx, helperCall{module: "retention", stdout: c.env.Stdout, args: []string{
		"sweep", "--lab-dir", l.home, "--checkout", checkout, flagLog, l.at(retentionLog),
	}}); err != nil {
		return ctxErr(ctx, failf("the retention sweep failed: %v", err))
	}
	c.logf("every eviction is recorded in %s", l.at(retentionLog))
	return nil
}

// cmdNightly is freshness-status --deep, then sweep, always both (E52, B13).
func (c *cli) cmdNightly(ctx context.Context, args []string) error {
	if err := noArgs(args, "vmtest nightly"); err != nil {
		return err
	}
	l, err := c.loadLab()
	if err != nil {
		return err
	}
	statusErr := c.freshnessStatus(ctx, l, true)
	if ctx.Err() != nil {
		return statusErr
	}
	statusCode := term.ExitOK
	if status, ok := errors.AsType[*exitStatusError](statusErr); ok {
		statusCode = status.code
	} else if statusErr != nil {
		c.errorLine(statusErr.Error())
		statusCode = term.ExitFailure
	}
	sweepErr := c.sweep(ctx, l)
	switch {
	case sweepErr != nil && statusCode == term.ExitOK:
		return sweepErr
	case sweepErr != nil:
		c.errorLine(sweepErr.Error())
	}
	if statusCode != term.ExitOK {
		return &exitStatusError{code: statusCode}
	}
	return nil
}

func (c *cli) cmdEnsureBase(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usagef("usage: vmtest ensure-base <base-key>")
	}
	l, err := c.loadLab()
	if err != nil {
		return err
	}
	return c.ensureBase(ctx, l, args[0])
}

// ensureBase builds a base that is absent or refused, and changes nothing otherwise (E53).
func (c *cli) ensureBase(ctx context.Context, l *lab, key string) error {
	b, err := c.baseFacts(ctx, l, key)
	if err != nil {
		return err
	}
	dir := l.at(basesDir, b.Name)
	if readable(filepath.Join(dir, "base.qcow2")) && readable(filepath.Join(dir, "base.json")) {
		v, err := c.verdictOnce(ctx, l, dir)
		if err != nil {
			return err
		}
		if !v.refused {
			c.logf("base %s is usable (freshness: %s)", b.Name, v.decision)
			c.sayf("VMTEST-BASE-CURRENT %s decision=%s", b.Name, v.decision)
			return nil
		}
		c.logf("base %s may not be used (freshness: %s): rebuilding it", b.Name, v.decision)
	} else {
		c.logf("base %s is absent: building it", b.Name)
	}
	return c.build(ctx, l, key)
}

// verdictOnce probes, judges one base, and removes the probe.
func (c *cli) verdictOnce(ctx context.Context, l *lab, dir string) (verdict, error) {
	p, err := c.probeUpstream(ctx, l)
	if err != nil {
		return verdict{}, err
	}
	v, err := c.freshness(ctx, l, p, dir, false)
	return v, errors.Join(err, p.remove())
}

func (c *cli) cmdRefreshBase(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usagef("usage: vmtest refresh-base <base-key|server|desktop|all>")
	}
	l, err := c.loadLab()
	if err != nil {
		return err
	}
	return c.refreshBase(ctx, l, args[0])
}

// refreshBase rebuilds every built base matching target (E54). A named key is matched before
// the manifest is asked, so a directory the manifest does not know cannot abort it.
func (c *cli) refreshBase(ctx context.Context, l *lab, target string) error {
	dirs, err := builtBases(l)
	if err != nil {
		return err
	}
	byProfile := target == profileServer || target == profileDesktop
	done := 0
	for _, dir := range dirs {
		name := filepath.Base(dir)
		key := strings.TrimSuffix(name, "-"+l.fedora)
		if target != "all" && !byProfile && key != target {
			continue
		}
		b, err := c.baseFacts(ctx, l, key)
		if err != nil {
			return err
		}
		if byProfile && b.Profile != target {
			continue
		}
		if b.Kind == kindFast {
			c.logf("refreshing %s: re-importing the published image and upgrading", name)
		} else {
			c.logf("refreshing %s: reinstalling from the verified media and upgrading (a full base; this takes a while)", name)
		}
		if err := c.build(ctx, l, key); err != nil {
			return err
		}
		done++
	}
	if done == 0 {
		return failf("no built base matches %s; build one with: vmtest build-base <base-key>", target)
	}
	c.sayf("VMTEST-REFRESH-DONE refreshed=%d", done)
	return nil
}
