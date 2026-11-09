package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/placeissued"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// placeDeps is the machine `seat place-issued` runs on: the real one, which a test replaces to
// answer a homeserver that always answers.
var placeDeps = placeissued.OS

const (
	msgPlaceHome     exits.Template = "teamchat seat place-issued needs HOME, an absolute path"
	msgPlaceRuntime  exits.Template = "teamchat seat place-issued needs XDG_RUNTIME_DIR, an absolute path"
	msgPlaceCheckout exits.Template = "teamchat seat place-issued takes --checkout DIR, an existing directory"
)

// runPlaceIssued is `seat place-issued --checkout DIR` (Plan 00246 Task 8.8, X22): the issued
// seat on stdin placed in the pool of the checkout's real path, in this user's seat store. The
// owner pipes it in from the host that issued it, as themself; nothing here reaches that host.
// Stdout is one line naming the seat and its team, and never a credential.
func runPlaceIssued(e Env, inv Invocation) term.ExitCode {
	getenv := e.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	home, runtime := getenv("HOME"), getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(home) {
		return exits.Write(e.Stderr, exits.New(exits.CodeHome, nil, msgPlaceHome))
	}
	if !filepath.IsAbs(runtime) {
		return exits.Write(e.Stderr, exits.New(exits.CodeRuntime, nil, msgPlaceRuntime))
	}
	state, err := paths.StateHome(home, getenv("XDG_STATE_HOME"))
	if err != nil {
		return exits.Write(e.Stderr, exits.New(exits.CodeHome, nil, msgPlaceHome))
	}
	key, err := checkoutKey(inv)
	if err != nil {
		return exits.Write(e.Stderr, err)
	}
	placed, err := placeissued.Place(context.Background(), placeDeps(state, runtime, key), e.Stdin)
	if err != nil {
		return placeissued.Report(e.Stderr, err)
	}
	if _, err := fmt.Fprintf(e.Stdout, "placed seat %s of team %s\n", placed.Seat, placed.Team); err != nil {
		return exits.Write(e.Stderr, err)
	}
	return term.ExitOK
}

// checkoutKey is the pool key of --checkout's real path: the directory with its links resolved,
// which is the path the launcher keys the same pool on from inside the checkout.
func checkoutKey(inv Invocation) (paths.CheckoutKey, error) {
	dir, given := inv.Flag("checkout")
	if !given {
		return paths.CheckoutKey{}, usage(msgPlaceCheckout)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return paths.CheckoutKey{}, usage(msgPlaceCheckout)
	}
	key, err := paths.CheckoutKeyOf(resolved)
	if err != nil {
		return paths.CheckoutKey{}, usage(msgPlaceCheckout)
	}
	return key, nil
}
