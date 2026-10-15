package vmtest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// A member is booted from a server base on an overlay under members/<name>/, on the same passt
// networking as a run, and left running when member-up exits; member-ssh drives it with the lab
// key and member-down removes it. The caller owns its lifetime, so a member is never swept.

const (
	memberSSHDefault = 1800
	memberPortFile   = "ssh-port"
)

var memberNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// memberName checks a name before it reaches a path or a domain name (E55).
func memberName(name string) error {
	if !memberNameRE.MatchString(name) {
		return failf("a member name is 1 to 40 of a-z, 0-9 and '-', not starting with '-'; got '%s'", name)
	}
	return nil
}

func memberDomain(name string) string { return "vmtest-member-" + name }

// domainListed is libvirt's own answer; an unreachable daemon is a failure, never "absent".
func (c *cli) domainListed(ctx context.Context, domain string) (bool, error) {
	res, err := c.env.Runner.Run(ctx, execx.Cmd{Name: virshBin, Args: []string{"-c", libvirtURI, "list", "--all", flagName}})
	if err != nil {
		return false, ctxErr(ctx, failf("virsh -c %s list failed", libvirtURI))
	}
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		if strings.TrimSpace(line) == domain {
			return true, nil
		}
	}
	return false, nil
}

// removeListedDomain destroys a listed domain unless it is shut off, then undefines it;
// a domain libvirt does not list is no error (E59).
func (c *cli) removeListedDomain(ctx context.Context, domain string) error {
	listed, err := c.domainListed(ctx, domain)
	if err != nil || !listed {
		return err
	}
	state, err := c.domstate(ctx, domain)
	if err != nil {
		return failf("cannot read the state of %s", domain)
	}
	if state != stateShutOff {
		if err := c.virshShow(ctx, "destroy", domain); err != nil {
			return failf("could not destroy %s: %v", domain, err)
		}
	}
	if err := c.virshShow(ctx, "undefine", "--nvram", domain); err != nil {
		return failf("could not undefine %s: %v", domain, err)
	}
	c.logf("removed guest %s", domain)
	return nil
}

// virshShow runs a virsh verb for its effect, its output on stderr.
func (c *cli) virshShow(ctx context.Context, args ...string) error {
	_, err := c.env.Runner.Run(ctx, execx.Cmd{
		Name: virshBin, Args: append([]string{"-c", libvirtURI}, args...),
		Stdout: c.env.Stderr, Stderr: c.env.Stderr,
	})
	return ctxErr(ctx, err)
}

// overlay creates a copy-on-write overlay of base at path.
func (c *cli) overlay(ctx context.Context, base, path string) error {
	_, err := c.env.Runner.Run(ctx, execx.Cmd{
		Name: qemuImgBin, Args: []string{"create", "-q", "-f", formatQcow2, "-F", formatQcow2, "-b", base, path},
		Stdout: c.env.Stderr, Stderr: c.env.Stderr,
	})
	if err != nil {
		return ctxErr(ctx, failf("could not create the overlay %s: %v", path, err))
	}
	return nil
}

func passtNetwork(port int) string {
	return "type=user,backend.type=passt,portForward0.proto=tcp,portForward0.range0.start=" + strconv.Itoa(port) + ",portForward0.range0.to=22"
}

// bootOverlay boots a guest from an overlay (E34): display and serial are the profile's.
func (c *cli) bootOverlay(ctx context.Context, g *guest, b baseFacts, disk string, display []string, serial string) error {
	tail := append(append([]string{}, display...), "--serial", serial)
	return c.virtInstall(ctx, g, b, "path="+disk+",format=qcow2,bus=virtio,discard=unmap,cache=unsafe", tail...)
}

