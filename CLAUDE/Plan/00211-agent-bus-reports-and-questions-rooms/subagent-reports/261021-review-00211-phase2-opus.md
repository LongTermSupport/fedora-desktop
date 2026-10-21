# Review: Plan 00211 Phase 2 (branch agent-a1a822193f01dd6b3-255f4a9c, tip da8ba5ae)

Read-only adversarial review, standing in for Fable, covering the qa-reviewer checklist.
Targeted tests run in the branch worktree: `test_admin_side_rooms.py`, `test_admin.py`,
`test_cli.py` (116 passed); `acceptance.bash --stage 2 --fake` (FAKE ACCEPTED).

## VERDICT: FIX-FIRST

The admin side (`helpers/agent_bus/admin.py`) is sound and well tested. What has to be fixed
first is in the acceptance: when there is no LXC coordinator, it skips and still passes, which
the Fail Fast rule forbids. As written, the plan may never be able to complete on the real host.

## Blocking

### B1. The test question and A5 assume the coordinator is an LXC member: it skips, then reports ACCEPTED

`_acceptance-stage2.inc.bash` `ask_test_question` (lines 216-224) picks the first present
`agent_bus_lxc_members` entry with `.role == "coordinator"`. When there is none, it prints a line
and **returns 0**. Stage 2 then prints `ACCEPTED` and `NEXT (the owner): the Element steps`.
Steps 4-6 of ACCEPTANCE-owner.md need a test question, and none was asked. `--after-owner`
(A5) then refuses ("no stage-2 run asked a test question"). This is the forbidden "skip and warn"
pattern: the verdict says ACCEPTED while one of SPEC §11.2's checks did not run.

