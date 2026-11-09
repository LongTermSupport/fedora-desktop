# Fact check: Task 3.20 and SPEC-team-wind-down.md section 7 (WC1-WC9)

| # | Claim | Verdict | Evidence |
|---|-------|---------|----------|
| 1 | `ccy-sessions set-going` types `/compact` as first input at or above the floor (Plan 00135 Task 8.2) | VERIFIED | src-go/internal/sessions/setgoing.go:26,478-484; 00135 PLAN.md:165 |
| 2 | On resume a below-floor session gets `continue` from set-going | VERIFIED (restore path) | setgoing.go:27,478. The code is the reboot-restore path; whether 3.17's `ccy-teams resume` goes through it is not shown |
| 3 | The supervisor injects a real `/compact` at an idle moment | VERIFIED | .claude/ccy/claude-supervise.py:18-21; claude-yolo:3634 passes `--arm` |
| 4 | LXC `agent-bus-claude` execs `claude` with no supervisor | VERIFIED | files/opt/claude-yolo/optional/agent-bus/agent-bus-claude:96 |
| 5 | `winddown.go` `lxcTmux`, tmux session `agent-bus-claude` | VERIFIED | winddown.go:66-68, 349 |
| 6 | A compaction's end is the transcript's `compact_boundary` entry | VERIFIED | src-go/internal/transcript/transcript.go:162,223 |
| 7 | Plan 00226 brings `cc` under the supervisor | VERIFIED | Plan 00226 PLAN.md overview |
| 8 | TW1, TW2, TW3, SD14 and the SPEC-lxc F1/F3 cross-references exist | VERIFIED | SPEC-team-wind-down.md:213-221; the F1/F3 text is in the same diff |
| 9 | `hooks-daemon issue-report` exists | VERIFIED | named in LLM-INSTALL.md and the rule table |
| 10 | Every kind has a tmux pane, on the host or in its container (WC7) | VERIFIED (partly) | winddown.go has prefixDirect "cc" and the LXC route. Host docker/vm members were not checked |
| 11 | The supervisor already accepts a wind-down request (WC1/WC8 say it does not) | VERIFIED as absent | No wind-down request path in claude-supervise.py |

## REFUTED

None.
