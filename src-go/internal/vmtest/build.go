package vmtest

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const (
	buildDiskSize      = "20G"
	installWaitMinutes = "90"
	secretBytes        = 24
	luksFile           = "luks-passphrase"
)

func (c *cli) cmdBuildBase(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usagef("usage: vmtest build-base <base-key>")
	}
	l, err := c.loadLab()
	if err != nil {
		return err
	}
	return c.build(ctx, l, args[0])
}

// building is one build in progress: what its teardown must undo (E27).
type building struct {
	c        *cli
	domain   string // empty once the domain is undefined by the build itself
	dir      string // empty once a successful build has removed it
	follower *follower
}

// teardown runs on every exit path of a build. A build that finished has cleared its domain and
// removed its directory, so only a failed build's are touched, and its directory is kept.
func (b *building) teardown(ctx context.Context) {
	b.follower.stop()
	b.c.dropDomain(ctx, b.domain)
	if b.dir != "" {
		b.c.logf("build directory kept for diagnosis: %s", b.dir)
	}
}

// dropDomain destroys (unless shut off) and undefines a domain libvirt knows; each failure is
// logged, never fatal, because it runs on the way out of a step that already failed.
func (c *cli) dropDomain(ctx context.Context, domain string) {
	if domain == "" || !c.domainKnown(ctx, domain) {
		return
	}
	if state, err := c.domstate(ctx, domain); err != nil || state != stateShutOff {
		if err := c.virshShow(ctx, "destroy", domain); err != nil {
			c.logf("could not destroy %s", domain)
		}
	}
	if err := c.virshShow(ctx, "undefine", "--nvram", domain); err != nil {
		c.logf("could not undefine %s", domain)
	}
}

// domainKnown is whether libvirt answers dominfo for the domain.
func (c *cli) domainKnown(ctx context.Context, domain string) bool {
	_, err := c.env.Runner.Run(ctx, execx.Cmd{
		Name: virshBin, Args: []string{"-c", libvirtURI, "dominfo", domain},
		Stdout: io.Discard, Stderr: io.Discard,
	})
	return err == nil
}

// buildBase builds a base from its published media (E15-E21).
func (c *cli) buildBase(ctx context.Context, l *lab, key string) (err error) {
	b, err := c.baseFacts(ctx, l, key)
	if err != nil {
		return err
	}
	if b.Kind != kindFast && b.Kind != kindFull {
		return failf("unknown base kind %s for %s", b.Kind, b.Name)
	}
	if b.Kind != kindFull && b.Profile != profileServer {
		return failf("a %s base must be kind full; %s is %s", b.Profile, b.Name, b.Kind)
	}
	p, err := c.probeUpstream(ctx, l)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, p.remove()) }()
	return c.buildFromProbe(ctx, l, b, p)
}

// buildFromProbe is the build once the probe has said what upstream publishes.
func (c *cli) buildFromProbe(ctx context.Context, l *lab, b baseFacts, p *probe) error {
	arts, err := c.fetch(ctx, l, b, p)
	if err != nil {
		return err
	}
	compose, err := p.single("VMTEST-FRESHNESS-COMPOSE-ID")
	if err != nil {
		return err
	}
	rawRev, err := p.single("VMTEST-FRESHNESS-REVISION")
	if err != nil {
		return err
	}
	probeRev, err := parseWhole("the probe's revision", rawRev)
	if err != nil {
		return err
	}
	if err = c.buildFloor(ctx, l, b, arts[0]); err != nil {
		return err
	}
	st := &building{c: c, domain: "vmtest-build-" + b.Name, dir: l.at(basesDir, b.Name+".build")}
	if c.domainKnown(ctx, st.domain) {
		return failf("a domain named %s already exists; a previous build did not clean up. Inspect it, then: virsh -c %s undefine --nvram %s", st.domain, libvirtURI, st.domain)
	}
	if err = os.RemoveAll(st.dir); err != nil {
		return failf("cannot clear %s: %v", st.dir, err)
	}
	if err = os.MkdirAll(st.dir, 0o755); err != nil {
		return failf("cannot create %s: %v", st.dir, err)
	}
	defer st.teardown(context.WithoutCancel(ctx))
	port, err := c.env.FreePort()
	if err != nil {
		return failf("%v", err)
	}
	g := &guest{c: c, key: l.at(sshKeyFile), domain: st.domain, port: port, dir: st.dir}
	disk := filepath.Join(st.dir, "disk.qcow2")
	installedAt := c.env.Now().Unix()
	if b.Kind == kindFast {
		err = c.seedFast(ctx, l, g, b, arts[0].File, disk)
	} else {
		err = c.installFull(ctx, l, g, b, p, arts[0].File, disk, st)
	}
	if err != nil {
		return err
	}
	upgraded, err := c.upgradeAndClean(ctx, l, g, b, st, probeRev)
	if err != nil {
		return err
	}
	return c.flatten(ctx, l, b, st, record{p: p, arts: arts, compose: compose, probeRev: probeRev, installedAt: installedAt, upgrade: upgraded})
}

