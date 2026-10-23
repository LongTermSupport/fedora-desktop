# Review: merge F44 into fix/checker-owned-values-sweep (round 1)

Scope: merge commit `4c0c6d626` only (parent 1 `3949d1cd5`, the branch; parent 2 `eaa947411`, F44).
Read-only review in the worktree of `fix/checker-owned-values-sweep`.

**Verdict: MERGE.** Nothing blocks. Both sides' intent holds in all three conflicted files. The
meta-deploy queue is accurate in what it claims. The sweep script edit is correct. Four
non-blocking findings follow.

## What was re-checked

- `git show HEAD --cc`, plus `git diff` against each parent for the three conflicted files and
  for the auto-merged 00161/00201/00211 acceptance files.
- `./acceptance.bash --stage 2 --fake` (00211) exits 0. All four new F44 A3 fake cases pass
  beside the branch's product-value reads: accepted ok, unaccepted ok, join-refused and
  report-refused.
- `python3 -m unittest tests.helpers.plan_acceptance.test_cited_constant_copy_rule` passes,
  so neither side's auto-merged hunks hold an `OWNED` value copy.
- The 00201 folder's `test_m7_check` passes. `bash -n` passes on the three bash files, and
  shellcheck is clean on the 00161 and 00211 `acceptance.bash`.
- `m7_check product-value helpers/agent_bus/declare.py PIN_FILE` prints
  `files/usr/local/share/agent-bus/tuwunel.pin`. `... membership.py MEMBERSHIP_VAR` prints
  `PINGBUS_MEMBERSHIP`. `session-paths` prints its 4 lines.
- `python3 -m helpers.self_update.affected_plays --old eaa947411 --new HEAD` lists
  play-claude-yolo.yml, play-agent-bus.yml and play-agent-bus-element.yml.

## 1. Conflicted files

- `CLAUDE/Plan/00161-agent-team-bus-matrix/_acceptance-u20.inc.bash:1303-1305`: F44's
  `u20_poll_launch` is kept on both named-session waits, and the branch's `${U20_MOUNT:?}` mount
  read is kept as well.
  - No start wait is left on bare `u20_poll "${U20_START_S}"`. The only one is inside
    `u20_poll_launch` at line 185.
  - `u20_read_names` runs as a deploy leg at `acceptance.bash:263`, after F44's
    `_image-build.inc.bash` source at line 131.
  - `U20_NAMED_DIR`, once readonly and now set by `u20_read_names`, is guarded in
    `u20_named_down` (`:1270`).
  - Sound.
- `CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/_acceptance-stage2.inc.bash:916-959`:
  F44's fake pingbus, `HOME`, per-mode run dir and `fake_a3_said` are all kept. The branch's
  `ELEMENT_FLATPAK_DIR`, `ELEMENT_ACCEPTED_DIR` and `ELEMENT_ACCEPTED_FILE` reads replace the
  `.var/app/im.riot.Riot`, `.config/pingbus` and `element-accepted` copies.
  - The source-time `CCY_PINGBUS_HOME` read is unaffected by the fixture's local `HOME`.
  - F44's `a3_join` reads `EXIT_OK` and `EXIT_NOTHING` from `helpers/pingbus/cli.py`, so it is
    in line with the branch's rule.
  - Sound.
- `CLAUDE/Plan/meta-deploy.list`: see section 2.

## 2. meta-deploy.list (HEAD)

The queue is: play-agent-bus.yml, play-claude-yolo.yml, `00211/deploy-stage-2.bash`, then the
00201 folder. The F44 entries for 00213 and 00214, the old changed-plays entry and the old long
play-claude-yolo text are all gone, which is correct.

Claims checked against the code:

- **Tuwunel pin reader.** `declare.pinned_tuwunel_version` now refuses an empty version
  (`helpers/agent_bus/declare.py:482-487`). Its callers:

  - the digest, via `declared_software`;
  - ccy-teams, via `declare.py status`/`overview`, reached through
    `src-go/internal/declare/declare.go:437`;
  - 00161's probe, `triage_probe.py:202-209`.

- **Admin tool in the digest.** `helpers/pingbus/bundle.py` `PACKAGES` holds
  `helpers/agent_bus`, so the admin zipapp changes and so does the digest.

- **Version bump.** CCY 3.100.13 and container 2.79.8 are bumped on the branch side
  (`claude-yolo`, `Dockerfile`).

- **ccy-teams and ccy-sessions.** `go list -deps` shows both depend on
  `internal/teams/membership`, and ccy-teams also on `internal/teams/hot` (`remove.go`). Both
  are built by play-claude-yolo.yml (`:965`, `:975`).