// virtInstall boots a guest in the shape every lab guest shares (E18, E34); the caller gives
// the disk and what follows it (display, serial port, cloud-init).
func (c *cli) virtInstall(ctx context.Context, g *guest, b baseFacts, disk string, tail ...string) error {
	args := []string{
		flagConnect, libvirtURI, flagName, g.domain, "--memory", strconv.Itoa(b.RAMMiB), "--vcpus", strconv.Itoa(b.VCPUs),
		flagCPU, "host-passthrough", flagImport, flagDisk, disk,
		"--osinfo", "fedora-unknown", "--boot", "uefi", "--memballoon", virtioModel, "--network", passtNetwork(g.port),
	}
	args = append(append(args, tail...), "--noautoconsole")
	if _, err := c.env.Runner.Run(ctx, execx.Cmd{Name: virtInstallBin, Args: args, Stdout: c.env.Stderr, Stderr: c.env.Stderr}); err != nil {
		return ctxErr(ctx, failf("virt-install could not boot %s: %v", g.domain, err))
	}
	return nil
}

// floor asks retention whether the host can carry one more disk and guest (§10); its
// VMTEST-DISK and VMTEST-RAM lines go to stderr.
func (c *cli) floor(ctx context.Context, path string, size int64, step string, ram int) error {
	_, err := c.helper(ctx, helperCall{module: "retention", stdout: c.env.Stderr, args: []string{
		"floor", "--path", path, "--base-size", strconv.FormatInt(size, 10), "--step", step, "--guest-mib", strconv.Itoa(ram),
	}})
	return ctxErr(ctx, err)
}

func (c *cli) cmdMemberUp(ctx context.Context, args []string) error {
	if len(args) != 2 {
		return usagef("usage: vmtest member-up <name> <base-key>")
	}
	name, key := args[0], args[1]
	if err := memberName(name); err != nil {
		return err
	}
	l, err := c.loadLab()
	if err != nil {
		return err
	}
	return c.memberUp(ctx, l, name, key)
}

// memberUp boots a member and leaves it running (E56); one that does not finish is torn
// down (E57).
func (c *cli) memberUp(ctx context.Context, l *lab, name, key string) error {
	b, err := c.baseFacts(ctx, l, key)
	if err != nil {
		return err
	}
	if b.Profile != profileServer {
		return failf("a member boots a server base; %s is %s", b.Name, b.Profile)
	}
	baseDir, dir, domain := l.at(basesDir, b.Name), l.at(membersDir, name), memberDomain(name)
	if !readable(filepath.Join(baseDir, "base.qcow2")) || !readable(filepath.Join(baseDir, "base.json")) {
		return failf("base %s is absent; build it with: vmtest ensure-base %s", b.Name, key)
	}
	listed, err := c.domainListed(ctx, domain)
	if err != nil {
		return err
	}
	if _, serr := os.Lstat(dir); serr == nil || listed {
		return failf("member %s already exists (%s or guest %s); remove it with: vmtest member-down %s", name, dir, domain, name)
	}
	v, err := c.verdictOnce(ctx, l, baseDir)
	if err != nil {
		return err
	}
	if v.refused {
		c.p.Print(c.env.Stderr, v.line+"\n")
		return failf("base %s may not be used (freshness: %s); refresh it with: vmtest ensure-base %s", b.Name, v.decision, key)
	}
	if err = os.MkdirAll(l.at(membersDir), 0o755); err != nil {
		return failf("cannot create %s: %v", l.at(membersDir), err)
	}
	if err = c.floor(ctx, l.at(membersDir), v.size, "run", b.RAMMiB); err != nil {
		return ctxErr(ctx, failf("the host cannot carry a member on %s; see the VMTEST-DISK/VMTEST-RAM lines above", b.Name))
	}
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return failf("cannot create %s: %v", dir, err)
	}
	if err = c.startMember(ctx, l, b, name, dir); err != nil {
		c.memberTeardown(context.WithoutCancel(ctx), name, dir, domain)
		return err
	}
	c.sayf("VMTEST-MEMBER-UP %s domain=%s", name, domain)
	return nil
}

