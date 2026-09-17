# Plan 00133: ccy auto compact window default and daemon check

**Status**: In Progress
**Created**: 2026-09-17
**Owner**: joseph
**Priority**: Medium

## Overview

Claude Code reads `CLAUDE_CODE_AUTO_COMPACT_WINDOW` to decide how large a
session may grow before auto-compaction summarises it. Left unset, the window is
whatever the model's default is; set too high, a long session accumulates
context — and cost — well past the point where compacting would have been
cheaper. CCY launches every session in this workflow, so CCY is the right place
to impose a sane default, and a project that genuinely wants a different window
should be able to say so declaratively in its own tracked `ccy.env`.

That is one half. The other half is independent detection: the hooks daemon
should notice when a project's window is absent or larger than the agreed
ceiling and say so, regardless of how the session was launched. The two
deliverables are deliberately complementary rather than redundant — CCY
configures, the daemon observes. A project already launched by CCY passes the
daemon's check silently, because it is already correct; a project launched some
other way gets told.

The daemon is an **external upstream project**, vendored here but not patchable
here, so that half is an issue to file upstream rather than code to write. This
repo's contribution to it is a correctly generated, verified issue body — and
the plan is explicit that the body must be produced by `hooks-daemon issue-report`, never hand-drafted, for reasons recorded under Task 3.1.

## Established facts

Everything in the tasks below rests on measurements already taken, not
assumptions. The full evidence — recovered strings, byte offsets, method, and
the precedence chain traced through the launcher and entrypoint — is in
[`research/auto-compact-window-facts.md`](research/auto-compact-window-facts.md).
The load-bearing findings:

- **The variable is real and honoured.** Confirmed by extracting it, with its
  parse-error and precedence strings, from the shipped Claude Code CLI binary.
  Both deliverables are live work, not inert.
- **The unit is tokens.** Accepted grammar is `auto`, or `100k..1M` tokens, with
  `600k`, `600000` and `600` all equivalent. This plan specifies **`600k`**.
- **The env var beats the `autoCompactWindow` setting.** Setting it means the
  window can no longer be changed from `/config`.
- **The effective threshold is `min(setting, model context window)`**, which is
  why `600k` is the right figure: it is a 1M-context guard, and 1M-context models
  are what this project runs.
- **Upstream recommends `auto`.** Overriding it is a deliberate, defensible
  choice, but it must be commented as such so it is not reverted as an error —
  and it raises a real design question for the daemon check, carried into
  Task 3.2.
- **A project's `ccy.env` genuinely overrides the default**, because the
  entrypoint sources it after the launcher's environment is in place and before
  it `exec`s `claude`.

## Goals

- CCY sets `CLAUDE_CODE_AUTO_COMPACT_WINDOW=600k` by default for every session
  it launches, using the same `${VAR:-default}` idiom as its sibling variables.
- A project overrides that default with one `export` line in its tracked
  `.claude/ccy/ccy.env`, and that override demonstrably wins.
- The override and the precedence order are documented in `docs/ccy.md`
  alongside the variables already there.
- A correctly generated upstream issue asks the hooks daemon to warn when the
  window is unset or above the ceiling, with a per-project override — filed by
  the owner, from a `hooks-daemon issue-report` body.
- The default is verified by observation on a real session, not by reading the
  diff.

## Non-Goals

- **Patching the hooks daemon.** It is an external upstream project. This plan
  files an issue; it does not write daemon code, and it does not edit anything
  under `.claude/hooks-daemon/`.
- **Filing the issue from inside this container.** The body must be generated,
  and the filing is the owner's action. See Task 3.1.
- **Choosing `600k` on technical grounds.** It is the operator's stated figure
  and this plan implements it. The plan records where it does and does not bite;
  it does not relitigate the number.
- **Changing the `autoCompactWindow` setting, `/config`, or any Claude Code
  settings file.** CCY sets an environment variable and nothing else.
- **Imposing the window on non-CCY sessions in this repo.** That is exactly the
  gap the daemon check exists to cover, which is why it is a separate half.
- **Baking the value into the container image.** Rejected with reasons in the
  research document.

## Tasks

### Phase 1: Establish (largely discharged)

- [x] ✅ **Task 1.1**: Verify `CLAUDE_CODE_AUTO_COMPACT_WINDOW` exists and is
  honoured by the installed Claude Code. **Done** — extracted from the shipped
  CLI binary with its parse-error and precedence strings. Had this failed, both
  deliverables would have been inert, which is why it was settled first.
- [x] ✅ **Task 1.2**: Establish the unit and accepted grammar. **Done** —
  tokens; `auto` or `100k..1M`; `600k` is valid and is the form to use.
- [x] ✅ **Task 1.3**: Establish where CCY should set it and prove the
  precedence chain leaves a project's `ccy.env` winning. **Done** — the
  launcher's `-e` block; chain traced through the entrypoint's `ccy.env` source
  to the `exec`.
- [x] ✅ **Task 1.4**: Establish how the daemon would warn and which component
  owns it. **Done** — `optimal_config_checker` already audits sibling env vars,
  so the upstream ask is an added check, not a new handler.
- [x] ✅ **Task 1.5**: **Owner decision — settled: `600k`.** The figure is the
  operator's and is not up for relitigation. Losing the `/config` control for the
  window is the accepted consequence of setting the variable at all.

### Phase 2: The CCY default

- [x] ✅ **Task 2.1**: Add the forwarded default to the `container_cmd run`
  argument list in `files/var/local/claude-yolo/claude-yolo`, immediately
  alongside the existing Claude Code environment flags, as
  `-e "CLAUDE_CODE_AUTO_COMPACT_WINDOW=${CLAUDE_CODE_AUTO_COMPACT_WINDOW:-600k}"`.
  The `${VAR:-default}` form is what preserves host and project override.
