# Adversarial review: "Tooling Is Go" policy and the qa-bash-limits gate

Read-only review on F44 at `da797a0b`. Scratch artefacts (gate binary, fixture repos, probe
scripts) are under `/workspace/untracked/scratch/fable-gate/`: `evade.bash` (evasion probes),
`ratchet.bash` (ratchet probes), `bisect.bash` (real-file mis-measurement), `real-unscanned.bash`.
Every "reproduced" claim below was run against a gate built from `src-go/cmd/qa-bash-limits`.

Severity: **blocker** = defeats the policy or will break the gate on untouched files;
**should-fix** = a real gap a determined or careless author will walk through; **nit** = quality.

---

## 1. Evasion (how to commit large bash that passes)

### E1. blocker — Any dot in a file name disables detection for shebang files

`files.go:22-27`: `if ext != ".j2" && strings.Contains(base, ".") { return false }`. An
extensionless deployed tool renamed to anything with a dot is invisible, whatever its shebang.

Reproduced (`evade.bash`, 192-line body with 10 functions, `declare -A`, `fzf`): all PASS

- `files/home/.local/bin/com.example.tool`
- `files/home/.local/bin/tool.txt`, `scripts/tool.bats`, `scripts/tool.zsh`, `files/home/.envrc`

Combined with the ratchet (R2 below) this is the cheapest full bypass: `git mv vmtest vmtest.cmd`,
change the playbook `src:`, run `--update` (entry dropped as "gone"), green.

**Fix:** decide candidacy by shebang first, extension second: read the first line of every regular
tracked file under a size cap (the scan is 0.26 s for 2,320 files; reading 256 bytes of each is
negligible) and treat `#!…(ba)?sh` as bash regardless of name; keep `.bash`/`.sh` as unconditional.
Exclude only binary-looking files (NUL in the head). Add a planted test for `tool.txt` with a shebang.

### E2. blocker — A Jinja first line hides a bash `.j2`

`IsCandidate` reads only line 1. `{# managed #}` or `{{ ansible_managed | comment }}` as line 1
followed by `#!/usr/bin/bash` → not a candidate. Reproduced: `files/home/.local/bin/tool.j2`
(Jinja first line) PASS; `files/etc/tool.bash.j2` PASS. The repo already has one such file
(`files/home/bashrc-includes/host-health-report.bash.j2`, line 1 `{{ ansible_managed | comment }}`;
exempt anyway, but it shows the idiom is in use).

**Fix:** for `.j2`, look for a bash shebang in the first N (say 5) non-Jinja lines, and treat
`*.bash.j2` / `*.sh.j2` as unconditional candidates (like `.bash`/`.sh`).

### E3. blocker — Extensionless file with no shebang

`files/home/.local/bin/tool` starting `set -e` (invoked as `bash tool` from a playbook/alias):
PASS. Not a huge audience, but it is the "forgot the shebang" case, which happens by accident.

**Fix:** with E1's fix, add a light content sniff for extensionless files under `*/bin/`,
`*/sbin/`, `scripts/`: first non-comment line matching `set -[euo]|^[A-Za-z_]+\(\)|^source |^\. `.
Or simply: every regular file under those directories is a candidate unless it has a non-shell
shebang. qa-bash.bash (shellcheck) probably already needs the same discovery; share it.

### E4. should-fix — Code lines are counted per physical line; `;`-joining is unlimited

A 200-statement file joined with `;` into one line is 1 code line. Reproduced: `one-line-joined`
was caught only by the assoc/UI flags; drop those and it is green. Nobody will do this by accident,
but a model told "stay under 60 lines" will. Nothing else in QA re-expands (no `shfmt`).

**Fix:** add a per-line byte cap (e.g. 160 chars) or count *statements* (split on `;`, `&&`,
`||`, `|` outside quotes) — or cheapest: cap total non-comment bytes (e.g. 4 KiB) alongside the
line count. State whichever you pick in the package doc.

### E5. should-fix — Function detection is line-anchored; several real definition forms are missed

`funcRe` is `^\s*…`. Reproduced PASS with 10 functions each:

