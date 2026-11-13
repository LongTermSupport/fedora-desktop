# Plan 00255: cc session sandbox and deploy broker

**Status**: In Progress (host triage read into RESEARCH.md §13; owner decisions D1-D9 open)
**Created**: 2026-11-13
**Owner**: joseph
**Priority**: Medium

## Overview

A `cc` session (Claude Code on the desktop, no container) runs as the owner, beside the owner's
own Matrix clients, browsers, password store and keyring. Today the only guard is Claude Code
deny rules, which match text on a command line and miss any program that opens a file without
naming it (issue #101). The owner chose two of #101's options together:

- **Seal the session** (option 3): cc starts claude, and everything it starts, in a sandbox
  where those folders do not exist and the session bus is reached through a filter that refuses
  the secrets service.
- **A deploy broker instead of blanket sudo** (option 4): a root-owned service runs a committed
  play at a commit that is on GitHub, on request. Agents ask it; they never hold root.

The two only work together. Inside the seal `sudo` already fails, but a sealed agent can still
get code run by an unsealed process of the owner's (tmux, the systemd user manager, the owner's
shell start-up files), and with passwordless sudo that is root. So the order is: broker first,
alongside today's sudo; then the seal, opt-in, then default; and only then passwordless sudo
goes. At no step does the owner lose the ability to run plays.

The facts behind this are in [RESEARCH.md](RESEARCH.md); host facts come from
[triage.bash](triage.bash), read into RESEARCH.md §13.

## Goals

- In a `cc` session, no file tool, Bash command or program it starts can read the Matrix
  clients' data, browser profiles, the password store or the keyring folder, or ask the secrets
  service for a secret.
- No `cc` session holds root, or a route to it through an unsealed process of the owner's.
- Agents in `cc` sessions keep running desktop plays, through the broker, unattended.
- Everything run as root by the broker is a commit on GitHub, logged with who asked.

## Non-Goals

- A dedicated agent user for `cc` (ruled out by the owner as the main path; `host` members in
  docs/agent-bus.md keep theirs).
- Claude Code's own sandbox setting (it covers Bash only, not the file tools, and restricts the
  network; #101). It may still run inside the seal.
- Changing ccy (container) sessions or project settings shared with them.
- Protecting against an agent that writes a harmful play and gets it onto a protected branch:
  D3 decides how much the broker asks for; the audit trail is the floor.

## Context & Background

- Issue #101; the owner's choice of options 3 + 4, and the constraint that agents keep running
  desktop plays.
