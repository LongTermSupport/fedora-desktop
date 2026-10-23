# n3e-10e: conformance review of three DBF remediations (fakeclock_count, goroutine_end, rawconn_probe)

Reviewer: conformance reviewer. Spec: Defence Before Fix 1.1.0 sections 3, 4, 7, 8 (cached copy). Branch
feat/bus-v2-n3e-10, tip eef4b0555. Nothing in the branch under review was changed. All runs were done in
throwaway detached checkouts of my own (review-n3e-10e-<sha>); drivers and logs are in untracked/scratch.

## Reproduction results (section 7), first

Entry point used: scripts/qa-go-qa.bash -t analyzers (the go-qa analyzers stage, which runs the project driver).
scripts/qa-changed.bash was NOT run: it is diff-scoped (a clean checkout has no diff) and pulls in the race test
suites on a loaded host. Every finding resting on "green through qa-changed" is therefore marked unverified.

| Run | Result |
| --- | --- |
| 7923769c1 (fakeclock defence), qa-go-qa.bash -t analyzers | exit 1, 28 x `error[project_fakeclock_count]`; single-rule vet: 28 untagged, 28 busfailpoints, 28 tuwunel. `go test ./rules ./driver` ok |
| 1ea729a07 (goroutine defence), same | exit 1, 15 x `error[project_goroutine_end]` (matches the report, kit.go:264 among them); `go test ./rules ./driver` ok |
| 7ee7309ec (rawconn defence), same | exit 1, 1 x `error[project_rawconn_probe]` at link/parent.go:62:21; 4 goroutine_end also remain (the declared "part" state); tests ok |
| eef4b0555 (final), same | exit 0, "analyzers passed"; single-rule vet of all three rules clean untagged, busfailpoints, tuwunel and in the nested qa module. `go-qa rule-doc` resolves all three identifiers; red messages print `error[<id>]` plus the DBF footer |
| Original 2 (pidfd): final with the old callback patched back in my scratch checkout | TestAKeeperDeadBeforeTheWatchReadsIsSeen FAILS at its 10 s bound ("the watch never saw a keeper that died before it read"); with pidfdReadable the watch tests pass (-race -count=3) |
| Original 1 (hook op) at 7923769c1 | `-race -count=300 -cpu 1,2,4 -run TestAnOpBeforeThePIDIsNotAnswered -timeout 120s` panics "test timed out after 2m0s"; at eef4b0555 the same run passes (4.4 s) |

Red and green are reproduced for all three rules and both originating symptoms. Not run: the full link package
under shuffle, qa-changed, the failpoints test stage. No suppression found: no nolint added, and rules/table.go
changes only register the rules in All().

## Findings, by severity

### F1 (high) The goroutine_end narrowing is not hazard-free and reopens the originating shape. Clauses 3.1 upper/lower bound, 3.3 Part B, 4 (doubt is Suppression)

Evidence: src-go/qa/rules/goroutineend.go:181-209 (signalsOnEveryReturn, added in ae6f7b136); the sentence in the
report (lines 200-203) and GoProjectRules.md:272-274: such a body "ends only through a signalled return, which an
owner can wait on as surely as a deferred close". A send is not a close: it blocks when nobody receives, which is
exactly the hazard of the originating link.read (blocked on an unbuffered `l.msgs <-`). The kit.go case is safe
only because `got` is buffered (cap 1), a fact the rule never checks.
Reproduced: a goroutine `for { m, err := recv(); if err != nil { msgs <- m; return }; msgs <- m }` over an
unbuffered channel nobody reads is reported by the 1ea729a07 driver and is clean under the final driver (probe file
created and removed in my scratch checkout). The narrowing therefore admits a goroutine with the same hazard as the
instance that started the remediation. The exclusion sentence is false for the shape the code admits, so under
3.3 Part B and section 4 this is a Suppression and belongs to the Owner.
Also: the narrowing lands in the same commit as the last instance fixes (ae6f7b136); its before-state is the
earlier commit where kit.go fired, not a commit of its own.
Resolve: narrow only where the send provably cannot block (a channel made buffered in the same function, or a send
inside a select with a quit/ctx arm), or restore the earlier rule and fix kit.go (or record it as a known instance
for the Owner); add a fixture for the unbuffered-loop shape as flagged.

