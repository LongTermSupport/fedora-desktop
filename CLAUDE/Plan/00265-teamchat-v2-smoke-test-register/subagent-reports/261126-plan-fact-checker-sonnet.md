# Fact check of REGISTER.md additions (R69-R76, edits to R11, R14, R36, R68)

Verified: verdict.go:461-464 gate; dc6b81aac, c8a011430 on F44 and in e67e28d72 (dc6b81aac); c219e89ce on F44;
nolink.go:43-69 ReadDown logic; client.go:223 (calls c.noLink, which calls ReadDown at nolink.go:100);
values.go:22; status.go:406-410; teamsync/session.go:959-968; chooser/teamchat.go:209-233; cmd.go:196;
lifecycle.go:26-28, 33-35, 47-73; play-github-cli-multi.yml 1396, 1416, 1445; rule project_root_create_follows_link
(src-go/qa/rules/rootcreate.go); ccy 3.124.80; Plan 00146 Not Started with decision 3 automatic restart;
u3/u6/u8/u9 are ancestors of v2-smoke; u1/u2/u4/u5/u7 each hold one empty "Park branch" commit; the two other agent
branches are ancestors of agent-a1aeb314.
ccy-session-token-failover appears nowhere in the repo except the register.

## REFUTED / stale
1. R71 "history items have no 'to' field": c8a011430 (on F44) adds it ("history, sent and reply say who an item is for").
   The claim is stale for F44 (R69 itself says c8a011430 covers most of R71). Also "neither is named in the help":
   kit/skill.txt:82 names counts.dropped_by (not `teamchat help status`, so only partly refuted).
2. R48 "three old agent branches ... agent-a1aeb314..." : only agent-aeffdbe7 exists locally; remote has
   agent-a1aeb314, a2be5458, adff76a1 (a2be and adff are ancestors of a1aeb). Wording ok, but "ci/v2-batch3..." exist only on origin.

## UNVERIFIABLE-HERE
Link logs, drop counts, 07:54Z times, seat kit digests/versions, issue #422 state, CI run 45773214021, Claude Code snapshot filter, live behaviour.
