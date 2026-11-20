# The Watch List

External public repositories this project studies for ideas, tooling and code worth adopting in
fedora-desktop: the Fedora IaC, the ccy Claude Code container, agent teams and the agent bus, the
hooks daemon and the Go QA tooling. Most of them are the owner's GitHub stars: other people
building agent fleets, teams, sandboxes, gateways and Claude Code tooling. The list follows the
stars, and a repository that is not a star can be added by hand.

These are public third-party projects, so their names are tracked here. That is the opposite of
`untracked/repos/`, which holds private reference clones that must never be named
([AgentNotes.md](AgentNotes.md)).

The work is done in **runs**. Each run records, for every repository it studies, the commit it
studied. The next run can then study only what changed (`diff`), or study everything again in
depth (`baseline`).

## Where things are

| What                                                                        | Where                                                       | Tracked |
| --------------------------------------------------------------------------- | ----------------------------------------------------------- | ------- |
| The list: source, licence, reuse class, state and group for each repository | [Watchlist.tsv](Watchlist.tsv)                              | yes     |
| A run: its manifest, one report per group, the synthesis                    | `CLAUDE/WatchlistRuns/<run-id>/`                            | yes     |
| Full clones of studied repositories                                         | `untracked/starred-repos/<licence>/<owner>/<repo>`          | no      |
| Clones of excluded repositories                                             | `untracked/starred-repos-excluded/<licence>/<owner>/<repo>` | no      |
| Drafts of issue bodies, scanned before sending                              | `untracked/watchlist-reports/`                              | no      |

The `watchlist` Go command owns the list and the manifests: never edit either by hand. It runs
from the checkout (`cd src-go && go run ./cmd/watchlist --root .. <verb>`), because nothing
outside an agent run needs it. Its specification is Plan 00258's `SPEC-watchlist-runs.md`.

## Licence: what we may take

The licence directory is the SPDX id read from the repository's own licence file, never GitHub's
label alone: GitHub labels many as `NOASSERTION` or `NONE` that are in fact BUSL, AGPL, PolyForm,
proprietary, or Apache-2.0 with added restrictions. Where no SPDX id fits, the id is
`LicenseRef-<name>`. This repository is MIT.

