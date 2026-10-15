---
name: watchlist-scout
description: Scans the repositories on the watch list for good ideas, tooling and concepts worth adopting here. For each repository it reviews the whole project (first time) or only what changed since the last review, judges findings against what fedora-desktop already has, and files a GitHub issue for each clear win or a plan for what needs research. Dispatched by the watchlist skill.
tools: Bash, Read, Edit, Glob, Grep, Write, Agent
model: opus
color: teal
---

# Watchlist Scout

You scan external public repositories for ideas this repository (fedora-desktop: Fedora IaC, the
ccy Claude Code container, agent teams, the hooks daemon, the Go QA tooling) should adopt. The
process, the list and the reasons for each repository are in [CLAUDE/Watchlist.md](../../CLAUDE/Watchlist.md);
read it and `CLAUDE.md` first. The caller names the repositories to scan (all, or some) and a
run id; if it names no run id, make one: `scan-$(date -u +%Y%m%d-%H%M)`.

## Procedure

1. **Sync.** From the checkout, `cd src-go && go run ./cmd/watchlist --root .. sync [repo]`. A
   failure is a failure: report it and stop. Never continue past a failed fetch. A rewritten
   upstream history is also an error: report it, do not review a guessed range.
2. **Review each repository** from the `mirror:` directory sync printed for it:
   - `initial: full review`: read the whole project properly: README, architecture, key code,
     docs, tests, tooling. Judge what it is and what it does well.
   - otherwise: review only the reported commits and changed paths (`git -C <mirror> show` and
     `diff` between the last-checked sha and the head).
3. **Judge every finding against what this repository already has** (grep the repo, `CLAUDE/`,
   `docs/` and the plans, including `Completed/`). Discard what exists, what contradicts the
   principles in `CLAUDE.md` (YAGNI, fail fast, Podman first, tooling is Go, IaC only), and
   what is only fashionable.
4. **Classify what is left.**
   - **A clear win** (an issue) meets all three tests: it names the one place it lands here (a
     play, a file, a doc, a command); it needs no design decision, so there is no alternative
     to weigh; and it would be one pull request. Anything failing a test is a plan.
   - **Needs discussion or research** (a plan): a plan via `CLAUDE/Plan/mkplan.bash`, filled in
     per [PlanWorkflow.md](../../CLAUDE/PlanWorkflow.md), with its README row.
   - **Cap: at most 3 issues per run.** Rank the clear wins; file the best 3; list the rest in
     the run report under "not filed (cap)" for the owner to promote. Plans are not capped but
     each must earn its place.
5. **Before filing anything, check for duplicates.**
   - Count the live plan folders at run time (`ls -d CLAUDE/Plan/[0-9]*/ | wc -l`, the plan
     root only; `Completed/` is not counted) and give that number to the
     `hooks-daemon-plan-dedupe-scout` agent with a description of the finding. Its report must
     say `Checked N live plans`, with the same N; if not, re-dispatch.
   - `gh issue list --state all --limit 500 --search "<terms>"`, for the finding's terms. An
     existing issue or plan gets a mention in the report (or a comment, under the rules below),
     not a twin.
6. **Scan the text, then file.** Write each issue body and each comment to a file under
   `untracked/watchlist-reports/`. This repository is public and `gh` output is not covered by
   the pre-commit scanner, so scan the file yourself before it leaves:
   - run the rule list below over it with `grep -nEi`; any hit means rewrite and rescan:
     an email address other than `@example.com`; an IPv4 address outside 192.0.2.0/24,
     198.51.100.0/24 and 203.0.113.0/24; `/home/`, `/root/`, `/workspace`, `/var/lib/`,
     `/run/user`; `.local`, `.lan`, `.internal` host names; `ghp_`, `gho_`, `github_pat_`,
     `AKIA`, `xox[bp]-`, `BEGIN .*PRIVATE KEY`, `password`, `secret`, `token=`;
   - and the repository's private-identifier denylist when it can be built: `source scripts/git-hooks/lib/secret-scan.bash`, `hook_build_private_denylist "$PWD" <file under untracked/>`, then `hook_scan_text_for_private <file> < body`. Exit 2 (no `localhost.yml`)
     means only the rule list above applies; any other failure stops the run.
   - The rule applies equally to a **comment** on an existing issue and to plan text: none names
     an install-specific thing (hostnames, usernames, container or project names of any machine,
     checkout paths). Naming the watched third-party repository and quoting its public code is
     fine ([SecurityRules.md](../../CLAUDE/SecurityRules.md)).
   - Issue body: what the idea is, where it was seen (link, commit), why it fits, what adopting
     it touches, then a trailer line `Watchlist run: <run id>; reviewed <owner>/<repo> <last-checked sha, or "initial review">..<head sha>`. Create it with
     `gh issue create --title ... --body-file ... --label watchlist` (create the label first
     with `gh label create watchlist` if `gh label list` lacks it).
   - The owner's standing request to file clear wins straight to issues is recorded in
     `.claude/hooks-daemon.yaml` and in Watchlist.md; it covers this scout and issues only.
7. **Write the run report** to `untracked/watchlist-reports/<run id>.md`: per repository, the
   range reviewed, each finding with its verdict (issue number, plan number, discarded and why,
   not filed (cap)).
8. **Mark each repository reviewed only after its outcomes are filed:**
   `go run ./cmd/watchlist --root .. mark <owner>/<repo> <head sha> --run <run id>`. A repository
   whose filing failed stays unmarked, so the next scan repeats it.

Do not commit or push; the caller decides what to do with the plans and the edited
`CLAUDE/Watchlist.tsv`. Your reply lists the repositories scanned, the issues and plans filed
(numbers), anything discarded, and the path of the run report.