This is a design error, not a corner case. The docs say a team has **at most one coordinator**
("a second coordinator is refused"), and on most teams the coordinator is a ccy session. A
throwaway coordinator (like A3's throwaway worker) therefore cannot be added without demoting
the real one. On a host where every coordinator is a ccy session, A5 can never run, so Task 4.2
(the fixtures A5 records) and the plan cannot complete. SPEC §11.2 says "the coordinator's
`pingbus history`". It does not say an LXC coordinator.

Fix, either way:

- Drive the real coordinator wherever it runs. For a ccy seat that means `podman exec` into its
  session container as the desktop user (rootless; the container has the read-only membership
  mount and `PINGBUS_MEMBERSHIP`, `files/var/local/claude-yolo/lib/agent-bus-seats.bash`). Pick
  the coordinator from `agent-bus list` (the registry holds the role), not from the
  `agent_bus_lxc_members` `.role` field, which can disagree with the registry.
- Or, as a minimum, make "no drivable coordinator" a REJECT (exit 1) that names why and what to
  declare, so stage 2 does not end with ACCEPTED and a NEXT the owner cannot complete.

Also fix the meta-deploy.list description, which says the run "asks a test question from an
LXC coordinator" and "posts two test reports (one from an LXC member)". Neither is unconditional.

### B2. The changelog link is wrong

`docs/ccy-changelog.md` says "Follows 3.99.13.7 (container 2.78.7)". F44 is at 3.99.13.8 /
2.78.8 (Plan 00213), so it must say "Follows 3.99.13.8 (container 2.78.8)". The branch's
3.99.13.9 / 2.78.9 is the correct next pair, but the merge will conflict on `CCY_VERSION`, the
Dockerfile LABEL and `REQUIRED_CONTAINER_VERSION`. Resolve to .9 / 2.78.9 and keep the two
container values equal. meta-deploy.list will also conflict: F44's c110c025 already dropped
the spike and stage-1 entries, which is the same intent, so keep only the stage-2 entry.

## Should fix (not blocking alone)

### S1. A4 also skips and passes

`a4_check` returns 0 when no LXC member is present. That matches stage 1's S2 precedent, but
the SPEC calls LXC "top priority". At least make the report and the final ACCEPTED line say
that A4 was not judged. As things stand, the ACCEPTED line claims "took reports".

### S2. SPEC §11.2 checks that are not implemented

- A2: "Every running member is joined to reports". `check_rooms` checks only coordinators.
- A3: "no session's inbox grows". Not checked.
- A5: "the coordinator's session received both as envelopes". Only `history` is read.

Either implement these or correct the SPEC, so the verdict does not vouch for more than it
checks.

### S3. Sudo lapse before the join wait

`deploy.bash` stage 2 runs `questions_joined` (`sudo -n agent-bus list`) directly after the ccy
image rebuild, and nothing primes sudo again. Ansible's `become` relies on the shared timestamp,
so a long rebuild with no become task near its end can let the timestamp lapse. The leg then
fails on sudo's own message, not the clear "has not joined" message. It still fails fast, but
it is confusing. Moving the wait to the first leg of `acceptance.bash --stage 2`, which primes
sudo before its log opens, removes the risk.

### S4. Cleanup registered late

In `a3_take`, `plan_on_cleanup a3_release` is registered only after two `jq` reads of
`member.json`. If either fails after `add-member` succeeded, the throwaway worker stays in the
registry. Register the cleanup right after `add-member`; the handle can be derived from the
seat, or read inside the release function.

### S5. Manual repair in the error text

The error for two adoptable candidates in `_find_side_room` tells the operator to hand-write
`room_id.<purpose>`, which is a manual host operation. It is a state that should never happen,
but under IaC the repair should be a command (for example `agent-bus adopt-room`) or at least
name the exact file and value.

## Verified as asked

- **Idempotence and crash safety.**
  - `createRoom` carries the back-link in `initial_state`. The ID file is written straight
    after, then the index.
  - A crash before the ID file is written is adopted on the next run: from the index if it names
    the room, else from the admin's `joined_rooms` with that back-link. Two candidates are
    refused, naming both. An ID file whose room has no valid create event or back-link is
    refused.
  - A re-run is a no-op: each state write happens only when the canonical form differs (test
    `a_second_run_changes_nothing`).
  - A room left by a create that failed half-way, with no back-link, is an orphan, never adopted
    and never trusted. Acceptable.
- **D4.** A team with no coordinator gets no questions room (`_sync_questions_members` returns
  early, test `no_questions_room_without_a_coordinator`), and check-rooms does not demand one.
  Once a team has had a coordinator and loses it, the room stays, closed: humans and agents at
  0 against `events_default` 10, with the closed topic.
- **Power levels match the SPEC** (`protocol.side_power_levels`).
  - Reports: humans 0, `users_default` and `events_default` 10, so the owner cannot post, while
    agents post at 10. Reactions, @room, state, invite, kick, ban and redact are at 100.
  - Questions: the main room's levels with reactions at 100.
  - `admin` is the version-12 creator and is rightly not in `users`.
- **Join rules.** Reports is restricted to members of the main room, and questions is
  invite-only. So a worker cannot join questions, which A3 checks for read, join and post
  (403 M_FORBIDDEN).
  - Restricted join: the syncer joins reports on every sync (`syncer._reports_room`). A4 checks
    one LXC member end to end, and the spike vouches for the restricted join itself.
- **Kick-before-invite.**
  - In `_sync_questions_members`, every joined or invited agent that is not a coordinator is
    kicked first, then power levels and topic are set, then the humans and coordinators are
    invited.
  - It is called from bootstrap, add-member, remove-member and set-role.
  - A removed account is kicked from the team room and every recorded side room before it is
    deactivated.
  - Since there is one coordinator at a time, a handover takes two `set-role` calls, so two
    agents are never in questions together.
- **The wait leg.**
  - 180 s, polling every 10 s.
  - On timeout, each coordinator that has not joined is named with its membership, and the
    message says what `invite` and `leave` mean: restart that session, then run meta-deploy
    again. Fail-fast, no silent pass.
  - `--fake` covers join, invite, leave, no ROOM line and no coordinator.
- **The throwaway worker.**
  - Each run leaves one deactivated account under a seat name used once (`a` and the date and
    time) and its report in the reports room. Matrix has no account deletion, and this is
    disclosed in "WHAT IT CHANGES". Acceptable.
  - Its token is dead after removal, and the auth header file is deleted.
- **Spike gate.** Stage 2 refuses unless the newest spike report has the PASS verdict, and runs
  acceptance stage 1 again as its pre-flight.
- **Plan state.** Tasks 2.1-2.5 are ticked and 2.6 is in progress (correct: not run). SPEC §5.4
  was corrected to `check-rooms`, with its reason.
- **No install-specific names** in the diff. `fedora-desktop` is used as the A3 repo, which is
  permitted.
- **Bash limits.** Every bash file is in the plan folder, so exempt.
