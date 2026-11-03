# Plan 00246: shell history clean and skip

**Status**: In Progress
**Created**: 2026-11-03
**Owner**: joseph
**Priority**: Medium

## Overview

Two helpers for the shell history kept by the history block in
`files/etc/profile.d/zz_lts-fedora-desktop.bash` and the Ctrl+R search in
`files/home/bashrc-includes/history-search.bash`. `nohist` runs a command in the current shell
and keeps the typed line out of history; `histclean` finds every history and Ctrl+R context entry
that contains a pattern, shows them, and removes them after one confirmation.

The typed `nohist ...` and `histclean ...` lines contain the very text to hide or remove, so
`HISTIGNORE` gains `nohist *`, `histclean *` and the bare words. A line bash keeps out of
history never moves the history number, so the Ctrl+R recorder skips it too.

## Goals

- `nohist cmd args` and `nohist 'cmd args'` run in the current shell (cd and exports take
  effect), exit with the command's status, and leave no history or context record.
- `histclean PATTERN` (fixed substring; `-e` for a Go regexp) lists matches from both files,
  asks `Remove N entries? [y/N]` once, rewrites each file atomically at mode 0600, and refuses
  to remove unconfirmed from a non-terminal.

## Non-Goals

- Removing a line from a running shell's in-memory list (`history -d` does that; the tool says so).

## Tasks

### Phase 1: build

- [x] ✅ **Task 1.1**: `internal/histclean` and `cmd/histclean`, tests first.
- [x] ✅ **Task 1.2**: `nohist` function in `bashrc-includes/nohist.bash`; `HISTIGNORE`; interactive-bash test.
- [x] ✅ **Task 1.3**: deploy in `play-basic-configs.yml`; docs; `meta-deploy.list`.

## Success Criteria

- [x] The tests prove the typed lines never reach history or context in a real interactive bash.
- [ ] The owner has run the play on the host.

## Delivery & Milestones

- Built on branch feat/histclean-nohist.
