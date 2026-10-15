package vmtest

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	netinst    = "Fedora-Everything-netinst-x86_64-44-1.1.iso"
	liveISO    = "Fedora-Workstation-Live-44-1.1.iso"
	guestRev   = "1700000001"
	repoinfo   = "Repo-id            : updates\nRevision           : " + guestRev + "\nBase URL           : https://mirror.example.org/updates/44/ (12 more)\n"
	recordJSON = "{\"name\": \"server-fast-44\"}\n"
)

// guestWorld answers what the guest is asked over ssh and what libvirt does to it.
type guestWorld struct {
	repoinfo      string
	cleanupOutput string
	installerLog  string
	afterInstall  string // the domain's state once the installer returns
	// remote answers a remote command the world does not know; nil is success with no output.
	remote func(remote string) (execx.Result, bool)
}

// serveMedia publishes every base's media with its CHECKSUM file and returns the probe that
// lists them.
func (k *kit) serveMedia() string {
	k.served = map[string]string{}
	var probe strings.Builder
	probe.WriteString("VMTEST-FRESHNESS-COMPOSE-ID Fedora-44-20261001.0\nVMTEST-FRESHNESS-REVISION " + testProbeRev + "\n")
	probe.WriteString("VMTEST-FRESHNESS-TREE-URL everything https://dl.example.org/44/Everything/x86_64/os\n")
	for _, a := range []struct{ base, file, variant string }{
		{"server-fast-44", imageName, "Cloud"},
		{"server-full-44", netinst, "Everything"},
		{"desktop-44", netinst, "Everything"},
		{"desktop-44", liveISO, "Workstation"},
	} {
		dir := "https://dl.example.org/44/" + a.variant + "/x86_64/"
		body := "media of " + a.file
		k.served[dir+a.file] = body
		entry := "SHA256 (" + a.file + ") = " + sum(body) + "\n"
		if checksum := dir + "Fedora-" + a.variant + "-44-1.1-x86_64-CHECKSUM"; !strings.Contains(k.served[checksum], entry) {
			k.served[checksum] += entry
		}
		probe.WriteString("VMTEST-FRESHNESS-ARTEFACT " + a.base + " " + a.file + " " + sum(body) + " label=44-1.1 variant=" + a.variant + " url=" + dir + a.file + "\n")
	}
	return probe.String()
}

// buildKit is a lab that can build every base: the probe lists each base's media, the web
// serves it, libvirt boots, and the guest upgrades, cleans and powers off.
func buildKit(t *testing.T) *kit {
	t.Helper()
	k := newKit(t)
	k.base("desktop", "desktop-44", "full", "desktop", "everything")
	k.base("server-full", "server-full-44", "full", "server", "everything")
	w := &guestWorld{
		repoinfo: repoinfo, cleanupOutput: "cleaned\nVMTEST-GUEST-CLEANUP-DONE\n",
		installerLog: "Running post-installation scripts\nInstallation complete\n", afterInstall: stateShutOff,
	}
	k.gw = w
	k.fake.On(helperArgv("probe_upstream"), execx.Result{Stdout: []byte(k.serveMedia())})
	k.withWeb()
	lv := k.withLibvirt()
	k.fake.OnFunc([]string{"virt-install"}, func(c execx.Cmd) (execx.Result, error) {
		if lv.virtFails {
			return execx.Result{ExitCode: 1}, nil
		}
		lv.domains = append(lv.domains, argAfter(c.Args, "--name"))
		lv.state = "running"
		if slices.Contains(c.Args, "--location") {
			lv.state = w.afterInstall
			dir := filepath.Dir(argAfter(c.Args, "--initrd-inject"))
			k.writeAbs(filepath.Join(dir, "console.log"), w.installerLog)
		}
		return execx.Result{}, nil
	})
	k.fake.OnFunc([]string{"virsh", "-c", libvirtURI, "start"}, func(execx.Cmd) (execx.Result, error) {
		lv.state = "running"
		return execx.Result{}, nil
	})
	k.fake.OnFunc([]string{"qemu-img"}, func(c execx.Cmd) (execx.Result, error) {
		if c.Args[0] == "create" {
			k.writeAbs(createdImage(c.Args), "overlay")
		}
		return execx.Result{}, nil
	})
	k.fake.OnFunc([]string{"cp"}, func(c execx.Cmd) (execx.Result, error) {
		body, err := os.ReadFile(c.Args[1])
		if err != nil {
			return execx.Result{}, err
		}
		k.writeAbs(c.Args[2], string(body)+" flattened")
		return execx.Result{}, nil
	})
	k.fake.On([]string{"scp"}, execx.Result{})
	k.fake.OnFunc([]string{"ssh"}, func(c execx.Cmd) (execx.Result, error) {
		remote := c.Args[len(c.Args)-1]
		if w.remote != nil {
			if res, ok := w.remote(remote); ok {
				return res, nil
			}
		}
		switch {
		case remote == "sudo dnf --quiet repoinfo updates":
			return execx.Result{Stdout: []byte(w.repoinfo)}, nil
		case remote == "sudo bash /tmp/guest-cleanup.bash":
			return execx.Result{Stdout: []byte(w.cleanupOutput)}, nil
		case strings.HasSuffix(remote, "systemctl poweroff"):
			lv.state = stateShutOff
		}
		return execx.Result{}, nil
	})
	k.fake.On([]string{"blkid"}, execx.Result{Stdout: []byte("Fedora-E-dvd-x86_64-44\n")})
	k.fake.On([]string{"openssl"}, execx.Result{Stdout: []byte("$6$salt$crypt\n")})
	k.fake.On(helperArgv("render_kickstart"), execx.Result{Stdout: []byte("rendered kickstart\n")})
	k.fake.On(helperArgv("serial_console"), execx.Result{Stdout: []byte("VMTEST-CONSOLE userspace\n")})
	k.fake.On(helperArgv("write_base_record"), execx.Result{Stdout: []byte(recordJSON)})
	return k
}

