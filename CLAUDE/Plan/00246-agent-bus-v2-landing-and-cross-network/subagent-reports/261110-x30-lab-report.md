# X30 lab: #209 landed and the scenario built (Task 8.8, 2026-11-10)

## Part 1: PR #209 on F44

- **Merge.** Merged with `--no-ff` as `4551a5bb5`. The conflicting files were all clean before
  the merge. Each conflict was resolved as the union of both sides:
  - PLAN.md: F44's S12 to S15 text, plus #209's paragraph.
  - `app.go`: F44's `Env.GOARCH` and #209's `FreeUDPPorts`.
  - `kit_test.go`: both sides' fields.
  - `meta-deploy.list`: F44's queue, with `x30-triage.bash` appended. The branch's 00201 and
    00249 entries had already run and were removed on F44, so they were not re-added.
- **Review: what is sound.**
  - Fail-fast holds throughout.
  - A failed boot drops the member's links record.
  - The RAM sum fails rather than counting a guest it cannot read as zero.
  - `--as` is checked against a login-name pattern, so an option cannot ride in as a user.
  - The triage is read-only, and placing it in the plan folder is right.
  - The tests drive the CLI through the kit's fakes, not unit internals.
- **Review: what was fixed**, in a follow-up commit, test first and seen red:
  1. F44's blocking rule `project_walk_open` (#191) failed on `link.go`. `memberRecord` opened
     a record found by a walk with no regular-file check, so a named pipe there would hang
     `link-down`, `member-up` and `member-address`. The rule already existed and caught this,
     so only the instance needed fixing. Non-regular records and ports files are now refused
     (E67a). The test plants a pipe with `fifotest`.
  2. `link-up` could record a port another link already holds. A link's ports are bound only
     when its members boot, so the free-port probe cannot see them. `link-up` now draws again,
     up to 8 times, then refuses.
     - No detector can express this class; it is fixed conventionally.
     - The sweep found no other instance: a member's SSH port is bound by passt before
       `member-up` returns.
- **F44's other newer rules.** `command-always-changed`, `duplicate-managed-block` and
  `arch-pinned-artefact` have nothing to judge in #209, which has no Ansible. The
  `arch-pin` gate passed on the merge.

## Part 2: what X30 lacked

**D1, the lab's subnets.**

- The host_vars mapping `vm_test_lab_networks` has no default.
- `play-vm-test-lab.yml` passes the mapping and `ip -j -4 route show table all` to the new
  helper `helpers.vmtest.lab_networks`, which has a unittest. The helper refuses:
  - a bad name;
  - a network that is not canonical IPv4 from /8 to /27;
  - overlapping networks;
  - any network over a non-default route.
- The play writes `VMTEST_NET_<NAME>` into `lab.env`.
- `vmtest lab-network <name>` (E74, Go, test first) reads it back.
- `localhost.yml.dist` shows an example.

**`x30-guest.bash`** is the fixture, with these steps: net, firewalld, zone, issuer,
block-ssh-out, hosts and stage-seat.

**`x30-lab.bash`** is the scenario: four guests on N1, N2 and N3. It checks:

- the networks and the routed path;
- the password sudo (D7);
- that egress ssh is blocked, with a control;
- the play on the bus with a DNS url and on the workers with no team;
- that the unit files equal `units print`;
- that TLS is pinned to the team CA;
- that plain http gets no answer;
- that the prober is refused fast;
- the relay-form issue;
- worker to worker in both directions with the coordinator absent (W1, W2);
- the host move in D6's order (M1 to M4).

**Queued in `meta-deploy.list`.** `changed-plays` picks up the lab play, and its description
says to set the mapping first. Then `x30-triage.bash`, then `x30-lab.bash`.

**Nothing has run on a host.** Only `bash -n`, the Go tests and the helper's tests ran here.

## Gaps in the code under test, met while writing the scenario

- **No take from the pool into a session.** No production code takes a seat that
  `place-issued` put in the pool into a session (`seat.Store.Take` has no caller; this is Task
  4.2). The fixture copies `seat.json` and the token itself, and the report says so.
- **`teamchat-admin restore` is refused in the production build "until DC8".** The host move
  therefore stops at the restore, and the scenario exits 2 (COULD NOT ESTABLISH).
  **Owner decision: DC8.**

## Faults found (dogfooding), and whether they are fixed or logged

- **#233 (logged):** `plan_deploy_leg` and `plan_gather_leg` run a leg as an `if` condition,
  so errexit is off inside the leg. A leg that fails half way can still pass. `x30-lab.bash`
  works around it with `|| return 1` on every statement.
- **#234 (logged):** D5. The play ledger records plays run against remote hosts, so the
  scenario turns the ledger off for its guest plays.
- **Not logged:** the hook `R-WRITE-OUTSIDE-PROJECT-ROOT` resolved `../untracked/...` against
  the session cwd, not the `cd` in the same command, and denied the write.
- **Not logged:** the shellcheck lint hook raises info-level SC2329 on every function that is
  passed to `plan_deploy_leg`. The QA gate counts only errors and warnings, so the hook and
  the gate disagree.
- **My slip:** one batched command piped to `tail`, which the brief forbids. The hook blocked
  it, so it never ran.

## Not done, and why

- **The helper's test was not seen red first.** It was written in the same batch as the
  helper, after a scanner blocked the first version of the test, so its failing state was
  never observed.
- **The qa-reviewer agent was not run.** It is the required final step of a plan, and this
  brief did not ask for it.
- **Nothing was pushed**, as the brief says.
