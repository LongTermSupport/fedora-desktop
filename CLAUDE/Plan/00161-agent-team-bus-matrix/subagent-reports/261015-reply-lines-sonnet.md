# M1.2 REPLY lines: report

Why it passed before: the check files were identical at 70ef1d02 and 14058c62 (only PLAN.md and the journal differ). The pingbus the host ran at 70ef1d02 predates Plan 00201 M3; the ccy play that deployed M3 ran afterwards, so the first run against REPLY lines failed M1.2. The check was right for the old output and wrong for the new.

Changes (acceptance_check.py): expect-human and expect-peer now require exactly the kind line then `REPLY`; the REPLY must carry the same team and event ID and the command `pingbus --team T say --to NAME`, NAME being the sender (a human's localpart, an agent's handle, or its short name). A PING check still requires exactly one PING line, so a stray REPLY after a ping fails. expect-absent already flags any HUMAN, PEER or event-naming line. No other check or `_acceptance-*.inc.bash` parses recv output beyond these (U23 uses expect-ping; U20 reads transcripts).

Tests: HUMAN+REPLY, PEER+REPLY (full and short name), PING with no REPLY, missing REPLY, mismatched event, team or sender. The fixtures also lacked the `copy` field that PING and PEER lines have had since Plan 00196, and one team-file test used a forge API the validator no longer accepts; both were failing and are fixed. 67 tests pass.

Relay (Plan 00194): relayed messages carry no REPLY line. The acceptance never enables relay mode, so it asserts nothing for them. Open design question for Plans 00194 and 00201: should a relayed HUMAN or PEER item also carry its reply command, and if so, where?
