# coral adoption audit (read-only)

Subject: `github.com/cdknorow/coral`, read at head `e785da3` from a shallow reference clone
(`untracked/repos/coral`, not tracked). Nothing was installed, built or run; every claim
below comes from reading the code, the git history, the GitHub API, or this repository.
Line references are `path:line` inside the coral clone unless they start with a path of this
repository. Anything not checked is marked **unverified**.

## Verdict

**Leave it (option e), and borrow three or four ideas into `ccy-teams` and the agent bus
(option d).** Do not adopt it, and do not fork it.

Coral is a capable, fast-moving product, but it is built on the opposite security model to
ours. It runs every agent as the user on the host, trusts every loopback connection with full
control, binds `0.0.0.0` by default, types free text from one agent into another agent's
terminal, and is heading towards a hosted SaaS hub. Making it fit rootless per-project Podman
containers would mean rewriting its launch path, its hook transport, its transcript indexing
and its auth model: that is a fork of a 190,000-line codebase that changes by about 100
commits a day. What it does well (outcome-conditioned task dependencies, wait-don't-poll,
per-agent cost, a phone view) is small to copy as ideas and does not need its code.

The owner's three questions, answered briefly:

1. **Adopt? Make it work with ccy/cc?** Not with ccy without the rewrite above (§3, §4).
   With `cc` it would work almost as is, because `cc` is Claude Code on the host and that is
   what coral launches. But coral would start its own `claude` processes on its own tmux
   socket rather than drive `cc`'s token picker and the `ccy` tmux server (`tmux -L ccy`), and
   it would add a listening port and loopback trust to a host that has the owner's
   credentials. "Works with cc" means "replaces cc", not "integrates with it".
2. **How opinionated?** Very: it is a product with a fixed shape (§3).
3. **Fork?** No (§7): too large, too fast-moving, and a style opposite to ours.

## 1. What coral is

Coral ("the control plane for the coding agents you already have", `README.md`) is a single Go
server plus a browser dashboard that launches agent CLIs (Claude Code, Codex, Gemini CLI
"agy", Pi.dev) in tmux sessions on the machine it runs on, and coordinates them through a
shared message board and task queue. Each agent is started with an injected settings file
that adds coral's own Claude Code hooks and a system prompt describing the board
(`coral-go/internal/agent/claude.go:159-280`, hooks at `:320-364`). Agents talk back to the
server over HTTP through the `coral-board` CLI and the `coral-hook-*` binaries, which find it
through `CORAL_URL` (`coral-go/internal/agent/agent.go:69-92`). The server keeps state in
SQLite under `~/.coral` (`coral-go/internal/config/config.go:95-130`, `CLAUDE.md`), indexes
every vendor's transcripts for search and cost, runs cron jobs and workflows in git worktrees
under `~/.coral/worktrees` (`coral-go/internal/startup/startup.go:783`), and serves a web and
mobile (PWA) dashboard with live terminals, permission-prompt answering and an "auto-accept"
mode. There are macOS app and tray builds (`coral-go/cmd/coral-app`, `cmd/coral-tray`), a
Windows build (ConPTY), and an optional outbound tunnel to "Coral Hub", a hosted service at
`app.coralai.ai` in closed beta (`coral-go/internal/cloud/manager.go:1-7`,
`coral-go/internal/config/tier_prod.go:14`).

### Facts and health