- `true; f1() { :; }` (definition after `;` on the same line) — `funcs-after-semicolon`
- `f1() { :; }; f2() { :; }; …` (ten definitions on one line) — `funcs-joined-on-one-line`
- `eval "$n() { … }"` in a loop — `eval-defined-functions` (the daemon blocks `eval` on
  Write, but not on Bash heredocs, and not in CI)

**Fix:** apply `cmdStart` to `funcRe` as it already is to `assocRe`/`uiRe`, and count every match
on the line, not one per line. Add these three strings to `TestMeasureFunctions`.

### E6. should-fix — assoc/UI detection misses trivial spellings

Reproduced PASS: `builtin declare -A m`, `\declare -A n`, `declare "-A" o`; `/usr/bin/fzf`,
`~/.fzf/bin/fzf`, `\fzf`, `timeout 5 fzf`, `xargs -0 fzf`, `fzf-tmux`, `"fzf"`, `$FZF_BIN`.
Also missing from the UI list entirely: `fzf-tmux`, `sk`, `fzy`, `peco`, `zenity`, `yad`,
`kdialog`, `rofi`, `dmenu`, `tmux display-menu`.

**Fix:** allow an optional path prefix (`(?:\S*/)?`) and a leading backslash before the command
word; add `builtin`, `timeout \S+`, `xargs( -\S+)*`, `nice`, `stdbuf`, `unbuffer` to `wrappers`;
add `fzf-tmux` and the common pickers to the UI list. Record the known gaps (variable-held command
names) in the package doc as accepted.

### E7. should-fix — A `<<` inside a string opens a phantom heredoc and blinds the rest of the file

`openedHeredoc` runs on `stripComment(line)`, which keeps string contents. `: "<<EOF"` or
`echo "usage: tool <<file"` opens a heredoc that never closes; every later function, `declare -A`
and `fzf` is skipped (code lines still count). Reproduced: `fake-heredoc-in-string` and
`heredoc-hyphen-delim` (`<<EOF-1`: the regex captures `EOF`, the terminator is `EOF-1`) were caught
by code lines only — all 10 functions, the assoc and the UI went unseen.