func parseWhole(what, raw string) (int64, error) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, failf("%s '%s' is not a whole number", what, raw)
	}
	return n, nil
}

// buildFloor refuses a rebuild the disk or the RAM cannot carry (E16, §10).
func (c *cli) buildFloor(ctx context.Context, l *lab, b baseFacts, first artefact) error {
	sizeOf := l.at(imagesDir, first.File)
	if existing := l.at(basesDir, b.Name, "base.qcow2"); readable(existing) {
		sizeOf = existing
	}
	info, err := os.Stat(sizeOf)
	if err != nil {
		return failf("cannot read %s: %v", sizeOf, err)
	}
	if err := c.floor(ctx, l.at(basesDir), info.Size(), "rebuild", b.RAMMiB); err != nil {
		return ctxErr(ctx, failf("the host cannot carry a rebuild of %s (stage: base); see the VMTEST-DISK/VMTEST-RAM lines above", b.Name))
	}
	return nil
}

// seedFast seeds a fast base from the published Cloud image and boots it with the lab key
// injected through cloud-init, then cloud-init disabled (E18).
func (c *cli) seedFast(ctx context.Context, l *lab, g *guest, b baseFacts, image, disk string) error {
	c.logf("seeding build disk from %s", image)
	if _, err := c.env.Runner.Run(ctx, execx.Cmd{
		Name: "cp", Args: []string{"--reflink=auto", l.at(imagesDir, image), disk},
		Stdout: c.env.Stderr, Stderr: c.env.Stderr,
	}); err != nil {
		return ctxErr(ctx, failf("could not seed %s: %v", disk, err))
	}
	if _, err := c.env.Runner.Run(ctx, execx.Cmd{
		Name: qemuImgBin, Args: []string{"resize", "-q", disk, buildDiskSize},
		Stdout: c.env.Stderr, Stderr: c.env.Stderr,
	}); err != nil {
		return ctxErr(ctx, failf("could not resize %s: %v", disk, err))
	}
	c.logf("booting %s (%d vCPU, %d MiB, SSH forwarded to 127.0.0.1:%d)", g.domain, b.VCPUs, b.RAMMiB, g.port)
	return c.virtInstall(ctx, g, b, "path="+disk+",format=qcow2,bus=virtio,discard=unmap",
		"--cloud-init", "clouduser-ssh-key="+l.at(sshKeyFile)+".pub,disable=on",
		flagGraphics, displayNone, "--serial", "file,path="+filepath.Join(g.dir, "console.log"))
}

// serialSpec is the virt-install --serial argument for a profile: a desktop's is a socket
// the host writes the LUKS passphrase to, a server's a plain file (§5.3a).
func serialSpec(profile, dir string) string {
	if profile == profileDesktop {
		return "unix,path=" + filepath.Join(dir, "serial.sock") + ",mode=bind"
	}
	return "file,path=" + filepath.Join(dir, "console.log")
}

