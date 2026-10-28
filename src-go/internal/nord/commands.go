package nord

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// ipService answers the address the machine reaches the internet from (E19).
const ipService = "https://api.ipify.org"

// disconnect is E17.
func (r *run) disconnect(ctx context.Context) term.ExitCode {
	if _, ok := r.check(ctx, needNM); !ok {
		return term.ExitFailure
	}
	up, ok := r.active(ctx)
	if !ok {
		return term.ExitFailure
	}
	return r.bringDown(ctx, up)
}

// bringDown brings down each of up, trying them all (E17).
func (r *run) bringDown(ctx context.Context, up []nmProfile) term.ExitCode {
	if len(up) == 0 {
		r.say("No NordVPN connection is active.")
		return term.ExitOK
	}
	code := term.ExitOK
	for _, p := range up {
		r.say("Disconnecting " + p.name + "...")
		if _, err := r.nmChange(ctx, nmByUUID("down", p.uuid)); err != nil {
			r.failf("could not disconnect %s: %v", p.name, err)
			code = term.ExitFailure
		} else {
			r.say("Disconnected " + p.name + ".")
		}
		if ctx.Err() != nil {
			return term.ExitCancelled
		}
	}
	return code
}

// listActive is E18.
func (r *run) listActive(ctx context.Context) term.ExitCode {
	if _, ok := r.check(ctx, needNM); !ok {
		return term.ExitFailure
	}
	all, ok := r.profiles(ctx)
	if !ok {
		return term.ExitFailure
	}
	up, ok := r.active(ctx)
	if !ok {
		return term.ExitFailure
	}
	r.print("Imported NetworkManager connections:")
	r.print("")
	if len(all) == 0 {
		r.print("  (none)")
		r.print("")
		r.print("Use 'nord connect <name>' to import and connect.")
		return term.ExitOK
	}
	for _, p := range all {
		mark := ""
		if isActivated(up, p.uuid) {
			mark = " (connected)"
		}
		r.print("  " + p.name + mark)
	}
	return term.ExitOK
}

func isActivated(up []nmProfile, uuid string) bool {
	for _, p := range up {
		if p.uuid == uuid && p.activated() {
			return true
		}
	}
	return false
}

// status is E19.
func (r *run) status(ctx context.Context) term.ExitCode {
	if _, ok := r.check(ctx, needNM); !ok {
		return term.ExitFailure
	}
	up, ok := r.active(ctx)
	if !ok {
		return term.ExitFailure
	}
	if len(up) == 0 {
		r.print("Not connected")
		return term.ExitFailure
	}
	anyUp := false
	for _, p := range up {
		if p.activated() {
			anyUp = true
			r.print("Connected: " + p.name)
		} else {
			r.print(fmt.Sprintf("Connecting: %s (%s)", p.name, p.state))
		}
		if p.device != "" {
			r.print("Device: " + p.device)
		}
	}
	if anyUp {
		r.print("Public IP: " + r.publicIP(ctx))
	}
	return term.ExitOK
}

// publicIP is the address ipService answers, or unknown and why (E19).
func (r *run) publicIP(ctx context.Context) string {
	res, err := r.runner.Run(ctx, execx.Cmd{Name: "curl", Args: []string{"-fsS", "--max-time", "5", ipService}})
	if missing, ok := errors.AsType[*execx.NotInstalledError](err); ok && missing != nil {
		return "unknown (curl is not installed)"
	}
	if exitErr, ok := errors.AsType[*execx.ExitError](err); ok && exitErr != nil {
		return fmt.Sprintf("unknown (curl failed (exit %d): %s)", exitErr.Code, strings.TrimSpace(string(res.Stderr)))
	}
	if err != nil {
		return fmt.Sprintf("unknown (%v)", err)
	}
	reply := strings.TrimSpace(string(res.Stdout))
	if net.ParseIP(reply) == nil {
		return fmt.Sprintf("unknown (not an IP address: %q)", reply)
	}
	return reply
}

// cleanup is E20.
func (r *run) cleanup(ctx context.Context) term.ExitCode {
	if _, ok := r.check(ctx, needNM); !ok {
		return term.ExitFailure
	}
	all, ok := r.profiles(ctx)
	if !ok {
		return term.ExitFailure
	}
	if len(all) == 0 {
		r.say("No nordvpn connections found.")
		return term.ExitOK
	}
	up, ok := r.active(ctx)
	if !ok {
		return term.ExitFailure
	}
	if !r.yes {
		if code, yes := r.confirmCleanup(ctx, all, up); !yes {
			return code
		}
	}
	r.say("Removing all nordvpn-* connections from NetworkManager...")
	removed, failed := 0, 0
	for _, p := range all {
		note := ""
		if isActivated(up, p.uuid) {
			note = " (connected: this disconnects it)"
		}
		r.say("  Removing " + prefix + p.name + note)
		if _, err := r.nmChange(ctx, nmByUUID("delete", p.uuid)); err != nil {
			r.failf("could not remove %s%s: %v", prefix, p.name, err)
			failed++
		} else {
			removed++
		}
		if ctx.Err() != nil {
			return term.ExitCancelled
		}
	}
	if failed > 0 {
		r.failf("removed %d connection(s); %d could not be removed.", removed, failed)
		return term.ExitFailure
	}
	r.say(fmt.Sprintf("Removed %d nordvpn connection(s).", removed))
	return term.ExitOK
}

// confirmCleanup is E20's question, default no, on the terminal (InteractiveScripts.md rule 6):
// the profiles it would remove, then whether to; the exit code when the answer is not yes.
func (r *run) confirmCleanup(ctx context.Context, all, up []nmProfile) (term.ExitCode, bool) {
	if !r.env.IsTerminal(r.env.Stdin) {
		r.failf("cleanup asks before it removes anything, and there is no terminal to ask on; run 'nord cleanup --yes' to remove without asking.")
		return term.ExitFailure, false
	}
	tty, err := r.env.OpenTTY()
	if err != nil {
		r.failf("cleanup asks before it removes anything, and there is no terminal to ask on; run 'nord cleanup --yes' to remove without asking.")
		return term.ExitFailure, false
	}
	var question strings.Builder
	for _, p := range all {
		mark := ""
		if isActivated(up, p.uuid) {
			mark = " (connected)"
		}
		question.WriteString("  " + prefix + p.name + mark + "\n")
	}
	fmt.Fprintf(&question, "Remove these %d connection(s) from NetworkManager?", len(all))
	yes, err := r.lines(tty).YesNo(ctx, ui.Here("nord", "Cleanup").About(plural(len(all), "connection")),
		question.String(), false)
	code := term.ExitOK
	switch {
	case err != nil:
		code = r.unanswered(ctx, err)
	case !yes:
		r.say("Nothing removed.")
	}
	if cerr := tty.Close(); cerr != nil {
		r.failf("close the terminal: %v", cerr)
		return term.ExitFailure, false
	}
	return code, err == nil && yes
}
