# Plan 00247: shell history clean and skip

**Status**: In Progress
**Created**: 2026-11-03
**Owner**: joseph
**Priority**: Medium

## Overview

Two helpers for the shell history kept by the history block in
`files/etc/profile.d/zz_lts-fedora-desktop.bash` and the Ctrl+R search in
`files/home/bashrc-includes/history-search.bash`. A line starting with `nohist=1` (any case) is kept
out of history; `histclean` finds every history and Ctrl+R context entry
that contains a pattern, shows them, and removes them after one confirmation.

The typed `nohist=1 ...` and `histclean ...` lines contain the very text to hide or remove, so
`HISTIGNORE` gains `[nN][oO][hH][iI][sS][tT]=1*`, `histclean *` and the bare word. A line bash keeps out of
history never moves the history number, so the Ctrl+R recorder skips it too.

## Goals

- A line starting with `nohist=1` in any case (`nohist=1 cmd`, `nohist=1; cmd`) leaves no history or
  context record; the Ctrl+R picker's header reminds of it and of histclean.
- `histclean PATTERN` (fixed substring; `-e` for a Go regexp) lists matches from both files,
  asks `Remove N entries? [y/N]` once, rewrites each file atomically at mode 0600, and refuses
  to remove unconfirmed from a non-terminal.

## Non-Goals

- Removing a line from a running shell's in-memory list (`history -d` does that; the tool says so).

## Tasks

### Phase 1: build

- [x] ✅ **Task 1.1**: `internal/histclean` and `cmd/histclean`, tests first.
- [x] ✅ **Task 1.2**: the `nohist=1` HISTIGNORE prefix and the picker header; interactive-bash test.
- [x] ✅ **Task 1.3**: deploy in `play-basic-configs.yml`; docs; `meta-deploy.list`.

## Success Criteria

- [x] The tests prove the typed lines never reach history or context in a real interactive bash.
- [ ] The owner has run the play on the host.

## Delivery & Milestones

- Built on branch feat/histclean-nohist.
