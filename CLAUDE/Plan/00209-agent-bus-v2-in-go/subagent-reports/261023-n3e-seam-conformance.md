# n3e-seam: Defence Before Fix conformance review

Branch `feat/bus-v2-n3e-seam`, defence commit `822931505`, fix `e248c1fe6`, catalogue `78532aaaa`,
final `de71902e4`. Reviewed against SPEC-n3.md 6.1 (AS1a, AS6) and the review brief's checklist.

Limit of this review: the method specification (sections 3, 4, 7, 8) was not given to me and I did
not find it in the tree. Findings cite the checklist item and SPEC-n3 clauses, not method-spec
clause numbers. Re-cite them if numbers are wanted.

## Reproduction (done by me, not taken from the report)

Driver: `go -C src-go/qa build -o <out> ./driver` (the `project-rules-tagged` stage's command in
`qa/go-qa.toml`), run as `go vet -vettool=<driver> -tags <tag> ./internal/teamchat/...` over a
`git archive` of `src-go` at each commit, in `untracked/scratch/rev/`. `qa/` is unchanged between
`822931505` and HEAD, so one driver serves both trees.

| Run                                                        | Result                                               |
| ---------------------------------------------------------- | ---------------------------------------------------- |
| analysistest `go test ./rules -run TestAgentBranch` (HEAD) | PASS                                                 |
| tree `8d2496eba`, untagged / busfailpoints / tuwunel       | exit 1 each, 12 `project_agent_branch` findings each |
| tree `de71902e4`, same three tag sets                      | exit 0, no output                                    |
| tree `de71902e4`, untagged `./...` (all src-go)            | exit 0, no output                                    |

The 12 red findings are `keeper/prepare.go:439,440,441,458`, `keeper/loop.go:288,289,290,482,539`,
`keeper/seats.go:122`, `link/link.go:268,423`: every AS1a site (link 272 and 298 are uses of the
`plain` local derived from the one comparison at 268, so one finding covers them). Red and green
match the build report and the catalogue. Not run: the whole go-qa stage list (lint, tests,
coverage); I ran the rule through the stage's driver command only. Coverage figures and the
goldens' pass are the report's claim, unverified by me.

## Findings

### Major

M1. The independent search and the reconciliation are absent. Checklist: independent search before
the rule, two techniques, what each found that the other could not check, reconciled in the rule's
favour. The build report records none; the rule was written from AS6's list and the only census is
the rule's own 12 findings. The sweep scope is not stated for the red count (the report says "all
of src-go" only for green). Resolve: record the search with its two techniques and what each could
not see, reconciled with the 12 findings, and state the sweep scope (`./internal/teamchat/...` per
tag set). My own cheap check found no launch literal and no use of the named values outside
`internal/teamchat`, and none unflagged inside it, so the count is probably right; the record is
what is missing.

M2. The rule misses obvious spellings that the catalogue's gap list does not name. Evidence: a probe
file in a scratch copy of the final tree (removed afterwards). Not flagged:
`strings.EqualFold(w.Launch, "plain")`, `strings.HasPrefix(s.Launch, "pla")`,
`strings.Contains("plain,x", w.Launch)`, `map[string]bool{"plain": true}[w.Launch]`,
`switch strings.ToLower(w.Launch) { case "plain": }`. Flagged: `slices.Contains([]string{"plain"}, w.Launch)`,
`w.Launch == "pl"+"ain"`, `(*w).Launch == "plain"`, `w[0].Launch == "plain"`. The declared gaps
(function result, copy in a variable, list in another variable) cover `x := w.Launch; x == "plain"`
and `isPlain(w.Launch)`, not a call or index that takes the field and a bare constant. AS6's fourth
bullet judges a composite literal only, so the rule matches the spec but is narrower than the
hazard. Resolve: extend the call bullet to a call given the field and any launch-value constant,
an index of a map holding launch-value keys and a switch tag that is a call over the field; or add
these to the catalogue's and the doc string's "Not caught" list. AS6 is the spec's, so a spec change
is the owner's to refer.

### Minor

m1. The next wider rule not built is not named, with its reason (checklist: rule width). Candidates:
any comparison of a string field with a literal in teamchat; a rule that follows a copy of the
field (dataflow).

m2. Scope is `internal/teamchat` and below (`inTree`), so `cmd/teamchat` and `cmd/teamchat-admin`
are outside the rule. Neither uses a launch value today, and the tagged stage passes only
`./internal/teamchat/...`, but the untagged stage runs `./...`. The catalogue does not say the
commands are excluded. Resolve: one sentence, or widen `inTree`.

m3. The real-tree red logs are in `/workspace/untracked/scratch/`, outside the repo, so the
committed record of the red on the tree is the catalogue paragraph and the commit message. My rerun
at `8d2496eba` reproduces them. `822931505` does hold only the rule, fixtures and table rows, so the
red proof is a commit of its own.

m4. The report does not say which instances were examined individually and which received the
change by pattern. The 12 sites moved by hand in one commit, likely all individual; the report is
silent. Not applicable: suppression, baseline and upward referral (no instance left; the rule is
permanent and blocking in the project-rules stages, and the green run went through the project
driver).

### Checked and sound

- The six `agentBranches` rows in `table.go` are exactly AS6's six, with reasons. `fn: ""` means a
  package-level non-function declaration only (`funcName`), so `state` and `cli` get no wider
  exemption than the spec gives; `agent` is exempt as owner; CL2's `checkClaudeCmd` needs no row.
- Every form AS6 lists and every way round it that AS6 names (local copy of a constant, named
  string constant, switch on the field, `slices.Contains` over `state.Launches` or a literal list,
  `string(agent) ==`) is in the fixture and flagged; analysistest passes with the real table. The
  report states a control (map-key check disabled, test failed); I did not repeat it.
- Order: AS5 goldens (`8d2496eba`), then the rule (`822931505`), then the move (`e248c1fe6`). The
  rule is registered in `All()` (16 rules).
- The catalogue row and section exist, say the rule was found in the keeper and link comparisons,
  and name the spec's `isPlain` gap plus two more.
- The seam is the remediation; there is no separate original defect, and the goldens written first
  and unchanged after stand for the reproducing test.
- Nothing was worked around; the `qa-changed` failure at `822931505` is reported, not hidden.
