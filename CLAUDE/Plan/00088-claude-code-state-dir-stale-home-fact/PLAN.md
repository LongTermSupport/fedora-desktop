# Plan 00088: claude code state dir stale home fact

**Status**: Not Started
**Created**: 2026-08-24
**Owner**: joseph
**Priority**: Medium

## Overview

A downstream deployment repo's live proof of `playbook-main.yml` on a real guest
(the first run in that harness able to actually exercise Plan 00086's
kernel-modules fix — see that plan) got past every prior defect in this proof
chain and then failed deep in `play-claude-code.yml`: "Check Whether the Claude
Code State Directory Exists" hard-failed with `Permission denied: [Errno 13] Permission denied: b'/root/.claude'`.

Root cause, confirmed by reading the actual source rather than guessed from the
log: `play-claude-code.yml` computes `claude_state_dir: "{{ ansible_facts['env']['HOME'] }}/.claude"` (line 18) as a play-level var. This
play runs with `become: false` — its own tasks should run as the real login user
— but `ansible.cfg` sets `gathering = smart` with a **persistent** on-disk fact
cache (`fact_caching=jsonfile`). The very FIRST play in `playbook-main.yml`,
`play-AA-preflight-sanity.yml`, runs with `become: true` at the PLAY level, which
elevates its implicit `setup` fact-gathering task too — so `ansible_facts['env'] ['HOME']` is captured as `/root` (root's home, via `sudo_flags=-HE`'s `-H`) at
the very start of the run. Every later play targeting the same host — including
`play-claude-code.yml`, run tens of plays later — inherits that SAME cached,
stale-for-its-own-context fact under `gathering=smart`, rather than re-gathering
fresh as the connecting user. This is deterministic and would reproduce on any
fresh host, not an artefact of this guest's history: confirmed by reading
`ansible.cfg` (`gathering = smart`, `fact_caching=jsonfile`) and
`playbook-main.yml`'s import order (preflight-sanity is play #1, `become: true`).

The rest of `play-claude-code.yml` already knows not to trust
`ansible_facts['env']['HOME']` for this exact reason: five other places in the
SAME file (lines 50, 61, 82, 99, 230, 259) build the real user's home explicitly
as `/home/{{ user_login }}` rather than reading it from gathered facts.
`claude_state_dir` is the one place in the file that never got the same
treatment.

This is a sixth, unrelated defect surfaced downstream while proving PR #33
(`RUN_BASH_GITHUB_ACCOUNTS=none`) end-to-end — none of the mechanisms proven so
far (PR #33/#34/#35/#36/#37) are implicated.

## Goals

- Make `claude_state_dir` resolve to the real target user's home reliably,
  independent of which play in `playbook-main.yml` happened to gather facts
  first and under what privilege context.
- Bring `claude_state_dir` in line with the rest of `play-claude-code.yml`'s
  own established convention (`/home/{{ user_login }}/...`), not invent a new
  pattern.

## Non-Goals

- Not changing `ansible.cfg`'s `gathering`/`fact_caching` settings, and not
  changing `play-AA-preflight-sanity.yml`'s `become: true` — both are
  legitimate on their own terms; the fix is to stop this one play depending on
  a fact that another play's privilege context can poison.
- Not auditing every other play for the same `ansible_facts['env']['HOME']`
  pattern — out of scope for this plan; flagged as a possible follow-up in the
  journal, not actioned here.

## Tasks

### Phase 1: fix

- [ ] ⬜ **Task 1.1**: change `claude_state_dir` in `play-claude-code.yml` from
  `"{{ ansible_facts['env']['HOME'] }}/.claude"` to
  `"/home/{{ user_login }}/.claude"`, matching the file's own established
  pattern for resolving the real target user's home.
- [ ] ⬜ **Task 1.2**: `ansible-playbook --syntax-check` clean;
  `./scripts/qa-all.bash` passes.
- [ ] ⬜ **Task 1.3**: commit, push on a branch, open PR against `F44`.
- [ ] ⬜ **Task 1.4**: independent review (peer review agent).
- [ ] ⬜ **Task 1.5**: merge once PASS/PASS WITH NITS.
- [ ] ⬜ **Task 1.6**: re-pin lts-infra's live-proof harness at this merged
  commit and re-run it — see lts-infra Plan 00045.

## Success Criteria

- [ ] A downstream live proof re-run gets past "Check Whether the Claude Code
  State Directory Exists" without a hard failure, and `claude_state_dir`
  resolves to the real login user's home (`/home/{{ user_login }}/.claude` on
  the proof guest), not `/root/.claude`. **Not yet run** — pending Task 1.6.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00088-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Recovery cron: 28837729 (shared session-wide failsafe, already running).
