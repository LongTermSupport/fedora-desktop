# QA Review — Plan 00133 close round 7

Reviewed: `42460224`, `8488e96e`, `569f8f57`, `be69ead2`, `c14ee52b`, plus
`CLAUDE/Plan/00112-gnome-extensions-enabled-state-declared/deploy.bash` and
`untracked/meta-deploy.bash`.

**Verdict**: BLOCK

I re-derived every load-bearing fact from the shipped binary
(`/usr/local/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`, 2.1.274, confirmed
from the adjacent `package.json`) rather than accepting round 6's account, this session's
correction, or the operator's. **The operator is right and round 6 was wrong**: `/config`
has no window row. But the correction that replaced it is itself wrong in its most copyable
detail, and it is already in the public changelog. That is the fourth wrong account. There
is a fifth, older one that three successive correcting commits walked past.

## Blocking

### 1. `docs/ccy.md:681,685` tells every user to reproduce the exact bug this plan exists to fix

```bash
# .claude/ccy/ccy.env
export CLAUDE_CODE_AUTO_COMPACT_WINDOW=1M
```

`1M` is not accepted by the environment variable, for precisely the reason the paragraph
100 lines above now explains at length. Traced: `zl("1M")` calls `N("1M")`, where
`l=/^[+-]?(\d+(\.\d*)?|\.\d+)[eE][+-]?\d+$/` (scientific notation) and
`c=/^[+-]?\d{1,3}([_,   ])\d{3}(?:\1\d{3})*$/` (group separators) at byte
189515150. Neither matches, so `N` falls off the end and returns `undefined` — the `??`
then falls through to `parseInt("1M",10)` = **1**. `tZ` returns
`{effective:1,status:"valid"}` (not NaN, >0, not >1e6), and `zv` applies
`Math.max(Z1e, 1)` = **100,000**.

So the documented per-project override silently yields a 100,000-token window: the
identical defect, the same mechanism, the same silence, in the user-facing page that
documents the defect.

`git log -L 675,690:docs/ccy.md` shows how it survived. `389d4104` introduced `1M` beside
`600k`. `8c87c646` edited the sentence directly above it from `600k` to `600000` and left
the code block. `42460224` and `be69ead2` rewrote the surrounding section twice more and
left it again. Three corrections, one paragraph, only the instance the author was looking
at fixed each time.

It also poisons Task 4.4. An operator following this example sets `1M`, reads `1M` back
from the container environment, and ticks "the project's value wins" — a green tick on a
value Claude Code floors to 100,000. That is Task 4.2 on 3.58.0 happening a second time,
teed up by the repo's own documentation.

**Fix**: use a digits-only value the variable accepts (e.g.
`export CLAUDE_CODE_AUTO_COMPACT_WINDOW=1000000`), in both the code block and the prose
sentence at `:685`.

### 2. The quoted `/autocompact` output is wrong twice, and the string it will actually print is `600k`

`docs/ccy-changelog.md:28` (public release note), `PLAN.md:210`, `PLAN.md:244` (Success
Criterion 2), and `research/auto-compact-window-facts.md:85`:

> `Auto-compact window: 600,000 tokens (from CLAUDE_CODE_AUTO_COMPACT_WINDOW)`

Neither half of that line is what the operator will see.

