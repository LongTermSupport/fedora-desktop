# Plan 00036: Host `cc` / Container `ccy` Functional Parity

**Status**: Not Started
**Created**: 2026-04-24
**Owner**: joseph
**Priority**: Medium

## Overview

The host-side Claude Code invocation (`cc`, an alias) and the containerised
YOLO variant (`ccy`, a wrapper script) have drifted in terminal behaviour. The
`ccy` container entrypoint exports two environment variables that materially
improve the paging and rendering experience — `CLAUDE_CODE_NO_FLICKER=1`
(fullscreen alternate-screen-buffer, flat memory usage) and
`CLAUDE_CODE_DISABLE_MOUSE=1` (native terminal selection, better over
SSH/tmux). The host currently exports neither, so `cc` has a noticeably
degraded UX compared to `ccy`.

This plan adds a new bashrc include that exports those two variables on the
host, wires it into the existing `~/.bashrc-includes/` auto-sourcing pattern
via Ansible, and teaches the `cc` alias to verify the include was sourced
(with a sentinel env var) so silent drift is caught at invocation time rather
than as a mysterious UX regression.

Scope is deliberately narrow: the host cannot and should not run in a
container, and vars that only make sense inside the container
(`IS_SANDBOX=1`, `CCY_DISABLE_SUSPEND=1`) must NOT be exported on the host.
Parity is pursued only for the terminal/rendering vars that are host-safe.

## Goals

- Export `CLAUDE_CODE_NO_FLICKER=1` and `CLAUDE_CODE_DISABLE_MOUSE=1` for
  every interactive bash session of `{{ user_login }}` (and `root`, to match
  existing include conventions)
- Deliver these via a new
  `files/home/bashrc-includes/claude-code-env.inc.bash` include that lands at
  `~/.bashrc-includes/claude-code-env.inc.bash`, auto-sourced by the existing
  include loop
- Upgrade the `cc` alias so it verifies a sentinel env var
  (`CC_PARITY_ENV_LOADED=1`) set by the include; if the sentinel is unset,
  it sources the include on-demand and continues (self-healing), printing a
  one-line warning so the user notices the drift
- Keep `cc` a minimal alias/one-liner that users can still `type cc` and
  understand at a glance — no giant wrapper script
- Document the full parity audit (see Context) so future drift is tracked,
  not guessed

## Non-Goals

- Running `cc` inside a container (that is what `ccy` is for)
- Exporting container-only vars on host: `IS_SANDBOX`, `CCY_DISABLE_SUSPEND`
- Changing `ccy` behaviour — only `cc` moves toward parity
- Merging `ENABLE_LSP_TOOL` or `phpantom-lsp` enabledPlugins into host
  `settings.json` (already handled separately / project-level; out of scope
  here)
- Moving the `cc` alias out of `play-claude-code.yml` into its own include
  file (the alias stays where it is; only the env vars move to the new
  include)
- Changing the `alias cc='claude update && claude'` update-on-launch
  behaviour

## Context & Background

### Where things live today

- **`cc` alias** — `playbooks/imports/play-claude-code.yml:51-59`
  (`blockinfile` into `~/.bashrc` under marker
  `# ANSIBLE MANAGED: Claude Code Integration`). Exact current block:
  ```
  # Claude Code CLI alias and PATH
  export PATH="$HOME/.local/bin:$PATH"
  alias cc='claude update && claude'
  ```
  No wrapper script exists; `cc` is a pure alias.
- **`ccy` wrapper** — `files/var/local/claude-yolo/claude-yolo` deployed to
  `/var/local/claude-yolo/claude-yolo` by
  `playbooks/imports/play-claude-yolo.yml:267-277`. Alias is set in
  `files/home/bashrc-includes/claude-yolo.bash.j2` (deployed at
  `play-claude-yolo.yml:279-294`): `alias ccy='/var/local/claude-yolo/claude-yolo'`.