**This is already mis-measuring a real file.** `files/var/local/docker-in-lxc:524` is
`cat > /usr/local/bin/ccy << '\''EOFCCY'\''` inside a `bash -c '…'` string; the regex takes the
delimiter as `\`, nothing closes it, and the gate counts 14 functions where the file has 20
(`bisect.bash` output: counting stops after `provision_container_direct`; `enter_container`,
`select_ccy_token`, `stop_container`, `destroy_container`, `show_status`, `main` are invisible).
The allowlist record `files/var/local/docker-in-lxc 649 14 -` is therefore wrong, and see R4 for
why that matters.

**Fix:** strip quoted spans (not just comments) before looking for `<<`; accept any non-space
delimiter word (`[^\s'"\\]+`) for unquoted heredocs; drop the `((` short-circuit in favour of
requiring `<<` not preceded by `<` and not inside `(( ))`. Add docker-in-lxc line 524 as a fixture.

### E8. should-fix — `#` lines inside a multi-line string are not counted as code

`x="` … 100 lines beginning `# data` … `"` → 1 code line. Reproduced PASS. Low risk of deliberate
abuse, but usage/help text and embedded config written this way are undercounted. With a
quote-aware scanner (E7) this falls out for free.

### E9. should-fix — Library splitting has no aggregate

A 3-line driver sourcing `"$0"-part*` (10 files × 59 lines) is green: `source-split` PASS. This
is the evasion a well-meaning author reaches for ("the gate says 60, so I'll make a lib").

**Fix:** either (a) aggregate: resolve static `source`/`.` of relative paths within the repo and
measure the closure (driver + sourced files) against the limits, or (b) policy: forbid new
`source`d bash libraries outright (a lib is a program) and detect `source`/`.` of a non-exempt
in-repo file as a flag like `assoc`. (b) is simpler and closer to the owner's line.

### E10. should-fix — Exempt paths are open to anyone naming a file right

Reproduced PASS with the 192-line offender at: `scripts/test-tool.bash`, `tests/x/tool.bash`,
`CLAUDE/Plan/00001-x/deploy.bash`, `CLAUDE/Plan/Cancelled/00001-x/deploy.bash`,
`files/home/bashrc-includes/tool.bash`, `.claude/skills/docs-qa/scripts/tool.sh`.

- `scripts/test-*.bash`: 74 files, 20,862 code lines, exempt and unbounded. A new
  `scripts/test-mygotool.bash` for a Go binary is also exempt, although the policy says Go code
  gets Go tests. Nothing checks the suite's subject is bash.
- `files/home/bashrc-includes/`: "must be shell" is true for the snippet, but the exemption
  removes the *size* limit too, so a 500-line interactive bashrc include is fine.
- `.claude/skills/<name>/`: pattern is per skill name; fine today, but a skill directory is also
  where a project author drops scripts.

**Fix:** turn directory exemptions into *ratcheted* categories rather than blind spots: bashrc
includes and test suites keep their current records on the allowlist (they may shrink, never
grow, and a *new* file there must be within limits). Only `run.bash` and daemon-owned vendored
paths deserve a true exemption. See P2 for plan folders.

### E11. nit — `bash -c` strings and shebang-embedded bash

`exec bash -c '…200 statements…'` is one code line; caught only via assoc/UI (`bash-c-string`).
The playbooks' own shebang `#!/usr/bin/env -S bash -c '…exec run.bash…'` shows the idiom exists.
Covered by E4's byte cap.

### E12. nit — Ansible `shell:` blocks and `copy: content:` bash

Not candidates; INVENTORY says the largest block is 28 lines, and `qa-ansible.bash` owns the
style there. Record the decision in the package doc so the next reviewer does not re-ask; consider
a 40-line cap on `shell: |` blocks in qa-ansible later.

---

## 2. Ratchet soundness

### R1. blocker — CI compares with `event.before`; `cancel-in-progress: true` opens a window

`.github/workflows/qa.yml:19-21` cancels the in-flight run for a ref when a new push arrives;
lines 122-124 set the base to `github.event.before`. Push 1 raises a record (or adds an entry) and
commits it; push 2 lands seconds later with anything. Run 1 is cancelled; run 2 compares HEAD's
allowlist with push 1's head, where the raise already exists → green. The raise is now in history
and every later run inherits it as its base. The same holds for a developer who runs
`qa-all.bash` (base = HEAD) after committing the raise: green locally, and only a CI run that
is allowed to finish catches it.

Reproduced the local half in `ratchet.bash` step (d): a hand-raised record fails against HEAD
before commit, passes `--base HEAD` after `git commit --no-verify`, fails against `HEAD~1`.

**Fix:** the base must be a point the change has *not* yet reached, independent of push
granularity: for every event compare with the **merge-base of HEAD and `origin/<default>`**
(fetch the default branch with enough depth, `git merge-base`), and for pushes *to* the default
branch compare with `event.before` **and** set `cancel-in-progress: false` for that ref (or key
the concurrency group on `github.sha`). Locally, default `--base` to
`$(git merge-base HEAD origin/F44 2>/dev/null || echo HEAD)` in `scripts/qa-bash-limits.bash`,
so the owner working on F44 compares against the last *pushed* state rather than the last commit.

### R2. blocker — Moving a listed file into an exempt path, or renaming it with a dot, launders it

`Tighten` drops any listed path that is "not scanned" (`allowlist.go:201-204`), and
`KindStale` tells the author to run `--update`. Reproduced (`ratchet.bash` b/b2):
`git mv scripts/old-tool.bash scripts/test-old-tool.bash` → `--update` drops the entry → green;
`git mv files/home/.local/bin/new-big files/home/.local/bin/new-big.cmd` → same. `CompareBase`
only objects to additions and raises, so a vanished entry is fine by design — but the gate cannot
tell "ported to Go and deleted" from "renamed into a blind spot".

**Fix:** (1) E1/E10 close the two destinations; (2) in `Evaluate`, when a listed path is gone,
look for a *rename* with `git diff --name-status -M --find-renames` against the base, or more
simply refuse `--update` to drop an entry whose basename still exists anywhere in `ls-files`
outside exempt paths; (3) have `--base` compare *removed* entries too and require that the file
is actually absent from `ls-files` (not merely unscanned) or the finding is "entry removed while
the file still exists".

### R3. should-fix — A base ref with no allowlist disables the added/raised check silently

`againstBase` (`app.go:224-227`): not found → notice on stderr, no findings. A PR opened against
any branch that predates the allowlist (every branch older than 2026-10-14), or a push whose
fallback default-branch fetch somehow lacks the file, passes with any number of new entries.
Reproduced in `ratchet.bash` step (e): "HEAD has no allowlist (it is being introduced): nothing
to compare" and the hand-added entry drew no `added` finding (the run failed only because my
fixture record was one short of the file size).

