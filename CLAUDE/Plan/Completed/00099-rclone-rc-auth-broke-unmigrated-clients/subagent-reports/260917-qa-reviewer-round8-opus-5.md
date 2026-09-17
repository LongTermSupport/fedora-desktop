# QA Review — Plan 00099 round-8 confirmation (commit 511ac75b)

**Verdict**: FIX-BEFORE-MERGE — one record-drift item. Fixes 2 and 3 are real, complete
and verified executing; fix 1 is complete except for one stale sentence under a
now-ticked box.

## Should fix

1. **A ticked success criterion still carries the sentence saying it is unticked** —
   `/workspace/CLAUDE/Plan/00099-rclone-rc-auth-broke-unmigrated-clients/PLAN.md:229-233`

   The box reads `- [x] No helper **this plan owns** differs from its deployed copy`,
   and its own trailing line reads `**Unticked again by Task 5.9** — this plan's own
   files changed after the host run`.

   This is not deliberate history. `git show 184212bc -- …/PLAN.md` flipped three boxes
   and replaced the `**Unticked again by Task 5.9**` sentence with a
   `Ticked by Task 5.9's host re-run…` clause for criteria 1 and 5 — criterion 3's box
   flipped and its sentence was left behind. Fix: replace that sentence with what
   re-ticked it (the 2026-09-17 check [7] zero-drift pass), matching the other two.

   This matters because it is the same finding-1 class this round was asked to confirm
   closed: a reader cannot tell from the file whether the criterion is met.

## Checked and clean

- **Fix 3, the branch actually fires.** The correction sits inside
  `elif [ "$client_addr" != "$RC_ADDR" ]` (`acceptance.bash:456-468`) — the only path on
  which the addresses differ, reached after the shape guard at `:443` rejects
  non-`host:port` stdout, so it cannot fire on a garbage string. It is independent of
  `mount_count`, so it also fires on the single-mount host where `[0]` printed `1 of 1`.
- **Fix 3 is executed, not merely written.** `falsification/falsify-round5-note.bash`
  lifts check [6] verbatim and drives both branches; run just now, the differing-address
  case prints all three `COVERAGE CORRECTION` lines and the mutant still dies with 127.
  This is the branch-never-executed trap the same file records, and it is covered.
- **Fix 3 is accurate, and scoped correctly.** `[6b]` runs
  `rclone-cache-warm --fast "$rc_mount"` (`:503`) — check [0]'s mount — so [0]'s claim
  about 6b stays true and the correction rightly names only [6]. Check [1] does probe
  `$RC_ADDR` (`:234`), so "auth ENFORCED only at `$RC_ADDR`" is exact. The `ok` at `:469`
  still counts a PASS, which is correct: the client did authenticate; only the
  *enforcement* claim was over-wide.
- **No remaining over-claim elsewhere in the gate.** `CHECK_CATALOGUE:35` says "at the
  address the CLIENT resolves" — no mount claim. The `--help` text makes no mount claim.
  The `ACCEPTED` line (`:663`) speaks of declared checks and assertions, not mounts.
- **Fix 2.** `PLAN.md:254-262` now states the drift WAS present and is gone, names the
  2026-09-17 check [7] zero-drift pass, and keeps the whole-host caveat. Consistent with
  the run log.
- **Index row.** `CLAUDE/Plan/README.md:155` no longer says "Awaiting a host re-deploy";
  its `ACCEPTED` / `COVERAGE: 9 of 9` / `0 failed` claim matches the run log verbatim.
- **Journal.** `JOURNAL/00099-Journal-26-09-17.md` gained a round-7 entry covering all
  three findings; ordering clean (plan-qa sweep reports no 00099 finding).
- **No regression.** The commit touches four files; `acceptance.bash` gains only three
  `note` calls and a comment — no assertion, count, exit path or `EXPECTED_CHECKS` entry
  changed, so Task 5.9's ACCEPTED evidence still describes the current gate. No deployed
  artefact, playbook, `files/**` or `lib/*.bash` in the diff, so no CCY version bump,
  Dockerfile LABEL or re-deploy is owed.
- **Status header.** `In Progress` is correct while `- [ ] qa-reviewer returns PASS`
  stands.

## Mechanical gates

- `qa-all.bash`: PASS, 979 files.
- `plan-qa --sweep`: 8 findings, 0 block, none against Plan 00099.
- `bash -n` + `shellcheck` on `acceptance.bash`: clean.
- `falsify-round5-note.bash`: MUTANT KILLED, exit 0.
- `ansible-playbook --syntax-check`: **not triggered** — no `*.yml` in the diff.
- `qa-helper-tests.bash`, `check_extension_compat`, extension ESLint: **not triggered** —
  no `helpers/`, `tests/helpers/` or `extensions/` files in the diff.

Fix the one `PLAN.md` sentence and this returns PASS.