| Item                 | Finding                                                                                                                                                                                                                                                                                          |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Language             | Go 1.26 server and CLIs; vanilla JS frontend (`coral-go/pkg/frontend/static`); a retired Python implementation still in the tree (`coral-python-legacy`)                                                                                                                                         |
| Size                 | ~84k lines of non-test Go, ~73k lines of Go tests, ~36k lines of JS, ~22k lines of legacy Python; ~305 HTTP routes registered in `coral-go/internal/server/server.go`                                                                                                                            |
| Dependencies         | chi, sqlx, two SQLite drivers (modernc and a SQLCipher fork), creack/pty, conpty, webview, systray, robfig/cron, nhooyr websocket, go-qrcode, fsnotify, beeep (`coral-go/go.mod`)                                                                                                                |
| Licence              | `LICENSE` is Apache-2.0, but release builds require accepting a proprietary EULA from "subgentic LLC" that calls the codebase its "exclusive property" and the licence "revocable" (`coral-go/pkg/license/terms.go`); the `dev` build tag skips it (`CLAUDE.md`)                                 |
| Age and activity     | Repository created 2026-02-18; ~1,670 commits; 255 commits in the four days before this audit; 101 GitHub releases, 15 of them (v1.4.3 to v1.4.17) in the last two days                                                                                                                          |
| Maintainers          | Effectively one person under three accounts; one outside contributor with 5 merged PRs. Commit subjects end in agent role tags such as "(Lead)", "(QA)", "(Backend)": the project is largely written by its own agent team, tracked on an internal board (`#33xx` numbers are not GitHub issues) |
| Issues and PRs       | 2 GitHub issues and 23 PRs in total: almost no public review or community traffic                                                                                                                                                                                                                |
| Tests and CI         | Large Go test suite, but CI runs only gofmt, the cloud/tunnel subset (`.github/workflows/cloud-tests.yml:42-48`), an encryption check and a release smoke test. No workflow runs `go test ./...`, `go vet` or a linter (**verified by reading every workflow file**)                             |
| Telemetry            | PostHog install/upgrade/daily-open events, on by default in release builds where the key is injected (`.github/workflows/release.yml:50`), opt-out by setting or `CORAL_TELEMETRY_DISABLED` (`coral-go/internal/tracking/posthog.go:1-120`); empty in a source build                             |
| Security fix cadence | Six releases titled as security fixes, four in the last week: shell injection in agent launch (v1.4.16), a board message running commands in a terminal agent (v1.4.17), input reaching the wrong agent (v1.4.14), dashboard script injection (v1.4.12) (`RELEASE_NOTES.md`)                     |
| Docs drift           | README still advertises the "local LLM proxy" for cost tracking; the server says proxy interception "is retired" (`coral-go/internal/server/server.go:741-743`)                                                                                                                                  |

## 2. Overlap and gaps against what we have

| Capability                                                               | coral                                                                                                                                                                                                                      | Ours                                                                                                                                                                            | Gap for us?                                                                    |
| ------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| Isolation of an agent                                                    | None: host process as the user, in tmux; capability lists become Claude `permissions` entries (`internal/agent/permissions.go:44`), which are advisory to the agent, not a sandbox                                         | `ccy`: rootless Podman container per project, project bind-mounted at `/workspace`, Claude state kept in the project (`files/var/local/claude-yolo/claude-yolo:2525-2535`)      | coral is far weaker; this is the deciding difference                           |
| Session persistence, attach, take over                                   | tmux sessions on its own socket, falls back to scanning the default socket (`internal/tmux/client.go:42-50`)                                                                                                               | `ccy` tmux under `systemd --user`, `ccy-sessions` picker, take-over (Plan 00158), survive a reboot (Plan 00135)                                                                 | Parity; ours is tied to containers                                             |
| Sleep and wake a whole team, resume by session id                        | Yes (kills the process, relaunches with `--resume`)                                                                                                                                                                        | `ccy-sessions` restore records and `podfreeze`/`lxcfreeze` freeze (Plans 00135, 00191)                                                                                          | Roughly parity                                                                 |
| Team definition and launch                                               | JSON teams in `~/.coral/teams`, roles, prompts, templates, AI-generated teams                                                                                                                                              | `ccy-teams` + `ccy --teams <seat>@<team>` seats (Plans 00161, 00177)                                                                                                            | coral has richer templates; ours is IaC-provisioned                            |
| Inter-agent messaging                                                    | Free-text board, messages typed into recipients' terminals; identity self-asserted from `CORAL_SUBSCRIBER_ID` with no per-agent secret (`internal/agent/agent.go:76-77`, `internal/server/routes/identity_guard.go:18-24`) | Agent bus: private Matrix homeserver per team, agent-to-agent **pings only** (closed verb + artefact reference), humans via Element (Plan 00161, `PROTOCOL.md`)                 | Ours is deliberately safer; coral's design is the risk ours was built to avoid |
| Task queue with outcome-conditioned dependencies, artefacts, review hold | Yes, the strongest feature (`README.md`, "Task queue with contracts")                                                                                                                                                      | No equivalent                                                                                                                                                                   | **Real gap**: worth borrowing as an idea                                       |
| Wait instead of poll                                                     | `coral-board wait --from <agent>`; server wakes the agent                                                                                                                                                                  | `pingbus wait`/`recv` and wake hooks (Plan 00161)                                                                                                                               | Parity in design                                                               |
| Web/phone dashboard, answer permission prompts remotely                  | Yes, plus "auto-accept" of prompts                                                                                                                                                                                         | Element client on desktop and phone for the bus; no terminal-in-browser; ccy runs with `--dangerously-skip-permissions` inside the container, so there are no prompts to answer | We have no browser terminal view; we do not want one listening on a port       |
| Cost per agent/team/branch                                               | Yes, from hooks and transcripts                                                                                                                                                                                            | Token usage via rate-limit headers in `ccy` (Completed Plan 00101); no per-team rollup                                                                                          | Small gap; an idea to borrow                                                   |
| Cross-vendor agents (Codex, Gemini, Pi)                                  | Yes                                                                                                                                                                                                                        | Claude Code only                                                                                                                                                                | Not a need today (YAGNI)                                                       |
| Scheduled runs and workflows in fresh worktrees                          | Yes (cron + worktree per run)                                                                                                                                                                                              | None in ccy                                                                                                                                                                     | Gap, not requested                                                             |
| Guard rails on agent actions                                             | Its own hooks only record state and deliver messages (`claude.go:320-364`)                                                                                                                                                 | Hooks daemon: PreToolUse guards, stop enforcement, plan workflow, secret protection                                                                                             | Ours is much stronger; coral merges our hooks in, it does not replace them     |
| Transcript search                                                        | Indexes `~/.claude/projects` on the host (`claude.go:18-24`)                                                                                                                                                               | `ccy` transcripts live in each project's `.claude/ccy/` inside the container mount                                                                                              | coral would not find ccy transcripts without changes                           |