- **Container entrypoint env exports** —
  `files/var/local/claude-yolo/entrypoint.sh:98-109`:
  - line 99: `export IS_SANDBOX=1`
  - line 103: `export CCY_DISABLE_SUSPEND=1`
  - line 108: `export CLAUDE_CODE_NO_FLICKER=1`
  - line 109: `export CLAUDE_CODE_DISABLE_MOUSE=1`
- **LSP / enabledPlugins merge** —
  `files/var/local/claude-yolo/entrypoint.sh:126-154` (jq-merges
  `ENABLE_LSP_TOOL` env and `phpantom-lsp` enabledPlugins into
  `/root/.claude/settings.json` inside container)
- **Bashrc-includes auto-source loop** —
  `playbooks/imports/play-basic-configs.yml:143-156`:
  ```yaml
  - name: Add bashrc includes sourcing
    ansible.builtin.blockinfile:
      marker: "# {mark} ANSIBLE MANAGED: Source bashrc includes"
      block: |
        if [ -d ~/.bashrc-includes ]; then
          for file in ~/.bashrc-includes/*; do
            [ -r "$file" ] && source "$file"
          done
        fi
      path: "{{ item }}"
      create: false
    loop:
      - /root/.bashrc
      - /home/{{ user_login }}/.bashrc
  ```
- **Existing include source files** (`files/home/bashrc-includes/`):
  `claude-devtools.bash`, `claude-yolo.bash.j2`, `shutdown-with-update.bash`,
  `usb-audio-fix.bash`. Existing `.inc.bash` convention on deployed host:
  `gh-aliases.inc.bash`, `lastpass-aliases.inc.bash`.
- **Existing directory creation** —
  `playbooks/imports/play-basic-configs.yml:120-129` creates
  `/root/.bashrc-includes` and `/home/{{ user_login }}/.bashrc-includes`
  (mode 0755). No need to recreate.

### Full parity audit: `cc` vs `ccy` behavioural differences

| Aspect                                                                          | `ccy` (container)                            | `cc` (host, current)                      | Worth parity?       | Notes                                                                  |
| ------------------------------------------------------------------------------- | -------------------------------------------- | ----------------------------------------- | ------------------- | ---------------------------------------------------------------------- |
| `CLAUDE_CODE_NO_FLICKER=1`                                                      | set                                          | unset                                     | **YES — this plan** | Host-safe terminal flag                                                |
| `CLAUDE_CODE_DISABLE_MOUSE=1`                                                   | set                                          | unset                                     | **YES — this plan** | Host-safe terminal flag                                                |
| `IS_SANDBOX=1`                                                                  | set                                          | unset                                     | NO                  | Container-only; bypasses root detection; wrong on host                 |
| `CCY_DISABLE_SUSPEND=1`                                                         | set                                          | unset                                     | NO                  | Container-only; paired with Ink ctrl+z Dockerfile patch; no-op on host |
| `ENABLE_LSP_TOOL`                                                               | merged into `settings.json` inside container | project-level settings may already set it | NO                  | Already handled at project level; user noted out of scope              |
| `phpantom-lsp` enabledPlugin                                                    | merged in-container                          | project-level                             | NO                  | Same as above                                                          |
| `--dangerously-skip-permissions` (YOLO)                                         | yes, enforced by wrapper                     | no                                        | NO                  | Intentional difference — that is the whole point of `ccy`              |
| Filesystem isolation (containerised workspace)                                  | yes                                          | no                                        | NO                  | Intentional; `cc` is "host power"                                      |
| Network isolation / podman networking                                           | yes                                          | no                                        | NO                  | Intentional                                                            |
| OAuth token source                                                              | `~/.claude-tokens/ccy/` (separate)           | host `~/.claude/`                         | NO                  | Intentional separation to avoid OAuth conflicts                        |
| Trust-dialog auto-accept, onboarding-flags pre-set, bypass-permissions accepted | yes (entrypoint writes `.claude.json`)       | host state persists naturally             | NO                  | Host accumulates its own state over time                               |
| SSH-key attachment via `--ssh-key`                                              | yes (ssh-agent inside container)             | host uses normal agent                    | NO                  | Host path is simpler and correct                                       |
| `claude update && claude` update-on-launch                                      | no (container-pinned version)                | yes (alias updates before launching)      | NO                  | Intentional; host can follow upstream                                  |