// E15-E21: a fast server base, end to end.
func TestE15FastBaseBuild(t *testing.T) {
	k := buildKit(t)
	k.wantCode(k.run("build-base", "server-fast"), term.ExitOK)
	disk := k.path("bases/server-fast-44/base.qcow2")
	if k.out.String() != "VMTEST-BASE-BUILT server-fast-44 "+disk+" refresh_state=complete\n" {
		t.Fatalf("stdout %q", k.out.String())
	}
	info, err := os.Stat(disk)
	if err != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("the base disk is %v %v", info, err)
	}
	if k.read("bases/server-fast-44/base.json") != recordJSON || k.exists("bases/server-fast-44.build") || k.exists("bases/server-fast-44/base.json.new") {
		t.Fatal("the record or the build directory is wrong")
	}
	order := []string{
		"retention floor --path " + k.path("bases") + " --base-size 35 --step rebuild --guest-mib 4096",
		"virsh -c qemu:///session dominfo vmtest-build-server-fast-44",
		"cp --reflink=auto " + k.path("images/"+imageName) + " " + k.path("bases/server-fast-44.build/disk.qcow2"),
		"qemu-img resize -q " + k.path("bases/server-fast-44.build/disk.qcow2") + " 20G",
		"--cloud-init clouduser-ssh-key=" + k.path(sshKeyFile) + ".pub,disable=on --graphics none --serial file,path=" + k.path("bases/server-fast-44.build/console.log") + " --noautoconsole",
		"fedora@127.0.0.1 sudo dnf -y --refresh upgrade",
		"fedora@127.0.0.1 sudo dnf --quiet repoinfo updates",
		guestCleanup + " fedora@127.0.0.1:/tmp/guest-cleanup.bash",
		"fedora@127.0.0.1 sudo bash /tmp/guest-cleanup.bash",
		"fedora@127.0.0.1 sudo systemd-run --quiet --on-active=2 systemctl poweroff",
		"virsh -c qemu:///session undefine --nvram vmtest-build-server-fast-44",
		"helpers.vmtest.write_base_record",
	}
	assertInOrder(t, k, order)
	rec := k.mustCall("write_base_record").Line()
	for _, want := range []string{
		"--fedora-version 44 --profile server --kind fast --name server-fast-44 --compose-id Fedora-44-20261001.0 --compose-label 44-1.1",
		"--artefact " + imageName + "=" + sum("media of "+imageName),
		"--recipe-digest " + sum("build-recipe-version=1\n", "cleanup\n"),
		"--installed-at 1792065600 --last-upgraded-at 1792065600",
		"--last-upgraded-revision " + guestRev + " --last-upgraded-mirror https://mirror.example.org/updates/44/ --refresh-state complete",
		"--base-sha256 " + sum("media of "+imageName+" flattened"),
		"--base-size 45 --base-mtime ",
	} {
		if !strings.Contains(rec, want) {
			t.Fatalf("the record lacks %q:\n%s", want, rec)
		}
	}
	if strings.Contains(rec, "--treeinfo-probe") || k.count("vmtest-session-provision") != 0 {
		t.Fatal("a fast server base got a full or desktop step")
	}
	k.wantErr("==> guest saw updates revision " + guestRev + " from https://mirror.example.org/updates/44/ (probe read " + testProbeRev + ")")
	k.wantErr("==> base server-fast-44 built: " + disk + " (45 bytes, refresh complete)")
	assertNoProbeFiles(t, k)
}