**(a) The number renders as `600k`, not `600,000`.** The formatter is `er`, imported from
`chunk-hf9yhhhe.js` by *both* `/autocompact` modules (verified: the text module's import at
byte 207500900, the dialog module's at 223234842). Its implementation (byte 190305571):

```js
var J={notation:"compact",maximumFractionDigits:1,minimumFractionDigits:1},
    Q={notation:"compact",maximumFractionDigits:1,minimumFractionDigits:0};
function gs(t){let e=t>=1000;return S4r("en-US",e?J:Q).format(t).toLowerCase()}
function er(t){return gs(t).replace(".0","")}
```

Reproduced with node: `er(600000)` -> `"600k"`, `er(100000)` -> `"100k"`,
`er(1000000)` -> `"1m"`.

So the verification line the plan documents will read
**`600k tokens (from CLAUDE_CODE_AUTO_COMPACT_WINDOW)`** — the exact spelling this entire
plan exists to ban from the environment variable. An operator who reads `600k` in that
panel and compares it with the plan's `600,000` will either conclude the fix failed, or
"helpfully" tidy the launcher back to `600k`. This is the most dangerous sentence in the
diff.

Corollary worth recording: on 3.58.0 that line read
**`100k tokens (from CLAUDE_CODE_AUTO_COMPACT_WINDOW)`** — not `100,000 tokens` as
`PLAN.md:226` and `JOURNAL:263` state.

**(b) In an interactive session it is a dialog subtitle, not a printed line.** There are two
`/autocompact` command objects at byte 198714136:

- `xYn` — `type:"local-jsx"`, `isEnabled:()=>!Te()`, loaded via the registry
  `ahe.autocompact` -> `chunk-edfdsfsj.js`;
- `xBe` — `type:"local"`, `isEnabled(){return Te()||Wn()}` -> `chunk-mar97ans.js`.

`Te()` is `!isInteractive()` (byte 189316137); `Wn()` is
`caps().workspace==="remote"` (byte 189334903). In a local interactive CCY TUI both are
false, so **the JSX dialog is the enabled variant**. That module (ending byte 223248067,
firing `tengu_autocompact_dialog_opened`) renders:

- panel title `Auto-compact window`
- subtitle `Current setting: 600k tokens (from CLAUDE_CODE_AUTO_COMPACT_WINDOW)`
- warning `CLAUDE_CODE_AUTO_COMPACT_WINDOW is set and takes precedence. Unset it to change this setting here.`

The literal `Auto-compact window: <n> tokens (from ...)` is produced by `g()` in the *other*
module — the non-interactive / remote-workspace path (byte 207501216). Documenting it as
the thing to read is the same class of error as the previous three: a string recovered
correctly and attributed to the surface the author assumed.

Also: when `configured > window` the render appends ` - capped to <X> by model`. Tell the
operator that a capped suffix is expected behaviour on a sub-600k-context model, not a
failure.

**Fix**: state what the dialog actually shows, including `600k`, and add the explicit note
that `600k` in that panel is the *rendering* of `600000` and must never be copied back into
the variable. Worth adding too: `/context` shows `Auto-compact window: 600k tokens` (byte
222981519) — the number without the source, a cheaper second corroboration.

## Should fix

### 3. `research/auto-compact-window-facts.md` — the correction landed in §2 only; §1 and §3 still carry the same misattribution, including the exact `/config` claim 3.58.3 retracted

- `:24-27` (§1): "the strings recovered around it are the `/config` UI copy for the setting"
  — they are not; they are the `/autocompact` command and dialog modules' strings.
- `:80` (§3): "Recovered `/config` string, verbatim" — that string is returned by `ort()`,
  the `/autocompact` handler (byte 207502177), and by the `/autocompact` dialog (byte
  223248067). Not `/config`.
- `:84-87`: "The `/config` panel labels its sources distinctly — `... tokens (from
  CLAUDE_CODE_AUTO_COMPACT_WINDOW)` vs ..." — verbatim the sentence that produced round 6's
  finding 5 and this session's 3.58.3 retraction, still live in the file every task cites as
  its evidence trail.
- `:89-90`: "a project can no longer change the window from the settings UI" — it never
  could from the settings UI.

The only auto-compact row in `/config` is at byte 207546212:
`"Auto-compact", value: s.autoCompactEnabled, type:"boolean"`. A toggle. Nothing else.

This is round 6's finding 3 recurring in the same file. Its own §2 banner says "each
section below is open to the same error" — and then §1 and §3 were left containing live
instances of it. The lesson was written down beside the thing it fixed and never
generalised, which is the recurrence pattern `CLAUDE/AgentNotes.md` names explicitly.

### 4. `docs/ccy-changelog.md:80` — the 3.58.3 entry claims the changelog is fixed; the changelog's own instance is not

`:22-23` says "Both this changelog and `docs/ccy.md` said the environment variable takes
the window out of `/config`'s hands." `docs/ccy.md:591` was fixed.
`docs/ccy-changelog.md:80`, inside the 3.58.0 entry, still reads "the window can no longer
be changed from `/config`". The supersession banner at `:66-68` enumerates only the value
and the grammar — it does not cover this claim, and by enumerating it implicitly invites
the reader to trust everything it left out.

### 5. `PLAN.md` Task 4.3 contradicts itself twelve lines apart, and `PLAN.md:112` is uncorrected

`:212-214` correctly states `/config` never had the row. `:224-228` then says "the original
`/config` requirement would have caught the whole defect outright — on 3.58.0 it would have
read `100,000 tokens (from CLAUDE_CODE_AUTO_COMPACT_WINDOW)`". False on both counts:
`/config` would have shown nothing at all, and the string would have been `100k`, not
`100,000`. Round 6's paragraph was carried over verbatim into a task that now retracts its
premise.

`PLAN.md:112` (Task 1.5) still reads "Losing the `/config` control for the window is the
accepted consequence".

### 6. `JOURNAL/` has no entry for the last three commits, and its newest entry still states the `/config` account

`be69ead2` (3.58.3, the `/autocompact` discovery), `8488e96e` (issue 46 corrected) and
`569f8f57` (Task 4.2 discharged) touched no journal file. The most recent entry
(`:261-268`, `:280`) is the round-6 response, which asserts "`/config` renders `source`
*and* the resolved number" and "Task 4.3 is reopened with the `/config` requirement
restored". Leaving that text is correct — the journal is append-only — but the retraction
owed to it has not been appended, so the plan's narrative record currently ends on a claim
the code refutes. Task 3.6's discharge is in the same position: `PLAN.md:174` records the
comment URL, the journal does not.

### 7. `00112/deploy.bash:104-118` — the recap parse works, but an empty recap passes vacuously and the pass line asserts a universal over an unstated population

The parse itself is sound against real output, and I checked it rather than assumed it.
`ansible.cfg` sets `stdout_callback = ansible.builtin.default`. In the installed ansible
(`/root/.local/pipx/venvs/ansible`), `default.py:302-321` emits `"%s : %s %s ..."` with
`hostcolor()` padded to `%-37s`, and `utils/color.py:95` only wraps in ANSI
`if num != 0`, so `changed=0   ` is never coloured and the literal `changed=0 ` is always
present verbatim. `^[^ ]+ +: +ok=` matches the coloured hostname because escape bytes
contain no spaces. `changed=10` correctly fails to satisfy `changed=0 `. `screen_only=True`
suppresses only the ansible *log file*, not stdout, so `> "${secondLog}" 2>&1` captures it.
**The parse works.**

The defect is the population. `if grep -E '^[^ ]+ +: +ok=' ... | grep -qv 'changed=0 '` is
false when the first grep matches *nothing*, and the script then prints
`[idempotency] second pass: changed=0 on every host.` The preceding guard only proves a
`PLAY RECAP` *banner* exists — and Ansible prints that banner with zero host lines whenever
no hosts matched, because the loop at `default.py:306` iterates `stats.processed`, which is
empty. The play is `- hosts: desktop`; any inventory or group change that empties that group
turns the assertion green and silent.

`:78` enumerates four failure modes — "a non-zero run, a missing log, an absent recap and a
non-zero changed count". The fifth, a recap with no hosts in it, is the one that reports
success. Fix: count the matched host lines, refuse zero, and make the pass line state the
number (`changed=0 on all N host(s)`), per the `COVERAGE: n of m` convention
`qa-version-pins` already prints.

### 8. `untracked/meta-deploy.bash --only` — the unnamed schedule really is unchanged, but a triage-only plan can never fail the batch, and a partial `--only` match is unguarded

Confirmed on the code, not the commit message: `PLAN_SEARCH_ROOTS+=("${PLAN_ROOT}/Completed")`
at `:51-53`, the status-filter bypass at `:62-66`, and the triage-counts-too rule at
`:72-76` are each gated on `${#ONLY_PLANS[@]} -gt 0`. **Without `--only`, nothing changed.**

Two problems with the named path:

- **A triage-only `--only` run always exits 0.** `:155-161` and `:185-191` record a non-zero
  `triage.bash` as a `note` and never increment `BAD`. For a plan with no `deploy.bash`,
  triage is the *only* unit, so the batch prints `all 2 unit(s) passed` whatever triage did.
  Two of the three plans this change was made for — `00066` and `00080` — are triage-only
  (checked: each ships `triage.bash` and nothing else; all three are in the Active root, not
  `Completed/`). Plan 00130's entire question is "does this script reach its own last line",
  and the harness change made to answer it cannot report the answer in its exit status. The
  `:152-154` rationale ("a fact-finder that could not collect everything is not a reason to
  refuse to deploy") is about gating a *deploy*; it does not transfer to the case where
  there is no deploy.
- **`:82-88` guards the zero case and not the partial.** "An `--only` that matched nothing
  is a typo" — but `--only 00066 --only 00079 --only 0080` runs two plans, exits 0, reports
  success, and silently never runs the third. This is the guard-the-empty-case,
  miss-the-partial shape from `CLAUDE/AgentNotes.md`, inside a script written to close a
  coverage gap. Fix: assert every `--only` argument selected a plan, and print
  `selected N of M named`.

Incidental: for a triage-only plan the "before" and "after" triage runs execute the same
read-only script twice with nothing in between.

Separately — `meta-deploy.bash` is gitignored. A harness that plan documents now instruct
operators to invoke by name (`00130/PLAN.md`) and that commit messages describe is not
transient plan scaffolding; per `CLAUDE/Plan/CLAUDE.md` a permanent harness belongs in
`scripts/`. Pre-existing rather than introduced here, but it means this change is
un-reviewable by anyone else and dies with the container.

### 9. `PLAN.md:262-278` Delivery and Milestones has no commit hash for 3.58.2 or 3.58.3

`8c87c646` was added (round 6 finding 10 — fixed). The final bullet describes the 3.58.2
correction with no hash, and the 3.58.3 `/autocompact` correction — a separate CCY release
with its own changelog entry — has no milestone line at all. Every other entry in that list
carries its hash.

## Nits

- `.claude/hooks-daemon.yaml` is **still** modified and uncommitted (round 6 nit 4, not
  fixed). The change is substantive — the worktree-seed entry for
  `.claude/hooks-daemon.env` made `optional: true` with a six-line rationale — and it has
  now survived six commits sitting in the working tree.
- The issue-46 comment says `--autocompact` "write[s] the `autoCompactWindow` **setting**".
  `ort()` does persist via `Zt("userSettings", ...)`, but the `--autocompact` CLI flag (byte
  204874842) only parses with `Flt` into a session option; it does not write settings.
  Minor, but it is public.

## Round 6 findings, walked

| # | Round 6 finding | State |
| - | ---------------- | ----- |
| 1 | "600-token window" is false | **fixed** — every survivor is inside a retraction |
| 2 | Fabricated grammar in public issue 46 | **fixed** — comment posted, and accurate |
| 3 | Stated root cause wrong / research file unexamined | **partial** — journal corrected, research §1/§3 still wrong (item 3) |
| 4 | Research file + Established facts assert falsified equivalence | **partial** — `PLAN.md:42-47` and research §2 fixed; §1/§3 not |
| 5 | Task 4.3 closure was a redefinition | **fixed in substance** — unticked, re-pointed at `/autocompact`; literal wrong (item 2), `:224-228` self-contradictory (item 5) |
| 6 | Success criterion 2 ticked on non-evidence | **fixed** — unticked; literal wrong (item 2) |
| 7 | SC1 claimed an unsourced 3.58.1 re-confirmation | **fixed** — and independently verified (below) |
| 8 | `qa-all.bash` ticked while the commit said it was red | **fixed** — it passes now, exit 0 |
| 9 | `CLAUDE/Plan/README.md:37` advertises `=600k` | **fixed** — now `600000` |
| 10 | Milestones missing the 3.58.1 line | **partial** — `8c87c646` added; 3.58.2/3.58.3 hashes absent (item 9) |
| 11 | Task 3.2 ceiling "defaulting to `600k`" | **fixed** — now `600,000` |
| nit 1 | 3.58.0 entry not marked superseded | **fixed** — but the banner misses the `/config` claim (item 4) |
| nit 2 | `docs/ccy.md` sources the rule to a web page | **fixed** — `:582-584` cites `zv`, `tZ`->`zl`, `Z1e=1e5` |
| nit 3 | "bare 100-1000 meaning thousands" unsourced | **fixed** — and confirmed against `Flt` (byte 196880814) |
| nit 4 | `.claude/hooks-daemon.yaml` dangling | **not fixed** |

## Checked and clean

- **The 100,000-floor account is correct**, re-derived independently: `zv` (196882539) ->
  `tZ` (194168301) -> `zl`/`N` (189515398 / 189515248), with `Z1e=1e5` and `kXe=1e6`
  (189727644). `zl("600k")` -> `N` returns `undefined` via a bare `return`, so `??` falls
  through — *not* `NaN`, which would have made the branch skip rather than floor ->
  `parseInt("600k",10)`=600 -> `Math.max(1e5,600)`=**100000**. Round 6's quotes are
  verbatim-accurate. The account is stated correctly in `docs/ccy-changelog.md:35-58`,
  `docs/ccy.md:564-584`, `claude-yolo:3129-3133`, `PLAN.md:42-47`, `research/...:38-54`, and
  the journal's correcting entry.
- **The 600-token claim is eradicated.** Repo-wide grep: every surviving occurrence sits
  inside a retraction (changelog 3.58.2, journal `:234`/`:240`, `PLAN.md:277`) or the
  round-6 report. The original at `JOURNAL:188` is correctly left in place under
  append-only discipline.
- **The issue-46 correcting comment is accurate.** Fetched with
  `gh issue view 46 --comments` and checked claim by claim against the binary: plain digits
  only; no `auto` (`parseInt("auto")`=NaN -> `status:"invalid"` -> branch skipped); helper
  shared with `BASH_MAX_OUTPUT_LENGTH` (byte 194168650); `6e5` and group separators;
  `600k`->600; floored 100,000 and capped 1,000,000; a rejected value has no effect at all;
  the `auto`/`500k`/`200`-shorthand grammar belongs to `Flt` and `ort` (`Flt` at 196880814:
  `endsWith("k")` -> x1000, bare `s>=100&&s<=1000` -> x1000); `600000` not `600k`. It carries
  no `/config` claim, so it did not inherit the fourth error. **This is the one artefact in
  the plan that was right first time.**
- **Task 4.2's re-tick is sound, and is not the 3.58.0 error recurring.** Verified
  first-hand from this session: `CLAUDE_CODE_AUTO_COMPACT_WINDOW=600000`, and
  `/workspace/.claude/ccy/ccy.env` contains no `AUTO_COMPACT` line. The 3.58.0 failure was
  not that 4.2 went green — 4.2 measured what it says it measures and was correct — it was
  treating that green as covering 4.3. `PLAN.md:202-206` now states the limit explicitly and
  leaves 4.3 open, which is the correct discharge. A different error, not the same one.
- **Version-bump discipline is clean across 3.58.1 -> 3.58.2 -> 3.58.3.** Each bump updates
  `CCY_VERSION` *and* its one-line comment (`claude-yolo:17`), each has a matching
  `docs/ccy-changelog.md` entry, no `lib/*.bash` was touched, and no image content changed —
  so leaving `REQUIRED_CONTAINER_VERSION` and the Dockerfile LABEL alone is correct.
- **Launcher correctness**: `claude-yolo:3168` —
  `-e "CLAUDE_CODE_AUTO_COMPACT_WINDOW=${CLAUDE_CODE_AUTO_COMPACT_WINDOW:-600000}"`. Correct
  value, correct idiom, both override paths intact. The banner comment at `:3121-3140` is
  accurate, including the `/autocompact`-reports-the-override line (verified: `ort` returns
  "CLAUDE_CODE_AUTO_COMPACT_WINDOW is set and takes precedence" when `source==="env"`, and
  the dialog freezes its selector on `h=c==="env"`).
- **IaC placement**: no playbook, task or vars file touched by these five commits; no
  Ansible run in the container; `00112/deploy.bash` is plan-local, tracked `100755`,
  `shellcheck -x` clean, and correctly built on `_planlib.inc.bash` (`plan_mode deploy`,
  `plan_require_host`, `plan_start_log`). Not extracting a new play was the right call.
- **Fail-fast**: no `failed_when`, `ignore_errors` or skip-and-continue introduced.
  `00112/deploy.bash`'s four explicit exits are the right shape; the fifth is missing (item 7).
- **Plan Commit Rule**: no untracked `CLAUDE/Plan/` directories; `git status` clean apart
  from `.claude/hooks-daemon.yaml`.
- **Public-repo safety**: no usernames, home paths, hostnames, private IPs or secrets in the
  diff or in the posted issue-46 comment.
- `CLAUDE/Plan/README.md:37` now reads `600000`.

## Mechanical gates

- `scripts/qa-all.bash`: **PASS** — exit 0, `QA passed: 995 files checked`;
  `docs: VENDORED: 8 verified, 0 unverifiable`.
- `hooks-daemon plan-qa --sweep`: exit 1, 8 advisories, **none against Plan 00133, 00112 or
  00130** (7 journal-ordering / path-existence on other plans, 1 journal-freshness).
- `ansible-playbook --syntax-check`: **N/A, stated rather than skipped silently** — none of
  the five reviewed commits touches `playbooks/`, `tasks/`, `vars/` or `environment/`.
  `qa-ansible-syntax` ran repo-wide inside `qa-all.bash` regardless: 82 playbooks OK (79
  under `playbooks/imports/`, 3 elsewhere).
- `qa-helper-tests.bash`, `helpers.gnome.check_extension_compat`, extension ESLint: **N/A** —
  no `helpers/`, `tests/helpers/` or `extensions/` change in these commits. All three ran
  inside `qa-all.bash` anyway and passed (1645 helper tests in 68 modules; 5 extensions).
- `shellcheck -x CLAUDE/Plan/00112-.../deploy.bash`: clean.