Only the top two rows are in scope for this plan.

### Why the sentinel check

The parity vars only work if the include was actually sourced. If a user
invokes `cc` from a non-interactive shell, from a tmux session started
before the include was deployed, or from a subshell that somehow skipped
`~/.bashrc`, the experience silently regresses. A sentinel env var
(`CC_PARITY_ENV_LOADED=1`) exported by the include lets the alias check
once at invocation time. If unset, the alias sources the include on-demand
and prints a single-line warning so the drift is visible.

## Tasks

### Phase 1: Design the include file

- [ ] ⬜ **Task 1.1**: Create
  `files/home/bashrc-includes/claude-code-env.inc.bash`
  - [ ] ⬜ Export `CLAUDE_CODE_NO_FLICKER=1`
  - [ ] ⬜ Export `CLAUDE_CODE_DISABLE_MOUSE=1`
  - [ ] ⬜ Export sentinel `CC_PARITY_ENV_LOADED=1`
  - [ ] ⬜ Top comment: purpose, cross-ref to
    `files/var/local/claude-yolo/entrypoint.sh:108-109`, note that
    `IS_SANDBOX` and `CCY_DISABLE_SUSPEND` are intentionally OMITTED
  - [ ] ⬜ File must be idempotent on re-source (pure `export` statements
    are already idempotent)

### Phase 2: Update the `cc` alias in Ansible

- [ ] ⬜ **Task 2.1**: Modify `playbooks/imports/play-claude-code.yml:51-59`
  - [ ] ⬜ Keep the existing marker
    `# ANSIBLE MANAGED: Claude Code Integration`
  - [ ] ⬜ Replace the simple `alias cc=...` with a shell function
    (still exposed as `cc`) that self-heals: if
    `$CC_PARITY_ENV_LOADED` is unset, source
    `~/.bashrc-includes/claude-code-env.inc.bash` (warning) then
    `command claude update && command claude "$@"`
  - [ ] ⬜ Use a `function` not an `alias` so that `"$@"` works and the
    sentinel check can run once per invocation
  - [ ] ⬜ Preserve `export PATH="$HOME/.local/bin:$PATH"`

### Phase 3: Ansible task to deploy the include

- [ ] ⬜ **Task 3.1**: Add a `copy` task to `play-claude-code.yml` (NOT
  `play-basic-configs.yml` — the include is Claude-Code-specific and
  belongs with its alias)
  - [ ] ⬜ Loop over `/root` and `/home/{{ user_login }}` (matches
    existing shutdown-with-update/usb-audio-fix pattern at
    `play-basic-configs.yml:166-186`)
  - [ ] ⬜ `src: "{{ root_dir }}/files/home/bashrc-includes/claude-code-env.inc.bash"`
  - [ ] ⬜ `dest: "{{ item }}/.bashrc-includes/claude-code-env.inc.bash"`
  - [ ] ⬜ Ownership / group derived from path (`{{ user_login }}` vs
    `root`)
  - [ ] ⬜ Mode `0644`
  - [ ] ⬜ Directory is already created by
    `play-basic-configs.yml:120-129` — rely on that
  - [ ] ⬜ Auto-sourcing is already wired by
    `play-basic-configs.yml:143-156` — no extra `blockinfile`
    needed

### Phase 4: Deploy and verify

- [ ] ⬜ **Task 4.1**: Run
  `ansible-playbook playbooks/imports/play-claude-code.yml`