- [x] ✅ **Task 2.2**: Add a short comment at that line recording **why** the
  default overrides a value upstream calls recommended, and that `600k` is a
  1M-context guard. Without it, a later reader sees only an override of a
  recommended setting and reverts it. Keep it to the current state — no history.
  The comment sits in a banner block immediately above `container_cmd run`, the
  file's existing convention for such notes: a comment inside the backslash-
  continued argument list is too fragile to keep.
- [x] ✅ **Task 2.3**: **Mandatory version bump.** Increment `CCY_VERSION` in
  `claude-yolo` and update its one-line description. The script self-checks its
  version against a stored hash and will refuse to run if the bump is missed, so
  this is not a formality.
- [x] ✅ **Task 2.4**: Add the matching entry to `docs/ccy-changelog.md`,
  required by the bump in Task 2.3.
- [x] ✅ **Task 2.5**: Add a row for the variable to the **"Claude Code
  environment CCY sets"** table in `docs/ccy.md`. That section already states
  the precedence rule in prose, so the row inherits it.
- [x] ✅ **Task 2.6**: Document the per-project override in `docs/ccy.md`'s
  `ccy.env` section, with a worked example. The example **must** use `export`
  — a bare assignment does not survive the `exec` into `claude`, as the
  entrypoint's own comment records.
- [x] ✅ **Task 2.7**: Run `./scripts/qa-all.bash`. Required before any commit
  touching Bash.

### Phase 3: The upstream daemon issue

- [ ] ⬜ **Task 3.1**: **Generate** the issue body with `hooks-daemon issue-report`, which writes to `untracked/issue-reports/`. Rule
  `R-UPSTREAM-ISSUE-UNVERIFIED-BODY` blocks `gh issue create` against that
  tracker with any body a generator did not produce: the tracker is **public**
  and an issue cannot be retracted. **Do not hand-draft a body anywhere in this
  plan folder** — Plan 00075 was closed precisely because its hand-drafted
  `upstream-report.md` could no longer be filed, and repeating that wastes the
  work twice.
- [x] ✅ **Task 3.2**: **Owner decision — settled**, so the generated body states
  it rather than asking. The daemon warns when the window is unset, when it is
  `auto`, or when it exceeds the ceiling; the ceiling is per-project configurable
  in the daemon config and defaults to `600k`. `auto` warns because it defers to
  the model rather than capping — the whole point of the check. No exemption for
  a model whose own window is smaller: the setting is a ceiling, and a ceiling
  that never binds is still correctly set.
- [ ] ⬜ **Task 3.3**: Ensure the generated body asks for the check to be added
  to the existing `optimal_config_checker`, which already audits sibling Claude
  Code environment variables, rather than for a new handler — a smaller and more
  acceptable request. Include the per-project config override from Task 3.2.
- [ ] ⬜ **Task 3.4**: **Owner action, outside this container.** Review the
  generated body and file it upstream. Nothing in this plan files it. Record the
  resulting issue URL in `JOURNAL/`.
- [ ] ⬜ **Task 3.5**: Confirm the body carries no identifying detail — this is
  a public repo posting to a public tracker, and the pre-commit secret scanner
  does not cover the `gh` CLI.

### Phase 4: Verify

- [ ] ⬜ **Task 4.1**: **HOST run.** Deploy the updated `claude-yolo` to the
  host via the owning playbook. Ansible must never run in this container.
- [ ] ⬜ **Task 4.2**: **HOST run.** Start a session with no `ccy.env` override
  and confirm from inside it that the variable is `600k` — read the live
  environment, do not infer it from the launcher source. A check that only
  re-reads the diff vouches for nothing.
- [ ] ⬜ **Task 4.3**: **HOST run.** Confirm Claude Code has actually *accepted*
  the value rather than merely received it: `/config` labels the window's source
  explicitly, and should attribute it to the environment variable. This is the
  step that distinguishes "the variable is set" from "the setting is in force".
- [ ] ⬜ **Task 4.4**: **HOST run.** Set a different value via `export` in a
  project's `ccy.env`, restart, and confirm the project's value wins. This is
  the override requirement from the brief and the one most likely to be silently
  wrong.
- [ ] ⬜ **Task 4.5**: **HOST run.** Confirm a host export before launch also
  overrides the default, exercising the `${VAR:-default}` idiom.
- [ ] ⬜ **Task 4.6**: Run the **`qa-reviewer`** agent as the final step, per
  `CLAUDE.md`. `qa-all.bash` is mechanical and passes green on work that is
  structurally wrong.

## Success Criteria

- [ ] A CCY session started with no project override reports
  `CLAUDE_CODE_AUTO_COMPACT_WINDOW=600k` from its live environment.
- [ ] Claude Code attributes its auto-compact window to the environment
  variable, confirming the value is in force and not merely present.
- [ ] A project setting the variable with `export` in its tracked
  `.claude/ccy/ccy.env` gets its own value, not `600k`.
- [ ] A host export before launch also overrides the CCY default.
- [x] `CCY_VERSION` is bumped and `docs/ccy-changelog.md` carries the entry.
- [x] `docs/ccy.md` documents the variable, the override with a correct
  `export` example, and the `min(setting, model window)` caveat.
- [ ] `./scripts/qa-all.bash` passes.
- [ ] The `qa-reviewer` agent reports no findings.
- [ ] An upstream issue exists, generated by `hooks-daemon issue-report`, asking
  for the check in `optimal_config_checker` with a per-project override —
  and its URL is recorded in `JOURNAL/`.
- [ ] No hand-drafted upstream issue body exists anywhere in this plan folder.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00133-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 1 established from measurement: the variable is real, takes tokens, and
  a project's `ccy.env` genuinely overrides the CCY default.
