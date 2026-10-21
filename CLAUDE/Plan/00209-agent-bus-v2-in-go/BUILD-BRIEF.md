# Brief: the full agent-team Go rewrite (for the dedicated build session)

From the team's coordinator session. The owner asked for this
directly: "smash out the full agent team go rewrite - fully reviewed, optimised, fine tuned. Use
haiku/sonnet/opus as required. Deliver this to an exceptionally high standard." You are the
dedicated build session for it. The coordinator stays coordinator: it merges, runs the full QA
gate, pushes, and sends host deploys. You do the work.

## Budget and pace

The owner says your token has a full week of usage left and the 7-day window ends in under 40
hours. Use it to deliver the best result: run sub-agents in parallel across the work, review
adversarially, and iterate. Do not spend tokens for the sake of it. Keep about 6 sub-agents running
at most, because they share this host with the coordinator's. Under load, timing-sensitive suites
give false failures.

## Scope

Rewrite the agent-team bus in Go as v2, a separate system beside v1 (00209's DESIGN-v2.md): the
host side too (the homeservers, the installer, the admin program, Element profiles and what
`ccy-teams` reads). Stage 1 (v1's `ccy-teams` open time) is an early owner-visible win. No
requirement may be lost (a Python quirk is not one: PLAN.md, "The owner's directions"). On the
session side v2 replaces:

- `helpers/pingbus/` (about 13k lines of Python, shipped as a zipapp): every CLI verb, the hooks,
  the watcher and waiter, inbox, outbox, envelope, protocol, seat, the status line, limits,
  config, and the relay.
- The agent-bus Claude plugin's hooks and skill text, wherever they call pingbus.
- The Go side that already exists: `src-go/cmd/ccy-teams` and `src-go/internal/agentbus` (its
  parser), and `src-go/internal/pingbus/statusline` (done by Plan 00213 Task 2.1, which passes the
  Python parity oracle).
- `helpers/agent_bus/` and `helpers/agent_bus_lxc/` (about 8.4k lines): CLAUDE.md says "Python that
  Ansible loads stays Python". Decide, per module, whether it is Ansible-loaded, and say so in the
  plan. A standalone CLI among them is in scope; a module the play imports is not, unless the
  design says otherwise and gives its reason.

## The plans that already exist (read these first, in this order)

1. **Plan 00209, agent-bus v2 in Go:** the design of record, and this brief's own plan:
   DESIGN-v2.md (which supersedes DESIGN.md), the RESEARCH-\*.md files, and the reviews in
   subagent-reports/. Its owner decisions (DESIGN-v2.md section 7) are not signed. The owner's standing
   instruction is "DO NOT GATE ON ME". So take each decision's recommended option, record it in
   PLAN.md as defaulted, and send the coordinator any decision that is irreversible or that
   contradicts the owner's words. Do not wait on any of them.
2. **Plan 00213, the agent-bus performance Go carve-out:** the owner's ordering. Carve the standalone
   pieces out first (the status line is done), then move the rest over gradually. It has the
   parity-oracle pattern: Python and Go run over the same fixtures, and the golden test's
   `-update` re-records from Python. Reuse that pattern only where Go replaces a v1 piece in place
   (stage 1's reads): v2 shares no format with v1, so it has no oracle against the Python
   (src-go/CLAUDE.md, "How a port is built"). It also records owner
   requirements:
   - the bus segment has its own TTL cache, separate from the hooks-daemon line;
   - the refresh is 25 s, because the daemon-owned supervisor treats the context sidecar as stale
     after 30 s;
   - Go stderr may differ in wording but keeps its shape, and empty stays empty.
3. **Plan 00211, the reports and questions rooms:** live work in Python. Phase 1 is deployed. Phase
   2 (room creation, deploy 2) is being built now by a coordinator sub-agent on another branch.
   The Go rewrite must carry 00211's behaviour (reports, questions, threads, asking a human, the
   side rooms; its SPEC.md), as behaviour, not as pingbus 0.2.0's verb names or formats.
   Do not edit 00211's files while its Phase 2 branch is open. Tell the coordinator what you need.
4. **Plans 00161, 00194 and 00201:** the bus history, live delivery and multi-team design. Read
   them for constraints. 00194 and 00201 still have Python tasks open; fold into the Go plan any
   that the rewrite supersedes, and tell the coordinator which.

Decide whether 00209 or 00213 is the plan of record for the build; 00209 looks like the natural
home. Record the choice in both plans, and avoid having two plans for one thing.

## How to work

- **Worktrees always.** You share /workspace (the same checkout and branch F44) with the
  coordinator. Never edit, commit or merge in /workspace itself; every change is made in a git
  worktree on its own branch. Sub-agents run with worktree isolation.
- **Spec first, test first.** Read src-go/CLAUDE.md and CLAUDE/GoProjectRules.md before any Go.
  v2 is built to a spec of intended behaviour, never as a line-by-line replay: the Python is a
  prototype, read for requirements and lessons (PLAN.md, "The owner's directions"). v2 shares no
  format, lock or wire with v1; it installs beside v1, touches nothing v1 uses, and teams move one
  at a time (DESIGN-v2.md section 1). Old and new versions of each system must run side by side
  on the host, in the ccy image and on LXC members, which are upgraded at different times. LXC
  teams are top priority.
- **Models:**
  - Haiku: first drafts of boilerplate against an existing oracle. The owner suggested it, and it
    worked for the status line, but treat its output as untrusted.
  - Sonnet: mechanical work, bookkeeping and small fixes.
  - Opus: design, the hard parts (hooks, watcher, locking, the delivery and loss paths, signals
    and process groups) and every adversarial review.
  - Fable: hit its weekly limit and returns 23 October, 7am Europe/London. Until then Opus stands
    in as the adversarial reviewer. Once Fable is back, use it for the final review of each
    milestone.
- **Review before every hand-over:** an adversarial reviewer (not the author) gives MERGE or
  FIX-FIRST, and that loops until MERGE. Then run the qa-reviewer agent as each plan's final step.
- **QA:**
  - Run `./scripts/qa-changed.bash` before each commit.
  - **Never run `./scripts/qa-all.bash`.** The coordinator runs it before every push. Two full
    runs at once exhaust the host.
- **Repository rules** (CLAUDE.md is binding):
  - fail fast;
  - never run Ansible, only syntax-check;
  - no install-specific names in any tracked file;
  - never push to F44, never force-push, never tag;
  - use the CCY/container version bump when the pre-commit hook demands it, and expect a re-bump
    at merge time, because other branches move the version;
  - write journal entries only through `CLAUDE/Plan/mkplan.bash --journal`.
- **Deploys:** add host runs to `CLAUDE/Plan/meta-deploy.list` on your branch, each with its
  description comment. The coordinator merges them and asks the host to run meta-deploy. Never
  hand anyone individual commands.

## Hand-over to the coordinator

Send short messages on the bus to the coordinator (`pingbus --team <team> say --to <coordinator> '<text>'`):

- when a milestone branch has passed review and qa-changed (give the branch, tip commit and the
  review verdict file);
- when a decision needs the owner;
- when you are blocked.

Put long material in files under the plan folder and send the path. The coordinator merges one
branch at a time with `--no-ff`, runs qa-all, pushes, deploys, and reports results back to you.
Milestones should be deployable on their own and safe beside the old code.

## Definition of done

- v2 does in Go what every pingbus verb, hook and long-running process does.
- The Python zipapp is retired from the host, the ccy image and the LXC member kit, once every
  team has moved and the owner agrees (DESIGN-v2-D-migration.md 7.2).
- The proof that v2 touches nothing of v1 passes, each team's migration passes its acceptance,
  and v2's own mixed-version window is proven safe.
- Host acceptance passes through meta-deploy.
- CPU per status-line render and per hook event is measured before and after; this is the
  performance goal of 00213.
- Docs and plans are current, every plan is closed through the qa-reviewer, and every Opus or
  Fable review is at MERGE.
