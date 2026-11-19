# Teamchat v2 audit: merged findings and fix plan

Eight Fable audits (2026-11-19), one per area. Each finding's evidence, failure scenario and
suggested fix is in its area report:

- [admin](audit-admin.md)
- [host](audit-host.md)
- [lxc](audit-lxc.md)
- [engine](audit-engine.md)
- [link](audit-link.md)
- [security](audit-security.md)
- [durability](audit-durability.md)
- [cross-cutting](audit-crosscutting.md)

Every finding was confirmed by reading the code; none was reproduced on a live host.

## Totals

| Area          | Critical | High | Medium | Low |
| ------------- | -------- | ---- | ------ | --- |
| admin         | 0        | 1    | 3      | 7   |
| host          | 0        | 3    | 9      | 7   |
| lxc           | 0        | 2    | 8      | 10  |
| engine        | 0        | 5    | 6      | 2   |
| link          | 0        | 3    | 6      | 5   |
| security      | 0        | 1    | 6      | 4   |
| durability    | 0        | 4    | 6      | 6   |
| cross-cutting | 0        | 5    | 5      | 4   |

The cross-area duplicates (XCUT-3 is HOST-1, XCUT-1 is ENGINE-5, LINK-3 is ENGINE-1) are
counted once below.

**Conditional critical:** ENGINE-1 and LINK-3. Deadlines mix the member's clock with the
server's, nothing checks skew, and the play sets up no time sync. If the bus VM resumes with
its clock more than 15 minutes behind, every new ping is stale on arrival. The fix treats it as
critical.

**What held up:**

- The openat2 guest-root core: no escape, symlink race or uncapped read (lxc).
- The link resumes from its stored position after a bus suspend with no loss or repeat
  (link).
- Health timing is monotonic (durability).

## Fix pull requests (Phase B of PLAN.md)

Each is test-first, its own branch, CI green, independently reviewed, and merged with --merge.

| PR                                 | Findings                                                                                                                | What it fixes                                                                                                                                                                                                                                                                                                 |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **B.1 seat url per kind**          | B1 (W4), HOST-8                                                                                                         | LXC seats use the bridge address; `url` is free. Branch `teamchat-b1-seat-url`, in progress.                                                                                                                                                                                                                  |
| **B.2a clocks and deadlines**      | ENGINE-1, LINK-2, LINK-3, ENGINE-2, ENGINE-3, HOST-5, HOST-6, DUR-11, DUR-12, DUR-15                                    | Deadlines on one clock, with skew measured against the server. Deadlines do not run while the team is known down. A re-fetched pending item is not stale. Time sync on the bus host in the play.                                                                                                              |
| **B.2b delivery across an outage** | LINK-1, LINK-4 to LINK-8, ENGINE-6 to ENGINE-9, SEC-1, SEC-2, SEC-3, LINK-10 to LINK-13                                 | A catch-up backlog is not rate-limited away. Recovery comes at once on reconnect. A queued send is never silently dropped, the outbox has an age and size policy, and one bad send cannot block the queue. A failed take does not leave a dead holding. No homeserver call is made under the allocation lock. |
| **B.2c suspend and outage proof**  | XCUT-4, XCUT-5, XCUT-6, LINK-9                                                                                          | A real-Tuwunel gate in CI, plus an end-to-end test: two members, the homeserver frozen and resumed (SIGSTOP, then SIGCONT, with the clock moved) and the network path cut. It asserts no loss, no duplicate, `(down)` shown, then recovery.                                                                   |
| **B.3 join codes**                 | W6 (approved design)                                                                                                    | `teamchat-admin member invite`, `teamchat seat join <code>`, the CA pinned by fingerprint in the code.                                                                                                                                                                                                        |
| **B.4 LXC on another host**        | W3 (approved design)                                                                                                    | The guest joins as a client: the static binary plus a user unit, from a play run on the container's host. `member/guest` and `unit.Render` are reused (lxc report).                                                                                                                                           |
| **B.5 host units and terminator**  | HOST-1, HOST-2, HOST-3, ADMIN-1, SEC-4, SEC-5, HOST-7 (HOST-4 is Task B.2d, coordinator ruling)                         | The health unit can probe its terminator. A terminator change restarts the terminator. A drop-in is reloaded. The admin token is sent only to a verified listener. The admin API is closed on the terminator, and the terminator's slots are bounded and logged.                                              |
| **B.6 LXC apply robustness**       | LXC-1 to LXC-5, LXC-8, ADMIN-3                                                                                          | The record is read and written under the team lock. Failed units are reset and reported. Every guest call has a bound and phases have a deadline. Host faults are not shown as offline. An issued credential is never discarded.                                                                              |
| **B.7 durability**                 | DUR-2 to DUR-10, DUR-13, DUR-14                                                                                         | `seal` pre-checks the export. The carry includes LXC members. One bad team no longer fails health and backup for all. Restart backoff, verified database backups, carry docs.                                                                                                                                 |
| **B.8 agent-facing text**          | ENGINE-4, ENGINE-5 / XCUT-1, ENGINE-10, ENGINE-12                                                                       | Exit 20's text tells the truth. The hook has bounds. The unbuilt verbs need decision D2.                                                                                                                                                                                                                      |
| **B.9 policy and admin**           | XCUT-2, HOST-9, HOST-11, HOST-12, LXC-6, SEC-7, SEC-8, SEC-10, ADMIN-2, ADMIN-4, ADMIN-6 to ADMIN-8, ADMIN-10, ADMIN-11 | The site-not-hostname refusal is wired. The approval gate covers every security field. `state` is checked by the play. CA rotation is crash-safe. `human password` never loses the new password. Quarantine is swept and secret modes are explicit.                                                           |
| **B.10 debloat and tests**         | XCUT-10 to XCUT-14, ADMIN-5, ADMIN-9, DUR-16, SEC-6, ENGINE-11, LXC-10                                                  | Test-only code is moved out of production packages. Duplicated helpers are merged. The fake homeserver is aligned with Tuwunel for parked and retired seats. The missing tests are added. Stubbed verbs are listed or removed.                                                                                |
| **B.11 docs against code**         | W9, XCUT-7, XCUT-8, LINK-14, DUR-8                                                                                      | Plan 00246 and `docs/agent-bus-cross-network.md` are corrected, the launch path is documented, and the carry gets an operator guide.                                                                                                                                                                          |