func assertInOrder(t *testing.T, k *kit, subs []string) {
	t.Helper()
	calls := k.lines("")
	i := 0
	for _, line := range calls {
		if i < len(subs) && strings.Contains(line, subs[i]) {
			i++
		}
	}
	if i != len(subs) {
		t.Fatalf("missing, or out of order: %q\ncalls:\n%s", subs[i], strings.Join(calls, "\n"))
	}
}

// E21: a guest whose mirror lags the canonical host leaves the base incomplete.
func TestE21RefreshStateIncomplete(t *testing.T) {
	k := buildKit(t)
	k.gw.repoinfo = strings.Replace(repoinfo, guestRev, "1600000000", 1)
	k.wantCode(k.run("build-base", "server-fast"), term.ExitOK)
	if !strings.HasSuffix(k.out.String(), "refresh_state=incomplete\n") || k.count("--refresh-state incomplete") != 1 {
		t.Fatalf("stdout %q", k.out.String())
	}
}

// E15, E17, E16, E20: the refusals, each before the step it guards.
func TestE15BuildRefusals(t *testing.T) {
	k := buildKit(t)
	k.base("odd", "odd-44", "slow", "server", "-")
	k.wantCode(k.run("build-base", "odd"), term.ExitFailure)
	k.wantErr("ERROR: unknown base kind slow for odd-44")

	k = buildKit(t)
	k.base("desk-fast", "desk-fast-44", "fast", "desktop", "-")
	k.wantCode(k.run("build-base", "desk-fast"), term.ExitFailure)
	k.wantErr("ERROR: a desktop base must be kind full; desk-fast-44 is fast")
	if k.count("curl") != 0 {
		t.Fatal("a refused base was fetched")
	}

	k = buildKit(t)
	k.fake.On(helperArgv("retention"), execx.Result{ExitCode: 1})
	k.wantCode(k.run("build-base", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: the host cannot carry a rebuild of server-fast-44 (stage: base); see the VMTEST-DISK/VMTEST-RAM lines above")
	if k.count("virt-install") != 0 {
		t.Fatal("built past a refused floor")
	}

	k = buildKit(t)
	k.lv.domains = []string{"vmtest-build-server-fast-44"}
	k.write("bases/server-fast-44.build/console.log", "the last attempt")
	k.wantCode(k.run("build-base", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: a domain named vmtest-build-server-fast-44 already exists; a previous build did not clean up. Inspect it, then: virsh -c qemu:///session undefine --nvram vmtest-build-server-fast-44")
	if k.read("bases/server-fast-44.build/console.log") != "the last attempt" || k.count("undefine") != 0 {
		t.Fatal("a refused build touched the previous one")
	}

	k = buildKit(t)
	k.gw.repoinfo = "Repo-id : updates\n"
	k.wantCode(k.run("build-base", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: guest repoinfo has no Revision line")

	k = buildKit(t)
	k.gw.repoinfo = strings.Replace(repoinfo, guestRev, "17x", 1)
	k.wantCode(k.run("build-base", "server-fast"), term.ExitFailure)
	k.wantErr("is not a whole number")
}

// E20, E27: a failed cleanup tears the domain down and keeps the build directory.
func TestE27FailedBuildTeardown(t *testing.T) {
	k := buildKit(t)
	k.gw.cleanupOutput = "VMTEST-GUEST-CLEANUP-DONE-ISH\n"
	k.wantCode(k.run("build-base", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: guest cleanup did not complete")
	if k.count("destroy vmtest-build-server-fast-44") != 1 || k.count("undefine --nvram vmtest-build-server-fast-44") != 1 {
		t.Fatalf("calls %v", k.lines("virsh"))
	}
	if !k.exists("bases/server-fast-44.build") || k.exists("bases/server-fast-44/base.qcow2") {
		t.Fatal("the failed build's directory was removed, or a base was made")
	}
	k.wantErr("==> build directory kept for diagnosis: " + k.path("bases/server-fast-44.build"))
	assertNoProbeFiles(t, k)
}

// E4, E27: a signal in the build ends it 143 with the domain torn down.
func TestE27BuildSignal(t *testing.T) {
	k := buildKit(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	k.gw.remote = func(string) (execx.Result, bool) {
		cancel(InterruptError{Signal: syscall.SIGTERM})
		return execx.Result{ExitCode: 255}, true
	}
	k.wantCode(k.runCtx(ctx, "build-base", "server-fast"), 143)
	if k.count("destroy vmtest-build-server-fast-44") != 1 || k.count("undefine --nvram vmtest-build-server-fast-44") != 1 {
		t.Fatalf("calls %v", k.lines("virsh"))
	}
}

// The port of scripts/test-vmtest-build-dir-cleanup.bash: refresh-base all builds several
// bases in one process; each successful build removes its own directory at once, and a failed
// build after them keeps its own.
func TestE21BuildDirectoriesAcrossOneRefresh(t *testing.T) {
	k := buildKit(t)
	var probe strings.Builder
	probe.WriteString("VMTEST-FRESHNESS-COMPOSE-ID c\nVMTEST-FRESHNESS-REVISION " + testProbeRev + "\n")
	for _, key := range []string{"server-a", "server-b", "server-c"} {
		k.base(key, key+"-44", "fast", "server", "-")
		k.builtBase(key + "-44")
		probe.WriteString("VMTEST-FRESHNESS-ARTEFACT " + key + "-44 " + imageName + " " + sum("media of "+imageName) + " label=44-1.1 variant=Cloud url=https://dl.example.org/44/Cloud/x86_64/" + imageName + "\n")
	}
	k.fake.On(helperArgv("probe_upstream"), execx.Result{Stdout: []byte(probe.String())})
	builds := 0
	k.gw.remote = func(remote string) (execx.Result, bool) {
		if remote != "sudo bash /tmp/guest-cleanup.bash" {
			return execx.Result{}, false
		}
		builds++
		if builds == 3 {
			return execx.Result{Stdout: []byte("no marker\n")}, true
		}
		return execx.Result{}, false
	}
	k.wantCode(k.run("refresh-base", "all"), term.ExitFailure)
	for _, n := range []string{"server-a-44", "server-b-44"} {
		if k.exists("bases/" + n + ".build") {
			t.Fatalf("%s: a successful build's directory was kept", n)
		}
	}
	if !k.exists("bases/server-c-44.build") {
		t.Fatal("the failed build's directory was removed")
	}
	if builds != 3 || k.count("probe_upstream") != 3 {
		t.Fatalf("builds %d probes %d", builds, k.count("probe_upstream"))
	}
	assertNoProbeFiles(t, k)
}

// E19: a full server base installs from the tree with the rendered kickstart.
func TestE19FullServerBase(t *testing.T) {
	k := buildKit(t)
	k.wantCode(k.run("build-base", "server-full"), term.ExitOK)
	render := k.mustCall("render_kickstart")
	if !strings.Contains(render.Line(), "--template "+k.path(ksServer)+" --set SSH_PUBKEY=ssh-ed25519 AAAA vmtest-lab --set FEDORA_VERSION=44 --set TREE_URL=https://dl.example.org/44/Everything/x86_64/os") ||
		strings.Contains(render.Line(), "--set-stdin") {
		t.Fatalf("got %s", render.Line())
	}
	install := k.mustCall("--location").Line()
	for _, want := range []string{
		"--location " + k.path("images/"+netinst) + " --initrd-inject " + k.path("bases/server-full-44.build/ks.cfg"),
		"--extra-args inst.ks=file:/ks.cfg inst.stage2=hd:LABEL=Fedora-E-dvd-x86_64-44 console=ttyS0,115200n8 plymouth.enable=0",
		"--boot uefi,firmware.feature0.name=secure-boot,firmware.feature0.enabled=no",
		"--serial file,path=" + k.path("bases/server-full-44.build/console.log") + " --noautoconsole --noreboot --wait 90",
	} {
		if !strings.Contains(install, want) {
			t.Fatalf("the install lacks %q:\n%s", want, install)
		}
	}
	if strings.Contains(install, "device=cdrom") || k.count("serial_console") != 0 {
		t.Fatal("a server install got the desktop's media or console")
	}
	if k.exists("bases/server-full-44.build/ks.cfg") {
		t.Fatal("the rendered kickstart outlived the install")
	}
	assertInOrder(t, k, []string{"qemu-img create -q -f qcow2 " + k.path("bases/server-full-44.build/disk.qcow2") + " 20G", "--location", "virsh -c qemu:///session start vmtest-build-server-full-44", "sudo dnf -y --refresh upgrade"})
	rec := k.mustCall("write_base_record").Line()
	if !strings.Contains(rec, "--treeinfo-probe ") || !strings.Contains(rec, " --tree everything") {
		t.Fatal("a full base's record lacks its tree")
	}
	k.wantErr("==> installing from https://dl.example.org/44/Everything/x86_64/os via " + netinst + " (Anaconda, cmdline mode on the serial console; up to 90 min)")
}

// E19: the installer's failures, each named.
func TestE19InstallerFailures(t *testing.T) {
	k := buildKit(t)
	k.gw.afterInstall = "running"
	k.wantCode(k.run("build-base", "server-full"), term.ExitFailure)
	k.wantErr("ERROR: the installer did not power off within 90 min (state: running); see " + k.path("bases/server-full-44.build/console.log"))

	k = buildKit(t)
	k.gw.installerLog = "kernel panic\n"
	k.wantCode(k.run("build-base", "server-full"), term.ExitFailure)
	k.wantErr("ERROR: the installer powered off without reporting completion; see " + k.path("bases/server-full-44.build/console.log"))

	k = buildKit(t)
	k.fake.On([]string{"blkid"}, execx.Result{Stdout: []byte("\n")})
	k.wantCode(k.run("build-base", "server-full"), term.ExitFailure)
	k.wantErr("ERROR: could not read the volume label of " + netinst)
	if k.count("--location") != 0 {
		t.Fatal("installed without a stage-2 label")
	}
}

// E19: a desktop base's two secrets reach the renderer and openssl on stdin only, and the
// LUKS passphrase is kept 0600 beside the base.
func TestE19DesktopBaseSecrets(t *testing.T) {
	k := buildKit(t)
	k.wantCode(k.run("build-base", "desktop"), term.ExitOK)
	render := k.mustCall("render_kickstart")
	stdin := stdinText(t, render)
	passphrase := strings.Repeat("BwcH", 8) // base64 of 24 bytes of 7
	if stdin != "LUKS_PASSPHRASE="+passphrase+"\nUSER_PASSWORD_CRYPT=$6$salt$crypt\n" {
		t.Fatalf("render stdin %q", stdin)
	}
	if !strings.HasSuffix(render.Line(), "--set FEDORA_VERSION=44 --set-stdin") || strings.Contains(render.Line(), "TREE_URL") {
		t.Fatalf("got %s", render.Line())
	}
	for _, c := range k.fake.Calls() {
		if strings.Contains(c.Line(), passphrase) || strings.Contains(c.Line(), "$6$salt") {
			t.Fatalf("a secret on a command line: %s", c.Line())
		}
	}
	if got := stdinText(t, k.mustCall("openssl passwd -6 -stdin")); got != passphrase+"\n" {
		t.Fatalf("openssl stdin %q", got)
	}
	pp := k.path("bases/desktop-44/luks-passphrase")
	info, err := os.Stat(pp)
	if err != nil || info.Mode().Perm() != 0o600 || k.read("bases/desktop-44/luks-passphrase") != passphrase+"\n" {
		t.Fatalf("passphrase %v %v", info, err)
	}
}

// E19: a desktop base boots both media, unlocks LUKS on the serial socket, installs the
// session runner and records both media.
func TestE19DesktopBaseMediaAndRunner(t *testing.T) {
	k := buildKit(t)
	k.wantCode(k.run("build-base", "desktop"), term.ExitOK)
	install := k.mustCall("--location").Line()
	if !strings.Contains(install, "--disk path="+k.path("images/"+liveISO)+",device=cdrom,readonly=on") ||
		!strings.Contains(install, "--serial unix,path="+k.path("bases/desktop-44.build/serial.sock")+",mode=bind") {
		t.Fatalf("got %s", install)
	}
	if k.count("--follow") != 2 || k.count("--unlock-with "+k.path("bases/desktop-44.build/luks-passphrase")) != 1 {
		t.Fatalf("serial console calls %v", k.lines("serial_console"))
	}
	assertInOrder(t, k, []string{
		"vmtest-session-provision.bash fedora@127.0.0.1:/tmp/vmtest-session-provision.bash",
		"sudo install -o root -g root -m 0755 /tmp/vmtest-session-provision.bash /usr/local/bin/vmtest-session-provision.bash",
		"rm -f /tmp/vmtest-session-provision.bash",
		"sudo bash /tmp/guest-cleanup.bash",
	})
	rec := k.mustCall("write_base_record").Line()
	if !strings.Contains(rec, "--artefact "+netinst+"="+sum("media of "+netinst)+" --artefact "+liveISO+"="+sum("media of "+liveISO)) {
		t.Fatal("the desktop record does not name both media")
	}
	if !strings.Contains(rec, "--recipe-digest "+sum("build-recipe-version=1\n", "cleanup\n", "ks desktop\n", "runner\n")) {
		t.Fatal("the desktop recipe digest")
	}
}
