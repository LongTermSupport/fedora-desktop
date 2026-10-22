# Review round 1: fix/lxc-add-already-declared (Plan 00201 Task 2.20)

Scope: `git diff F44...HEAD` (6 commits, 64d5bc3b6..9778b10d3). Read: Task 2.20, CLAUDE.md,
the declare.py, Go declare client, lxcadd, create wizard and rule diffs, the meta-deploy and
changelog entries, the version bump.

Run: `go test ./internal/teams/... ./internal/declare/...` ok; `go test ./rules/` (src-go/qa) ok;
`python3 -m unittest tests.helpers.agent_bus.test_declare` OK. One probe (below, finding 1)
under `untracked/scratch/review-r1/`.

## Verdict: FIX-FIRST

The lxc-add fix itself is right: the original fault (the member's own earlier entry counted as
a duplicate of its answer) is gone in `_among`/`derive_lxc`; an identical member writes nothing
and applies after one yes; a differing one names keys and updates only on choice; the
self-coordinator case is handled (`holding.self`, "Keep it the coordinator"); takeover plus
update is consistent (dry-run with role cleared, then demote, then `--update`). Nothing new
touches offline/stopped handling: `LxcCompare` reads the file only, and `sayStopped` and the
running-only container list are unchanged. Version bump (CCY 3.99.14.13, container 2.78.16,
Dockerfile label and REQUIRED_CONTAINER_VERSION in step) and the meta-deploy wording match what
the play does. No install-specific names in the added lines (placeholders only). The lxcadd
tests run the real declare.py through the fake runner, so they exercise the production path.

One misclassification in declare.py blocks the merge; the rest are smaller.

## Findings

### 1. (FIX) `set_values` gives 78 for a value the owner can retype

`helpers/agent_bus/declare.py:1142`. `problem == refusal(before)` is taken to mean "refused for
a key the change leaves alone", as the comment says, but the code never checks that. When the
key that is wrong in the file is the very key being set, and the new value is wrong in the same
way, the teamfile message is identical (`description: must be 1 to 120 characters ...`,
`port: must be an integer from ...`: the messages carry no value), so a retypeable value is
reported as "refused as it stands ... fix it in the file first" (78), and the settings screen
stops where it should ask again.

Probe (the branch's own `SetTest` fixture, description `"a\tb"` in the file, then
`set --value description '"c\td"'`): exit **78**, "team alpha's entry in agent_bus_teams is
refused as it stands (team file: description: ...); fix it in the file first". The branch's own
test shows that a good description passes (exit 0) on the same file, so this answer was
retypeable. That is the "genuinely retypeable value now 78" case.

Fix: give 78 only when the refused key is one the change does not touch. Take the key from the
message (`team file: <key>: ...`, as `_fail` writes it) and check it is not in `changes`.
Otherwise judge the changed values over a copy of `before` with the failing key put right.
Add a test: retyping a bad value for the key already at fault is 64.

### 2. (should fix) A declared member that differs only on keys lxc-add never asks can be joined only by deleting them

`src-go/internal/teams/lxcadd/adder.go:296-323` (`sayDeclared`), with `_replace_lxc`
(`declare.py:1408`). lxc-add never asks host, seat, service or human_text, and `entry()` never
gives them. An entry that carries any of them (a hand-set host or seat override, `service: true`)
is therefore always "declared otherwise". The only choices are "Update entry N", which removes
those keys, or "Stop; change nothing". Removing a seat or host override can move the member's
handle, and removing `service` changes how it runs. The Stop branch prints only "Nothing was
changed." and does not say that `ccy-teams apply <team>` joins the member as declared. That
path is the one Task 2.20 promises ("applies after one yes"). The keys are listed before the
choice, so nothing happens silently, but in this case the owner's only way to join is
destructive.

Fix (either): offer a third choice, "Apply entry N as declared; write nothing", which uses the
writeNothing path. Or at least have the Stop line name `ccy-teams apply <team>`, as
`confirmApply`'s No line already does.

### 3. (minor) A declare.py crash is told as "a refusal of the file"

`src-go/internal/teams/lxcadd/adder.go:648` and `src-go/internal/teams/create/wizard.go:750`
(`unanswerable`). Any `*declare.Error` that is not ErrUsage or ErrInterrupted gets "That is a
refusal of \<host_vars>, not of the answer ... fix the entry it names". That includes ErrFailed,
such as a Python traceback (exit 1) or an argparse error (exit 2). The run stops, so fail-fast
holds, but the owner is pointed at the file for a tool fault: an error read as a different
verdict.

Fix: keep that wording for `errors.Is(err, declare.ErrRefused)`. For anything else, say
"declare.py failed (exit N)" with its stderr, and stop with the same code.

### 4. (minor, docs drift) Two behaviour changes are not recorded where the owner reads them

- `add-repo` of a repository the team already trusts was a 64 usage error and is now
  `UNCHANGED` with exit 0 (`declare.py:951`, `:1658`). The chooser's widen path
  (`chooser/widen.go:116`) now goes on to apply instead of failing. That is probably the better
  behaviour, but neither Task 2.20, the 3.99.14.13 changelog entry (`docs/ccy-changelog.md:20`)
  nor the meta-deploy entry mentions it, and declare.py ships in the image.
- `set`, `describe` and `element` refusing an entry that is wrong as it stands is now 78, not 64
  (finding 1's code). The settings screen therefore stops instead of re-asking, and this is not
  in the task or the changelog either. Only the independent-search report mentions these changes.

Fix: one clause each in Task 2.20 and in the changelog entry.

### 5. (nit) `_replace_lxc` drops a trailing comment on a line it rewrites

`declare.py:1428` replaces or removes the whole line. A `checkout: /x  # main` comment is lost,
and the read-back check cannot see that. `set_values` refuses an edit that would lose a comment
(DeclareError). Apply the same guard here.

### 6. (nit) The rule `project_refusal_kind` is satisfied by any kind mention in the function

`src-go/qa/rules/refusalkind.go`, `checkRefusalKind`. Any `Err*` identifier of internal/declare,
or any `.Code` or `.Is` on a `declare.Error`, anywhere in the function body clears every recovery
in it. A function that forwards `de.Code` as its exit code and still re-asks on every kind passes.
This is acceptable as a first defence (red 4, green 0), but the Doc could say that the rule checks
for a kind being read, not for the branch on it.

### 7. (nit) The write-nothing path shows no summary

`confirmApply` (`adder.go:~705`) skips the box with the expected handle that the add and update
paths show. Showing it would let the owner see which handle the apply should give before the
yes.

## Checked and found sound

- 64/78 split in `_judge_lxc`: a value refused alone gives 64; the file's entries refused as they
  stand give 78, before the answer is considered; the answer refused only among the entries gives 78,
  naming its position. `derive_lxc` makes the same split and drops the member's own entry from
  `kept`.
- `lxc-check` with team, container and user judges the answer in place of the member's own entry,
  so the original "entries 5 and 6" clash cannot arise.
- `add_lxc --dry-run`: no difference gives UNCHANGED (exit 0); a difference without `--update`
  gives 78; with `--update` the entry's lines are printed. `summary` passes `--update` exactly
  when `writes()` is update.
- gh lookups: `ghNotFound` needs both `*execx.ExitError` and "(HTTP 404)" in gh's captured
  stderr. 401, 403, 5xx and no network all end the run with gh's words. A missing gh is not an
  ExitError, so it is an error too. The owner lookup no longer runs under `gum spin`, which would
  have hidden stderr.
- m7_check D1 reads the column starts from the heading and fails naming the heading when it is
  absent.
