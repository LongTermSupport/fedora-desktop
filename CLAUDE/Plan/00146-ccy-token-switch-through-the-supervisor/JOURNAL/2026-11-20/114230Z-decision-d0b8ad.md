## 11:42 · decision · — — Owner: HTTPS proxy transport; several front ends over one switch core, ccy-sessions first

The owner decided the transport and the front ends.

- **Decision 9, transport:** the placeholder-swapping HTTPS proxy, and never `ANTHROPIC_BASE_URL`: "absolutely - we cannot tolerate losing the context window size". The base URL caps 1M-context models at 200K.

- **Decision 10, front ends:** several ways to switch, over one well-designed, DRY mechanism:

  - in the session, through the status bar or a Claude Code mod;
  - `ccy-sessions`, including bulk-switching many sessions to a new token;
  - automatic failover across a team's defined token set, done by `ccy-teams`.

  The proof of concept is the simplest of these, `ccy-sessions`.

PLAN.md is updated to match:

- the overview describes the proxy, and the base-URL forwarder is now only background;
- Task 0.1 is the CONNECT proxy;
- new Task 0.1b is `internal/tokenswitch`, the one place that writes `current`;
- Phase 2 is the front ends: 2.1 the `ccy-sessions` proof of concept (one session and bulk), 2.3 the status line and mod, 2.4 `ccy-teams` failover;
- decision 8 is restated for the proxy.

Automatic failover was a non-goal. It is now Task 2.4: it is built last, stays off unless a team enables it, and is blocked on decision 3 (terms), because it is the use that community proxies report account bans for. Decisions 2, 3 and 5 to 8 are still open.
