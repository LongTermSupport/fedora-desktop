# Task 3.3: the play side of LXC members on v2

Plan 00209 Task 3.3 (gate A met, Plan 00246 INSTALL-PATH.md). Scrubbed: every name below is a
placeholder.

## What was built

**The play** (`playbooks/imports/play-teamchat.yml`):

- `tc_lxc` is `teamchat_lxc_members | default([])`. The input assert checks its shape before any
  change: a list of mappings, each with `team`, `container`, `user`, `checkout` and a
  whole-number `seat` (a string or a boolean seat is refused), a `state` of present or absent
  when given, a team declared present, and no two entries with one (team, container, checkout,
  seat). Every other rule (grammars, seat range, keys, handles, the guest's facts) stays the Go
  reader's. The conditions are ordered so a later one only meets mappings with every key.
- `team approve` and `team apply` get `{"team": <entry>, "lxc_members": <that team's entries>}`,
  so the LXC members are approved as security fields (AG1) and a team with a present member is
  served on the bridge (its effective networks gain the bridge subnet, C 3.2.5).
- Two new tasks, tagged `teams`, `changed_when` on `^CHANGED\t` lines:
  - "Park and Remove the LXC Members That Leave": `lxc apply --phase absent --only-teams <present teams in the run's scope>`, after the approval and before `team remove`.
  - "Place and Join the Declared LXC Members": `lxc apply --phase present --only-teams <tc_scoped_present>` (so a base unit change widens it as it widens `team apply`), after
    `team apply`.
  - Both read `tc_lxc_declaration`: `{"teamchat_teams", "teamchat_lxc_members"}` plus
    `teamchat_site` when it is defined (declare.Decode's keys). `--only-teams` is always given,
    explicitly listing the acted teams; the task is skipped when that list is empty (a scope
    naming only absent teams), since no `--only-teams` would mean every team.
- `teamchat_address` stays refused (see below). The header, the inputs list and the fail
  message say what is accepted.

**Go gap closed, test first** (red seen: "team t1 has no install stamp"):

- `health.ErrNotStamped`: `Stamps.Origin`'s error for a team with no stamp, same message text.
- `host/lxc` validate: the absent phase passes by a team with no stamp **and** no LXC record
  (`neverApplied`). The play runs the absent phase before `team apply`, so a team's first play
  met "no install stamp" and failed. The present phase, and an unstamped team that has a record,
  still fail as before. Refactored the per-team step into `act` (gocognit).
- Tests: `TestAbsentPassesByATeamNeverApplied` (host/lxc), `TestStampsOriginOfOneTeam` extended
  (health), and `installplay/lxc_test.go`: the variable reads `paths.HostVarLXCMembers`; the
  address is still refused (`paths.HostVarAddress`); the per-team stdin's keys decode through
  `declare.DecodeInput` with one LXC member; the declaration's keys are `paths.HostVarTeams`,
  `paths.HostVarLXCMembers`, `declare.HostVarSite` and decode through `declare.Decode`; both
  tasks' argv, stdin and changed_when; the order approve, absent phase, remove, apply, present
  phase. The existing path-equality test is unchanged and passes.
- The Jinja of the new conditions and templates was also evaluated outside Ansible, with
  Ansible's own filter plugins, over good and bad inputs (each bad case refused by the expected
  condition). `ansible-playbook --syntax-check` passes. No play was run.

**Docs and plan**: a "v2 team's LXC members" section in `docs/agent-bus-lxc-members.md`; the
as-built note under DESIGN-v2-C-host.md 3.5.1 step 13; INSTALL-PATH.md items 3 and 6; PLAN.md
Task 3.3; a journal entry.

## Findings

1. **Design order defect, C 3.5.1**: step 12 approves after step 10's absent phase, but `lxc apply` holds every acted team against its approval in both phases, so a run that approves a
   changed LXC entry would fail at step 10 with 70. The approval now runs first. It only writes
   `approved.json` and needs no stamp, so nothing else moves.
2. **First-play defect** (above), fixed in Go.
3. **The bus address is not needed for the LXC path.** An LXC member reaches a desktop-hosted
   homeserver on the LXC bridge's address: with a present LXC member the team's effective
   networks gain the bridge subnet (`declare.EffectiveNetworks`, built), the homeserver's TLS
   terminator listens on the bridge address, and the leaf names every listen address
   (`host/leaf.go`). So the team declares `url: https://<bridge address>:<port>`. The
   `teamchat0` dummy interface (C 3.5.1 step 08) is NetworkManager work in the play, not Go, and
   is still not built; Go's half (the `address` in the input, its `/32` as an own network, the
   default URL) is built. Left refused, as the brief allowed.
4. **Removing a team with LXC members needs two runs.** The absent phase acts on present teams
   only (`Declaration.Inputs`), `team remove` does not touch guests, and an entry of an absent
   team is refused (by the play and by `declare.Decode`). So the owner removes the entries
   first, then the team. Documented. Whether `team remove` should park and clean a team's LXC
   seats itself is an owner question for later; nothing breaks meanwhile.
5. **An entry with no `state` key in `teamchat_teams`**: the play's `tc_present` counts it as
   present, but the Go reader requires `state` (team apply already refused it before this
   change). Pre-existing and unchanged; the owner's declarations should give `state`.
6. **Firewall**: the LXC bridge is still in firewalld's `trusted` zone on this desktop, so the
   team's rich rule adds no wall for LXC members (INSTALL-PATH.md section 1, the zone move is
   unbuilt). Not a blocker for tonight; noted.
7. **v1 holdback**: a team whose origin is `migration` gets every LXC member held back as a
   failure (`v1NotBuilt`, N6). A fresh v2 team (origin `v2`) is not affected.

## Not done

- **Task 3.4's VM-lab acceptance is not written.** The lab's pattern (Plan 00246 `x30-lab.bash`,
  about 600 lines plus a guest checker) puts guests on passt with no LXC inside; an LXC member
  needs a guest with lxc installed, a privileged container made from a downloaded image, an
  agent user, a checkout with a GitHub origin, then the play with host_vars and checks of the
  kit, unit, bundle and desired file. That is a plan-local orchestrator of the same size, not a
  small addition, so it is left for Task 3.4.
- **Nothing queued in `meta-deploy.list`.** The host run needs the owner's own host_vars
  (`teamchat_teams` with the bridge url, `teamchat_lxc_members`, `teamchat_approve_teams`),
  which no tracked file may carry. The run is `play-teamchat.yml` with those set, after CI.
- No tool fault met that needed an issue: the hooks' lint reports while a change was half made
  were stale, as they say.