### F2 (high) goroutine_end excludes every _test.go goroutine on a false premise, and the excluded code was never searched. Clauses 3.3 Part B, 3.4 scope, 3.1

Evidence: goroutineend.go:56 skips test files; the Doc (lines 32-34) and the report give "a test binary leak check
answers for those". leakcheck is wired into two packages only (engine, link; grep leakcheck). There are 84 `go`
statements in _test.go files across src-go, none judged. The searcher scope was "first-party non-_test.go files"
(261023-n3e-10d-goroutine-search.md line 3), yet the originating symptom was a leak seen in a test binary. Part B:
code a Narrowing excludes MUST be searched to clause 3.1 standard; 3.4: sweep all first-party source where the
pattern can occur. The sentence cannot be written for the packages without leakcheck, so the exclusion is a
Suppression for the Owner.
Resolve: judge the 84 test goroutines (or run the rule over tests and read the count) and fix or refer; or wire
leakcheck into every package that starts one, and say so.

### F3 (medium) Decisions that section 4 reserves to the Owner are recorded as "None". Clauses 3.1 step 4, 3.4, 4

All three reports say "Decisions referred to the owner: None". Three things belong there:
a) rawconn_probe tests "callback reads its descriptor", narrower than its own hazard sentence ("answers without
   testing its descriptor"). `func(fd uintptr) bool { _ = fd; return false }` passes (the Doc admits it under "Not
   caught"). The wider rule was not built because "what counts as a test is open-ended" (hard to build) and "the
   search found no callback" (no instance today): the two reasons clause 4 bullets 3 and 4 send to the Owner.
b) goroutine_end: the next wider rule (awaited on every path) is not built as "a whole-program question" (hard to
   build): Owner. The run-time half (leakcheck) covers only link and engine.
c) The searcher ended-but-unwaited goroutines (stdin/tty readers in pingbus-status-line, statusline,
   freeze/chooser, open/choose; units/bus.go:30 dialBus; execx/session.go:64 grace path; engine/teams.go:177 team
   goroutine "waited up to stopWait, then abandoned and logged") are knowingly left as they are, each with a
   "bounded/by design" sentence. The search found them, the rule is narrower than the search, and they are accepted
   unfixed: clause 4 "accepting a known instance as unfixed" and "rule narrower than the search showed". The report
   itself calls the engine one "a decision of the design, not of this remediation".
Resolve: refer a), b), c) with counts (c: 4 classes, about 7 sites), or widen the rule and fix.

### F4 (medium) Commit db70c7c45 is not what the record says it is. Clauses 3.3 Part A, 7 (report must match commits)

db70c7c45 is described as "the pidfd watch tests the pidfd on every call (DBF instance and original)" and listed as
remediation 3 commit only. It also rewrites link/server.go (the `stopping` context replaced by a `serving` cancel
map and serve()), which the report attributes to remediation 2 ("the per-request watcher in server.go is gone").
Remediation 2 commit list (1ea729a07, 09eb8ba57, ae6f7b136) does not include it. A reviewer reproducing
remediation 2 instance fixes from its listed commits misses this one.
Resolve: correct the record (or split the change). No defect found in the change itself.

### F5 (medium) The permanent gate does not run the new rules on tagged builds, though the sweep scope claims them. Clauses 3.4 scope, 3.5

