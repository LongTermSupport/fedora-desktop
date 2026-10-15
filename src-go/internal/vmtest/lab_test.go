package vmtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E7: lab.env is data. A shell line in it is refused, never run (B1).
func TestE7LabEnvIsParsedNotExecuted(t *testing.T) {
	cases := map[string]string{
		"a command":      "VMTEST_FEDORA_VERSION=44\ntouch /tmp/owned\n",
		"a substitution": "VMTEST_FEDORA_VERSION=$(id -u)\nVMTEST_REPO_URL=https://x\nVMTEST_REPO_BRANCH=b\n",
		"a repeated key": "VMTEST_FEDORA_VERSION=44\nVMTEST_FEDORA_VERSION=45\n",
		"no version":     "VMTEST_REPO_URL=https://x\nVMTEST_REPO_BRANCH=b\n",
		"an http url":    "VMTEST_FEDORA_VERSION=44\nVMTEST_REPO_URL=http://x\nVMTEST_REPO_BRANCH=b\n",
		"no branch":      "VMTEST_FEDORA_VERSION=44\nVMTEST_REPO_URL=https://x\n",
	}
	for name, body := range cases {
		k := newKit(t)
		k.write("lab.env", body)
		k.wantCode(k.run("ensure-base", "server-fast"), term.ExitFailure)
		if !strings.Contains(k.err.String(), "ERROR: ") || strings.Contains(k.err.String(), "\n==>") {
			t.Fatalf("%s: %s", name, k.err.String())
		}
		if k.count("validate_manifest") != 0 {
			t.Fatalf("%s: the run went on past a bad lab.env", name)
		}
	}
	k := newKit(t)
	k.write("lab.env", "# the lab\n\nVMTEST_FEDORA_VERSION=44\nVMTEST_REPO_URL=https://x\nVMTEST_REPO_BRANCH=b\nOTHER=1\n")
	k.builtBase("server-fast-44")
	k.gate(0, "current")
	k.wantCode(k.run("ensure-base", "server-fast"), term.ExitOK)
}

// E8: every deployed file the lab needs is named when it is missing; the dirs are created.
func TestE8LabFilesRequired(t *testing.T) {
	for _, rel := range []string{"lab.env", "scenarios.json", "ssh/lab-key", "ssh/lab-key.pub", "guest-cleanup.bash"} {
		k := newKit(t)
		if err := os.Remove(k.path(rel)); err != nil {
			t.Fatal(err)
		}
		k.wantCode(k.run("ensure-base", "server-fast"), term.ExitFailure)
		k.wantErr(strings.TrimSuffix(filepath.Base(rel), ".pub"))
	}
	k := newKit(t)
	if err := os.RemoveAll(k.gpg); err != nil {
		t.Fatal(err)
	}
	k.wantCode(k.run("ensure-base", "server-fast"), term.ExitFailure)
	k.wantErr("distribution-gpg-keys is installed by play-vm-test-lab.yml")

	k = newKit(t)
	k.builtBase("server-fast-44")
	k.gate(0, "current")
	k.wantCode(k.run("ensure-base", "server-fast"), term.ExitOK)
	if !k.exists("images") || !k.exists("bases") {
		t.Fatal("the lab dirs were not created")
	}
}
