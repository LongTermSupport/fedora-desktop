# Plan 00145: ccy key menu steers away from keys that cannot push

**Status**: In Progress
**Created**: 2026-10-02
**Owner**: joseph
**Priority**: Medium
**Type**: Bug Fix

## Overview

At launch, ccy's SSH key menu (`discover_and_select_ssh_keys` in
`files/var/local/claude-yolo/lib/ssh-handling.bash`) probes every `~/.ssh/github_<alias>`
key for push access to the project's remote. It marks the keys that have it, and makes a
lone match the ENTER default. It then lists every identity anyway, and accepts any number
typed without a word. The owner typed the number of a key the probe had just shown cannot
push, and only found out when the session's first `git push` was refused.

The probe already knows the answer, so the fix is in the menu. When at least one key
can push, the menu offers only those keys. The full list is one keystroke away, and
picking a key from it that cannot push needs an explicit "yes". When no key can push (a
brand-new repo, a remote nobody has access to yet, no remote at all), the menu is the
full list it is today, with no extra question, because there is nothing better to steer
towards.

## Goals

- With one or more push-capable keys, the first menu lists only them; ENTER takes the
  first.
- The full list stays reachable from that menu (`a`), and picking a key in it that the
  probe did not mark asks `Use it anyway? [y/N]`; anything but `y` re-prompts.
- With no push-capable key, the menu and its behaviour are unchanged.
- The menu is covered by unit tests in the existing `ccy-ssh-handling` QA gate.

## Non-Goals

- Changing how push access is probed (`probe_gh_keys_for_remote` is unchanged).
- Probing the project remote's own key (deploy-key alias) or the forwarded ssh-agent for
  push access. Both stay in the full list as unverified identities.
- `--ssh-key` on the command line: an explicit key is the user's statement and is not
  second-guessed here.

## Tasks

### Phase 1: Tests first

- [x] ✅ **Task 1.1**: Add menu cases to `scripts/test-ccy-ssh-handling.bash`, driving
  `discover_and_select_ssh_keys` with stubbed discovery and probe functions and scripted
  stdin: the short list, ENTER, `a` then a verified key, `a` then an unverified key with
  `n` then `y`, `0`, and the unchanged menu when nothing can push. Six of the nine failed
  against the old menu, "one key can push, 1 takes it" among them (it took `alpha`).

### Phase 2: The menu

- [x] ✅ **Task 2.1**: Implement the short list, the `a` escape and the confirmation in
  `discover_and_select_ssh_keys`; bump `CCY_VERSION` to 3.71.0 and add the
  `docs/ccy-changelog.md` entry. The confirmation prompt is listed in
  `scripts/test-ccy-session-registry.bash` as a sub-prompt a restored launch cannot be
  waiting at unattended.
- [x] ✅ **Task 2.2**: `./scripts/qa-all.bash` green; `qa-reviewer` agent over the diff.
  The review (`subagent-reports/261002-qa-reviewer-opus.md`) found no BLOCK. Its
  FIX-BEFORE-MERGE findings are fixed and tested: an over-long number slipped through the
  range test as a pick; the remote's key and the agent, never probed, were called unable to
  push; ENTER after `a` could land on a key needing the question; the loop had no retry
  cap (now 3, per CLAUDE/InteractiveScripts.md).

### Phase 3: Host

- [ ] 🚫 **Task 3.1**: **HOST**: deploy `play-claude-yolo.yml`, launch ccy in a project two
  keys can see but only one can push to, and check the menu offers only that one. Blocked
  on the owner: Ansible never runs in the ccy container, and the menu needs a person.

## Success Criteria

- [ ] A launch in a project with a push-capable key cannot end up on a key that cannot
  push without the user answering `y` to a question that says so.
- [ ] A launch in a project no key can push to shows today's full menu.
- [ ] The `ccy-ssh-handling` gate covers every branch of the new menu.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00145-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- (none yet)
