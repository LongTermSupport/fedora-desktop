package vmtest

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// E60: verify checks a response under the bridge's key and keeps verify_response's status.
func TestE60Verify(t *testing.T) {
	k := newKit(t)
	checkout := strings.TrimSuffix(k.home, "/lab")
	k.fake.On([]string{"systemd-escape", "--path", checkout}, execx.Result{Stdout: []byte("some-slug\n")})
	key := filepath.Join(k.userHome, ".config/vmtest-bridge/some-slug/response.key")
	k.writeAbs(key, "k")
	k.fake.On(helperArgv("verify_response"), execx.Result{Stdout: []byte("VMTEST-RESPONSE-VERIFIED r1\n")})
	k.wantCode(k.run("verify", "r1"), term.ExitOK)
	c := k.mustCall("verify_response")
	if !strings.HasSuffix(c.Line(), "--checkout "+checkout+" --key "+key+" --run-id r1") || k.out.String() != "VMTEST-RESPONSE-VERIFIED r1\n" {
		t.Fatalf("got %s, stdout %q", c.Line(), k.out.String())
	}
	k.wantErr("==> verdict of record: " + filepath.Join(k.userHome, ".local/state/vmtest-bridge/some-slug/service.log"))

	k.fake.On(helperArgv("verify_response"), execx.Result{ExitCode: 1})
	k.wantCode(k.run("verify", "r1"), term.ExitFailure)

	k = newKit(t)
	k.fake.On([]string{"systemd-escape"}, execx.Result{Stdout: []byte("other\n")})
	k.wantCode(k.run("verify", "r1"), term.ExitFailure)
	k.wantErr("no bridge signing key at")
	if k.count("verify_response") != 0 {
		t.Fatal("verified without a key")
	}
}