// startMember boots the member's overlay of its base and waits for its SSH.
func (c *cli) startMember(ctx context.Context, l *lab, b baseFacts, name, dir string) error {
	port, err := c.env.FreePort()
	if err != nil {
		return failf("%v", err)
	}
	g := &guest{c: c, key: l.at(sshKeyFile), domain: memberDomain(name), port: port, dir: dir}
	overlay := filepath.Join(dir, "overlay.qcow2")
	if err = c.overlay(ctx, l.at(basesDir, b.Name, "base.qcow2"), overlay); err != nil {
		return err
	}
	c.logf("member %s: booting %s on %s (SSH forwarded to 127.0.0.1:%d)", name, g.domain, b.Name, port)
	if err = c.bootOverlay(ctx, g, b, overlay, []string{flagGraphics, displayNone}, "file,path="+filepath.Join(dir, "console.log")); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, memberPortFile), []byte(strconv.Itoa(port)+"\n"), 0o644); err != nil {
		return failf("cannot record the member's port: %v", err)
	}
	return g.waitSSH(ctx)
}

// memberTeardown removes what a member-up that did not finish left: the guest, the overlay and
// the port (which another process may since have taken). The directory and its console log
// stay for diagnosis until member-down (E57). Failures here are reported, not fatal.
func (c *cli) memberTeardown(ctx context.Context, name, dir, domain string) {
	if err := c.removeListedDomain(ctx, domain); err != nil {
		c.logf("could not remove guest %s: %v", domain, err)
	}
	for _, f := range []string{"overlay.qcow2", memberPortFile} {
		if err := os.Remove(filepath.Join(dir, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			c.logf("could not remove %s: %v", filepath.Join(dir, f), err)
		}
	}
	c.logf("member not started; its console log is kept in %s until: vmtest member-down %s", dir, name)
}

// cmdMemberSSH runs a command in a member, bounded, and exits with its status (E58).
func (c *cli) cmdMemberSSH(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return usagef("usage: vmtest member-ssh <name> <command>")
	}
	name := args[0]
	if err := memberName(name); err != nil {
		return err
	}
	portFile := c.paths.at(membersDir, name, memberPortFile)
	body, err := os.ReadFile(portFile)
	if err != nil {
		return failf("no member %s is up (no %s); start one with: vmtest member-up %s <base-key>", name, portFile, name)
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil || port <= 0 {
		return failf("%s does not hold a port", portFile)
	}
	key := c.paths.at(sshKeyFile)
	if !readable(key) {
		return failf("no lab SSH key at %s; deploy the lab first", key)
	}
	limit := memberSSHDefault
	if raw := c.env.Getenv("VMTEST_MEMBER_SSH_TIMEOUT"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit <= 0 || strconv.Itoa(limit) != raw {
			return failf("VMTEST_MEMBER_SSH_TIMEOUT must be a positive number of seconds, got '%s'", raw)
		}
	}
	g := &guest{c: c, key: key, port: port}
	// Bounded twice: keepalives end a session whose guest stopped answering, and the bound
	// ends one whose command never finishes.
	sshArgs := g.sshArgs([]string{"-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=4"}, args[1])
	sshArgs = append(sshArgs, args[2:]...)
	bounded, cancel := c.bound(ctx, time.Duration(limit)*time.Second)
	defer cancel()
	_, runErr := c.env.Runner.Run(bounded, execx.Cmd{Name: sshBin, Args: sshArgs, Stdin: c.env.Stdin, Stdout: c.env.Stdout, Stderr: c.env.Stderr})
	code, err := statusOf(ctx, bounded, runErr)
	if err != nil {
		return err
	}
	if code == timedOut {
		c.errorLine("member-ssh " + name + ": the command did not finish within " + strconv.Itoa(limit) + "s (VMTEST_MEMBER_SSH_TIMEOUT)")
	}
	if code != 0 {
		return &exitStatusError{code: term.ExitCode(code)}
	}
	return nil
}

// cmdMemberDown removes a member's guest and directory; nothing there is no error (E59).
func (c *cli) cmdMemberDown(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usagef("usage: vmtest member-down <name>")
	}
	name := args[0]
	if err := memberName(name); err != nil {
		return err
	}
	if err := c.removeListedDomain(ctx, memberDomain(name)); err != nil {
		return err
	}
	dir := c.paths.at(membersDir, name)
	if _, err := os.Lstat(dir); err == nil {
		if err := os.RemoveAll(dir); err != nil {
			return failf("cannot remove %s: %v", dir, err)
		}
		c.logf("removed %s", dir)
	}
	c.sayf("VMTEST-MEMBER-DOWN %s", name)
	return nil
}
