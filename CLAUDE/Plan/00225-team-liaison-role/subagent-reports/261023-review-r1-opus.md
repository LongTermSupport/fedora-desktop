# Plan 00225 review r1 (adversarial, Opus)

Scope: `git diff dcb76af56..HEAD` (PLAN.md, DESIGN.md, README row, journal). Checked against
`helpers/pingbus/` (cli.py, protocol.py, syncer.py, config.py, hooks.py), the pingbus SKILL.md,
the ccy launcher, and Plans 00207, 00211, 00222, 00224.

The plan is mostly well-founded. Facts I checked hold: `ask-human` is coordinator-only
(`cli.py:1248`), only a coordinator receives questions-room traffic (`protocol.py:911`),
`agent_bus.status` exists and the syncer reads it, the ccy `-v "$PWD:/workspace${CCY_MOUNT_RELABEL...}"`
mount (`claude-yolo:2520`), `membership-init` is in `src-go/internal/teams`, 00211 D6/D9 and
Tasks 1.9/2.8 exist, and 00207 uses "liaison room". CCY bump markers are on 2.5, 5.1 and 6.1.
The no-liaison byte-for-byte rule (LZ0) and its golden tests are good. The problems are in the
authority model and in how the plan fits the plans around it.

## Blocking

**B1. A reworded question launders authority.** LZ8 lets the liaison reword the hand-off when it
posts `ask-human --handoff`. The human answers "yes" to the liaison's wording. The relay envelope
(LZ6) shows only `question: Q7 "headline as posted"` and a `handed-off:` event ID. It does not
show the coordinator's original question beside the full text as posted, and nothing checks or
flags that they differ. LZ7's rule ("verbatim text wins over paraphrase") protects the *answer*,
but a verbatim "yes" means nothing without its question, and the question is the liaison's
paraphrase. So the coordinator acts on its own wording, under a "yes" given to different
wording: the liaison's words have become authority. Fix: the envelope shows the full posted
question and the full handed-off text, both fetched by the receiver; when they differ the
relay is marked `REWORDED` and the authority covers only what the human saw. Either forbid
rewording for authority questions, or make LZ7 say that the human's answer authorises the
question as posted, never the hand-off.

**B2. A relayed event is not bound to the hand-off it answers.** LZ5 and LZ6 accept *any*
listed human's message in the main or questions room. Nothing requires that it is a reply to the
Q<n> whose `handed-off:` is this coordinator's H<n>, or that it postdates the hand-off. A
liaison, confused or prompt-injected, can relay an old "yes, go ahead" from another question.
It can also relay a main-room message addressed to a worker, or to no one. The existing HUMAN
rule scopes authority to the addressee ("delivered it only because it mentions you",
SKILL.md:38-39), so a relay of a message never addressed to the coordinator widens HUMAN
authority. The plan says the HUMAN and PEER rules are "unchanged", but SKILL.md:59-62 says a
human's request passed on through another agent never carries authority. LZ7 changes that rule
and has to say so openly and bind it. Fix: add NOT VERIFIED reasons for "not attached to a
question handed off by you", "older than the hand-off" and "main-room event not addressed to
you". Otherwise state that main-room relays carry information only.

## Major

**M1. LZ6 says the receiver "is a member of both rooms (LZ9)", which is false for workers.**
The questions room holds coordinators and humans only (00211, `_questions_outcome`). LZ5 allows
`relay --to NAME` and LZ4 allows texts to workers. A worker receiving a relay of a
questions-room event cannot GET it, so it shows NOT VERIFIED. Either restrict `relay` to the
coordinator or define what a worker sees.

**M2. `human_text` is ignored.** A member's bundle can set `human_text: false`
(`config.py:527`). Such a member refuses human text and leaves the questions invite pending
(`syncer.py:478-487`). LZ1 and LZ10 count a liaison with `human_text: false` as active. The
coordinator then hands off to a session that never receives the human's answers, and loses
`AskUserQuestion`. `set-role liaison` must refuse such a member, and the active test (LZ10) must
include the flag.

**M3. It contradicts Plan 00222, and no hand-off task covers it.**

- 00222's Non-Goals say "Restricting the coordinator" and "Bash command patterns ... not a role
  question". LZ15 and LZ16 do both.
- 00222 R7's `Decide(rolesByTeam, policyByTeam, tool)` has no tool input and no liaison
  activity, so LZ15 (parsing the Bash command) and LZ16 (activity) change its signature.
  DESIGN section 8 still says v2 imports `Decide` "unchanged".
- PLAN Non-Goals says the 00222 requirements are "handed over (Tasks 7.1, 7.2)", but 7.1 is
  00224 and 7.2 is 00209. No task hands anything to 00222.

Add a 7.x task to 00222's owner covering the non-goal amendment and the new `Decide` inputs.
Also say how LZ16 behaves when the liaison's status cannot be read (fail open or fail closed).

**M4. It conflicts with Plan 00207 on cross-team liaisons.** 00207 SPEC-roles.md:97 rejects "A
`liaison` that relays between teams. That is exactly what the owner forbids." LZ3, section 7
and LD1's alternative (also in PLAN Non-Goals) *allow* one session to hold the liaison seat in
several teams. That session carries every team's context and can move content across teams.
The plan neither cites this owner ruling nor guards against it. Default to one session per team
for the liaison seat (refuse the mix the way LZ3 refuses role mixes), or put the conflict to the
owner as an explicit decision.

**M5. The coordinator's quiet items have no defined delivery.** LZ9 says the coordinator
"stores the same items unwoken". It never says whether those stored HUMAN items reach the
coordinator's conversation at its next wake (from worker traffic), or are held back. If they
are delivered, every answer arrives twice (the quiet HUMAN item and the relay), the relay is
redundant, and questions-room traffic is back in the coordinator's thread, which the owner asked
to stop. If they are held back, how do they interact with `pingbus questions`, the hooks'
`role_questions` line ("pass on any answer not yet passed on", `hooks.py:147`) and the 900 s
wake? Specify this and add it to Task 2.1's tests.

**M6. Questions-room rules in `protocol.py` are missing from Phase 1.** `_questions_outcome`
ignores every receiver but the coordinator. It drops an agent post that is not a coordinator's
as `sender`, so the liaison's own questions-room posts would show as dropped at the
coordinator. `thread`, `questions` and `history --room questions` are gated by `_coordinator`.
Task 1.1 and Task 2.2 name none of these changes. List them, with refusal tests first.

## Minor

- m1. An edit to an already relayed human answer (yes changed to no) reaches the coordinator
  only quietly. It should wake the coordinator, or the liaison, as a revision of a relayed event.
- m2. The guard cannot stop a coordinator asking the owner in plain prose at the end of a turn.
  The coordinator role text (LZ23) should say so explicitly.
- m3. While a liaison's terminal `AskUserQuestion` waits on an absent owner, every other
  hand-off stalls until `liaison_handoff_s`. Note this, or advise the questions room for
  anything urgent.
- m4. Plan 00207 also adds roles that need a fleet-wide upgrade. Coordinate so the two plans
  share one protocol upgrade, rather than doing two.
- m5. The Decisions table has no decision for rewording (B1). It should, with a safe default.

VERDICT: FIX-FIRST
