# Adversarial review: branch `watchlist/build` (Plan 00200)

Reviewed `git diff efbbe7cb origin/watchlist/build` (one commit, `ee9fbaa1`). Read-only. Line
numbers are those of the branch's files.

Checks run in a temporary worktree of the branch: `go vet`, `go test -count=1` on
`internal/watchlist`, `internal/gitx`, `cmd/watchlist` (all pass), `golangci-lint run` on the
same (0 issues). Two live checks against real git: a force-push experiment in a scratch
repository (finding 1), and a fetch of `cdknorow/coral` to verify the seeded sha (finding 11).

Severity: **BLOCK** must be fixed before merge; **FIX** should be fixed in this plan; **MINOR**
at the owner's discretion; **OK** is a hunted-for failure that is not present.

## 1. BLOCK: a force-pushed upstream is not detected; sync reports a wrong range as if correct

`src-go/internal/watchlist/app.go:207-214` (`printRange`) and `src-go/internal/gitx/gitx.go:143-153`
(`HasCommit`). W19 is implemented as "the mirror holds the object": `git cat-file -e <last>^{commit}`.
A rewrite of the branch upstream does **not** remove the old commits from the mirror: `git fetch`
keeps them as unreachable objects until a gc prunes them (default two weeks or more).

Proved with real git: upstream with commits `one`, `two`; mirror cloned at `two`; upstream rewound to
`one` and given `two-rewritten`; mirror fetched with exactly the command W14 specifies.

```
HasCommit(old) = TRUE after force-push
is-ancestor(old, head) = FALSE: the rewrite is detectable
log old..head:
4e333d5 two-rewritten
```

Failure scenario: the watched repository rewrites its default branch (squash of a release branch,
a rebased `main`, a reset after a leaked secret). `sync` prints `last-checked`, `head`, and
`commits (N)` listing only the replacement commits, with no error. The reviewed commits that were
removed are never mentioned, `git diff last head` silently mixes their reversal into the "changes
since the last check", and the scout reviews a fiction. Weeks later, after gc, the same situation
flips to the W19 error, so the behaviour is also non-deterministic. W19 in `SPEC-watchlist.md:60-61`
and its test `TestSyncWithALostLastShaIsAnErrorNotAFullReview` (`app_test.go:268-283`) encode the
wrong oracle, so the test is green on a defect.

Fix: add `gitx.IsAncestor(ctx, dir, ancestor, descendant)` over `git merge-base --is-ancestor`
(exit 1 is `false`, like `HasCommit`). In `printRange`, require `IsAncestor(last, head)`; the error
text stays as it is. Reword W19 to "a `last_sha` that is not an ancestor of the head (the remote
rewrote its history)". Red test: fake answers `cat-file -e` with success and `merge-base --is-ancestor` with exit 1; assert exit 1 and no `commits (` on stdout.

## 2. FIX: `mark` records any sha the mirror holds, not one on the branch nor one at or after the last review

`app.go:243-284` (`mark`). The only commit check is `HasCommit`. Consequences:

- After the force-push in finding 1, the old sha can be re-marked, or any pruned-later object marked.
- A sha from the branch's past (an ancestor of `last_sha`) is accepted: the next sync re-reports
  already reviewed commits, and the TSV says the review went backwards.
- Nothing ties the marked sha to the head `sync` reported, so a typo that happens to name another
  commit in the mirror passes.

Fix: in `mark`, require `IsAncestor(sha, refs/remotes/origin/<branch>)` (sha equal to the head
passes) and, when `last_sha` is set, `IsAncestor(last_sha, sha)`. Add W23 wording and two red
tests (unreachable sha; sha older than `last_sha`). This closes the "false reviewed state" the
owner asked about.

## 3. FIX: the scout files public, permanent issues with a thin definition of "clear win", no cap, no durable authorisation, and no scanner pass

`.claude/agents/watchlist-scout.md:30-43`.

- **Bound.** "A clear, defined win ... concrete and fits" is the whole criterion. An opus agent
  reviewing 192 commits (finding 11) with instructions to file a ticket per win can file a dozen
  issues in one run. Define the win: it names the one place it lands here (a play, a file, a doc),
  needs no design decision, and would be one PR with no plan; anything with an alternative to
  weigh is a plan. Cap issues per run (the rest go in the run report for the owner to promote).
