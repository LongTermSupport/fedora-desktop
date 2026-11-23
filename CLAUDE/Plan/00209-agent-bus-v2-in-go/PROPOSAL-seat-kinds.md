# Proposal: seat kinds in teamchat v2

Status: APPROVED by the owner (Q66; the decisions are recorded in section 7). Being built, test
first. It was a proposal for the owner. v2 only: v1 gets nothing (owner: "we need to get rid of v1
ASAP"). Paths are relative to the repository root. Marks: FACT (read in code or design),
INFERENCE (follows from facts), UNKNOWN (not established). Read-only research; nothing was run.

## 0. Why

- Host seats run deployments and desktop triage, never tests or QA.
- Machine-QA plan 00263 (DESIGN section 3) wants a guard that refuses QA commands in a host seat.
- The hooks-daemon (its plan 00630) wants to ask "what kind is this session, per team?" without
  reading any token.
- The owner asked: host seats are not workers; maybe teams need more seat kinds; one seat may be
  coordinator in one team and worker in another.

## 1. What v2 does today

**A word clash to avoid.** v2 already uses two words. `role` is the wire role
(coordinator, worker, probe). `kind` is the member's transport (podman, lxc, host). So the new
concept needs a third name in code (see section 2).

- **FACT: roles are three constants.** `proto.RoleCoordinator`, `RoleWorker`, `RoleProbe`
  (`src-go/internal/teamchat/proto/roster.go:19-21`). The roster is per team, one entry per
  handle, each with a `Role` (`proto/roster.go:60`, `RosterMember`).
- **FACT: the wire accepts only two for membership lines.** `roleLine` refuses any role other
  than coordinator or worker (`proto/membership.go:205`). The session's hook text has three
  templates (`member/hooktext/hooktext.go:36-38`); a worker is told to "take direction from the
  team's humans and its coordinator" (`:37`).
- **FACT: the coordinator is one declared handle, not a role flag.** `teamchat_teams[].coordinator`
  is "optional; at most one per team" (`DESIGN-v2-C-host.md:1685`). C30 says it is declared in
  one place, and may be declared before its member exists (`:107`). DC6 makes `role apply`
  converge the roster to the declaration (`:1084-1124`). Every roster member that is not the
  coordinator is a worker: the host derives the role by comparing the handle with the
  coordinator (`health/view/view.go:205-208`, `apply/member.go:602-605`, `apply/apply.go:444-447`).
  So there is no declared per-member role today.
- **FACT: the coordinator is a gated field.** C46/DC18: `coordinator` is a security field, applied
  only after the owner approved it with the password (`DESIGN-v2-C-host.md:56`).
- **FACT: a session holds one role per team.** `state.Told.Roles` is `map[TeamID]string`
  (`member/state/session.go:51-55`); a told coordinator pairs with a team told as a worker
  (`:105`). `StatusTeam` has one `Role` per held team (`member/state/status.go:42-52`). The
  roster is per team, so a seat that holds two teams is a coordinator in one and a worker in
  the other today, with no change. The owner's requirement is already met by the shape; only a
  test is missing (UNKNOWN: whether one exists; I did not search the tests).
- **FACT: the status line shows a role letter per team.** `letters` maps coordinator to `c`,
  worker to `w`, probe to `p` (`member/statusline/segment.go:27`); `piece` draws
  `<team>: <member> (<r>)` (`:118`).
- **FACT: how a session learns its role.** The launcher passes only the desired-membership mount
  as `TEAMCHAT_MEMBERSHIP` (`src-go/internal/ccystart/ccystart.go:53,84`); that holds seats
  (team, seat), not roles (`member/state/desired.go:29-41`). The link reads the roster and
  tells the session its role (`member/link/membership.go:202-207`, `told.Roles`). `status.json`
  carries the role per team: `role` is a nullable string, "null until the roster names one"
  (`member/state/status.go:44`, closed set `coordinator|worker|probe|null` at `:79`).
- **FACT: the `worker` assumption is behavioural.** Verdicts treat only the coordinator
  specially (`proto/verdict.go:607`); only `probe` gets special verb rules (`proto/verbs.go:76`).
  `IsMember` accepts coordinator or worker (`proto/roster.go:413`). No code says a worker runs
  tests; the "worker" meaning is the hook text and convention.
- **FACT: what is readable without secrets.** `status.json` holds team, role, failed, delivery,
  counts, member short name, colour, down, stalled (`member/state/status.go:42-52`). No token.
  It sits in the session's run directory, found from `TEAMCHAT_RUNTIME` (`member/client/client.go:161-164`),
  opened `homefs.Private` (`member/statusline/segment.go:100`); seat-store files are mode 0600
  (`member/state/strict.go:36`), the seat directory 0700 (`member/state/writer.go:23`). INFERENCE:
  only the session's own user can read it, which suits a hooks-daemon running as that user.
  The seat store also holds the credential, so a probe must never walk it.
- **FACT: `status.json` decoding is strict** (`member/state/status.go:62`), with a `v` field. A
  new field needs a decoder change and tests, not just a writer change.
- **FACT: the roster member `kind` is the transport.** `"kind": "podman"` in the roster
  (`DESIGN-v2-C-host.md:898`); `member add --kind K` (`:1008`); a host-kind runtime exists for
  the acceptance probe (`:507-518`).

## 2. The kinds (proposal)

Name in code: **`duty`** (wire role and transport kind are taken). In prose, "seat kind".
Keep the wire role as it is (coordinator or worker); duty is a host-side fact, not a protocol
change, so no area-A change and no wire major.

| Duty        | Meant for                                    | May                                      | May not                         |
| ----------- | -------------------------------------------- | ---------------------------------------- | ------------------------------- |
| coordinator | the team's one named handle (C30), unchanged | direct, ask humans, review               | nothing new                     |
| worker      | the default for podman and lxc members       | code, test, run QA, push work branches   | push protected lines without CI |
| host        | deployments and desktop-level triage         | run deploy and queue verbs, read, triage | run tests, QA, builds           |
| reviewer    | read and review                              | read, run read-only checks, report       | push to protected lines         |
| observer    | read only                                    | read, `status`, `questions`              | send to rooms, push, run QA     |

Five is the small set. I would build `coordinator`, `worker`, `host` first and add `reviewer`
and `observer` only when a use appears (YAGNI). INFERENCE: a coordinator that is also a host
seat is the hard case: if a host member is the coordinator, its duty is `coordinator` and the
"no QA" rule is lost. Recommended: `declare check` refuses a host-kind member as coordinator
(OD1).

**What can be enforced, and where.**

- Host: no QA commands. Enforceable by the hooks-daemon (a PreToolUse rule that asks the probe,
  section 4) AND statically by cc deny rules: Task 5.1 already passes `--extra-settings <tmp>/cc-deny.json` to a host launch (`DESIGN-host-cc-v2.md:187-190`, decision H4), so the deny
  list for a host seat can name the QA scripts at launch with no probe. Two layers; the static
  one needs no daemon. Teamchat itself cannot (it never sees a shell command).
- Observer: no sending. Teamchat can enforce it on its own verbs, as it does for `probe`
  (`proto/verbs.go:76`); INFERENCE: a new rule in the verb table plus the roster check. The
  rule must hold on the server side, not only in the hook text.
- Reviewer: no push to protected lines. Not a teamchat matter (it does not see git). The
  hooks-daemon (git push guard) or a cc deny rule on `git push` to the protected refs. The
  forge trust config (`trust.repos[].branches`) says which branches count, but is about who may
  be believed, not who may push (INFERENCE).
- Worker: today's default; nothing new.

## 3. Declaration

Declare it where the roster is declared, in `teamchat_teams[]`, as one sparse map:

```yaml
teamchat_teams:
  - team: <team>
    coordinator: <handle>          # unchanged (C30, DC6)
    duties:                        # optional; only non-default members
      <handle>: reviewer
```

- **One truth per fact (C30).** The coordinator stays only in `coordinator`. `duties` may not
  name the coordinator (`declare check` refuses it): the coordinator's duty is derived.
- **Default follows from the member kind.** Roster `kind` host gives duty `host`; podman and lxc
  give `worker`. `duties` is only an override, so most teams declare nothing.
- **Per team.** The map is inside one team's entry and handles are per team, so one seat can be
  coordinator in team A, host in team B and worker in team C. INFERENCE: this relies on one seat
  holding a distinct handle per team, as `Told.Roles` per team implies (FACT above).
- **Approval.** `duties` is a security field, added to C46's list: widening a seat from `host` to
  `worker` lets it run QA, and narrowing is harmless but cheap to gate the same way. Same
  password approval and `team apply` (DC18). This adds one field to the approval comparison
  (`declare check`, `team apply`; DESIGN-v2-C-host.md:56, :748-767).
- **Carry.** The host stores the applied duty in the roster file's member entry (C: the roster
  entry beside `kind`, `human_text`). INFERENCE: it is host-side only; do not put it in the
  `teamchat.roster` state event, to keep area A unchanged. UNKNOWN: whether the link can read
  host-side roster fields now. The link reads the Matrix roster event, not the host file
  (INFERENCE from `member/link/membership.go`), so the host would have to hand the duty to the
  seat another way, for example in the bundle or the desired-membership file. This is the
  main open design point (OD2).

