# The Watch List

External public repositories this project keeps an eye on and scans at intervals, looking for
good ideas, tooling, concepts and inspiration for fedora-desktop: the Fedora IaC, the ccy Claude
Code container, agent teams, the hooks daemon and the Go QA tooling.

These are public third-party projects, so their names are tracked here. That is the opposite of
`untracked/repos/`, which holds private reference clones that must never be named
([AgentNotes.md](AgentNotes.md)). The watch list's local mirrors live in `untracked/watchlist/`
(gitignored, one directory per repository, `<owner>__<repo>`).

## What a scan produces

Each scan of a repository ends in one of two outcomes per finding, never in a bare note:

- **a GitHub issue** in this repository, for a clear, defined win (what to adopt, why, where it
  would land here);
- **a plan** (`CLAUDE/Plan/mkplan.bash`) where the idea needs discussion or research first.

A finding this repository already has, or that does not fit its principles (YAGNI, fail fast,
Podman first, tooling is Go), produces nothing but a line in the run report.

**The owner's standing request.** In the owner's words: "outcome to be GH tickets created, or
plans created as appropriate - plans for where there is more discussion/research needed -
straight to gh tickets for clear wins that are defined and clear". So the `watchlist-scout`
files an issue for a clear win without asking, and a plan for anything that needs discussion.
It is recorded as `watchlist-issue-filing` under `standing_authorisations` in
`.claude/hooks-daemon.yaml`. It covers the scout filing issues and plans from a watch-list scan,
and nothing else: no other agent or task may treat it as permission to file issues.

**A clear win** names the one place it lands here, needs no design decision, and would be one
pull request. A scan files at most 3 issues; further wins go in the run report for the owner to
promote. Each issue carries the `watchlist` label, the run id and the reviewed range, and its
text is scanned for secrets and install-specific names before it is sent (the repository is
public and the pre-commit scanner does not see `gh`).

## Ideas, reusable code, anything else valuable

A scan looks for three things: **ideas** to adopt (a design, a feature, a rule), **code** this
repository could take as it is, and **anything else of value** (a test technique, a security
finding, a tool worth installing, a defect upstream that affects us). Each becomes an issue or a
plan by the rules above.

**Code is reused only under a licence that allows it.** This repository is MIT. Before a finding
proposes copying or vendoring code, the scout records the watched repository's licence (its
SPDX id from `gh repo view <owner>/<repo> --json licenseInfo`, confirmed against its LICENSE file,
and any per-file headers on the code in question):

