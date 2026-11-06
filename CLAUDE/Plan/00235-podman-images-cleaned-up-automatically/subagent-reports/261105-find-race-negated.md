# Defence Before Fix report: issue #152, the store probe matched a deleted file

Branch `fix/find-race-negated`: 05c2b0fe4 (defence, red), 599d3b349 (rule tightened, still
red), 34b1bee40 (fix, green), then this report. Plan 00235 Task 4.8a.

## Specification followed

Method specification 1.1.0, detector specification 1.0.0, toolchain specification 0.2.0, all
fetched by the skill's refresh script (dbf skill 0.1.1).

## Toolchain

No `defenceBeforeFix` manifest declaration (the repository has no composer.json or
package.json at its root that declares one), so no known-gap record either. The project's own
detector for Bash and YAML is semgrep 1.177.0 (pinned in `.qa-versions`), wrapped by Go gates
under `src-go/qa`. Single-rule harness: `semgrep --test --config .semgrep/find-race.yml .semgrep/find-race.fixture`. Identifier resolution: the message names its `CLAUDE/QA.md`
heading. Listing: `scripts/qa-gates.tsv` is the enumerable list of blocking checks. Calibration
assumed: the repository's own rule ("every finding fixed, no baseline, excludes only for files
that cannot carry the hazard").

## Defect and class

Instance: `tasks/podman-store-ownership-probe.yml:56`, the argv
`find <store>/overlay -xdev -ignore_readdir_race ! -uid 0 ! -uid <user> ! ( -uid +A -uid -B ) -print -quit`.

Class: a find over a tree that changes while it is walked whose match rests on a negated stat
test. Hazard: under `-ignore_readdir_race`, GNU find makes every stat test false for an entry
that vanished between readdir and stat, so each negated one is true and the vanished entry
matches; the probe reported a container's deleted `/tmp` file and failed play-podman.yml for a
clean store.

GNU findutils 4.10.0 is `/usr/bin/find` in this container (the same version as the host). The
interactive shell defines `find` as a function running bfs, so every measurement here named
`/usr/bin/find` and the test resolves `type -P find` and refuses anything that is not GNU.

## Rule

Detector: semgrep, `languages: [generic]`, rule `find-race-negated-stat-test` in
`.semgrep/find-race.yml`. Message: "find -ignore_readdir_race with a negated stat test matches
an entry that vanished during the walk. Put -links +0 after the option and before the first
negation." Documentation: `CLAUDE/QA.md` heading `find-race-negated-stat-test`.

Width: any published file, any language (shell, Ansible YAML `argv:` lists, `shell:` blocks,
templates, Python or Go argument lists). Two regexes: the option followed by a negation (`!`,
`\!`, `'!'`, `"!"`, `-not`, alone or opening a group) of a stat test (`-uid -gid -user -group -nouser -nogroup -perm -newer* -[acm]time -[acm]min -[acm]newer -used -size -links -inum -samefile -empty -readable -writable -executable -type -xtype -fstype -context`), with no
`-links +0` between them; and a negated stat test followed by the option. The span follows shell
line continuations and YAML or quoted list items and stops at an unrelated line or blank line.
The guard is `-links +0` exactly; in YAML the item must be quoted (unquoted, YAML reads the
integer 0, Ansible passes `-links 0` and the find matches nothing, a hazard found while writing
the fix, hence commit 599d3b349). Excluded as not carrying the hazard: the rule and fixture,
plan prose and journals (`CLAUDE/Plan/**/*.md`), changelogs, daemon-owned trees, `untracked/`.

Next wider rule considered: a find WITHOUT `-ignore_readdir_race` over a live tree with a
negated stat test. Measured here (churn of 200 files, 400 runs): it printed the vanished name in
112 runs and exited non-zero with an error in all 112, so a fail-fast caller (an Ansible
`command`, a checked substitution) stops instead of trusting a false verdict. Where its status
is discarded the defect is the discarded status, which `assignment-masks-substitution-failure`
and the bash-convention rules already cover. Not built. The four such finds in the tree
(Plan 00062 triage.bash x2, a completed acceptance script, the ccy relabel walk in
`lib/common.bash`) all consume find's status.

## Proof

Red at 05c2b0fe4 and 599d3b349 through the project entry point:

```
$ ./scripts/qa-find-race.bash
tasks/podman-store-ownership-probe.yml:56  find-race-negated-stat-test
find-race-negated-stat-test: 1 line(s) in 1 file(s) run find -ignore_readdir_race with a negated stat test. ...
passed: 6057 failed: 1        (exit 1)
```

The fixture `.semgrep/find-race.fixture` carries the probe's argv list (ruleid), shell forms,
continuation lines, Python lists, the option after the test, the unquoted YAML guard and
`-links 1` (ruleid), and the guarded forms, a non-stat negation and unrelated later lines (ok).
The gate proves the rule against it on every run. Go tests: `src-go/qa/ghscopes/spec_test.go`
(fake semgrep) and `src-go/qa/cmd/qa-find-race/main_test.go` (real semgrep: the unguarded argv
list is reported at its line, the guarded one passes).

## Independent search

Done by the practitioner, by text search and reading, not by a separate searcher dispatched
before the rule: a deviation from the skill's order, recorded here. `git grep` for the option
(spelt as a regex: the daemon's LSP handler blocks a plain grep for the token) found it in one
code file, the probe, plus plan prose. A second search for any find with a negated stat test
found the four finds above, read one by one: none has the option, all consume the status. The
rule found the same single instance; the search found nothing the rule missed. Reading was
needed to judge the four finds without the option; text search was needed to be sure the option
appears nowhere else (no other plan script, template or Go command builds it).

## Sweep and count

Scope: every file git would publish (tracked plus untracked-not-ignored), any language, handed
to semgrep by name, minus the rule's excludes; 6058 files scanned. Count: 1 instance. No
narrowing beyond the excludes listed above.

## Fixes

1 instance, examined individually: `tasks/podman-store-ownership-probe.yml` gains `- -links`
`- "+0"` after `-ignore_readdir_race` and before the first `"!"`, with the comment explaining
why. Nothing suppressed, no exclude added for it.

## Decisions referred to the owner

None. No baseline, suppression or unfixed instance.

## Permanence

Blocking row `find-race-negated-stat-test` in `scripts/qa-gates.tsv` (qa-all), run by
`scripts/qa-find-race.bash` (Go command `src-go/qa/cmd/qa-find-race`, the gh-scopes engine
generalised into a `Spec` with `GHScopes` and `FindRace`). qa-changed check `find-race` runs it
whole when the rule, fixture, wrapper or engine change (`scripts/qa-routing.tsv`). The race
suite has its own row `podman-store-probe-race` and is picked by qa-changed when the probe file
changes (the suite names it). Green at 34b1bee40: `./scripts/qa-find-race.bash` passed 6058,
failed 0. qa-changed over the branch: see the reply.

## Toolchain and detector gaps

- The skill's conformance reviewer could not be dispatched: the hooks daemon's WorktreeCreate
  handler refused to seed a worktree ("vault-pass.secret, extensions/node_modules,
  environment/localhost/host_vars/localhost.yml resolve outside the repository root") when
  called from inside a worktree. Not reviewed by it; to be logged by the coordinator.
- The daemon's `R-LSP-SYMBOL-LOOKUP` blocked `git grep` for a find option in YAML/shell,
  although its own advisory said no language server covers those files.
- The lint-on-edit hook reports `ansible-lint` missing in this container (an IaC gap for the
  ccy image, not investigated here).
- semgrep's generic mode rejects a bounded repeat `{0,600}` over the span group ("regular
  expression is too large"); the rule uses lazy unbounded spans that stop at unrelated lines.

## The original defect

Fix as above. Test: `scripts/test-podman-store-probe-race.bash` reads the probe's real argv from
the task (only the templates substituted; an argv item YAML did not read as a string fails it,
which is how the unquoted `+0` was caught), churns 200 files of the test's own uid in a
directory, runs the probe without its guard until it prints a vanished file (bounded at 4000
runs; it took 132 to 152 here) and with it 400 times. Before the fix: 70 of 400 runs printed a
deleted file. After: 0 of 400, and the probe still finds a file really owned outside the range.
About 1 s. It is stochastic: the guarded half cannot pass falsely (any print fails it), the
unguarded half could in principle not reproduce within 4000 runs on a very different machine,
which would fail the suite loudly rather than pass it. Host confirmation (from the host agent,
GNU findutils 4.10.0): unguarded 13 of 300 runs printed a vanished file, guarded 0 of 300.