**Fix:** now that the allowlist is on F44, make "base has no allowlist" an error
(`ExitCannotRun`) unless `--allow-missing-base` is passed; or compare against the default
branch's allowlist whenever the chosen base lacks one.

### R4. blocker — There is no migration path when the measurer changes

Records are per-file numbers from *this* heuristic; `--update` never raises; `--base` rejects a
raise; `--seed` refuses to run when the file exists. Fixing any measurement bug (E5-E7 above —
docker-in-lxc alone goes 14 → 20 functions) makes untouched allowlisted files "grew past its
record" with no legal way to repair the allowlist. The only route is a hand edit committed
against `--base` protest, i.e. the exact thing the ratchet forbids, on the very commit that makes
the gate more honest. The gate is therefore frozen at its first heuristic.

**Fix:** version the measurer. Put `# measurer: 1` in the allowlist header; have the gate refuse
to run when its `MeasurerVersion` differs, and provide `--remeasure` that rewrites every record
to the current measurement (up or down) *and* stamps the new version. `CompareBase` allows raises
only when the version line changed in the same diff; a raise with an unchanged version is still
rejected. Test: a planted raise with an unchanged version fails; with a bumped version passes.

### R5. should-fix — A shrink anywhere reddens the tree until `--update`, with merge conflicts baked in

`KindTighten` is a failure. Two branches each shrinking a different file both run `--update` and
both rewrite the sorted file; the merge conflicts on neighbouring lines. More importantly a
*refactor that merely deletes a comment line* does not fail, but deleting one code line does,
which trains authors to leave dead code in. Consider making `tighten` an advisory with `--update`
the fix, and failing only when the record is above the measurement by more than a tolerance
(or at all only in CI).

### R6. nit — `--update` writes the allowlist with mode 0600

`allowlistMode = 0o600`; on disk the tracked file is `600` now (`stat`), which git does not
record, so clones differ from the working tree and a `files` drift check elsewhere may notice.
Use 0o644.

---

## 3. Measurement correctness (beyond the evasions above)

- **Correct:** `${x#y}`, `$#`, `${#arr[@]}`, `"a # b"` are not comments (`stripComment` is
  quote-aware); blank/comment/shebang lines are not code; `<<<` and `1 << n` do not open heredocs;
  `command -v fzf`, `fzf_cmd=1`, `case … in fzf)` on one line are not UI.
- **False positive:** a `case` arm on its own line — `  fzf) …;;` / `  gum) …;;` — matches
  `uiRe` (`)` is in the terminator class). Reproduced (`ui-case-arm-false-positive`: flagged
  "interactive UI"). A dispatcher that *names* pickers in a case statement gets a UI flag. Fix:
  exclude a match whose terminator is `)` when nothing but whitespace precedes the word.