- **MIT, BSD, ISC, Apache-2.0** and similar permissive licences: reusable, keeping the copyright
  and licence notice with the copied code (and Apache-2.0's NOTICE, if any).
- **MPL-2.0**: only as whole, separate files that keep their licence.
- **GPL, AGPL, LGPL, SSPL, BUSL, Elastic, any non-commercial or "source available" licence, or
  no licence at all**: never copied. The idea may still be adopted, written afresh here.

The issue or plan states the licence and which of these applies. A licence change upstream
(the LICENSE file in a scanned range) is itself a finding.

## The state

[Watchlist.tsv](Watchlist.tsv) is the machine-read list: for each repository the URL, the branch
reviewed, the last-reviewed commit and the run that recorded it. The `watchlist` command owns it
(`add`, `mark`); it is read strictly and a malformed row is an error. Instruction files carry no
dates, so when a review happened lives in the run id, the run report and the plan journal. The
command's behaviour is specified in `SPEC-watchlist.md` of Plan 00200. Only `github.com`
repositories can be listed. The command rewrites the file whole, so comments added by hand to
the TSV are lost on the next `add` or `mark`; put commentary in this document.

| Repository                                                                                                                | Why it is watched                                                                                                           | Areas to look at                                                                                                     |
| ------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| [cdknorow/coral](https://github.com/cdknorow/coral)                                                                       | A multi-agent Claude Code orchestrator with a shared message board and task queue: the nearest neighbour of ccy agent teams | Task dependencies and wait-don't-poll messaging, agent role and team models, cost tracking, hooks usage              |
| [devvmhq/devvm](https://github.com/devvmhq/devvm)                                                                         | One sandbox VM per project with its toolchain, for a person or a coding agent, and a proxy in front of the agent            | The proxy: per-agent permission control, OAuth key hot-swap, gh calls limited to named repos under an org-wide token |
| [mvschwarz/openrig](https://github.com/mvschwarz/openrig)                                                                 | Persistent agent teams across Claude Code, Codex and Pi, with roles, shared context and owned work                          | Team, role and work-ownership models against ccy agent teams and the bus                                             |
| [kstenerud/yoloai](https://github.com/kstenerud/yoloai)                                                                   | Agent sandboxing that treats the agent as a security risk, the stance ccy takes                                             | Isolation boundaries, what it allows and denies                                                                      |
| [jingkaihe/matchlock](https://github.com/jingkaihe/matchlock)                                                             | A Linux sandbox securing AI agent workloads                                                                                 | Its lock-down model against ccy's                                                                                    |
| [Edmonds-Commerce-Limited/claude-code-hooks-daemon](https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon) | The hooks daemon this repository runs                                                                                       | New handlers and config worth enabling; upstream fixes to issues filed from here                                     |
| [Maxteabag/hotseat](https://github.com/Maxteabag/hotseat)                                                                 | A dashboard and CLI for Claude Code and Codex accounts, their quota and work a usage limit stopped                          | Token pools, usage limits, account hand-over against ccy's token management                                          |
| [Maxteabag/clarp](https://github.com/Maxteabag/clarp)                                                                     | A self-hosted server, PWA and desktop app for coding agents on your own machine                                             | Remote access to local agent sessions                                                                                |
| [Yu-369/VibeCurb](https://github.com/Yu-369/VibeCurb)                                                                     | Injects taste into AI workflows so agents produce less generic output                                                       | How it steers output quality, as prompts or hooks                                                                    |
| [trailofbits/coop](https://github.com/trailofbits/coop)                                                                   | An isolated VM for Claude Code and Codex, from a security firm                                                              | Its threat model and isolation against ccy's container                                                               |
| [coipond/coi](https://github.com/coipond/coi)                                                                             | A machine per agent, with root and Docker                                                                                   | Docker inside the sandbox; per-agent machines                                                                        |
| [eugene1g/agent-safehouse](https://github.com/eugene1g/agent-safehouse)                                                   | Agents read and write only what they need                                                                                   | Path-scoped permission profiles                                                                                      |
| [clawkwork/clawk](https://github.com/clawkwork/clawk)                                                                     | A disposable Linux VM per coding agent                                                                                      | VM lifecycle and reset                                                                                               |
| [madarco/agentbox](https://github.com/madarco/agentbox)                                                                   | Several agents in parallel sandboxed VMs from one command                                                                   | Parallel orchestration of sandboxes                                                                                  |
| [webcoyote/sandvault](https://github.com/webcoyote/sandvault)                                                             | Agents in their own OS user account plus a sandbox                                                                          | User-account isolation                                                                                               |
| [nolabs-ai/nono](https://github.com/nolabs-ai/nono)                                                                       | Zero-trust sandboxing for agent runtimes                                                                                    | The permission model and its enforcement                                                                             |
| [smol-machines/smolvm](https://github.com/smol-machines/smolvm)                                                           | An embeddable, branchable VM for local agents                                                                               | Branching VM state, fast boot                                                                                        |
| [rudderlabs/devbox](https://github.com/rudderlabs/devbox)                                                                 | One sandbox per task with no real secrets inside                                                                            | How credentials reach a sandbox without being in it: the credential-proxy idea                                       |
| [delta-box/deltabox-lite](https://github.com/delta-box/deltabox-lite)                                                     | A sandbox whose changes are diffed, committed or aborted                                                                    | Reviewing and rolling back an agent's changes                                                                        |
| [KarpelesLab/teamclaude](https://github.com/KarpelesLab/teamclaude)                                                       | A proxy pooling several Claude Code and Codex accounts                                                                      | OAuth key hot-swap through a proxy; check the provider's terms before adopting                                       |
| [liaohch3/claude-tap](https://github.com/liaohch3/claude-tap)                                                             | Intercepts and inspects coding-agent API traffic                                                                            | Where a proxy can observe and filter agent calls                                                                     |
| [agentgateway/agentgateway](https://github.com/agentgateway/agentgateway)                                                 | A proxy for agents and MCP servers with policy                                                                              | Authorisation at the proxy, MCP permission control                                                                   |
| [awslabs/cli-agent-orchestrator](https://github.com/awslabs/cli-agent-orchestrator)                                       | Multi-agent orchestration of coding CLIs in tmux                                                                            | Against ccy teams and the agent bus                                                                                  |
| [agent-of-empires/agent-of-empires](https://github.com/agent-of-empires/agent-of-empires)                                 | Managing several agents from a TUI or the web                                                                               | Session management, remote access                                                                                    |
| [standardagents/dmux](https://github.com/standardagents/dmux)                                                             | An agent multiplexer over git worktrees                                                                                     | Worktree-per-agent workflow                                                                                          |
| [OrchestratorInc/agent-orchestrator](https://github.com/OrchestratorInc/agent-orchestrator)                               | Supervises fleets of agents from task to merged PR                                                                          | The supervision loop through to merge                                                                                |
| [herdrdev/herdr](https://github.com/herdrdev/herdr)                                                                       | A runtime for coding agents, widely used                                                                                    | Session runtime and lifecycle                                                                                        |
| [Ark0N/Codeman](https://github.com/Ark0N/Codeman)                                                                         | Self-hosted mission control for coding agents                                                                               | Dashboards and session supervision                                                                                   |
| [pchalasani/claude-code-tools](https://github.com/pchalasani/claude-code-tools)                                           | Productivity tools for Claude Code and other CLIs                                                                           | tmux and session helpers                                                                                             |
| [karanb192/claude-code-hooks](https://github.com/karanb192/claude-code-hooks)                                             | Claude Code hooks for safety, cost and observability                                                                        | Hook ideas against the hooks daemon's                                                                                |
| [Maciek-roboblog/Claude-Code-Usage-Monitor](https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor)                 | Real-time usage monitoring with predictions                                                                                 | Usage and limit prediction against ccy's token usage                                                                 |
| [starbaser/ccproxy](https://github.com/starbaser/ccproxy)                                                                 | Hooks into every Claude Code request and response                                                                           | Request-level control in a proxy (licence unclear: ideas only)                                                       |
| [DevDock-AI/claude-unlimited](https://github.com/DevDock-AI/claude-unlimited)                                             | Rotates subscriptions and keys at a usage limit                                                                             | Key rotation mechanics (check the provider's terms)                                                                  |
| [ldegio/agtop](https://github.com/ldegio/agtop)                                                                           | A top-style view of agent sessions                                                                                          | Session monitoring (GPL: ideas only)                                                                                 |
| [generalaction/emdash](https://github.com/generalaction/emdash)                                                           | A parallel coding-agent environment                                                                                         | Running agents side by side                                                                                          |
| [sodiumsun/agenttrail](https://github.com/sodiumsun/agenttrail)                                                           | Local observability for agents                                                                                              | What an agent did, recorded and shown                                                                                |
| [clash-sh/clash](https://github.com/clash-sh/clash)                                                                       | Detects merge conflicts across worktrees early                                                                              | Conflict warnings between parallel branches                                                                          |
| [maximhq/bifrost](https://github.com/maximhq/bifrost)                                                                     | An LLM and MCP gateway                                                                                                      | Routing, RBAC and per-user identity at a gateway                                                                     |
| [ThinkWatchProject/ThinkWatch](https://github.com/ThinkWatchProject/ThinkWatch)                                           | An LLM and MCP gateway with per-user identity                                                                               | Access control and audit at a gateway                                                                                |
| [IT-Hjaelp-Nu/keybox](https://github.com/IT-Hjaelp-Nu/keybox)                                                             | A credential broker giving agents short-lived, scoped GitHub tokens                                                         | Per-repo GitHub token scoping (new and small)                                                                        |
| [tonyweckstrom1987/holvi](https://github.com/tonyweckstrom1987/holvi)                                                     | A credential broker for agents                                                                                              | Scoped, short-lived credentials (new and small)                                                                      |

Coral's initial full review is Plan 00193's `AUDIT-coral.md`. It is recorded as reviewed at the
commit that audit read, so the first scan covers only what changed after it.

## The process

1. `watchlist sync` clones any missing mirror and fetches the rest. For each repository it prints
   the last-reviewed commit, the new head, the commits between them and the changed paths. A
   repository never reviewed is reported as `initial: full review`, and each report names the
   mirror directory. A failed fetch is an error, never a skip. So is a force-pushed upstream:
   a last-reviewed commit that the head no longer descends from is reported as a rewritten
   history, not as a range.
2. The [`watchlist-scout`](../.claude/agents/watchlist-scout.md) agent reviews: the whole project
   for an initial repository, only the diff range otherwise. It judges each finding against what
   this repository already has, files issues and plans as above (checking for duplicates first),
   and writes a run report to Plan 00200's `subagent-reports/<run id>.md`. The report is
   tracked: it is what the run id in `Watchlist.tsv` resolves to, and it records why each
   finding was filed or discarded, which the next scan of that repository builds on. Only the
   drafts of issue bodies and comments stay in `untracked/watchlist-reports/`, scanned there
   before `gh` publishes them.
3. Only after its outcomes are filed does the scout run `watchlist mark <repo> <sha> --run <id>`.
   A scan that dies midway leaves the repository unmarked, so the next scan repeats it rather
   than losing it. `mark` accepts only a commit on the watched branch that descends from the
   last review.

The command runs from the checkout, not from an installed binary, because nothing outside an
agent run needs it:

```bash
cd src-go && go run ./cmd/watchlist --root .. sync
```

Installing it on the host would go through `tasks/build-go-command.yml` like `ccy-teams`, and is
a decision for when a human wants to run it by hand.

## Using it

The owner's entry point is the [`watchlist` skill](../.claude/skills/watchlist/SKILL.md):

- `/watchlist` scans every repository;
- `/watchlist <owner>/<repo>` scans one;
- `/watchlist add <url>` adds a repository and runs its initial full review.

## How scans recur

Scans are run by hand with `/watchlist` for now. Choosing a mechanism that runs them at intervals
(a persistent cron, a scheduled agent, a plan task reminding the owner) is an open decision for
the owner, tracked in Plan 00200.

## Adding a repository by hand

Do not edit the TSV for a new repository: `watchlist add <url>` validates the URL (https, github.com only),
refuses duplicates and records the default branch. Add its row to the table above with the reason
it is watched and the areas to look at.