// randomToken is 24 random bytes in base64: a throwaway secret.
func (c *cli) randomToken() (string, error) {
	raw := make([]byte, secretBytes)
	if _, err := io.ReadFull(c.env.Random, raw); err != nil {
		return "", failf("cannot read random bytes: %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// renderKickstart renders the deployed kickstart for the base into ks (0600). A desktop's two
// secrets, the LUKS passphrase (kept beside the base for every later boot) and the user's
// password crypt, reach the renderer on stdin only (E19).
func (c *cli) renderKickstart(ctx context.Context, l *lab, b baseFacts, treeURL, ks string) error {
	template, err := kickstartFor(l, b.Profile)
	if err != nil {
		return err
	}
	pub, err := os.ReadFile(l.at(sshKeyFile + ".pub"))
	if err != nil {
		return failf("cannot read %s.pub: %v", l.at(sshKeyFile), err)
	}
	args := []string{"--template", template, "--set", "SSH_PUBKEY=" + strings.TrimRight(string(pub), "\n"), "--set", "FEDORA_VERSION=" + l.fedora}
	var stdin io.Reader
	if b.Profile == profileDesktop {
		secrets, serr := c.desktopSecrets(ctx, filepath.Dir(ks))
		if serr != nil {
			return serr
		}
		stdin = strings.NewReader(secrets)
		args = append(args, "--set-stdin")
	} else {
		args = append(args, "--set", "TREE_URL="+treeURL)
	}
	out, err := os.OpenFile(ks, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return failf("cannot create %s: %v", ks, err)
	}
	_, runErr := c.helper(ctx, helperCall{module: "render_kickstart", args: args, stdin: stdin, stdout: out})
	if err := errors.Join(runErr, out.Close()); err != nil {
		return ctxErr(ctx, failf("could not render %s: %v", template, err))
	}
	return nil
}

// desktopSecrets makes the throwaway LUKS passphrase (saved 0600 in dir) and the user's
// password crypt, as the renderer's stdin lines.
func (c *cli) desktopSecrets(ctx context.Context, dir string) (string, error) {
	passphrase, err := c.randomToken()
	if err != nil {
		return "", err
	}
	if err = os.WriteFile(filepath.Join(dir, luksFile), []byte(passphrase+"\n"), 0o600); err != nil {
		return "", failf("cannot save the LUKS passphrase: %v", err)
	}
	password, err := c.randomToken()
	if err != nil {
		return "", err
	}
	res, err := c.env.Runner.Run(ctx, execx.Cmd{
		Name: "openssl", Args: []string{"passwd", "-6", "-stdin"},
		Stdin: strings.NewReader(password + "\n"), Stderr: c.env.Stderr,
	})
	crypt := strings.TrimSpace(string(res.Stdout))
	if err != nil || crypt == "" {
		return "", ctxErr(ctx, failf("openssl could not crypt the throwaway password: %v", err))
	}
	return "LUKS_PASSPHRASE=" + passphrase + "\nUSER_PASSWORD_CRYPT=" + crypt + "\n", nil
}

// installFull installs a full base with Anaconda from the one named tree, driven by the
// rendered kickstart, powering off when done; then boots the installed system (E19). A
// desktop's follower, running when it returns, is left in st for the caller to stop.
func (c *cli) installFull(ctx context.Context, l *lab, g *guest, b baseFacts, p *probe, first, disk string, st *building) error {
	treeURL, err := p.treeURL(b.Tree)
	if err != nil {
		return err
	}
	iso, extra := first, []string{}
	if b.Profile == profileDesktop {
		if iso, err = p.artefactWithPrefix(b.Name, "Fedora-Everything-netinst-"); err != nil {
			return err
		}
		live, lerr := p.artefactWithPrefix(b.Name, "Fedora-Workstation-Live-")
		if lerr != nil {
			return lerr
		}
		extra = []string{flagDisk, "path=" + l.at(imagesDir, live) + ",device=cdrom,readonly=on"}
	}
	ks := filepath.Join(g.dir, "ks.cfg")
	if err = c.renderKickstart(ctx, l, b, treeURL, ks); err != nil {
		return err
	}
	if _, err = c.env.Runner.Run(ctx, execx.Cmd{
		Name: qemuImgBin, Args: []string{"create", "-q", "-f", formatQcow2, disk, buildDiskSize},
		Stdout: c.env.Stderr, Stderr: c.env.Stderr,
	}); err != nil {
		return ctxErr(ctx, failf("qemu-img failed: %v", err))
	}
	// Name the installer's stage-2 medium: without inst.stage2 the installer's dracut scans
	// every medium for a squashfs, and with the Live ISO attached it booted the live session
	// instead of Anaconda (the first desktop build). The netinst's volume label pins it.
	res, err := c.env.Runner.Run(ctx, execx.Cmd{Name: "blkid", Args: []string{"-o", "value", "-s", "LABEL", l.at(imagesDir, iso)}, Stderr: c.env.Stderr})
	label := strings.TrimSpace(string(res.Stdout))
	if err != nil || label == "" {
		return ctxErr(ctx, failf("could not read the volume label of %s", iso))
	}
	c.logf("installing from %s via %s (Anaconda, cmdline mode on the serial console; up to %s min)", treeURL, iso, installWaitMinutes)
	var f *follower
	if b.Profile == profileDesktop {
		f = c.follow(ctx, g.dir)
	}
	args := append([]string{
		flagConnect, libvirtURI, flagName, g.domain, "--memory", strconv.Itoa(b.RAMMiB), "--vcpus", strconv.Itoa(b.VCPUs),
		flagCPU, "host-passthrough", flagDisk, "path=" + disk + ",format=qcow2,bus=virtio,discard=unmap",
	}, extra...)
	args = append(args,
		"--location", l.at(imagesDir, iso), "--initrd-inject", ks,
		"--extra-args", "inst.ks=file:/ks.cfg inst.stage2=hd:LABEL="+label+" console=ttyS0,115200n8 plymouth.enable=0",
		"--osinfo", "fedora-unknown", "--memballoon", virtioModel,
		"--boot", "uefi,firmware.feature0.name=secure-boot,firmware.feature0.enabled=no",
		"--network", passtNetwork(g.port), flagGraphics, displayNone, "--serial", serialSpec(b.Profile, g.dir),
		"--noautoconsole", "--noreboot", "--wait", installWaitMinutes)
	_, runErr := c.env.Runner.Run(ctx, execx.Cmd{Name: virtInstallBin, Args: args, Stdout: c.env.Stderr, Stderr: c.env.Stderr})
	f.stop()
	if runErr != nil {
		return ctxErr(ctx, failf("the installer failed: %v; see %s", runErr, filepath.Join(g.dir, "console.log")))
	}
	if err = c.installed(ctx, g, ks); err != nil {
		return err
	}
	if b.Profile == profileDesktop {
		st.follower, err = g.unlock(ctx, g.dir, filepath.Join(g.dir, luksFile))
	}
	return err
}

// installed checks that the installer powered off having finished, removes the rendered
// kickstart (it carried the passphrase) and boots the installed system.
func (c *cli) installed(ctx context.Context, g *guest, ks string) error {
	state, err := c.domstate(ctx, g.domain)
	if err != nil {
		return failf("cannot read the state of guest %s after the install", g.domain)
	}
	console := filepath.Join(g.dir, "console.log")
	if state != stateShutOff {
		return failf("the installer did not power off within %s min (state: %s); see %s", installWaitMinutes, state, console)
	}
	log, err := os.ReadFile(console)
	if err != nil || (!bytes.Contains(log, []byte("Installation complete")) && !bytes.Contains(log, []byte("Running post-installation scripts"))) {
		return failf("the installer powered off without reporting completion; see %s", console)
	}
	if err = os.Remove(ks); err != nil {
		return failf("cannot remove the rendered kickstart %s: %v", ks, err)
	}
	c.logf("install complete; booting the installed system")
	return c.virshShowFail(ctx, "start", g.domain)
}

func (c *cli) virshShowFail(ctx context.Context, args ...string) error {
	if err := c.virshShow(ctx, args...); err != nil {
		return ctxErr(ctx, failf("virsh %s failed: %v", strings.Join(args, " "), err))
	}
	return nil
}

// upgraded is what the guest's upgrade saw.
type upgraded struct {
	revision int64
	mirror   string
}

// upgradeAndClean upgrades the guest, reads what it saw, installs a desktop's session runner,
// cleans the guest and powers it off (E20).
func (c *cli) upgradeAndClean(ctx context.Context, l *lab, g *guest, b baseFacts, st *building, probeRev int64) (upgraded, error) {
	if err := g.waitSSH(ctx); err != nil {
		return upgraded{}, err
	}
	c.logf("upgrading packages in the guest")
	if err := g.do(ctx, "sudo dnf -y --refresh upgrade"); err != nil {
		return upgraded{}, err
	}
	info, err := g.output(ctx, "sudo dnf --quiet repoinfo updates")
	if err != nil {
		return upgraded{}, err
	}
	rawRev, err := repoFact(info, "Revision")
	if err != nil {
		return upgraded{}, err
	}
	rev, err := parseWhole("the guest's updates revision", rawRev)
	if err != nil {
		return upgraded{}, err
	}
	baseURL, err := repoFact(info, "Base URL")
	if err != nil {
		return upgraded{}, err
	}
	u := upgraded{revision: rev, mirror: strings.Fields(baseURL)[0]}
	c.logf("guest saw updates revision %d from %s (probe read %d)", u.revision, u.mirror, probeRev)
	if b.Profile == profileDesktop {
		if err = c.installSessionRunner(ctx, l, g); err != nil {
			return upgraded{}, err
		}
	}
	if err = c.cleanAndPowerOff(ctx, l, g, st); err != nil {
		return upgraded{}, err
	}
	return u, nil
}

// installSessionRunner puts the session runner in a desktop base: gnome-session launches it
// through the autostart entry the kickstart wrote, and it waits for the host's run.env.
func (c *cli) installSessionRunner(ctx context.Context, l *lab, g *guest) error {
	c.logf("installing the session runner in the guest")
	if err := g.scp(ctx, l.at(sessionRunner), "/tmp/vmtest-session-provision.bash"); err != nil {
		return err
	}
	for _, remote := range []string{
		"sudo install -o root -g root -m 0755 /tmp/vmtest-session-provision.bash /usr/local/bin/vmtest-session-provision.bash",
		"rm -f /tmp/vmtest-session-provision.bash",
	} {
		if err := g.do(ctx, remote); err != nil {
			return err
		}
	}
	return nil
}

// cleanAndPowerOff cleans the guest for the snapshot, powers it off and undefines it.
func (c *cli) cleanAndPowerOff(ctx context.Context, l *lab, g *guest, st *building) error {
	c.logf("cleaning the guest before snapshot")
	if err := g.scp(ctx, l.at(guestCleanup), "/tmp/guest-cleanup.bash"); err != nil {
		return err
	}
	out, err := g.output(ctx, "sudo bash /tmp/guest-cleanup.bash")
	c.p.Print(c.env.Stderr, out)
	if err != nil || !hasLine(out, "VMTEST-GUEST-CLEANUP-DONE") {
		return ctxErr(ctx, failf("guest cleanup did not complete"))
	}
	// Scheduled, so the SSH session ends cleanly rather than being severed.
	if err = g.do(ctx, "sudo systemd-run --quiet --on-active=2 systemctl poweroff"); err != nil {
		return err
	}
	if err = g.waitShutoff(ctx); err != nil {
		return err
	}
	st.follower.stop()
	st.follower = nil
	if err = c.virshShowFail(ctx, "undefine", "--nvram", st.domain); err != nil {
		return err
	}
	st.domain = ""
	return nil
}

func hasLine(out, want string) bool {
	for line := range strings.SplitSeq(out, "\n") {
		if line == want {
			return true
		}
	}
	return false
}

// repoFact is one field of `dnf repoinfo` output: the first non-empty value of its line.
func repoFact(info, field string) (string, error) {
	for line := range strings.SplitSeq(info, "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(name) == field && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
	}
	return "", failf("guest repoinfo has no %s line", field)
}

// record is what the base record is written from.
type record struct {
	p           *probe
	arts        []artefact
	compose     string
	probeRev    int64
	installedAt int64
	upgrade     upgraded
}

// flatten moves the disk into place, writes the record and finishes the build (E21).
func (c *cli) flatten(ctx context.Context, l *lab, b baseFacts, st *building, r record) error {
	baseDir := l.at(basesDir, b.Name)
	disk := filepath.Join(baseDir, "base.qcow2")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return failf("cannot create %s: %v", baseDir, err)
	}
	if err := os.Rename(filepath.Join(st.dir, "disk.qcow2"), disk); err != nil {
		return failf("cannot move the disk into %s: %v", baseDir, err)
	}
	if pp := filepath.Join(st.dir, luksFile); readable(pp) {
		if err := os.Rename(pp, filepath.Join(baseDir, luksFile)); err != nil {
			return failf("cannot move the LUKS passphrase beside the base: %v", err)
		}
	}
	c.logf("hashing the flattened base")
	sha, err := fileSHA256(disk)
	if err != nil {
		return err
	}
	info, err := os.Stat(disk)
	if err != nil {
		return failf("cannot read %s: %v", disk, err)
	}
	state := "incomplete"
	if r.upgrade.revision >= r.probeRev {
		state = "complete" // §4.4a: the guest's mirror had caught up with the canonical host
	}
	digest, err := recipeDigest(l, b.Kind, b.Profile)
	if err != nil {
		return err
	}
	args := []string{
		flagFedoraVersion, l.fedora, "--profile", b.Profile, "--kind", b.Kind, flagName, b.Name,
		"--compose-id", r.compose, "--compose-label", r.arts[0].Label,
	}
	for _, a := range r.arts {
		args = append(args, "--artefact", a.File+"="+a.SHA256)
	}
	args = append(args, "--recipe-digest", digest)
	if b.Kind == kindFull {
		args = append(args, "--treeinfo-probe", r.p.path, "--tree", b.Tree)
	}
	args = append(args, "--installed-at", strconv.FormatInt(r.installedAt, 10), "--last-upgraded-at", strconv.FormatInt(c.env.Now().Unix(), 10),
		"--last-upgraded-revision", strconv.FormatInt(r.upgrade.revision, 10), "--last-upgraded-mirror", r.upgrade.mirror,
		"--refresh-state", state, "--base-sha256", sha, "--base-size", strconv.FormatInt(info.Size(), 10),
		"--base-mtime", strconv.FormatInt(info.ModTime().Unix(), 10))
	if err := c.writeRecord(ctx, baseDir, args); err != nil {
		return err
	}
	if err := os.Chmod(disk, 0o444); err != nil {
		return failf("cannot make %s read-only: %v", disk, err)
	}
	if err := os.RemoveAll(st.dir); err != nil {
		return failf("cannot remove %s: %v", st.dir, err)
	}
	st.dir = ""
	c.logf("base %s built: %s (%d bytes, refresh %s)", b.Name, disk, info.Size(), state)
	c.sayf("VMTEST-BASE-BUILT %s %s refresh_state=%s", b.Name, disk, state)
	return nil
}

// writeRecord has write_base_record write base.json.new, then renames it over base.json.
func (c *cli) writeRecord(ctx context.Context, baseDir string, args []string) error {
	next := filepath.Join(baseDir, "base.json.new")
	out, err := os.OpenFile(next, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return failf("cannot create %s: %v", next, err)
	}
	_, runErr := c.helper(ctx, helperCall{module: "write_base_record", args: args, stdout: out})
	if err := errors.Join(runErr, out.Close()); err != nil {
		return errors.Join(ctxErr(ctx, failf("could not write the base record: %v", err)), removeIfPresent(next))
	}
	if err := os.Rename(next, filepath.Join(baseDir, "base.json")); err != nil {
		return failf("cannot move the base record into place: %v", err)
	}
	return nil
}
