# Plan 00115: A play's shebang routes through run.bash, so `./playbooks/…/play-x.yml` just works on password sudo

**Status**: In Progress
**Created**: 2026-09-14
**Owner**: joseph
**Priority**: High

## Overview

Plan 00114 gave run.bash a single-play mode so a play could be run on a password-sudo box
without knowing about `--ask-become-pass`. It left two ways to run a play: the play's own
shebang, which cds to the repo root and execs `ansible-playbook` directly and therefore still
dies on password sudo, and `./run.bash <playbook>`, which works everywhere. Two ways is one
too many, and the one people reach for first is the broken one.

This plan makes the shebang the single way. Every play's first line becomes

```
#!/usr/bin/env -S bash -c 'p=$(realpath "$0") && exec "${p%/playbooks/*}/run.bash" "$p" "$@"'
```

which hands the play, by absolute path, to the `run.bash` at the root of whichever checkout it
lives in. run.bash's single-play mode does the rest. The line is 93 bytes, under the 127-byte
shebang limit, and resolves the root from any working directory because `realpath` makes the
path absolute before the `/playbooks/` suffix is stripped.

The one hazard is recursion: run.bash itself executed play files in five places (the runner's
three become branches, the headless optional-play loop, the main-playbook call). Each now
calls `ansible-playbook <file>` explicitly. The headless case is the one that would have
broken every unattended guest build, because the child inherits `RUN_BASH_HEADLESS=1` and
run.bash refuses a single play in headless mode.

## Goals

- All plays under `playbooks/` carry the new shebang; `scripts/make-playbooks-executable.bash`
  writes it and `scripts/qa-ansible.bash` asserts it, byte for byte.
- run.bash never executes a play file; every internal invocation is `ansible-playbook`.
- Docs show plays run by path again; the run.bash help says the shebang does the routing.
- Proven: a play run by path on a password-sudo box reaches the `BECOME password:` prompt
  through run.bash; a headless run still converges.

## Non-Goals

- Removing run.bash's positional playbook argument. The shebang depends on it.
- Making a play runnable from a streamed run.bash. Plays exist only in a checkout.

## Tasks

### Phase 1: Route

- [x] ✅ **Task 1.1**: New shebang in `make-playbooks-executable.bash` (with the previous
  form as the legacy line it migrates) and in `qa-ansible.bash`; script run across
  `playbooks/`.
- [x] ✅ **Task 1.2**: run.bash's five play-file executions become explicit `ansible-playbook`.
- [x] ✅ **Task 1.3**: Docs return to `./playbooks/…` invocations; `docs/playbooks.md` and the
  README explain that the shebang routes through run.bash; help text updated.

### Phase 2: Proof — BLOCKED BY Phase 1

- [ ] ⬜ **Task 2.1**: `qa-all.bash` green (bar the pre-existing docs findings).
- [ ] ⬜ **Task 2.2**: Live, password-sudo guest: `./playbooks/imports/play-claude-yolo.yml --check` from the repo root and from `/` both reach run.bash's sudo probe and the
  `BECOME password:` prompt.
- [ ] ⬜ **Task 2.3**: Headless still converges: an estate guest rebuild or a headless smoke
  proves no recursion and no `interactive-only` abort.

## Success Criteria

- [ ] `./playbooks/imports/play-x.yml` on a password-sudo box prompts once and runs.
- [ ] No play file is executed by run.bash itself.
- [ ] `qa-ansible` asserts the new shebang on every play.

## Delivery & Milestones

- Delivery commit: pending.