## 3. How opinionated it is

Very. It is a product with a fixed shape, not a toolkit. Concrete assumptions, and the cost of
changing each:

| Assumption                                                                       | Evidence                                                                                                                                                                                                                                              | Cost to change                                                                                                                                                                             |
| -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Agents run as host processes of the same user                                    | `BuildLaunchCommand` produces a host shell line run in tmux (`internal/agent/claude.go:159-280`)                                                                                                                                                      | **High.** A `cli_path_claude` wrapper is allowed (`internal/agent/cli_path.go`), but see the next four rows                                                                                |
| Launch inputs are host temp files                                                | Settings and prompt written with `os.CreateTemp` in the host temp dir and passed by path, prompt via `$(cat …)` (`internal/agent/agent.go:434-454`, `claude.go:248, 273`)                                                                             | Medium: a wrapper would have to mount or copy them into the container                                                                                                                      |
| Agents reach the server at `127.0.0.1`                                           | `CORAL_URL` is forced to `127.0.0.1` when bound to all interfaces (`internal/agent/agent.go:79-87`)                                                                                                                                                   | Medium-high: inside a container network namespace that is the container itself; needs a host gateway address, and then the request is no longer loopback, so it needs the API key (see §5) |
| Hooks are binaries on the agent's PATH                                           | `coral-hook-agentic-state` etc. injected as bare commands (`claude.go:320-364`)                                                                                                                                                                       | Medium: the binaries must be baked into the ccy image and versioned with the server                                                                                                        |
| Claude state is `~/.claude/projects` on the host, keyed by host path             | `HistoryBasePath`, `PrepareResume` (`claude.go:18-24, 282-317`)                                                                                                                                                                                       | High: ccy keeps state per project under `.claude/ccy`, every cwd is `/workspace`, so the path key collides across projects                                                                 |
| tmux, with a fallback to the user's default tmux server                          | `internal/tmux/client.go:42-50`                                                                                                                                                                                                                       | Low to set a socket; the fallback sees whatever runs on the default server. ccy uses its own server (`tmux -L ccy`), so the two would not see each other                                   |
| Data in `~/.coral`, worktrees in `~/.coral/worktrees`, teams in `~/.coral/teams` | `config.go:160-176`, `startup.go:783`                                                                                                                                                                                                                 | Low: `--home` / `CORAL_DATA_DIR`                                                                                                                                                           |
| Listens on TCP `0.0.0.0:8420` by default, plain HTTP                             | `internal/config/config.go:114`; README presents `--host 0.0.0.0` as an option, but it is the default                                                                                                                                                 | Low to set `CORAL_HOST=127.0.0.1`; TLS needs `--tls-cert` (v1.4.17)                                                                                                                        |
| Loopback is fully trusted                                                        | `internal/auth/middleware.go:42-55`; the code's own note says `/send`, `/keys`, `/answer-prompt` and auto-accept are not peer-checked (`internal/peercred/peercred.go:1-24`)                                                                          | High: a design decision across ~305 routes                                                                                                                                                 |
| Config is JSON and SQLite settings, edited from the dashboard                    | Teams JSON, settings in the DB (`internal/server/routes/system.go`)                                                                                                                                                                                   | Medium: no declarative file Ansible could own end to end (**unverified** that every setting has an env or file equivalent)                                                                 |
| Hosted hub and EULA in release builds                                            | `tier_prod.go:14`, `internal/license/eula.go`                                                                                                                                                                                                         | Low in a source build (`-tags dev`), but that is a build we would own                                                                                                                      |
| Errors are logged and execution carries on                                       | e.g. `writeTempFile` falls back to a predictable path and continues (`agent.go:440-447`); tmux sends Enter "even if send-keys reported an error" (`internal/tmux/client.go:535-537`); ~750 discarded error values in non-test Go (rough `grep` count) | Not changeable: it is the codebase's style, opposite to our fail-fast rule                                                                                                                 |

