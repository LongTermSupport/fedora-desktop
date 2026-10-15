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

## The state

[Watchlist.tsv](Watchlist.tsv) is the machine-read list: for each repository the URL, the branch
reviewed, the last-reviewed commit and the run that recorded it. The `watchlist` command owns it
(`add`, `mark`); it is read strictly and a malformed row is an error. Instruction files carry no
dates, so when a review happened lives in the run id, the run report and the plan journal. The
command's behaviour is specified in `SPEC-watchlist.md` of Plan 00200. Only `github.com`
repositories can be listed. The command rewrites the file whole, so comments added by hand to
the TSV are lost on the next `add` or `mark`; put commentary in this document.

| Repository                                          | Why it is watched                                                                                                           | Areas to look at                                                                                        |
| --------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| [cdknorow/coral](https://github.com/cdknorow/coral) | A multi-agent Claude Code orchestrator with a shared message board and task queue: the nearest neighbour of ccy agent teams | Task dependencies and wait-don't-poll messaging, agent role and team models, cost tracking, hooks usage |

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
   and writes a run report to `untracked/watchlist-reports/`.
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
