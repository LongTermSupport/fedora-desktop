# Fix round 3: the noauto follow-up review's findings

Review: `noauto-followup-review.md` (committed with the first fix). Branch
`plan-xhost-lxc-members`. Every finding was re-checked against the code first; all held.

| #   | Finding                                       | Result | Commit    |
| --- | --------------------------------------------- | ------ | --------- |
| 1   | 00203 R9 passes a missing or masked unit      | fixed  | 80557cac2 |
| 2   | no-autostart acceptance coverage and verdicts | fixed  | 3a1627222 |
| 3   | meta-deploy.list descriptions stale           | fixed  | 3a1627222 |
| 4   | 3.122.34 changelog claims an image change     | fixed  | 2c6c2d8a3 |
| n   | SPEC-n3 D22, PROPOSAL-ssh-members notes       | fixed  | b28ac112d |

DBF: none of these is a class a semgrep rule or analyzer can express (each is a verdict
rule judging the wrong state, or prose that says something untrue), so each is fixed
conventionally with a red/green harness.

## 1. R9 judges LoadState and the exact UnitFileState

`_acceptance-robust.inc.bash`: `systemctl show` now also gives `LoadState`, and
`r9_unit_judged` passes only `LoadState=loaded`, `UnitFileState` static or disabled,
`Restart=no` and no wants link. ACCEPTANCE.md R9 and PLAN.md Task 2.4 say the same.

Harness `untracked/scratch/r9-judge-test.bash` (sources the function; 14 cases, now with
missing, masked, linked, empty and error states). Red, the old rule:

```
old rule: 'Restart=no UnitFileState= ' PASSES (bug)
old rule: 'Restart=no UnitFileState=masked ' PASSES (bug)
```

Green: `GREEN: every case passed`.

## 2. lxc-no-autostart-acceptance.bash

- `checkUnit` sets `unitFound` only once the unit is seen present; v1 and v2 counts add it.
- Per generation: fewer units found than online service members declared is COULD NOT
  ESTABLISH (exit 2), for v1 and v2 alike (v2 was only the zero case).
- A run that judged no unit and proved no boot is exit 2 with its reason ("every declared
  container is offline ... nothing was judged", or no unit found and no reboot proven).
- A failed `lxc-stop` or `lxc-start` in the owner-approved reboot is exit 2 with a message;
  `lxc-start` is not tried after a failed stop.
- The header states the exit-2 cases.

Harness `untracked/scratch/noauto-accept-test.bash`: copies the script into a fake tree with
`_planlib`/`_members` stubbed, a fake `sudo` (lxc-attach runs each probe against a fake
per-container `/etc/systemd/system`), and drives the reboot prompt through `script` (a pty).
Red against the script before the fix (`noauto-accept-red.log`):

```
ok   all units present (exit 0)
FAIL v2 fewer units than declared: want exit 2 ..., got 0   (printed PASS)
FAIL v1 counts only found units: ... got 1, and "COVERAGE v1: 1 unit(s) checked for 1"
FAIL every container offline: want exit 2 and [nothing was judged], got 0   (printed PASS)
FAIL lxc-stop fails during reboot: want exit 2 and [lxc-stop], got 1
FAIL lxc-start fails during reboot: want exit 2 and [lxc-start], got 1
ok   clean reboot (exit 0)
RED: 5 scenario(s) failed
```

Green after the fix: all 7 scenarios ok (`noauto-accept-green.log`).

## 3. meta-deploy.list

The claude-yolo entry names ccy 3.122.34 / container 2.84.40 (the final versions; no bump in
this round) and that the Start line shows only on this host's `service: true` LXC member.
The acceptance entry says its checks are read-only, a stopped container is reported offline
and skipped, a reboot happens only on the owner's yes at the prompt, and what exit 2 means.
Order unchanged; the Q15 sudo play stays last. `test-meta-deploy.bash` passes.

## 4. The 3.122.34 changelog entry

Checked: `scripts/git-hooks/lib/ccy-image-source-paths.bash files/var/local/claude-yolo`
regenerates exactly the committed `image-source-paths`, which has no `bus/declare` (its
`go list -deps` over the builder stage's packages is what that script runs). The entry now
says nothing in the image changed and the bump to 2.84.40 was not needed (costing one rebuild
that changes nothing inside). Versions are not lowered. The earlier report's
"`bus/declare` is image source" line carries a correction.

Hook fault? No. Replaying commit 559481d83 through `ccy-image-staged.bash` (a temporary index
of that tree, `CCY_BASE=559481d83^`, its non-launcher files) listed no image file, so the
pre-commit hook would not have demanded the container bump: it was the agent's choice. Nothing
was added to issue #352.

## Nits

- SPEC-n3 n3-D22: `lxc.start.auto` removed from the keys read, with a pointer to B.12.
- PROPOSAL-ssh-members.md: one short Plan 00257 note at each of the four places (two table
  cells, the "Starting at boot" bullet, the probe list); no rewrite.

## QA

- `qa-changed.bash` (dummy vault word): passed over all changed files, before the commits.
- `python3 -m unittest tests.helpers.agent_bus_lxc.test_offline_member_rule`: OK.
- shellcheck on both changed scripts: clean.