## 4. Fit with our constraints

| Constraint                            | Fit                                                                                                                                                                                                                                                            |
| ------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Rootless Podman container per project | **Does not fit.** coral has no container awareness at all (no Podman/Docker code in `internal/`). Running coral itself inside one ccy container would put every agent in one container, which defeats per-project isolation                                    |
| Agents inside containers              | Only through a wrapper `cli_path`, with launch files, hook binaries, server address and transcript paths all needing translation (§3)                                                                                                                          |
| Strict IaC / Ansible                  | Poor. A binary download plus a systemd unit is easy; the team, settings and hub state live in SQLite edited through the dashboard                                                                                                                              |
| Fail fast                             | Opposite style (§3, last row)                                                                                                                                                                                                                                  |
| Go tooling direction                  | Same language, but a 190k-line monolith is not something to fold into `src-go/`                                                                                                                                                                                |
| Public repo, no secrets               | Neutral: its state stays in `~/.coral`; its API key is shown as a QR code and accepted in the query string (`internal/auth/keystore.go:301-311`), so it ends up in browser history and logs                                                                    |
| Anything listening on a port          | Fails by default (`0.0.0.0`, plain HTTP, loopback trusted). Our only listener is the agent bus homeserver: `127.0.0.1` plus chosen addresses behind the firewall, also without TLS, but every client authenticates and agents may only send pings (Plan 00161) |

## 5. Security concerns

1. **Loopback is root of trust.** Any process on the host that can open a TCP connection to
   `127.0.0.1:8420` gets full control: type into any agent's terminal, answer its permission
   prompts, turn on auto-accept, read every transcript (`internal/auth/middleware.go:42-55`).
   That includes every agent coral itself runs, so one prompt-injected agent can drive all the
   others. Coral's own comment admits this and limits the peer-uid check to the hub routes
   (`internal/peercred/peercred.go:3-24`).
2. **Listens on every interface by default** over plain HTTP (`config.go:114`); remote access
   is gated by one long-lived API key that can be passed in the URL.
3. **Free-text agent-to-agent messages typed into terminals.** The board delivers messages by
   typing into the recipient's tmux pane. A board message running shell commands in a terminal
   agent was fixed only in v1.4.17; launch-time shell injection from imported team definitions
   only in v1.4.16 (`RELEASE_NOTES.md`). Identities are self-asserted (now validated for
   control characters, not authenticated: `identity_guard.go:18-24`). Our bus forbids
   agent-to-agent free text for exactly this reason.
4. **Auto-accept of permission prompts**, with a fixed list of "high-risk" actions held back
   (`internal/server/routes/auto_accept_risk.go:10-55`): a deny-list, so anything it does not
   recognise is accepted.
5. **No sandbox.** Agents run as the user with the user's credentials, SSH keys and home
   directory. Coral's "read_only" capability is a Claude Code permission entry, not
   enforcement.
6. **Large browser attack surface.** About 318 `innerHTML` assignments across 53 JS files
   render agent output and board text; a dashboard script-injection fix shipped in v1.4.12.
   Individual sites were **not audited**.