Evidence: src-go/qa/go-qa.toml [analyzers] runs the driver untagged; the three teamchat-failpoints stages run
golangci-lint, plain `go vet -tags busfailpoints` (no -vettool) and tests. 20 _test.go files carry
busfailpoints/tuwunel tags and the fakeclock and rawconn rules judge test files. I ran the rules by hand on those
tags at the final commit: clean; at the red commit the tagged counts equal the untagged ones, so no instance is
hidden today, but a future instance in a tagged test file passes the gate.
Resolve: add the vettool to the tagged vet stage, or record it as a mechanism gap where gaps are recorded.

### F6 (medium) The report contradicts itself and claims runs it does not evidence. Clauses 3.5, 7

a) Report lines 402-417 ("Package proof ... Not complete ... Neither is fixed here; see ...c-handoff") are stale:
   remediations 2 and 3, earlier in the same file, fix exactly those two defects. The package-level status after
   the fixes (shuffled link runs) is not stated anywhere.
b) "Green through ./scripts/qa-changed.bash on the final commit" (all three) has no recorded output. Analyzer-stage
   green is reproduced above; the full qa-changed run is unverified.
c) The logs for remediations 2 and 3 (pidfd-red.log, pidfd-green.log, leak-red.log, ge-*.log) live under
   untracked/scratch and do not travel with the commit. I reproduced the pidfd red/green myself, so this is a
   record weakness, not a failure.

### F7 (low-medium) Independence and ordering of the searches. Clause 3.1

- All three records say the searcher did not open the rule and give an "Attribution by technique" section.
- The second technique in each ("reading") judges the hits of the first (a regex inventory), so both share the
  regex blind spot. The searchers did list spelling variants (wg.Go, AfterFunc, NotifyContext, helper wrappers),
  the accepted way to search a spelling-defined class, so this is not a failure, but saturation is asserted, not
  shown.
- Ordering evidence from history: the goroutine search record is in the red commit 1ea729a07 and the readiness
  record in 09eb8ba57 (before the rawconn red 7ee7309ec), consistent with "before the rule". The fakeclock search
  record first appears in 62aaa7e76, after the red and instance commits; that it ran first rests on the report
  word (unverified).

### F8 (low) Rule decisions live only in the report, not with the rule. Clauses 3.3 Part B, 8.5, 8.7

The next wider rule not built (all three) is in the subagent report only. `go-qa rule-doc` and GoProjectRules.md
(lines 237-320) carry the narrowing facts and "Not caught" lines but not the named wider rule and its reason, so a
practitioner arriving cold cannot find them through the defence listing. The "no per-analyzer pages under
docs_dir" gap is also recorded only in the report, although the identifier does resolve (`go-qa rule-doc`).
Resolve: copy the wider-rule line and the gap into the catalogue section.

## Checked and found conforming

- Red committed alone and before fixes: 7923769c1, 1ea729a07, 7ee7309ec each hold rule, fixture and docs (plus the
  search record for goroutine) with the instances still present; each pair of commits stays reachable by hash.
- Counts 28 / 15 / 1 reproduced exactly; the fakeclock per-package split (3+4+1+1+3+8+7+1) matches.
- Fixtures exist per rule (badcount, badgo, badraw) with clean and flagged lines; tests pass at every defence commit.
- No suppression, baseline or excuse row; BlockUntil deleted rather than silenced; fixes are structural (AwaitDue,
  owners that wait, a per-call poll). I read the pidfd, server and fakeclock fixes; I did not re-read all of the
  28 + 14 instance edits, so "each examined individually" is taken from the report for the rest.
- The fakeclock narrowing (WallMS int64) is hazard-free by type, was found by the rule own sweep, and has a
  fixture line.
- Original defects fixed last with reproducing tests (above); TestAKeeperDeadBeforeTheWatchReadsIsSeen pins the
  reported pidfd behaviour.
- Identifier: the message prints error[project_...]; `go-qa rule-doc <id>` prints what, why and how to fix for
  all three.
- Report headings match the template used by the earlier hidden_bindings report; the CCY version bump and
  changelog accompany the execx change.
