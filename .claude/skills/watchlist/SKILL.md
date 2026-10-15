---
name: watchlist
description: Scan the watch list of external public repositories for ideas, tooling and concepts worth adopting in fedora-desktop. "/watchlist" scans all, "/watchlist <owner>/<repo>" scans one, "/watchlist add <url>" adds a repository and runs its initial full review. Findings become GitHub issues or plans.
argument-hint: "[<owner>/<repo> | add <url>]"
disable-model-invocation: false
user-invocable: true
allowed-tools: Agent, Bash, Read, Edit
---

# Watch list

A shim. The list, the process and the outcomes are in [CLAUDE/Watchlist.md](../../../CLAUDE/Watchlist.md);
the work is done by the `watchlist-scout` agent.

## Dispatch

- No argument: dispatch `watchlist-scout` to scan every repository on the list.
- `<owner>/<repo>`: dispatch it for that repository only.
- `add <url>`: first `cd src-go && go run ./cmd/watchlist --root .. add <url>` (it refuses
  duplicates and non-https URLs; stop and report if it fails), add the repository's row to the
  table in `CLAUDE/Watchlist.md` with the reason it is watched, then dispatch `watchlist-scout`
  for it. It will be reported as `initial: full review`.

Give the scout a run id (`scan-<UTC date and time>`). When it returns, relay its summary: the
issues and plans filed and the run report path. The scout does not commit; the plans and the
updated `CLAUDE/Watchlist.tsv` it leaves are committed and pushed by this session.