- Prior art: the self-update runner (Plan 00137) already runs plays for the user from a
  root-owned clone, with the become password on an inherited descriptor
  ([RESEARCH.md §6](RESEARCH.md#6-prior-art-the-self-update-runner-plan-00137)).
- `run.bash`'s unattended play path accepts exactly that caller (RESEARCH.md §5).
- The deny rules `cc --teams` passes stay as a second layer (RESEARCH.md §2).

## Owner Decisions (open)

Each has a recommendation; nothing past Phase 0 starts until D1-D3 are answered.

- **D1. How the session is sealed.** (a) bubblewrap, the tool Flatpak uses, started by cc
  around claude; (b) a systemd service per session. **Recommend (a)**: it keeps claude in the
  terminal pane it starts in, needs no root, and hides a folder by mounting an empty one over
  it. (b) puts the session in a background service, which the terminal-based cc does not fit.
  *Triage (§13.1, 13.2, 13.7): bwrap is installed, user namespaces are on, bwrap nests, and
  `sudo` fails inside it; (a) stands.*
- **D2. Passwordless sudo on this desktop, at the end.** (a) remove it for the owner's account
  entirely: the owner types the sudo password in their own terminal (remembered for some
  minutes), and plays go through the broker or `meta-deploy.bash`; (b) keep it for the owner's
  terminals. **Recommend (a)**: there is no way to give it to the owner's terminals but not to
  something an agent persuaded one of them to run, which is the gap the seal leaves.
- **D3. What the broker asks before running a play as root.** (a) the commit is on a protected
  branch on GitHub (so CI passed); (b) as (a), and plays not on a short "agents may run alone"
  list wait for the owner's click on a desktop notification; (c) every run waits for a click.
  **Recommend (b)**: unattended for the plays agents run all the time, the owner in the loop
  for the rest. (a) alone means any pushed play can run as root, recorded but not stopped.
- **D4. Which desktop services the sealed session may talk to.** The filter can only list what
  is allowed (RESEARCH.md §10), not refuse one thing. **Recommend**: notifications, opening
  links in the browser, and whatever triage shows the session uses that cannot start programs
  or hand out secrets; refused: the secrets service, Flatpak's "run on host", the systemd user
  manager, the terminal and the GNOME Shell. *Triage (§13.4) adds to the refused list: the
  keyring's own name, GNOME Online Accounts and Identity, the session manager, PackageKit and
  Software, and Mutter's remote desktop, input capture and screen cast. The portal front end
  carries many interfaces besides opening links, so it is allowed by `--call` rules for the
  calls needed, not by `--talk`.*
- **D5. tmux from inside a sealed session.** cc runs in tmux, and tmux can type into any pane,
  including an unsealed one. **Recommend**: hide it; managing teams and sessions (`ccy-teams`)
  is done from the owner's own terminal, not by an agent in a sealed cc.
- **D6. gh and git logins, if they live in the keyring** (triage says). **Recommend**: move gh's
  login to its own file, which the seal leaves readable, rather than open the secrets service.
  *Triage (§13.4): every gh login is in the keyring, so D6 is needed before the seal; git has
  no global credential helper, and how it authenticates pushes is not established.*
- **D7. Where the broker's sudo password comes from.** **Recommend**: the owner types it once
  into a broker set-up command run with sudo; it is kept in a root-only file, as the
  self-update runner keeps its own. Not in the vault, which the session can reach.
- **D8. What else is hidden.** **Recommend**: also hide ssh private keys (pushes keep working
  through the ssh agent; config and known hosts stay readable), `~/.gnupg`, and the checkout's
  vault password file. *Triage (§13.3): no password store here; browser data is in both native
  folders and Flatpak folders, so the fixed list covers both forms.*
- **D9. A way to start cc unsealed.** **Recommend**: none in cc once Phase 4 lands; the plain
  `claude` command stays for the owner's own use.

## Tasks

### Phase 0: Host facts

- [x] ✅ **Task 0.1**: Run [triage.bash](triage.bash) `--active` on the host (queued in
  `meta-deploy.list`): sandbox tools, kernel settings, which folders exist, session bus names,
  where gh/git keep logins, sudo rules and defaults, escape sockets, sudo and nesting inside
  bwrap. Done 2026-11-13 through `meta-deploy.bash` at F44 `384266ef3`: all 7 legs answered.
- [ ] 🔄 **Task 0.2**: Read the report into RESEARCH.md (facts, no install-specific names); put
  D1-D9 to the owner with the findings. Facts are in RESEARCH.md §13 and noted beside D1, D4,
  D6, D8 and Tasks 1.2, 3.3; putting D1-D9 to the owner remains.

### Phase 1: Deploy broker, alongside today's sudo (nothing removed)

- [ ] ⬜ **Task 1.1**: Go command `deploy-broker` in `src-go/cmd/`, logic in
  `src-go/internal/deploybroker/`, tests first through fakes (src-go/CLAUDE.md). Client:
  `deploy-broker run <play> --at <sha>` streams the run and returns its exit code. Server (root,
  socket activated): checks the caller is the desktop user (peer credentials), fetches origin
  into its root-owned clone, checks the commit by D3, checks the play path is a play under
  `playbooks/`, takes the shared play lock (`internal/playlock`), and runs the play as the user
  through `run.bash`'s unattended path with the become password on an inherited descriptor (as
  `helpers/self_update/cycle.py` does), with the user's session environment so desktop plays
  work. Every run is logged with the caller, play, commit and result.
- [ ] ⬜ **Task 1.2**: Install it from the play that installs cc (`play-claude-yolo.yml`, via a
  tasks file), not a new play: binary, socket and service units, root-only state, the D7 set-up
  command, `kernel.yama.ptrace_scope=1` (the play run as the user holds the password;
  play-self-update.yml sets the same for the same reason). Decide with D3 whether the clone is
  shared with the self-update runner. *Triage (§13.2, 13.6): ptrace scope is 0 on the host
  today, and the self-update runner is not set up there (no clone, no sudoers rule), so the
  broker cannot rely on that play for either the setting or the clone.*
- [ ] ⬜ **Task 1.3**: `deploy.bash` and `acceptance.bash` (R9, coverage line): the broker runs a
  desktop play asked for from a cc session; refuses a commit not on GitHub, one not on a
  protected branch, a path outside `playbooks/`, and a caller that is not the desktop user.

### Phase 2: Agents use the broker

- [ ] ⬜ **Task 2.1**: `meta-deploy.bash` entries and `run.bash --changed` reachable through the
  broker (a `deploy-broker` mode for a plan folder or `changed-plays`), so the owner's queue and
  an agent's request are the same run.
- [ ] ⬜ **Task 2.2**: Agent docs (CLAUDE/AgentNotes.md, the cc system prompt): in a cc session,
  plays go through the broker; never `sudo`.
- [ ] ⬜ **Task 2.3**: Interactive `run.bash` without passwordless sudo: confirm it prompts once
  for the sudo password and passes it to ansible; fix if it assumes passwordless sudo (D2).
- [ ] ⬜ **Task 2.4**: Every in-session `sudo` caller (RESEARCH.md §4 and §12: ccy-teams,
  agent-bus park/role changes, lxcfreeze, reclaim, consumer, ftpcamera): route through the
  broker, give it a scoped rule, or document it as the owner's to run outside cc. Without
  passwordless sudo these callers ask for the password once (`sudo -v`), which nothing inside
  a sealed session can answer ([FACTCHECK.md](FACTCHECK.md) R1).

### Phase 3: The seal, opt-in

- [ ] ⬜ **Task 3.1**: Seal in `cc-claude` (already Go, and the program cc starts): with
  `cc --sealed`, it re-runs itself under bwrap (D1) with its own process namespace; hides the
  folders from `matrixclients/clients.json` plus the fixed list (browsers, password store,
  keyring folder, D8); hides the tmux socket (D5) and the systemd user manager's private
  socket; mounts the owner's shell start-up files, autostart and user unit folders read-only;
  starts `xdg-dbus-proxy` with the D4 allowlist and points the session bus at it. Unit tests
  on the argument list; the hidden-path list is read from clients.json, not copied.
- [ ] ⬜ **Task 3.2**: `cc --teams` and `cc --bus` seal the same way (around `pingbus seat exec`), with the pingbus home and seats left writable.
- [ ] ⬜ **Task 3.3**: Install bwrap and xdg-dbus-proxy from the play (if triage shows them
  missing; *triage §13.1: both are installed here, so the play only declares them*) and the cc
  changes; ccy version bump if anything under
  `files/var/local/claude-yolo/` changes.
- [ ] ⬜ **Task 3.4**: `acceptance.bash` run from inside a sealed session (a checker the owner
  starts in one): each hidden folder unreadable by `cat`, a Python `open`, and a recursive
  grep from `~`; the secrets service and every refused bus name refused; `sudo -n true` fails;
  tmux unreachable; and normal work passes: git push, gh, `qa-changed.bash`, a broker play.

### Phase 4: Sealed by default, passwordless sudo removed

- [ ] ⬜ **Task 4.1**: cc seals by default (D9); the opt-in flag goes.
- [ ] ⬜ **Task 4.2**: Only after Phase 3 acceptance passes and the owner confirms plays through
  the broker are working for them: `play-basic-configs.yml` removes the passwordless sudo block
  on this desktop too (D2), through a variable the developer profile sets, so a server or
  consumer install is unchanged.
- [ ] ⬜ **Task 4.3**: Acceptance: the owner's terminal asks for the sudo password; a sealed
  session's broker play still runs; `meta-deploy.bash` still runs.

### Phase 5: Docs and close

- [ ] ⬜ **Task 5.1**: docs/ for cc (the seal, what it hides, how to run a play) and
  docs/agent-bus.md's cc `host` row; the deny rules documented as the second layer they are.
- [ ] ⬜ **Task 5.2**: Comment on #101 with the outcome; qa-reviewer agent on the whole plan.

## Dependencies

- Builds on: Plan 00137 (self-update runner, the play lock), Plan 00177 (cc --teams deny rules).
- Related: Plan 00254 (Lightning per team, a client the seal hides), Plan 00252 (ccy egress
  proxy: credentials held outside the agent's reach, the same idea for ccy).

## Success Criteria

- [ ] From inside a sealed cc session, none of the hidden folders can be read by any route
  tested in Task 3.4, and the secrets service is refused.
- [ ] No cc session can reach root: `sudo` fails inside; passwordless sudo is gone (D2).
- [ ] An agent in a sealed cc session runs a desktop play through the broker, unattended for
  the D3 list, and the run is logged with its commit.
- [ ] The owner could run plays at every point between phases.
- [ ] `./scripts/qa-changed.bash` green on each change; CI green on the protected branch.

## Risks & Mitigations

| Risk                                                        | Mitigation                                                                                 |
| ----------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| A route out of the seal is missed                           | Phase 4 removes passwordless sudo, so a missed route is not also root; Task 3.4 tests each |
| The broker becomes "root for any pushed play"               | D3 (b): the owner's click for plays not on the list; every run logged with its commit      |
| The become password is read from the running play           | ptrace scope 1 (Task 1.2); the seal's own process namespace hides the play's process       |
| gh/git pushes break because their logins are in the keyring | Triage first; D6 moves the login                                                           |
| ccy-teams or other tools stop working inside a sealed cc    | Task 2.4 inventory; D5 decides tmux; the seal is opt-in until acceptance                   |
| Owner locked out of plays mid-way                           | Sudo goes last (Task 4.2), only after the broker and the seal are proven                   |

## Delivery & Milestones

- Plan written; triage queued.
- Host triage run and read into RESEARCH.md §13; D1-D9 to the owner next.