## 4. Reading it: `teamchat whoami --json`

Prefer a verb over teaching probes the file format. `status.json` is strict and versioned
(`v`); a verb hides that.

```
teamchat whoami --json
{"v":1,"teams":[{"team":"<team>","member":"<short>","role":"worker","duty":"host"}]}
```

- Reads only `status.json` through `TEAMCHAT_RUNTIME` (the statusline's own path,
  `member/statusline/segment.go:99-116`). No link call, no token, no network, no seat store.
  `duty` is added to `StatusTeam` beside `role` (null until known), so the status line may
  later show it.
- Exit codes (reusing the existing set; `exits.CodeNoSession` is 69, `client.go:158`):
  0 read ok (the list may be empty); 69 no session (no `TEAMCHAT_RUNTIME` or not a run
  directory); 65 file unreadable or fails strict decode (INFERENCE: the nearest existing
  code; to be checked against `internal/teamchat/exits`).
- Probe call by the hooks-daemon: `teamchat whoami --json`, and "is any team of this session
  `duty == host`" is the guard question. Each team is answered separately.
- **When teamchat is absent (v1, no team, binary missing, exit 69):** the probe answers "no
  kind known". The guard then does NOT refuse on the probe alone: a v1 or teamless session has
  no declared duty. The static cc deny list (section 2) is what stops a host seat when the
  probe cannot answer. FAIL-FAST note for the QA plan: "unknown" must be a distinct answer
  from "worker", and a guard that cannot tell must say so in its output, not stay silent.