- **Authorisation.** `.claude/hooks-daemon.yaml:236-242` records only `subagent-delegation`. The
  owner's "straight to gh tickets" was said once in a conversation; nothing durable records it.
  Record it in `CLAUDE/Watchlist.md` ("the owner has asked that clear wins are filed as issues
  without asking") so a later session does not stall or, worse, treat it as implied for other agents.
- **Traceability.** The issue body has no run id and no reviewed range; add both, and a `watchlist`
  label so the issues are findable and the dedupe search (`gh issue list --search`) can filter on it.
- **Leak surface.** The public-repo rule at lines 39-42 is right but relies on the agent's judgement
  alone. The pre-commit scanner does not see `gh` CLI output (noted in project memory). Require the
  body to be written to a file under `untracked/watchlist-reports/` and passed through
  `scripts/git-hooks/lib/secret-scan.bash` (the function `scripts/test-secret-scan.bash` exercises)
  before `gh issue create --body-file`. Say explicitly that a **comment** on an existing issue is
  issue text under the same rule (line 37 proposes comments; line 39 only says "issue and plan text").
- **Dedupe.** `gh issue list --state all --search "<terms>"` has a default `--limit 30`; add
  `--limit 200`. The dedupe scout is told "how many plan folders `CLAUDE/Plan/` holds" but the
  daemon agent reconciles against *live* plans (`hooks-daemon-plan-dedupe-scout.md:41-52`); say
  whether `Completed/` is counted.

## 4. FIX: `CLAUDE/Watchlist.tsv` is routed to no QA by qa-changed

`scripts/qa-routing.tsv` has no rule matching `CLAUDE/Watchlist.tsv` (the only `.tsv` rows are
`scripts/qa-*.tsv`, line 40). W25 (`TestTheShippedListLoads`) runs only when a `src-go/` file
changes. Scenario: the skill's `add` path, or a hand edit, changes the TSV alone; `qa-changed.bash`
selects nothing; a malformed row reaches `main` and the next `/watchlist` dies at `load()`. Fix:
add `also	CLAUDE/Watchlist.tsv	go	covered` next to line 40, and an expectation in
`scripts/test-qa-changed.bash` that a plan for `CLAUDE/Watchlist.tsv` contains `check go`.

## 5. FIX: the scout and the skill lack the tools their procedures need

- `.claude/agents/watchlist-scout.md:5` gives `Bash, Read, Glob, Grep, Write, Agent`. Step 4 fills
  in a scaffolded `PLAN.md` and adds "its README row" to `CLAUDE/Plan/README.md`: that is an
  `Edit`. Without it the agent either rewrites the whole README with `Write` (R-WRITE-CLOBBER, and
  a 300-line file retyped) or edits through Bash heredocs, which the daemon warns route around the
  content guards. Add `Edit`.
- `.claude/skills/watchlist/SKILL.md:7` `allowed-tools: Agent, Bash, Read`, yet the `add` path
  (lines 20-23) edits the table in `CLAUDE/Watchlist.md` and commits. Add `Edit`.
- The scout prompt never says where the mirror is for a non-initial repository (sync prints
  `mirror:` only for `initial`, W16); it relies on the reader knowing the `<owner>__<repo>`
  convention from step 2. Print `mirror:` in every report (W17, W18) or spell the path in the prompt.

## 6. FIX: spec expectations with no test, and one test on the wrong oracle

- W5: port (`https://github.com:443/o/r`) and fragment (`.../r#x`) are untested; the regexp rejects
  both today, but nothing holds it there.
- W13: the URL form is tested only in exact case (`urlBeta`); the case-insensitive URL match
  (`strings.EqualFold` at `app.go:120`) has no test.
- W14 last sentence ("clone progress and git's own chatter are not passed to stdout"): untested;
  it holds only because `gitx.run` captures output, which nothing asserts.
- W19: tested with the wrong oracle (finding 1).
- W23 as reworded by finding 2: two tests missing.

PLAN.md task 1.3 claims "one test group per expectation"; after the above it will be true.

## 7. MINOR: the host is accepted by the pattern but ignored everywhere else

`list.go:30` allows any host; `Entry.Name()` and `MirrorDir()` (`list.go:46-55`) drop it, `Parse`
dedupes on `owner/repo` alone (`list.go:87`), and `find` trims only `https://github.com/`
(`app.go:117`). `https://github.com/o/r` and `https://gitlab.com/o/r` are therefore the same entry
and the same mirror directory, and `sync gitlab.com/o/r` cannot be named except by full URL.
Either pin the host to `github.com` (the scout uses `gh`, so YAGNI says this) or key by host too.

## 8. MINOR: two dead ends give only git's raw message

- Default branch renamed upstream (`master` to `main`): `FetchBranch` fails for ever with
  `fatal: couldn't find remote ref`; nothing says "edit the branch column".
- A mirror directory left by an interrupted clone (kill during `git clone`): `refresh`
  (`app.go:191-203`) sees it exists, fetches, and fails for ever with "not a git repository";
  nothing says "delete the mirror and sync again". A cheap guard: when the directory exists but
  `<dir>/.git` does not, say so.

## 9. MINOR: `Format` discards hand-written comments in the TSV

`list.go:126-137` writes the canonical header and rows only. The header itself says the command
edits the file, so this is consistent; it is worth one sentence in `CLAUDE/Watchlist.md`, "comments
you add to the TSV are lost on the next mark".

## 10. MINOR: `scripts/go-binary-suites.tsv` contradicts itself

Line 1 still says "The Go commands a play builds and deploys", while the amended comment (lines
9-10) says every command under `src-go/cmd` has a row including the undeployed `watchlist`.
Reword line 1 to "The Go commands under src-go/cmd and what drives each one". The `watchlist	qa-go`
row itself is right: `test-qa-changed.bash:276-287` requires it and accepts `qa-go`.

## 11. PLAN.md claims, fact-checked

| Claim                                                                    | Verdict                                                                                                                                                                                                                                                    |
| ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1.1 SPEC W1-W25                                                          | True; W19 is specified with the wrong criterion (finding 1).                                                                                                                                                                                               |
| 1.2 gitx mirror methods, tests first                                     | Methods present and tested; journal records the red. Cannot verify ordering, no reason to doubt it.                                                                                                                                                        |
| 1.3 "one test group per expectation"                                     | Not quite (finding 6).                                                                                                                                                                                                                                     |
| 2.1 TSV seeded at the commit `AUDIT-coral.md` read; indexed in CLAUDE.md | True. `AUDIT-coral.md:3` says `e785da3`; the seeded full sha is an ancestor of coral's `main` today, with 192 commits after it. CLAUDE.md index row is present and accurate. Note for task 2.4: the first live scan is a 192-commit diff, not a short one. |
| Success: `go test -race` and `golangci-lint` pass                        | `go test` and `golangci-lint` pass on the branch (run here); `-race` not re-run.                                                                                                                                                                           |
| Success: W25, the shipped list loads                                     | True.                                                                                                                                                                                                                                                      |
| Success: `add` against a real remote read the default branch             | Journal claim; plausible (`git ls-remote --symref` against coral gives `ref: refs/heads/main` here). Not reproduced.                                                                                                                                       |
| Non-goal: not installed on the host                                      | Consistent with `go-binary-suites.tsv` and `Watchlist.md`; see finding 10 for the header wording.                                                                                                                                                          |

## 12. OK: hunted for and not present

- **Sync after a failed fetch**: `FetchBranch` error aborts the repository and the run (W21,
  `app.go:166-186`); `unchanged` can only print after a successful fetch and rev-parse. Tested at
  `app_test.go:286-330`.
- **Atomic TSV write**: `fsx.WriteAtomic` writes `<path>.tmp.<pid>` beside the file and renames
  (`fsx.go:15-24`); `sync` never writes (tested).
- **Strict load**: missing file is an error, four fields exact, sha/run coupled, duplicates refused,
  line numbered in errors; CRLF files are refused because `\r` fails `\S+`.
- **URL / path traversal**: components are `[A-Za-z0-9._-]+`; `..` as a component becomes the single
  directory name `..__..`, never a parent reference; `.git`, slashes, `@`, `:` and `?` are refused.
  `MirrorDir` is `filepath.Join(root, "untracked", "watchlist", owner+"__"+repo)` and cannot leave
  `untracked/watchlist/`.
- **Exit codes and stderr**: usage is exit 64 on stderr only; failures are exit 1 with `ERROR:`;
  stdout is payload only. Tested.
- **Docs drift**: `CLAUDE/Watchlist.md`, the CLAUDE.md index row, `CLAUDE/Plan/README.md` row and
  the skill/agent descriptions agree with each other and with the code, bar findings 5 and 10.
- **Install-specific names** in the diff: none found.

## Summary for the coordinator

BLOCK: 1 (force-push undetected; W19 oracle wrong in spec, code and test).
FIX: 2 (`mark` accepts off-branch or backwards shas), 3 (scout's issue filing: bound, cap,
authorisation record, scanner pass, label, run id), 4 (qa-changed routes `Watchlist.tsv` to
nothing), 5 (scout and skill lack `Edit`; mirror path only printed for initial), 6 (untested W5
port/fragment, W13 URL case, W14 chatter; W23 additions).
MINOR: 7 to 10.
