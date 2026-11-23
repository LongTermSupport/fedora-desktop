package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/kit"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// assembleKit and ownBinary are the assembly and the running program, which a test replaces.
var (
	assembleKit = kit.Assemble
	ownBinary   = func() (io.ReadCloser, error) {
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		return os.Open(filepath.Clean(self))
	}
)

const msgKitInto exits.Template = "teamchat kit assemble takes --into this build's kit directory, {}"

// runKitAssemble is `kit assemble --into DIR` (Plan 00209 DESIGN-host-cc-v2.md section 6 step
// 2): the host kit of this build (paths.HostKitDir of its own digest), assembled from the running
// program into DIR, which must be that directory, existing and empty. The install play runs it as
// root before the kit's current link moves.
func runKitAssemble(e Env, inv Invocation) term.ExitCode {
	d, err := sourceDigest(e.Digest)
	if err != nil {
		return exits.Write(e.Stderr, err)
	}
	digest, err := paths.ParseDigest(d)
	if err != nil {
		return exits.Write(e.Stderr, err)
	}
	want := paths.HostKitDir(digest)
	if into, given := inv.Flag("into"); !given || into != want.String() {
		return exits.Write(e.Stderr, usage(msgKitInto, exits.Name(want.String())))
	}
	binary, err := ownBinary()
	if err != nil {
		return exits.Write(e.Stderr, err)
	}
	err = assembleKit(want.String(), want, binary)
	return exits.Write(e.Stderr, errors.Join(err, binary.Close()))
}
