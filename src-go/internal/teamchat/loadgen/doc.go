// Package loadgen is teamchat-loadgen, the load generator of Plan 00268 Task 2.1: a test team
// on a lab homeserver, made by the real rooms engine, and teamchat's traffic sent into it
// through proto.Composer, one pattern at a time or as a replay of the traffic probe's counts,
// for a Lightning signed in as the team's human to be watched under (Phase 2). It is a test
// tool: no play deploys it, and it runs from the checkout (`go run ./cmd/teamchat-loadgen`).
//
//   - LG1 the target. `--homeserver` and `--server-name` are required, with no default and no
//     environment fallback. The server name is a lower-case DNS name under `.test`, the name
//     RFC 6761 reserves for testing and no registry delegates, so no real homeserver has one
//     (a team's own server is `<team>.teamchat.internal`). The URL is plain http to a loopback
//     IP literal (127.0.0.0/8 or ::1, the matrix client's own rule for plain http), with no
//     user, path, query or fragment: the lab server, the generator and Lightning run on one
//     machine. A team's server on a bus host also listens on loopback, so the name is what
//     tells them apart, and the server's own answer is held to it: the admin account the
//     registration made must be on that server name, or the setup stops before it writes
//     anything more. A setup also needs the server's registration secret, which a team's server
//     keeps for root alone. A run re-checks the team file's target the same way.
//   - LG2 setup, once, on a fresh lab server. The registration shared secret is read from the
//     file `--registration-key-file` names, never from argv. The team's admin is registered with
//     it, so a server that already has one (not fresh) refuses the setup. Then N agent accounts
//     (`--agents`, 2 to 64; the first the coordinator, the rest workers) and one human account
//     are made with random passwords, the three rooms converged by `rooms.Converge` from the
//     roster as the host derives it, every account given a token and joined to the rooms its
//     role is in (main and reports for every agent, questions for the coordinator, all three for
//     the human). The team file, `team.json`, mode 0600, is written in `--dir`, a new directory
//     made 0700: the tokens and the human's password, for Lightning's sign-in. Its path and the
//     human's user ID are printed on stderr, the password never.
//   - LG3 a run reads the team file, checks LG1 again, and reads the roster from the main room's
//     state as a member does, so every event is composed against the roster the server holds.
//   - LG4 the patterns, each one switch and one rate, in units an hour (1 to 36000; 0 or absent
//     is off). A unit is sent back to back:
//     `--pings` a ping in main from one agent to the next, then its answer (`ack`) in the
//     ping's thread; `--questions` a question thread: the coordinator's question, the human's
//     post in its thread, the coordinator's thread post, its close, and the retitle of the
//     closed question; `--notices` an agent's own `disconnected` or `back` notice, in turn;
//     `--reports` an agent's report; `--receipt-events` an agent's `teamchat.receipt`
//     (delivered) of the newest main item to its sender, in reports; `--receipts` a human post
//     in main mentioning every agent (at most 16), then each one's `m.read` receipt of it, as
//     each link posts one for each human item it delivers; `--presence` an agent's presence
//     set to the other of online and offline, in turn; `--big` agent text of `--big-bytes`
//     bytes (1 to 4096, default 4096, the agent text limit) to the next agent, with its HTML
//     beside it. Agents are taken in turn for each pattern. The flood and duplicate windows of
//     a sender (Composer.Admit) are not spent: the load is the client's, not a seat's.
//   - LG5 the run's length: `--for` (1s to 168h) for the patterns; the units of the patterns
//     are spread evenly, unit k of a pattern at k times its interval from the start.
//   - LG6 the replay: `--replay FILE`, the JSON of `teamchat-admin team traffic --json` (v 1),
//     as a schedule. Each row's count is spread evenly over its hour, the hours kept in order
//     from the start of the run; `--speed` (1 to 3600) divides every offset. A row is one kind:
//     main ping (no relation) a ping, main ping (thread) an answer, main text (none, or reply)
//     agent text, plain or a reply to the newest main event, main notice a notice, main human
//     post (no typed key, no relation) a human post and its `m.read` receipt by the
//     coordinator, reports report a report, reports receipt a receipt event, questions question
//     a question, questions question (edit) a retitle, questions thread (thread) a thread post
//     in the newest question, questions human post (thread) the human's post in the newest
//     question's thread. A kind's prerequisite that does not exist yet (an answer with no ping,
//     a thread post with no question) is made first and counted as an extra. Every other row
//     (state, membership, another type or relation) refuses the replay, naming each, unless
//     `--skip-unmade`, which leaves them out and names each left out on stderr. Patterns and a
//     replay are not mixed.
//   - LG7 the payload: stdout is one JSON object when the run ends, the events made by kind,
//     the extras by kind and the units by pattern; progress and the setup's report go to
//     stderr.
//   - LG8 fail fast: any refused composition or failed request stops the run with the kind, the
//     account and the cause. An interrupt stops it after the request in flight; what was made
//     is still the payload, with exit 130.
package loadgen
