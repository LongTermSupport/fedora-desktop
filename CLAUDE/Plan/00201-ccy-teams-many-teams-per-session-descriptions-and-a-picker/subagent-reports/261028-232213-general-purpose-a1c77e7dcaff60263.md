I've built `acceptance-consent.bash`, a plan script that runs step 08 by itself, so the owner no longer types any `! mv` lines. Nothing ran cc, tmux, sudo or a team in this container. The unit tests and `./scripts/qa-changed.bash` pass, but the script itself has not run on the host yet.

**Branch:** `agent-a1c77e7dcaff60263-26ee570f`, one commit `f5e887240`, not pushed or merged.

**What it does, start to finish:**
1. **Puts back anything a killed run left.** If a held-aside copy of `element-accepted` exists, it is renamed back over whatever is at the record's name; that file is copied into the run directory first. I chose to restore rather than refuse: the held copy is the owner's own record, and whatever stands in its place says the same thing (accepted).
2. **Sets up.** It makes two throwaway teams (`consent201a`, `consent201b`) with the existing acceptance legs. It makes a scratch git checkout outside the repo with no `.claude/`, and asks `agent-bus seat choose` for each team's new seat and its handle.
3. **Holds the record aside.** It renames it in the same directory to `element-accepted.held-by-plan00201-consent-check`. If there was no record, it leaves a marker so the way back removes the one Yes writes. It also saves cc's token record (`~/.claude/.last-launch.conf`), which every cc launch rewrites.
4. **No.** It runs the owner's `cc --teams` in a private tmux server, in its own systemd user scope (cc refuses a tmux server that dies with a terminal tab). It checks that the question names both handles, then answers No. It then checks that cc exited 1 saying nothing was recorded, that there is no record, that the checkout still has no `.claude/`, and that neither handle became a team member.
5. **Yes.** It runs cc again and answers Yes, and answers Desktop if the token chooser appears. It checks that the record was written (owner-only, by this run) and that both seats are held by the named handles.
6. **Puts everything back.** It restores the record straight away, ends the session with Ctrl-C, and checks that the seats are released. It then restores cc's token record, removes the checkout, purges the teams, and checks that nothing of them remains and the record is byte for byte as it was.

The record is put back first however the run ends: success, failure, Ctrl-C or SIGTERM. Only a kill -9 escapes that, and step 1 of the next run covers it.

The pass/fail judgements are in `consent_check.py`, written test first (35 tests). It reads the words it waits for from `consent.go` and `token-management.bash` rather than copying them. ACCEPTANCE-owner.md step 08 and the PLAN.md sub-item now say the script does it, and there is one journal entry.

**What may still need someone's hands:**
- **sudo inside the private tmux.** cc creates each new host seat with sudo, and sudo's cached password only covers the terminal it was typed in, not the tmux pane. If sudo asks there, the script prints how to attach (`tmux -L plan00201-consent-<pid> attach -t consent`) and waits 15 minutes. Only the owner can type that password; if nobody does, it exits 2. `acceptance.bash` already passed on the host with sudo calls that would have failed if a password were needed, which suggests it won't ask, but that hasn't been seen.
- **Only provable on the host:** gum's `y`/`n` keys, the systemd scope satisfying cc's tmux check, and Ctrl-C ending the claude session (there is a fallback kill if it doesn't).
- **Not undone:** like any cc launch, the Yes run runs `claude update` and may add the scratch path to `~/.claude.json`. While the record is held aside (a few minutes), a new `cc --teams` started by the owner would ask the consent again; sessions already running are not affected.

**meta-deploy.list entry** (I did not add it):
```
# Plan 00201 Task 2.7, ACCEPTANCE-owner.md step 08: cc --teams's Element consent, No then Yes,
# driven in a private tmux on two throwaway teams (consent201a, consent201b) in a scratch
# checkout, all removed after. Holds ~/.config/pingbus/element-accepted aside and puts it back
# however the run ends (and puts back first what a killed run left). Prompts for sudo once; if
# sudo also asks inside the private tmux, it prints how to attach and waits (exit 2 if nobody
# types it). Exit 2 is COULD NOT ESTABLISH.
CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/acceptance-consent.bash
```

**Full report:** `<worktree>/untracked/scratch/261028-00201-consent-report.md`