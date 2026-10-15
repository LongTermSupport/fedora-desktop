package vmtest

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	imageName    = "Fedora-Cloud-Base-44.qcow2"
	imageBody    = "the published image"
	checksumName = "Fedora-Cloud-44-1.1-x86_64-CHECKSUM"
	imageDir     = "https://dl.example.org/44/Cloud/x86_64/images/"
	imageURL     = imageDir + imageName
)

// withWeb installs a web server holding k.served, and a gpg that verifies.
func (k *kit) withWeb() {
	k.fake.OnFunc([]string{"curl"}, func(c execx.Cmd) (execx.Result, error) {
		body, ok := k.served[c.Args[len(c.Args)-1]]
		if !ok {
			return execx.Result{Stderr: []byte("curl: (22) 404\n"), ExitCode: 22}, nil
		}
		k.writeAbs(argAfter(c.Args, "-o"), body)
		return execx.Result{}, nil
	})
	k.fake.On([]string{"gpg2"}, execx.Result{})
}

// fetchKit is a lab whose probe lists one image, served with its signed CHECKSUM.
func fetchKit(t *testing.T) *kit {
	t.Helper()
	k := newKit(t)
	sha := sum(imageBody)
	k.fake.On(helperArgv("probe_upstream"), execx.Result{Stdout: []byte(
		"VMTEST-FRESHNESS-COMPOSE-ID Fedora-44-20261001.0\nVMTEST-FRESHNESS-REVISION " + testProbeRev + "\n" +
			"VMTEST-FRESHNESS-ARTEFACT server-fast-44 " + imageName + " " + sha + " label=44-1.1 variant=Cloud url=" + imageURL + "\n")})
	k.served = map[string]string{
		imageURL:                imageBody,
		imageDir + checksumName: "-----BEGIN PGP SIGNED MESSAGE-----\nSHA256 (" + imageName + ") = " + sha + "\n",
	}
	k.withWeb()
	return k
}

// E14: download, signed CHECKSUM, agreement with the probe, hash.
func TestE14FetchVerifiesTheArtefact(t *testing.T) {
	k := fetchKit(t)
	k.wantCode(k.run("fetch", "server-fast"), term.ExitOK)
	if k.read("images/"+imageName) != imageBody || k.exists("images/"+imageName+".partial") {
		t.Fatal("the image was not downloaded into place")
	}
	curl := k.lines("curl ")
	if len(curl) != 2 || curl[0] != "curl -fL --progress-bar -o "+k.path("images/"+imageName+".partial")+" "+imageURL ||
		!strings.HasSuffix(curl[1], "/images/"+checksumName) {
		t.Fatalf("got %v", curl)
	}
	gpg := k.lines("gpg2")
	if len(gpg) != 2 || !strings.Contains(gpg[0], "--quiet --import "+k.gpg+"/RPM-GPG-KEY-fedora-44-primary") ||
		!strings.HasSuffix(gpg[1], "--verify "+k.path("images/"+checksumName)) {
		t.Fatalf("got %v", gpg)
	}
	k.wantErr("==> downloaded " + imageName + " (19 bytes)")
	k.wantErr("==> GPG signature verified on " + checksumName)
	k.wantErr("==> verified " + imageName + " against the signed CHECKSUM and releases.json")
	if k.out.Len() != 0 {
		t.Fatalf("fetch printed a payload: %q", k.out.String())
	}
	assertNoProbeFiles(t, k)
	home := argAfter(strings.Fields(gpg[0]), "--homedir")
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("the gpg home %s was left behind", home)
	}

	// A second fetch asks only for what changed, and keeps the copy the server did not resend.
	k.fake.OnFunc([]string{"curl"}, func(c execx.Cmd) (execx.Result, error) {
		k.writeAbs(argAfter(c.Args, "-o"), "")
		return execx.Result{}, nil
	})
	k.wantCode(k.run("fetch", "server-fast"), term.ExitOK)
	if !slices.Contains(k.lines("curl "), "curl -fL --progress-bar -o "+k.path("images/"+imageName+".partial")+" -z "+k.path("images/"+imageName)+" "+imageURL) {
		t.Fatalf("no conditional GET: %v", k.lines("curl "))
	}
	k.wantErr("==> " + imageName + " is current (not modified upstream)")
}

func TestE14FetchRefusals(t *testing.T) {
	k := fetchKit(t)
	delete(k.served, imageURL)
	k.wantCode(k.run("fetch", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: download failed: " + imageURL)
	if k.exists("images/" + imageName + ".partial") {
		t.Fatal("the partial download was kept")
	}

	k = fetchKit(t)
	k.served[imageURL] = ""
	k.wantCode(k.run("fetch", "server-fast"), term.ExitFailure)
	k.wantErr("no local copy and the server sent nothing: " + imageURL)

	k = fetchKit(t)
	k.fake.OnFunc([]string{"gpg2"}, func(c execx.Cmd) (execx.Result, error) {
		if slices.Contains(c.Args, "--verify") {
			return execx.Result{Stderr: []byte("gpg: BAD signature\n"), ExitCode: 1}, nil
		}
		return execx.Result{}, nil
	})
	k.wantCode(k.run("fetch", "server-fast"), term.ExitFailure)
	k.wantErr("gpg: BAD signature\n")
	k.wantErr("ERROR: GPG signature verification FAILED for " + checksumName + " — refusing the artefact")

	k = fetchKit(t)
	k.fake.On([]string{"gpg2"}, execx.Result{Stderr: []byte("gpg: no valid OpenPGP data found\n"), ExitCode: 2})
	k.wantCode(k.run("fetch", "server-fast"), term.ExitFailure)
	k.wantErr("gpg: no valid OpenPGP data found\n")
	k.wantErr("ERROR: could not import " + k.gpg + "/RPM-GPG-KEY-fedora-44-primary")

	k = fetchKit(t)
	if err := os.Remove(k.gpg + "/RPM-GPG-KEY-fedora-44-primary"); err != nil {
		t.Fatal(err)
	}
	k.wantCode(k.run("fetch", "server-fast"), term.ExitFailure)
	k.wantErr("no Fedora 44 release key at")

	k = fetchKit(t)
	k.served[imageDir+checksumName] = "SHA256 (other.qcow2) = " + sum("x") + "\n"
	k.wantCode(k.run("fetch", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: " + checksumName + " has no SHA256 entry for " + imageName)

	k = fetchKit(t)
	k.served[imageDir+checksumName] = "SHA256 (" + imageName + ") = " + sum("x") + "\n"
	k.wantCode(k.run("fetch", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: releases.json (" + sum(imageBody) + ") and " + checksumName + " (" + sum("x") + ") disagree about " + imageName)

	k = fetchKit(t)
	k.served[imageURL] = "tampered"
	k.wantCode(k.run("fetch", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: SHA-256 MISMATCH for " + imageName + ": expected " + sum(imageBody) + ", got " + sum("tampered"))

	k = fetchKit(t)
	k.fake.On(helperArgv("probe_upstream"), execx.Result{Stdout: []byte("VMTEST-FRESHNESS-REVISION 1\n")})
	k.wantCode(k.run("fetch", "server-fast"), term.ExitFailure)
	k.wantErr("ERROR: the probe listed no artefact for server-fast-44")
	assertNoProbeFiles(t, k)
}
