package cli

import (
	"context"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/guest"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const msgGuestFlags exits.Template = "teamchat guest check takes --user U and --team T"

// guestCheck is `guest check --user U --team T` on the real guest (GU2). Parse has checked
// both flags; an Invocation without them is refused rather than trusted.
func guestCheck(e Env, inv Invocation) term.ExitCode {
	name, given := inv.Flag(userFlag.Name)
	u, err := paths.ParseGuestUser(name)
	if err != nil || !given || inv.Team == nil {
		return guest.Finish(e.Stderr, guest.OpCheck, usage(msgGuestFlags))
	}
	return guest.Finish(e.Stderr, guest.OpCheck, guest.NewChecker().Check(context.Background(), u, *inv.Team, e.Stdin))
}