7. **Hosted hub.** Release builds default to `app.coralai.ai`; nothing is sent until the user
   connects (`config.go:136-150`), but once connected the hub's "View" level reads every
   terminal, chat, board and file (v1.4.17 notes). That is a third party with read access to
   private repositories.
8. **Telemetry** on by default in release binaries (PostHog).
9. **Account terms.** Coral's own EULA says using consumer Claude OAuth tokens "in third-party
   products is strictly not permitted by Anthropic" and puts that risk on the user
   (`pkg/license/terms.go`, section 4). Whether driving the official `claude` CLI through
   coral counts is a policy question this audit **cannot verify**; it is flagged because coral's
   authors flag it.

## 6. Maintenance risk

- **Bus factor of one**, with most code written by that person's agent team. There is little
  public review: 23 PRs ever.
- **Churn.** ~100 commits and several releases a day. Any fork diverges within hours; any
  upstream PR competes with an agent team rewriting the same files.
- **Commercial direction.** The EULA, a paid licence removed only in v1.4.16, a "Growth and
  supporter sales plan" in `specs/`, and a hosted hub in closed beta. The open core could
  change terms again; the Apache-2.0 grant on code already published stays.
- **CI does not run the main test suite** (only the cloud subset and smoke tests), so green
  releases do not show the whole suite passed.

## 7. Options

| Option                                            | Cost                                                                                                                                                                                                                                                                                                                  | Benefit                                        | Verdict                                                              |
| ------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- | -------------------------------------------------------------------- |
| (a) Adopt as is                                   | Agents leave their containers and run on the host with `--dangerously-skip-permissions`-equivalent reach; a port listening on all interfaces; telemetry; EULA                                                                                                                                                         | Dashboard, board, task queue, cost view today  | **No**: breaks the core isolation model                              |
| (b) Adopt and send PRs upstream                   | Needs container-aware launch (a launcher abstraction instead of a host shell line), a non-loopback agent transport with per-agent tokens, per-project transcript roots, loopback-default bind, fail-fast error handling: an architecture change the maintainer has not asked for, at a churn rate of ~100 commits/day | If accepted, coral would work with ccy         | **No**: low odds, high cost                                          |
| (c) Fork and own                                  | 190k lines (plus 36k JS) we did not write, in a style opposite to our rules; we would carry the security fixes ourselves, which arrive several a week; the work in (b) still has to be done                                                                                                                           | Full control                                   | **No**: more code than all of `src-go/`, for features we mostly have |
| (d) Borrow ideas into ccy-teams and the agent bus | Small, plan-sized pieces of Go in `src-go/`                                                                                                                                                                                                                                                                           | The one real gap closed, in our security model | **Yes**                                                              |
| (e) Leave                                         | Nothing                                                                                                                                                                                                                                                                                                               | Nothing                                        | Yes, together with (d)                                               |

Ideas worth borrowing (each a candidate plan, none started):

1. **Outcome-conditioned task dependencies with a review hold**: a task names what blocks it
   and on which outcome (success, failure, termination), what artefacts it needs, and a
   finished task can wait in `review_pending` without blocking its worker. This would fit the
   bus's ping-plus-artefact protocol as a task record.
2. **Wait, don't poll**: we already designed `pingbus wait`; coral confirms the shape works.
3. **Per-agent and per-team cost rollup** from the transcripts ccy already keeps.
4. **A board-health monitor**: remind after 30 minutes idle, escalate to the orchestrator
   after 60 (`internal/config/config.go:124-125`).

## 8. What would change the recommendation

- Coral gains a pluggable launcher that can run each agent in its own container, with an
  authenticated agent-to-server transport that does not rely on loopback trust. Then (b)
  becomes worth a trial, read-only, on a loopback bind.
- The owner wants mixed-vendor teams (Codex, Gemini) soon. Coral is the most complete option
  seen for that, but it would still need the container work.
- The owner wants a browser terminal dashboard more than isolation, for example on a
  throwaway VM with no credentials. Then (a), on that VM only, bound to loopback and reached
  through an SSH tunnel, is a reasonable experiment.
- A second maintainer and CI that runs the full suite would reduce the maintenance risk, but
  not the design mismatch.

## Not verified

- Whether every setting has a file or environment equivalent that Ansible could own.
- How a request from a rootless Podman container to the host appears to coral (loopback or
  not) under each Podman network mode.
- The XSS safety of individual `innerHTML` sites.
- Anthropic's position on coral-style wrapping of the `claude` CLI.