- **deploy-stage-2.** It execs `deploy.bash --stage 2`, which runs, in order:

  1. the spike/stage-1 pre-flight;
  2. play-agent-bus.yml;
  3. play-claude-yolo.yml;
  4. acceptance `--stage 2`.

  This matches the description, including "both run by the entries above".

- **The 00211 checker reads listed** (session home, `STATE_DIR`, inbox/consumed/dropped names,
  Element profile and accepted paths) match `_acceptance-stage2.inc.bash:21-47`.

- **The 00201 M7 reads listed** (`CHECKOUT_SEATS`, `INSTALL_STATE_ROOT`, `STATE_ROOT`,
  `APPLIED_DIR`, `SESSIONS_DIR_TEMPLATE`) match the branch's `_acceptance-m7.inc.bash` diff.

The order is sound: the plays first, so delivery of the product change does not depend on
00211's spike gate, then the two acceptance runs. The two playbook entries repeat the plays
deploy-stage-2 runs. That has a reason (the CCY bump must ship even if the 00211 pre-flight
refuses) and costs little.

## 3. sweep_wrong_product_proofs.py

- `product_value` (`:37-41`) calls the same `m7_check product-value` the converted checkers
  call, and asserts exit 0 with exactly one line.
- The `PIN_FILE` value is relative and resolved under `ROOT` by `mutate`.
- Both reads happen while no mutation is active: the `value_row` mutations are restored in
  `finally`, and `plain_marker` is built before `MEMBERSHIP_OFF` is mutated.
- `json.dumps` builds the same `env.json` as the old literal.

Correct.

## Findings (none blocking)

1. **Low: meta-deploy.list:35-40.** The play-agent-bus.yml description says only the admin
   tool is reinstalled.
   - Problem: `bundle.py` `PACKAGES = ("helpers/pingbus", "helpers/agent_bus")`, so the pingbus
     zipapp bytes change too. The play therefore also reinstalls pingbus on the host and in the
     kit, and copies it into every running LXC member. "Nothing a user sees changes" is still
     true.
   - Separately, `affected_plays` also names `optional/common/play-agent-bus-element.yml`. It
     uses `helpers/agent_bus/element.py`, which does not import declare, so its behaviour is
     unchanged. If that play has run on this host, the freshness check may name it stale
     afterwards.
   - Fix: add one sentence on the pingbus re-copy. Either note the element play's expected
     staleness, or queue it if it runs here.
2. **Low: meta-deploy.list, 00211 entry.**
   - Problem: the stated reason for a rerun, "only a host run proves those reads", applies
     equally to the branch's other converted checkers. None of their host runs is queued:

     - 00213 `c5_cpu` (`MEMBERSHIP_VAR`);
     - 00161 U20 and the triage probe;
     - 00203 M2/M3/M4/M6/robust;
     - 00209 S1;
     - 00194 goodbye.

     The merge dropped the 00213 entry because F44 passed it, but that pass predates this
     branch's 00213 conversion.

   - Fix: either queue the reruns that are cheap and pending, or say in PLAN.md Task 2.22 that
     only 00211 and 00201 are rerun, and that the scratch-copy and `--fake` proofs stand for
     the rest. The branch side already made this choice, so it does not block the merge.
3. **Advisory: `CLAUDE/Plan/_image-build.inc.bash:20,41`** (F44).
   - The probe holds copies of the lock directory `ccy/image-builds` and of the record format
     `<pid> <began> <image>`. Their owners are `files/var/local/claude-yolo/lib/image-build-lock.bash:44`
     and `src-go/internal/ccylaunch/buildlock.go:68`.
   - Neither owner names them as constants, so the rule's `OWNED` cannot cover them. The copy
     is mitigated: `scripts/test-planlib.bash` drives the launcher's own lock library.
   - The same applies to F44's `a3_failed` grep (`_acceptance-stage2.inc.bash:415`) of
     config.py's inline Element refusal text (`helpers/pingbus/config.py:447`).
   - Fix: add both to Task 2.22's "REFERRED TO THE OWNER" list (inline values the product does
     not name).
4. **Nit: `sweep_wrong_product_proofs.py:251`.**
   - `plain_marker` appends the literal `=off`, a copy of `MEMBERSHIP_OFF`'s pre-mutation value.
   - Fix: `product_value("helpers/pingbus/membership.py", "MEMBERSHIP_OFF")` before the mutate.
     The mutate anchor necessarily stays literal.
