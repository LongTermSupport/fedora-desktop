# Plan 00231: shutdown with update runs without a password prompt

**Status**: In Progress
**Created**: 2026-10-27
**Owner**: joseph
**Priority**: High

## Overview

On a fresh consumer install the owner ran `shutdown-with-update` bare and it refused:
"must be run through sudo from the user whose sessions are to be warned". The command was
reached only through a `sudo` alias in `~/.bashrc-includes`, which applies only in an
interactive shell opened after `play-basic-configs.yml` ran (the shell the install ran in
predates it), never from a script, `ssh host cmd`, or a typed full path. Once through sudo
it would have refused again: a consumer install has no ccy (`play-claude-yolo.yml` ends
itself there), and the script refused when `ccy-sessions` was missing.

So the script now hands itself to sudo by its installed path when it is not root; a scoped
sudoers drop-in lets the desktop user through without a password for exactly the argument
lists the script accepts; and a consumer install, recorded by the play in a root-owned
`/etc/fedora-desktop/install-audience`, has no sessions to warn and still updates and goes
down. The earlier work on this script is
[Plan 00173](../00173-shutdown-with-update-always-goes-down/PLAN.md). The grant is the core
risk, so it is held by a Defence Before Fix rule, `sudoers-rule` in `qa-ansible`
([QA.md](../../QA.md#sudoers-rule)), over every sudoers rule the repository installs.

## Goals

- A bare `shutdown-with-update` / `reboot-with-update`, with any argument list the script
  accepts, runs as root with no password prompt for the desktop user, on every profile and
  audience.
- A consumer install (no ccy) updates, counts down and goes down, saying in one line that
  there are no sessions to warn; a developer install with `ccy-sessions` missing still refuses.
- The grant is no escalation route, argued point by point below and held by a QA rule.

## Non-Goals

- Porting the script to Go (decision D1).
- Changing what the script updates, or its desktop and server behaviour (Plan 00173).

## Tasks

### Phase 1: Build

- [x] ✅ **Task 1.1**: Tests first in `scripts/test-ccy-sessions-reboot.bash`: the re-exec
  (exact sudo argv, name and arguments kept), the no-ccy consumer path, a missing or unknown
  audience record, a root run with no `SUDO_USER`, `--in` above 9999, a minute that is not
  digits never evaluated, and the drop-in admitting exactly the argument lists the parser
  accepts. Red before the change, green after.
- [x] ✅ **Task 1.2**: `shutdown-with-update` re-execs through sudo when not root, reads the
  install audience, caps `--in` at 9999, refuses a non-numeric minute seam; shrunk to stay
  under its ratchet record (pipx and rustup one loop).
- [x] ✅ **Task 1.3**: `play-basic-configs.yml` records the audience, installs
  `/etc/sudoers.d/fedora-desktop-shutdown-with-update` (visudo-validated, root:root 0440) and
  removes the old alias files; `files/home/bashrc-includes/shutdown-with-update.bash` is gone.
- [x] ✅ **Task 1.4**: DBF: the `sudoers-rule` check in `src-go/qa/ansible`, proven against
  `tests/fixtures/ansible-sudoers/`, green over the real tree (the self-update and agent-bus
  rules included), red on a planted bare command.
- [ ] ⬜ **Task 1.5**: Host acceptance check (`acceptance.bash`) and the meta-deploy entry.
- [ ] ⬜ **Task 1.6**: DBF conformance review and qa-reviewer pass.

### Phase 2: Deploy and accept

- [ ] ⬜ **Task 2.1**: The host runs `play-basic-configs.yml` (changed-plays) and this plan's
  `acceptance.bash` through meta-deploy.
- [ ] ⬜ **Task 2.2**: The owner runs a bare `shutdown-with-update` on the consumer laptop: no
  prompt, it updates, counts down and powers off.

## Technical Decisions

- **D1: stay in bash.** The change fits by shrinking (the record went down, from 290 code
  lines and 298 statements to 289 and 295 or fewer). A Go port would put a Go build into
  `play-basic-configs.yml`, the first play of a fresh install, before the Go toolchain is
  installed on a consumer machine, and is a separate piece of work.
- **D2: pin arguments in sudoers and validate them in the script.** The drop-in lists each
  argument list the parser accepts (literal, or an anchored expression for `--in`'s 1 to
  9999), as the agent-bus and self-update rules do, so a mistyped or hostile option never
  reaches the root script; the parser stays the second, strict gate (exit 64 on anything
  else).
- **D3: the audience comes from a root-owned record**, not from whether `ccy-sessions`
  exists in the user's home, so the warnings cannot be switched off by deleting a user file,
  and a developer box whose ccy is missing still refuses (fail fast).
- **D4: no `Defaults` in the drop-in.** Whether a command-scoped `secure_path` is honoured
  could not be verified here (no sudo in the container); the rule relies on Fedora's global
  `env_reset` and `secure_path`, as the two existing drop-ins do, and the acceptance check
  probes both with a planted `PATH` program and `BASH_ENV` on the real host.
- **D5: the alias goes.** With the script elevating itself the alias is a second route to the
  same thing, and the cause of the report: it exists only in shells opened after the play.

## Security argument

1. **The target cannot be modified by the user.** `/usr/local/bin/shutdown-with-update` is
   copied `root:root 0755`, `reboot-with-update` is a root-owned link to it, and
   `/usr/local/bin` and above belong to the filesystem package (no play changes them). The
   `sudoers-rule` check holds every such deploy; the acceptance check stats the real files.
2. **Arguments are fixed.** sudo admits only the pinned lists; the script re-checks each.
3. **The environment is reset.** No `SETENV`, no `env_keep`, no `Defaults` in any rule the
   repository installs (held by `sudoers-rule`). The script's test seams (`WITH_UPDATE_*`)
   are dropped by `env_reset`; the one that reaches `$(( ))` is refused unless digits.
4. **Nothing the root run reads or runs is the user's.** Root reads only
   `/etc/fedora-desktop/*` (root:root 0644 in a root 0755 directory) and writes no file.
   Every user program (`ccy-sessions`, `flatpak --user`, pipx, rustup) runs through
   `sudo -u "$SUDO_USER" -H`, as the user; `SUDO_USER` is set by sudo. The re-exec uses the
   installed path, never `$0`.
5. **The root commands cannot be steered.** dnf, fwupdmgr, `flatpak --system`, `shutdown`
   and `systemctl` read only root-owned configuration (`/etc/yum.repos.d`, `/etc/dnf`,
   `/etc/fwupd`, `/var/lib/flatpak`); no play makes any of them user-writable, and sudo sets
   `HOME` to root's.

## Success Criteria

- [ ] On the consumer laptop a bare `shutdown-with-update` reaches the update and the
  countdown with no prompt, and powers off.
- [ ] The acceptance check passes on the host.
- [x] The test suite and `qa-ansible` (with `sudoers-rule`) pass.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00231-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 1 build on the work branch, awaiting the host deploy.