- **When the link is down but the file exists:** duty is static for a run, so the last file is
  still right; staleness (`written_ms`) is not a reason to ignore duty. INFERENCE.

## 5. Fit with Task 5.1 (host `cc` members on v2)

- Host members enrol with `member add --kind host` (`DESIGN-v2-C-host.md:1008`); their default
  duty is `host` from that kind, so no enrolment flag changes.
- DESIGN-host-cc-v2.md changes: section 6 step 3 (`--extra-settings`) gets a stated content for a
  host seat, the QA deny set, and step 5's `cc-run` writes `cc-deny.json` from the seat's duty.
  A new step is added between step 4 and 5: the duty field (declaration, roster entry, status.json,
  `whoami`), test first. Section 7 gets the owner decisions below.
- Order: the declaration, `status.json` field and `whoami` do not need host members at all (a
  podman seat can hold a duty), so they can land BEFORE 5.1, and 5.1 then only adds the host
  default and the deny set. Landing after 5.1 also works, but the first host seats would run
  without the static guard. Recommended: before, or as 5.1's first step.

## 6. v1

Nothing. v1 has no duty and is not changed, as the owner said.

## 7. Owner decisions (each with a recommendation)

- **OD1. Can a host member be the coordinator?** Recommended: no; `declare check` refuses it.
  (Otherwise the host rule is lost, or duty needs two axes: role and duty both.)
- **OD2. How the duty reaches the seat.** Options: (a) in the desired-membership file the
  host writes for the session; (b) in the roster state event (an area A change, a wire
  addition); (c) host-side only, read by `whoami` from a host file. Recommended: (a), no wire
  change. Needs a short design check of who writes the desired file for host seats.
- **OD3. Is `duties` approval-gated (DC18)?** Recommended: yes, the whole field.
- **OD4. Which kinds to build now?** Recommended: coordinator, worker, host only; `reviewer`
  and `observer` named here but built when needed.
- **OD5. Name in code: `duty`.** Recommended, since `role` and `kind` are taken; say if you
  prefer `seat_kind`.

**The owner's answers (Q66, verbatim): "a no / b up to you / c yes / d approved / e approved".**

- OD1: a host member is never a coordinator; `declare check` refuses it.
- OD2: left to us; recommendation (a) is taken. The duty travels in the desired-membership file
  the host writes for the session, with no wire change.
- OD3: yes, the whole `duties` field is approval-gated (DC18).
- OD4: coordinator, worker and host are built now; reviewer and observer later.
- OD5: the field is named `duty`.
