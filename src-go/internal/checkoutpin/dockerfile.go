package checkoutpin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The custom Dockerfile (Plan 00266 review finding 7), the fourth kind the store approves:
//
//   - D1. `ccy-teams internal checkout-pin dockerfile <unattended> <into>`, which the launcher
//     asks before it builds a project image, run in the checkout: .claude/ccy/Dockerfile is read
//     once through an os.Root of the checkout (C1: a link at its name, or a .claude/ccy leading
//     out of it, is refused), and its digest must be the one recorded. Then those bytes are
//     written into <into>, a private directory of the launcher's, as a new 0600 file named
//     Dockerfile, and its path is printed: the launcher builds that copy, so what is built is
//     what was approved, whatever a live container of the checkout writes meanwhile. No
//     Dockerfile prints nothing, exit 0.
//   - D2. With true (a restore, a restart's relaunch, a launch with nobody to ask), content no
//     person approved is exit 1 naming the file and the approve verb. With false, it shows
//     every line and asks [y/N]: only a yes records it and copies it; a no, or no terminal, is
//     exit 1 and records nothing, naming --disable-custom-docker (launch on the base image) and
//     the approve verb.
//   - D3. `ccy-teams approve dockerfile [CHECKOUT]` shows every line of it and records it on a
//     yes (V3).
//
// A Dockerfile chooses the session image: its entrypoint and every control the image holds, and
// RUN steps against the build caches; a container writes its checkout, so without a pin it chose
// its next session's image.

// Dockerfile is the custom Dockerfile, in its checkout.
const Dockerfile = ".claude/ccy/Dockerfile"

// KindDockerfile approves the custom Dockerfile.
const KindDockerfile Kind = "dockerfile"

const usageDockerfile = "usage: ccy-teams internal checkout-pin dockerfile true|false DIR"

// DockerfileRead is the custom Dockerfile's bytes and their digest, read once through an
// os.Root of the checkout, and whether there is one.
func DockerfileRead(checkout string) (content []byte, digest string, present bool, err error) {
	content, present, err = ReadFile(checkout, Dockerfile)
	if err != nil || !present {
		return nil, "", false, err
	}
	sum := sha256.Sum256(content)
	return content, hex.EncodeToString(sum[:]), true, nil
}

// Dockerfile is D1 and D2.
func (c Command) Dockerfile(ctx context.Context, args []string) term.ExitCode {
	if len(args) != 2 || (args[0] != "true" && args[0] != "false") || !filepath.IsAbs(args[1]) {
		return c.usage(usageDockerfile)
	}
	dir, err := c.checkout("")
	if err != nil {
		return c.fail(err)
	}
	content, digest, present, err := DockerfileRead(dir)
	if err != nil {
		return c.fail(err)
	}
	if !present {
		return term.ExitOK
	}
	store, err := Locate(c.Getenv)
	if err != nil {
		return c.fail(err)
	}
	err = store.Verify(dir, KindDockerfile, digest)
	refused, isRefusal := errors.AsType[*RefusalError](err)
	switch {
	case err == nil:
	case !isRefusal:
		return c.fail(err)
	case args[0] == "true" || c.Confirm == nil:
		c.say("✗ " + err.Error())
		return term.ExitFailure
	default:
		c.say("The custom Dockerfile of " + dir + " is not the one approved: " + refused.Why + ". A container session can write it.")
		c.say("It would build this checkout's session image (its entrypoint, and RUN steps on this machine's build caches):")
		c.showFiles([]PinnedFile{{Name: Dockerfile, Content: content}})
		yes, err := c.Confirm(ctx, "Build this Dockerfile, and approve it for this checkout's launches and restores?")
		if err != nil {
			return c.fail(err)
		}
		if !yes {
			c.say("Not built; nothing recorded. Launch on the base image with ccy --disable-custom-docker, or approve it later: " + ApproveVerb(KindDockerfile, dir))
			return term.ExitFailure
		}
		if err := store.Record(dir, KindDockerfile, digest, c.Now()); err != nil {
			return c.fail(err)
		}
		c.say("✓ Approved the Dockerfile shown; a change to it is asked about again.")
	}
	copied, err := writeCopy(args[1], "Dockerfile", content)
	if err != nil {
		return c.fail(fmt.Errorf("could not copy %s/%s into %s: %w", dir, Dockerfile, args[1], err))
	}
	if _, err := io.WriteString(c.Stdout, copied+"\n"); err != nil {
		return term.ExitFailure
	}
	return term.ExitOK
}

// shownDockerfile is D3's screen and the digest it would record.
func (c Command) shownDockerfile(dir string) (string, bool) {
	content, digest, present, err := DockerfileRead(dir)
	if err != nil || !present {
		c.say(fmt.Sprintf("✗ no %s to approve in %s (%v)", Dockerfile, dir, err))
		return "", false
	}
	c.say("The custom Dockerfile " + dir + " builds its session image from (its entrypoint, and RUN steps on this machine's build caches):")
	c.showFiles([]PinnedFile{{Name: Dockerfile, Content: content}})
	return digest, true
}
