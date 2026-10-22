# Plan 00215 qa-reviewer, round 2 (`feat/opencode` at d5563c6d8)

Saved by the coordinator from the qa-reviewer's reply (the agent is read-only).

**Verdict: FIX-BEFORE-MERGE.** Two small fixes, nothing blocking.

## Should fix

1. **The play does not create the provider-key directory.** `~/.claude-tokens/ccy/provider-keys/`
   is named nowhere in `playbooks/`; the play makes its siblings `tokens/` and `projects/` at
   0700 (`playbooks/imports/play-claude-yolo.yml:1235-1249`). Fix: a matching 0700 task; the key
   file stays the owner's.
2. **The changelog contradicts meta-deploy.** The 3.100.6 entry says "the next launch rebuilds
   it once"; meta-deploy and the play (`play-claude-yolo.yml:1265`, "the play always builds")
   say the play builds it. Fix: reword the changelog.

## Nit

- The `FROM claude-yolo:base` gap (Task 1.4) should also be a line in `docs/ccy.md` "opencode".

## Checked and clean

Versions (CCY 3.100.6, LABEL and `REQUIRED_CONTAINER_VERSION` 2.79.1, changelog); placement
(`agent-choice` in `CCY_LIBS`, sourced, in the play's install list, no new play); gate wiring
(`qa-gates.tsv`, `CLAUDE/QA.md`, `addedgates_test.go`); README index row; bash-limits allowlist
shrank; plan task states; the `launch-opencode` golden shows the real `podman run` with the key
by name only, `CCY_NO_SUPERVISOR=1`, `PINGBUS_MEMBERSHIP=off`; no public-repo leaks.
`test-ccy-agent-choice.bash` 65 passed; `plan-qa --sweep` nothing for 00215.

## Answered (next commit)

All three: the play's "Create Claude YOLO Provider Keys Directory" task (0700); the changelog
says the play builds the image; `docs/ccy.md` names the `:base` gap and `ccy --rebuild`;
meta-deploy says the play makes the directory.