- [ ] ⬜ **Task 4.2**: Open a fresh terminal and verify:
  - [ ] ⬜ `echo "$CC_PARITY_ENV_LOADED"` prints `1`
  - [ ] ⬜ `echo "$CLAUDE_CODE_NO_FLICKER"` prints `1`
  - [ ] ⬜ `echo "$CLAUDE_CODE_DISABLE_MOUSE"` prints `1`
  - [ ] ⬜ `type cc` shows the new function (not the old alias)
- [ ] ⬜ **Task 4.3**: Negative-path check:
  - [ ] ⬜ In a shell,
    `unset CC_PARITY_ENV_LOADED CLAUDE_CODE_NO_FLICKER CLAUDE_CODE_DISABLE_MOUSE`
  - [ ] ⬜ Run `cc --version` (or similar cheap invocation) and
    confirm: self-heal message prints, sentinel is set again, vars
    are populated for the invocation
- [ ] ⬜ **Task 4.4**: Live UX sanity check — invoke `cc`
  interactively, scroll and select text, compare perceived paging /
  flicker / selection behaviour against `ccy` baseline (user
  confirms the UX gap closed)

### Phase 5: Documentation

- [ ] ⬜ **Task 5.1**: Update
  `CLAUDE/Plan/00036-cc-ccy-parity/PLAN.md` Notes & Updates as
  phases complete
- [ ] ⬜ **Task 5.2**: If a reference to `cc` exists in `CLAUDE.md` or
  `docs/`, add a one-liner noting the new include file (skip if no
  existing mention — not creating doc for its own sake)

## Technical Decisions

### Decision 1: New include file vs in-place blockinfile into `~/.bashrc`

**Context**: Could have added another `blockinfile` in
`play-claude-code.yml` that writes the exports directly into `~/.bashrc`.

**Options Considered**:

1. Inline exports in `~/.bashrc` via `blockinfile`
   - pros: one fewer file
   - cons: breaks the established bashrc-includes convention, scatters
     Claude Code state across two places, harder to disable ad-hoc, no
     reusable sentinel pattern
2. Dedicated include file sourced by the existing auto-loop (chosen)
   - pros: matches every other Claude-related bash config in the repo
     (`claude-devtools.bash`, `claude-yolo.bash`), user can disable by
     `chmod -r` on the one file, sentinel pattern is natural
   - cons: new file

**Decision**: Option 2. The repo already has a strong
`~/.bashrc-includes/*.bash` convention (five existing live files on host),
and the auto-source loop already handles discovery. Adding to the
convention is lower friction than diverging from it.

**Date**: 2026-04-24

### Decision 2: `function cc` vs `alias cc`

**Context**: The current `cc` is an alias. Aliases can't run a sentinel
check before exec cleanly, can't use `"$@"`, and can't easily warn-and-
self-heal.

**Options Considered**:

1. Keep as alias; accept that the sentinel only works if bashrc sourced
   cleanly. If not, user sees degraded UX silently.
2. Convert to a bash function that: checks sentinel, self-heals if
   unset, then `command claude update && command claude "$@"`.

**Decision**: Option 2. A function is one extra line of bash and
directly addresses the "is my include actually loaded" question.
`type cc` still works and is still human-readable. The function name
`cc` shadows nothing real.

**Date**: 2026-04-24

### Decision 3: Extension `.inc.bash` vs `.bash`

**Context**: The repo uses both. `files/home/bashrc-includes/` source
dir has `.bash` files today (`claude-devtools.bash`,
`usb-audio-fix.bash`), but the deployed host also has `.inc.bash` files
(`gh-aliases.inc.bash`, `lastpass-aliases.inc.bash`).

**Options Considered**:

1. `claude-code-env.bash` — matches sibling source files
2. `claude-code-env.inc.bash` — matches newer convention, clearer about
   "this is a sourced include, not an executable script"

