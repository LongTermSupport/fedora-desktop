# Plan 00161 design review 2: security and privacy

Lens: trust boundaries now that human text reaches agents and members span hosts. Owner
answers 1-7 are honoured (warden removed, D14). Answer 8 is honoured only in part (B3).

## Blockers

**B1. A same-user process can act as the human (DESIGN §8, §9 rows "code in an agent's
checkout" and "another process of the same user", §5.2 `host` rows, D17).** Element Desktop
is not confined, and it keeps the human's access token under `~/.var/app/im.riot.Riot/`.
Any process running as that desktop user can read it, including bare-desktop members,
`agent-bus-claude` sessions and the code in their checkouts. With that token, the process
can send `@room` text that every agent on every host receives as a `HUMAN` line. Section 9
says such code can only misdirect its own agent, which no longer holds. Fix: refuse
`host`-type members under any user that runs an Element team profile (bare-desktop agents
run under a dedicated user), state the limit in §9, and add a check for it (F5).

**B2. Root on the homeserver host, and anyone holding its backups, can now command remote
agents (§0, §3.7, §9 first row, §2 placement).** Human passwords are kept at rest in
`secrets/humans/` and copied into every backup tar. Fixes:

- Show a human password once and store nothing; `human password` resets it.
- Leave human passwords out of backups, and document that backups hold the root of trust.
- Add an optional `member.json` key `"human_text": false`. A member joining a team whose
  homeserver is on another host can then accept pings only. It only narrows what the
  member accepts, so a writable bundle is fine.
- State in §9 that joining a team gives its homeserver operator the power to instruct
  your agent.

**B3. The `ccy.env.local` opt-in goes through a closed issue (§5.3, D18, U23, revision
report row 8).** Daemon issue #88 was withdrawn: ccy now owns the dist
(`ccy_env_local_dist_text`, `CCY_ENV_LOCAL_DIST_VERSION`, CCY 3.83.0). Fix:

- U21 adds `#export PINGBUS_TEAMS=<team>[,<team>]` to the dist text and bumps the dist
  version and `CCY_VERSION`.
- Remove #88 from U23.
- Make Plan 00160 Task 3.3 (mount `ccy.env.local` read-only) a dependency. Otherwise a
  session can rewrite its own role and active teams.

## Other findings

**F1. A real hostname can leak (§2 Member, PROTOCOL §3 `<host>`).** The fallback to the
short hostname puts it into handles, and agents write handles into `done` references,
journals and comments on a public forge. Fix: when neither role variable is set,
`add-member` and `suggest-handle` should refuse unless `--host` is given explicitly.

**F2. A reply fallback can pass agent text off as human text (PROTOCOL §7).** A reply
whose fallback quotes an agent's text (`> <@agent…> text`) arrives as a `HUMAN` line. Fix:
when `m.in_reply_to` is present, strip the leading `> ` fallback block, and add a P6 case.

**F3. The `@room` claim is false (PROTOCOL §7, §8; DESIGN §4).** `notifications.room`
controls only push notifications, so agents can send `m.mentions.room: true`. The sender
check makes this harmless, but the text needs correcting and P6 should cover it.

**F4. "No egress" is overstated (§3.3, §3.5, D5, P1).** `IPAddressAllow` works in both
directions, so Tuwunel can open connections to any `allow_from` host (the WireGuard subnet,
a docker bridge). Fix: say so, and make P1 fail on any Tuwunel socket whose local port is
not `<port>`.

**F5. The checks do not prove everything they claim (§10).**

- P6 proves filtering at pingbus, not "only the addressed one", because every account
  receives everything. Retitle it.
- Add P6 cases: a removed human posts; a human sends a forged ping notice; F2; F3.
- Add to P7: `GET /login` offers only `m.login.password`; a human cannot set state,
  invite or redact; a human cannot join the admin room.
- Add P8: every bundle path is `git check-ignore`d, and a token planted in Element's
  profile is refused (B1).

**F6. Status state keys are unbounded (PROTOCOL §8).** `agent_bus.status: 0` lets an agent
write that type under any state key that does not start with `@` (for example `""`).
Fix: receivers ignore keys that are not the sender's user ID, plus a size cap.

**F7. PLAN.md Phase 2 is stale.** It still lists U00-U30 and the warden milestone, and
its success criterion says "no agent can send free text". Apply the revision report's
follow-up.