## Decisions for the owner (through the coordinator)

- **D1. Restore in production (DUR-1).** Backups run daily, but restore is refused in the
  production build until host checks H-C5 and H-C15 pass (Plan 00209 DC8). These are host proofs
  to run, not code. Proposal: queue them in `meta-deploy.list` with B.7, so restore becomes real.
  Until then, the status record will say "backups not restorable" instead of reporting them as
  healthy.
- **D2. The verbs agents are taught but which are not built (ENGINE-5, XCUT-1):** `ask`,
  `thread` and `questions`. Either build them, since v1 has the equivalents and "no degrade"
  argues for it, or remove them from the skill and the hook text now and build them later.
  Proposal: remove the text in B.8 now, so agents stop hitting internal errors, and build the
  verbs as their own pull request after B.2.
- **D3. `teamchat_approve_teams` (HOST-10).** Any change to the inventory can approve itself,
  so the owner's gate (DC18) is only as strong as the review of host_vars. Keep this, which is
  the owner's 2026-11 IaC choice, or require `team approve` under password sudo for security
  fields.
- **D4. LXC containers on the bus host.** The lxc audit notes that once B.4 exists, the
  same-host `lxc apply` path (about 6,500 lines, root writing into guests) could be retired in
  favour of every container joining as a client. That drops a capability, so it is the owner's
  call. Proposal: keep it. Decide after B.4 has run on the estate.
- **D5. Privileged containers only (LXC-7).** Unprivileged containers are refused by the
  same-host path. With B.4, any container can join as a client, so no change is proposed.

## Order

1. B.1 (in progress).
2. B.5, B.6 and B.8 in parallel: their areas don't overlap.
3. B.2a, then B.2b, then B.2c: the suspend requirement, in that order because they share the
   link and engine.
4. B.3, then B.4.
5. B.7, B.9, B.10, B.11.

The estate's own IaC moves its teamchat pin after B.1, B.2 and B.4 land.