**Decision**: Option 2 (`.inc.bash`). The `.inc` infix is
self-documenting — anyone who `ls`es the dir immediately knows these
are sourced, not executed. The auto-source glob
(`~/.bashrc-includes/*`) is extension-agnostic so either works.

**Date**: 2026-04-24

### Decision 4: Deploy from `play-claude-code.yml` or `play-basic-configs.yml`

**Context**: `play-basic-configs.yml` already deploys several includes
(`shutdown-with-update.bash`, `usb-audio-fix.bash`). Could add one more
there.

**Options Considered**:

1. Add to `play-basic-configs.yml` (lower-level base config)
2. Add to `play-claude-code.yml` (keeps all Claude Code state together)

**Decision**: Option 2. The include is semantically Claude-Code-specific.
Putting it alongside the `cc` alias means both land together on any host
running the Claude Code playbook, and aren't deployed on hosts that skip
Claude Code. It also keeps the alias-plus-include pair visible in a
single playbook file.

**Date**: 2026-04-24

## Success Criteria

- [ ] `files/home/bashrc-includes/claude-code-env.inc.bash` exists and
  exports the three vars
- [ ] Fresh terminal session shows `CC_PARITY_ENV_LOADED=1`,
  `CLAUDE_CODE_NO_FLICKER=1`, `CLAUDE_CODE_DISABLE_MOUSE=1`
- [ ] `type cc` shows a function that references the sentinel
- [ ] Unsetting the sentinel then running `cc` self-heals and warns
  once
- [ ] Host `cc` paging / rendering UX matches `ccy` subjectively (user
  sign-off)
- [ ] No change to `ccy` behaviour
- [ ] Ansible playbook is idempotent (`--check --diff` clean on second
  run)

## Risks & Mitigations

| Risk                                                                                                               | Impact | Probability | Mitigation                                                                                                                                     |
| ------------------------------------------------------------------------------------------------------------------ | ------ | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| `CLAUDE_CODE_NO_FLICKER` interacts badly with some terminals (alternate screen buffer issues on certain emulators) | Med    | Low         | User can `chmod -r ~/.bashrc-includes/claude-code-env.inc.bash` to disable without re-running Ansible; include file is a single point to tweak |
| `CLAUDE_CODE_DISABLE_MOUSE=1` removes mouse interactivity that a user might rely on (scrollwheel in some widgets)  | Low    | Low         | Documented in include-file top comment; same behaviour `ccy` already has, user already tolerates there                                         |
| Converting `cc` from alias to function breaks muscle-memory tooling (e.g. scripts that rely on `alias cc`)         | Low    | Very Low    | `cc` is interactive-only in practice; no scripts source it                                                                                     |
| Include sourced twice (user already has it exported, include re-exports)                                           | Nil    | n/a         | Pure `export` is idempotent                                                                                                                    |
| Future Claude Code release changes the env var names                                                               | Low    | Low         | Include file is one place to update; monitor Anthropic docs                                                                                    |
| `type cc` output becomes less glanceable after function conversion                                                 | Low    | Med         | Keep function body to ~6 lines with a clear comment                                                                                            |

## Timeline

- Phase 1: Design the include file
- Phase 2: Update the `cc` alias in Ansible
- Phase 3: Ansible task to deploy the include
- Phase 4: Deploy and verify
- Phase 5: Documentation

## Notes & Updates

### 2026-04-24

- Plan created. Research confirmed `cc` lives in
  `play-claude-code.yml:51-59` as a pure alias, no wrapper script
  exists. `ccy` wrapper is at `/var/local/claude-yolo/claude-yolo`,
  entrypoint at `files/var/local/claude-yolo/entrypoint.sh:98-109` is
  where the parity vars live. Bashrc-includes convention is
  established and auto-sourcing loop is already in
  `play-basic-configs.yml:143-156`. Parity audit table in Context
  section tracks the full behavioural delta — only two rows (the
  terminal UX vars) are in scope for this plan.