- **Line continuation** (`\`) inflates the count; conservative, acceptable, but say so in the doc.
- **CRLF** files: `TrimSpace` handles `\r`; fine.
- **Heredoc terminator** compared with `TrimSpace(line) == delim`: a terminator followed by a
  trailing space (invalid bash anyway) is fine; a `<<-` body with tabs is fine.
- **Symlinks** are skipped (`scan.go:26`): a tracked symlink `scripts/x.bash -> ../big` is fine
  because the target is scanned under its own path — unless the target lives in an exempt or
  undetected path (E1/E10). Minor.

---

## 4. Policy quality

### P1. should-fix — CLAUDE.md says "Enforced by" under a list with an unenforced bullet

`CLAUDE.md:44-59`: "A bash file must stay within **all** of these … no data parsing beyond one
`jq -r` … **Enforced by** `scripts/qa-bash-limits.bash`". The journal (07:08) admits the data
parsing clause is "written but not gated". A reader of CLAUDE.md cannot tell. README.md:277
repeats the clause as if it were part of the gate. QA.md correctly lists only four checks.

**Fix:** either gate it heuristically — flag a file with ≥2 `jq` invocations, any `jq -r`
whose filter contains `|` or `[]` or `to_entries`, any `yq`, `python -c`, `python3 -` heredoc —
or mark the bullet in CLAUDE.md "(reviewed, not gated)". Heuristic gating is ~20 lines and
catches the 69 files INVENTORY flagged "J"; I recommend gating it.

### P2. should-fix — Plan-folder bash is exempt, yet PlanScriptStandards *mandates* new bash

`CLAUDE/PlanScriptStandards.md:3`: "A plan that needs anything run ships a runnable `*.bash` in
its own folder", built on `_planlib.inc.bash` (395 lines, 25 functions, allowlisted). Live plan
folders hold 77 bash files / 10,470 code lines; Completed 100 / 14,969; Cancelled 3 / 714. Every
new plan will add orchestrators with `set -e`, logging, state and probes: that is tooling by any
reading of the owner's line, and the exemption makes it the one place in the repo where bash can
still grow without limit. "Transient scaffolding" is not what the Completed/ tree shows: it is
kept forever.

The root `CLAUDE.md` "Bash stays right for" list (lines 52-54) does not mention plan scaffolding
at all; only QA.md and `files.go` do. So the exemption is undocumented at the policy level.

**Fix (decision for the owner):** either (a) state the exemption in CLAUDE.md with its reason
and bound it (plan scripts within limits or they call a Go `plan` tool; `_planlib` functionality
moves into `mkplan`/a `planctl` Go binary), or (b) keep plan folders exempt but make the
exemption *new-plan only* (any plan numbered after 00192 is checked). (b) is coherent with "line
in the sand"; (a) is the honest end state.

### P3. should-fix — `run.bash` (2,849 code lines, 67 functions) is exempt rather than ratcheted

INVENTORY's own recommendation is to shrink `run.bash` to a bootstrap stub. An *exemption* lets
it grow unbounded; an *allowlist record* lets it only shrink, which is what the owner wants. Same
for `.claude/init.sh` (vendored, fine to exempt) versus the project-owned `scripts/test-*.bash`.
Move `run.bash` and the test suites from `Exemptions` to the allowlist.

### P4. nit — Dead and overlapping exemptions

`tests/` matches 0 bash files; `.claude/hooks-daemon/` is gitignored (0 files). Harmless, but a
test `TestEveryExemptionMatchesSomething` (or a reported "0 files" per exemption in `--verbose`)
would keep the table honest. `TestExemptionTableIsSound` currently only checks the reason is ≥20
characters.

### P5. nit — Threshold DRY

60/4 are stated in `measure.go`, `app.go` usage, `scripts/qa-bash-limits.bash:7`, `CLAUDE.md`,
`README.md`, `QA.md`, INVENTORY, PLAN, JOURNAL. The docs are fine; the three code copies should
be one: build the usage string from the constants, and drop the numbers from the wrapper's header.
A doc test that greps CLAUDE.md for `MaxCodeLines` would stop drift.

### P6. nit — Pragmatism of 60/4

62 of 170 checked files are within limits; two unlisted files sit at 58 and 49 code lines
(`scripts/agent-mailbox-watch.bash`, `scripts/qa-stt-limits.bash`). Every new `qa-*.bash`
wrapper around a Go gate must fit 60 lines including the LLM-friendly header convention, which is
achievable (`qa-bash-limits.bash` is 32). The function cap of 4 is the one that will bite:
`die`, `log`, `usage`, `main` is four. Fine as a forcing function; just expect the first wave of
"why can't I have a fifth helper" and answer with E9's rule (no libs).

### P7. nit — Interactive scripts rule is now partly obsolete

`CLAUDE/InteractiveScripts.md` still describes bash re-prompt loops for human-run scripts under
`files/home/.local/bin/`; those scripts are now Go by rule (`gum`/`fzf`/`select` are flags). Add
one line pointing at `src-go/internal/ui`.

---

## 5. Gate code quality (Go)

- **Architecture:** clean separation (files / measure / scan / allowlist / app); `os.Root` for
  path confinement; git only through `gitx`; `sink` for write-error capture. Good.
- **Error handling:** `IsCandidate` on an unreadable file aborts the whole run (`ExitCannotRun`)
  — correct fail-fast. `--update` returns findings *and* writes: fine, documented.
- **Tests that pass while broken:**
  - `TestMeasureHeredocForms` has no unquoted hyphenated delimiter, no `<<` inside a string, no
    escaped quote (E7) — the real-file bug slipped through.
  - `TestMeasureFunctions` has no "after `;`" or "two on one line" case (E5).
  - `TestIsCandidate` has no `tool.txt`-with-shebang negative that documents the dot rule as a
    *decision*, so E1 reads as accidental.
  - `TestBaseWithoutAnAllowlistIsNoted` pins the silent pass in R3 as desired behaviour.
  - `TestExemptionTableIsSound` tests string length.
- **qa-changed trigger gap:** `scripts/qa-changed.bash:222` runs the gate for `*.bash | *.sh | *.j2`, the Go source, and *listed* paths. A **new extensionless** bash file (the `bin/` tools
  are all extensionless) does not trigger it; `qa-all.bash` before push does. Add
  `files/*/bin/* | files/*/sbin/* | scripts/*` (or re-use the gate's own candidate test by
  calling the binary with `--only <path>`).
- **Summary line** (`app.go:258`): "170 bash file(s) checked: 279 exempt, 108 allowlisted, 62
  within limits" reads as 170 ⊇ 279. Print
  `BASH-LIMITS-OK 449 bash files: 279 exempt, 170 checked (108 allowlisted, 62 within limits)`
  and update the `qa_gate_detail` regexp in `qa-all.bash:130` and `TestCleanTreePasses`.
- **Wrapper:** `scripts/qa-bash-limits.bash` builds into `mktemp` under `/tmp` and executes it;
  a `noexec` `/tmp` (hardened hosts, some CI images) fails with a confusing "permission denied".
  Build into `"$REPO_ROOT/untracked/scratch/"` or `go build -o "$(mktemp -d -p "$REPO_ROOT/untracked")"`.
- **qa-changed.bash:23** comment is garbled: "any bash file, the bash- the bash-limits gate".
- **Performance:** 0.26 s over 2,320 tracked paths; no concern. Reading a 256-byte head for every
  dotless/j2 file is already done; E1's fix (every file) costs nothing measurable.
- **Allowlist parsing:** strict and good (duplicates, field count, flags). A path with a trailing
  space is correctly "stale" (reproduced). Consider rejecting paths that are not `filepath.IsLocal`.

---

## Recommended order

1. R1 + R3 (CI base: merge-base with default branch; missing base is an error) — the ratchet is
   the policy; today it can be slipped past in two ways.
2. R4 (measurer version + `--remeasure`) — prerequisite for shipping any of the measurement fixes
   without hand-editing the allowlist.
3. E1 + E2 + E3 + E10/R2 (candidate discovery by shebang/content; exempt dirs become ratcheted
   categories; `--update` refuses to drop an entry whose file still exists) — closes the rename
   laundering.
4. E5 + E6 + E7 + the case-arm false positive (quote-aware scanner, `cmdStart` on functions,
   wider wrappers) — then `--remeasure`.
5. E4/E9 (byte cap; sourcing a non-exempt in-repo file is a flag) and P1 (gate data parsing).
6. P2/P3 decisions for the owner (plan folders; `run.bash` and test suites ratcheted, not exempt),
   docs alignment (CLAUDE.md exemption list, "Enforced by" wording, InteractiveScripts pointer),
   summary line, qa-changed trigger, 0644, `/tmp` build dir, comment typo.
