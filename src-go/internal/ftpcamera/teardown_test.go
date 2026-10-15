package ftpcamera

import (
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// hotspotKit is a server kit whose WiFi card can be an access point and whose hotspot profile
// exists; active says whether it is already up.
func hotspotKit(t *testing.T, active bool) *kit {
	t.Helper()
	k := serverKit(t)
	k.path("nmcli")
	k.writeConfig("HOTSPOT_CONNECTION=Camera AP\n")
	onNM(k, "-t -f DEVICE,TYPE device", "wlp1:wifi\n", 0)
	onNM(k, "-g WIFI-PROPERTIES.AP device show wlp1", "yes\n", 0)
	onNM(k, "-t -f NAME connection show", "Wired\nCamera AP\n", 0)
	up := active
	k.fake.OnFunc([]string{"nmcli", "-t", "-f", "NAME", "connection", "show", "--active"}, func(execx.Cmd) (execx.Result, error) {
		if up {
			return execx.Result{Stdout: []byte("Wired\nCamera AP\n")}, nil
		}
		return execx.Result{Stdout: []byte("Wired\n")}, nil
	})
	k.fake.OnFunc([]string{"sudo", "-n", "nmcli", "connection", "up"}, func(execx.Cmd) (execx.Result, error) {
		up = true
		return execx.Result{}, nil
	})
	onNM(k, "-g ipv4.method connection show Wired", "auto\n", 0)
	k.fake.On([]string{"nmcli", "-g", "ipv4.method", "connection", "show", "Camera AP"}, execx.Result{Stdout: []byte("shared\n")})
	k.fake.On([]string{"nmcli", "-g", "GENERAL.DEVICES", "connection", "show", "Camera AP"}, execx.Result{Stdout: []byte("wlp1\n")})
	k.fake.On([]string{"nmcli", "-g", "802-11-wireless.ssid", "connection", "show", "Camera AP"}, execx.Result{Stdout: []byte("ftp-camera\n")})
	ipAddr(k, "wlp1", hotspotIP)
	k.feedOn(true, tailArgv)
	k.feedOn(false, []string{"inotifywait"})
	return k
}

// E44, E48, E49, E56: --hotspot brings the profile up, opens nm-shared, and takes it down.
func TestHotspotOwnedBySession(t *testing.T) {
	k := hotspotKit(t, false)
	ctx := interruptAt(k, time.Second)
	k.wantCode(k.runCtx(ctx, "--hotspot"), term.ExitOK)
	k.wantRan("sudo -n nmcli connection up Camera AP",
		"sudo -n firewall-cmd --zone=nm-shared --add-service=ftp --quiet",
		"sudo -n firewall-cmd --zone=nm-shared --add-port=30000-30100/tcp --quiet",
		"sudo -n nmcli connection down Camera AP")
	k.wantOut("Activating hotspot 'Camera AP'...\n",
		"  Hotspot:      on   (SSID: ftp-camera, IP: "+hotspotIP+", dev: wlp1)\n",
		"                     (managed by ftp-camera, will close on exit)\n",
		"  FTP URL:      ftp://camera@"+hotspotIP+"/\n",
		"Deactivating hotspot 'Camera AP'...\n")
	if k.slept[0] != 2*time.Second {
		t.Errorf("no wait for the AP: %v", k.slept)
	}
}

// E44: an already active hotspot is used and left alone.
func TestHotspotAlreadyUp(t *testing.T) {
	k := hotspotKit(t, true)
	ctx := interruptAt(k, time.Second)
	k.wantCode(k.runCtx(ctx, "--hotspot"), term.ExitOK)
	k.wantOut("Hotspot 'Camera AP' already active — using as-is, will not tear down on exit.\n",
		"(pre-existing, left alone on exit)\n")
	k.wantNotRan("sudo -n nmcli connection")
}

// E44: the refusals.
func TestHotspotRefusals(t *testing.T) {
	k := serverKit(t)
	k.wantCode(k.run("--hotspot"), term.ExitFailure)
	k.wantErr("ERROR: --hotspot needs nmcli (NetworkManager CLI)\n")

	k = hotspotKit(t, false)
	onNM(k, "-t -f NAME connection show", "Wired\n", 0)
	k.wantCode(k.run("--hotspot"), term.ExitFailure)
	k.wantErr("ERROR: NetworkManager connection 'Camera AP' not found\n", "    ./vault.bash set ftp_camera_hotspot_password    # 8-63 chars\n")
	k.wantNotRan("sudo -n systemctl start")

	k = hotspotKit(t, false)
	k.fake.On([]string{"sudo", "-n", "nmcli", "connection", "up"}, execx.Result{ExitCode: 4})
	k.wantCode(k.run("--hotspot"), term.ExitFailure)
	k.wantErr("ERROR: could not bring the hotspot up: nmcli connection up Camera AP: ")
	k.wantNotRan("sudo -n nmcli connection down")
}

// E57: more than twenty files: the summary has no file list.
func TestUploadSummaryManyFiles(t *testing.T) {
	k := serverKit(t)
	k.feedOn(true, tailArgv)
	k.feedOn(false, []string{"inotifywait"})
	for i := range 21 {
		k.put("DCIM/F"+strings.Repeat("x", i)+".dat", []byte("x"), testStart.Add(time.Minute))
	}
	ctx := interruptAt(k, time.Second)
	k.wantCode(k.runCtx(ctx), term.ExitOK)
	k.wantOut("  Files: 21\n  Total: 21B\n")
	if strings.Contains(k.out.String(), "Fxx.dat  1B") {
		t.Error("listed more than twenty files")
	}
}

func TestUploadSummaryNothingReceived(t *testing.T) {
	k := serverKit(t)
	k.feedOn(true, tailArgv)
	k.feedOn(false, []string{"inotifywait"})
	ctx := interruptAt(k, time.Second)
	k.wantCode(k.runCtx(ctx), term.ExitOK)
	k.wantOut("Upload summary:\n  (no files received)\n")
}

// E57: the closing sort pass's failure is a note, not the exit.
func TestClosingSortFailureIsANote(t *testing.T) {
	k := serverKit(t)
	k.feedOn(true, tailArgv)
	k.feedOn(false, []string{"inotifywait"})
	delete(k.onPath, "exiftool")
	ctx := interruptAt(k, time.Second)
	k.wantCode(k.runCtx(ctx), term.ExitOK)
	k.wantErr("ERROR: exiftool not found — run play-photography.yml first\nnote: the sort pass did not complete — continuing with teardown.\n")
}

// E55: an interrupt before the monitors start still tears down, exit 0.
func TestInterruptDuringStart(t *testing.T) {
	k := hotspotKit(t, false)
	ctx := interruptAt(k, 2*time.Second)
	k.wantCode(k.runCtx(ctx, "--hotspot"), term.ExitOK)
	k.wantOut("FTP server stopped.\n")
	k.wantNotRan("sudo -n systemctl start")
}
