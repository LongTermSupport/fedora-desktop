package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/kit"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// placeKit and ownBinary are the placement and the running program, which a test replaces.
var (
	placeKit  = kit.Place
	ownBinary = func() (io.ReadCloser, error) {
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		return os.Open(filepath.Clean(self))
	}
)

const msgKitInto exits.Template = "teamchat kit assemble takes --into this build's kit directory, {}"

// runKitAssemble is `kit assemble --into DIR` (Plan 00209 DESIGN-host-cc-v2.md section 6 step
// 2): the host kit of this build (paths.HostKitDir of its own digest), placed whole at DIR, which
// must be that directory, from the running program by kit.Place: assembled in a sibling and
// renamed into place, a whole kit left alone, anything else replaced. Stdout is one line,
// "assembled DIR" or "whole DIR". The install play runs it as root before the kit's current
// link moves.
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
	placed, err := placeKit(want, binary)
	if err = errors.Join(err, binary.Close()); err != nil {
		return exits.Write(e.Stderr, err)
	}
	state := "whole"
	if placed {
		state = "assembled"
	}
	_, err = fmt.Fprintf(e.Stdout, "%s %s\n", state, want)
	return exits.Write(e.Stderr, err)
}