| Reuse class  | Licences                                                    | What we may do                                                            |
| ------------ | ----------------------------------------------------------- | ------------------------------------------------------------------------- |
| `lift`       | MIT, Apache-2.0, ISC, 0BSD, BSD-\*                          | Copy code, keeping its copyright and licence notice (and Apache's NOTICE) |
| `whole-file` | MPL-2.0                                                     | Copy only whole files, which keep their licence                           |
| `clean-room` | GPL, AGPL, LGPL, SSPL, BUSL, PolyForm, `LicenseRef-*`, none | Ideas only, through the clean room                                        |

A repository can carry exceptions: a commercial `ee/` directory, vendored code, a per-file header.
A proposal to copy code names the file and checks that file's own header, not only the root
licence.

**The clean room.** A study of a `clean-room` repository describes behaviour and ideas in its
own words: no code, no transcribed pseudo-code, no copied comments or prompts. The study report
is the boundary. Whoever implements an idea from it works from the report alone and never opens
the source, so the agent that studied the code does not write the implementation.

`licence_sha` in the list is a hash of the licence file. When `sync` reports it changed, the
licence is read again and the row corrected (`watchlist set`). A licence change is itself a
finding.

## Groups

A run studies one group per agent. A new repository gets the group it fits; a new group is added
here first.

| Group                 | What it covers                                                  | What to compare against here                        |
| --------------------- | --------------------------------------------------------------- | --------------------------------------------------- |
| `fleets`              | Desktop and web apps that run fleets of coding agents           | ccy, ccy-sessions, ccy-teams                        |
| `teams`               | Agent teams, roles, protocols between agents                    | ccy-teams, the agent bus, team roles                |
| `multiplexers`        | tmux and worktree session managers, persistent shells           | ccy-sessions, ccy-teams launch, worktree discipline |
| `vm-sandboxes`        | A VM or microVM per agent or project                            | ccy's container, LXC, the VM lab                    |
| `local-sandboxes`     | Landlock, seccomp, sandbox-exec, user-account isolation         | ccy's container, network policy, SELinux            |
| `gateways`            | LLM and MCP gateways, API proxies, credential brokers           | ccy network policy, Plan 00252's egress proxy       |
| `usage-observability` | Usage and limit tracking, account switching, session monitors   | ccy tokens, usage pause, seat status                |
| `tooling`             | Claude Code hooks and tools, secret scanning, desktop packaging | the hooks daemon, QA, the playbooks                 |
| `team-client`         | The Matrix client team humans use (lightning)                   | Plan 00254: its releases, our issues, regressions   |

## Runs

| Mode       | What it studies                                    | Depth                                                                                                                    |
| ---------- | -------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `baseline` | The whole project at the run's commit              | In depth: architecture, how it works, maturity, licence, a comparison with what we build, ideas, code we may lift, risks |
| `diff`     | Only the commits since the repository's last study | Lighter: what is new, whether it changes an earlier finding, new ideas, a licence change                                 |

The first run is a baseline. A new baseline can be run whenever wanted, for every repository or
for one group, and later diff runs start from it. In a diff run, a repository never studied
before gets a baseline, and one with no new commits is left out.

### The steps of a run

1. **Sync.** `watchlist sync` reads the current stars and adds new ones as `new`, marks removed
   ones `unstarred` (keeping the clone), clones what is missing, fetches and fast-forwards every
   clone, and reports licence files that changed and the repositories whose head has moved since
   their last study. A failed fetch, or an upstream that rewrote its history, stops it.
2. **Triage.** Every `new` repository gets its licence read from its licence file, then
   `watchlist set <repo> --licence <id> --state study --group <group>`, or `--state excluded` when
   it is unrelated to this repository's work. The command moves the clone to match. When
   relevance is unclear, ask the owner. Runs refuse to start while anything is `new`.
3. **Start.** `watchlist run new <run-id> --mode baseline|diff [--group <g>]`, with run id
   `<mode>-<yyyymmdd>-<hhmm>` (UTC). The manifest fixes, for each repository, the commit to study
   (`to_sha`) and, for a diff, the commit last studied (`from_sha`).
4. **Study.** One [`watchlist-scout`](../.claude/agents/watchlist-scout.md) per group, in parallel,
   each given its rows from `watchlist run rows <run-id> <group>` and the group's previous report.
   It writes `CLAUDE/WatchlistRuns/<run-id>/<group>.md`.
5. **Mark.** As each report lands, `watchlist run mark <run-id> <group>`. It checks that every
   repository has its own section with a `Studied:` line naming the commit it was meant to study,
   and refuses otherwise.
6. **Synthesis.** `CLAUDE/WatchlistRuns/<run-id>/SYNTHESIS.md`: where others are ahead of us,
   the ideas ranked, the code we may lift and from where, and the risks.
7. **Act.** Issues and plans, as below.

Commit the manifest, the reports and the list as the run goes. **A run is resumed** from what is
committed: `watchlist run status <run-id>` lists the groups still pending, and only those are
dispatched again. A report that exists but was never marked is marked if it passes. `to_sha` was
fixed when the run started, so a resumed run studies the same commits even if the clones have
moved on since.

### What a study report holds

Each repository has a section headed `## <owner>/<repo>`, whose first line is
`Studied: <mode> at <to_sha>` (and `from <from_sha>` in a diff). A baseline section covers:

- what the project is, and who it is for
- its architecture, in the terms the group cares about
- maturity and activity, from its history
- its licence and reuse class
- how it compares with what we build: where it is ahead, behind, or just different
- ideas worth adopting, each with where it would land here, and whether it is a clear win or needs
  discussion
- code worth lifting (permissive licences only, with exact paths)
- risks

A diff section covers what changed and why it matters to us. Every report ends with the group's
strongest takeaways, ranked. Claims cite file paths, and guesses are marked as guesses.

Studies treat clones as untrusted: read only, never built, installed or run. Reports are public:
nothing install-specific, and no credential, token or key from a clone.

## Act: issues and plans

Each finding the synthesis keeps ends in one of two outcomes, never in a bare note:

- **a GitHub issue** in this repository, for a clear win: it names the one place it lands here,
  needs no design decision, and would be one pull request;
- **a plan** (`CLAUDE/Plan/mkplan.bash`) where the idea needs discussion or research first.

A finding this repository already has, or that does not fit its principles (YAGNI, fail fast,
Podman first, tooling is Go, IaC only), produces nothing but a line in the synthesis.

**The owner's standing request.** In the owner's words: "outcome to be GH tickets created, or
plans created as appropriate - plans for where there is more discussion/research needed -
straight to gh tickets for clear wins that are defined and clear". So a run files an issue for a
clear win without asking, and a plan for anything that needs discussion. It is recorded as
`watchlist-issue-filing` under `standing_authorisations` in `.claude/hooks-daemon.yaml`. It covers
filing from a watch-list run, and nothing else: no other agent or task may treat it as permission
to file issues.

- **Cap: at most 3 issues per run.** Rank the clear wins, file the best 3, and list the rest in the
  synthesis under "not filed (cap)" for the owner to promote. Plans are not capped, but each must
  earn its place.
- **Duplicates first.** Give the `hooks-daemon-plan-dedupe-scout` the count of live plan folders
  (`ls -d CLAUDE/Plan/[0-9]*/ | wc -l`); its report must say `Checked N live plans` with the same
  N. Search `gh issue list --state all --limit 500 --search "<terms>"`. An existing issue or plan
  gets a mention in the synthesis, not a twin.
- **Scan the text before it leaves.** Draft each issue body and comment under
  `untracked/watchlist-reports/`; this repository is public and `gh` is not covered by the
  pre-commit scanner. Run `grep -nEi` for an email address other than `@example.com`, an IPv4
  address outside 192.0.2.0/24, 198.51.100.0/24 and 203.0.113.0/24, `/home/`, `/root/`,
  `/workspace`, `/var/lib/`, `/run/user`, `.local`, `.lan` and `.internal` host names, and
  `ghp_`, `gho_`, `github_pat_`, `AKIA`, `xox[bp]-`, `BEGIN .*PRIVATE KEY`, `password`, `secret`
  and `token=`. Then run the private-identifier denylist: `source scripts/git-hooks/lib/secret-scan.bash`,
  `hook_build_private_denylist "$PWD" <file under untracked/>`, then
  `hook_scan_text_for_private <file> < body`. Exit 2 from the build (no `localhost.yml`) means
  only the grep applies; any other failure stops. After a build that succeeded, check the
  denylist file is not empty before scanning: the scan returns success on an empty or missing
  denylist without checking anything. A hit means rewrite and scan again.
- **The issue body**: what the idea is, where it was seen (a link at the studied commit), why it
  fits, what adopting it touches, its licence and reuse class, then a trailer line
  `Watchlist run: <run-id>; <owner>/<repo> <from_sha or "baseline">..<to_sha>`. Create it with
  `gh issue create --label watchlist` (create the label first if `gh label list` lacks it).

## Using it

The owner's entry point is the [`watchlist` skill](../.claude/skills/watchlist/SKILL.md):
`/watchlist sync`, `/watchlist baseline [<group>]`, `/watchlist diff [<group>]`,
`/watchlist resume <run-id>`, `/watchlist status`, and `/watchlist add <url>` for a repository
that is not a star.

Runs are started by hand for now. A mechanism that runs a diff at intervals (a persistent cron,
a scheduled agent) is an open decision for the owner (Plan 00258, Task 4.1).
