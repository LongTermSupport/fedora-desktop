# QA Review — Plan 00133 close round 6

Reviewed: `8c87c646` plus `cb71a26f`, `a2019db4`, `2fc9a6aa`, and the Phase 2 delivery `389d4104`.

**Verdict**: BLOCK

I disassembled the shipped Claude Code CLI (v2.1.274,
`/usr/local/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`) to check the
corrected record against the code rather than against the new docs. The launcher fix is
right. **The post-mortem's account of what went wrong is factually false, and the false
account is now in a user-facing changelog, in `docs/ccy.md`, in `PLAN.md`, in the
`JOURNAL/`, and in a public upstream issue.**

## Blocking

### 1. "a 600-token window" is false. The real window was 100,000 tokens — the binary floors it

`docs/ccy-changelog.md:24`, `docs/ccy.md:572-573`,
`CLAUDE/Plan/00133-.../PLAN.md:181-182,189-190,211-212`,
`CLAUDE/Plan/00133-.../JOURNAL/00133-Journal-26-09-17.md:188,197`

Extracted from the binary at byte offset 196882539 (the resolver `zv`):

```js
if(process.env.CLAUDE_CODE_AUTO_COMPACT_WINDOW){
  let U=tZ("CLAUDE_CODE_AUTO_COMPACT_WINDOW",process.env.CLAUDE_CODE_AUTO_COMPACT_WINDOW,Z1e,kXe);
  if(U.status!=="invalid"){let he=Math.max(Z1e,U.effective);
    return{window:Math.min(m,he),configured:he,source:"env"}}}
```

`Z1e=1e5`, `kXe=1e6` (byte offsets 189727644 / 189727652). The parser chain is `tZ`
(byte 194168301) then `zl` (byte 189515398):

```js
function tZ(e,n,r,o){if(!n)return{effective:r,status:"valid"};let s=zl(n);
  if(isNaN(s)||s<=0){...status:"invalid"...}
  if(s>o){...status:"capped"...}return{effective:s,status:"valid"}}

function zl(e){let n=String(e).trim();return N(n)??parseInt(n,10)}
function N(e){ /* scientific notation (6e5), or group separators _ , NBSP — no k/M */ }
```

So `zl("600k")` -> `N` declines -> `parseInt("600k",10)` = `600`. `tZ` returns
`{effective:600,status:"valid"}`. Then `Math.max(1e5, 600)` = **100000**, and
`window = Math.min(model_context, 100000)`.

**The env var can never produce a window below 100,000 tokens.** If a value is rejected
(`status:"invalid"`) the whole branch is skipped and the env var has *no* effect at all.
Either way, 600 is impossible.

The parse-to-`600` claim is correct; every statement of its *consequence* is not.
"Compacting at roughly 600 tokens", "the behaviour of a window of 600 tokens rather than
600,000", "a session that could not hold a thought" are inventions — asserted as
established fact, in exactly the register the post-mortem condemns.

The real story is both true and more useful: 600,000 silently became 100,000, a 6x tighter
ceiling, still large enough for a session to boot and work, which is why it read as
"compacting constantly" rather than as total failure. The 100k floor is worth documenting
precisely because it is what makes a bad value survivable, and therefore easy to miss.

`docs/ccy-changelog.md:24` is the worst instance: a public release note stating a mechanism
the shipped binary refutes.

### 2. The fabricated grammar escaped the repo into a public upstream issue, and the corrected record does not address it

`untracked/issue-reports/issue-report-20260917-100110.md:45`, filed as
<https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon/issues/46> per
`PLAN.md:159-164`.

The filed body says:

> The variable's accepted grammar is `auto`, or 100k..1M expressed as `600k`, `600000` or
> `600`, so the parse has three equivalent spellings to normalise.

That is the invented equivalence, stated to an external maintainer as a specification,
asking them to build a normaliser for spellings the environment variable does not accept.
Line 18 of the same body additionally sets the requested default ceiling at `600k`.

`8c87c646` rewrote the plan's internal record and left the one copy that is outside this
repository's control untouched. There is no task, no unticked item and no journal line
acknowledging it.

**Fix**: add a task to post a correcting comment on issue 46. A correcting *comment* is not
a created issue body, so `R-UPSTREAM-ISSUE-UNVERIFIED-BODY` does not block it. Re-verify the
grammar from the binary before writing it — do not restate the web page.

### 3. The stated root cause is wrong, which is the self-serving part

`JOURNAL/00133-Journal-26-09-17.md:205-213` versus
`research/auto-compact-window-facts.md:22-40`.

The journal says the equivalence claim "was written from plausibility" and that the lookup
"was never made". Both are untrue of this plan's own record.
`research/auto-compact-window-facts.md:24-26` presents a **verbatim recovered binary
string** as its source:

> `Couldn't parse '...'. Expected 'auto' or 100k..1M tokens (e.g. 500k, 200000, or 200 as shorthand)`

I confirmed that string is genuinely present in the binary (byte offset 207502401, with an
escaped en-dash). **The research was not fabricated — it was misattributed.** The
surrounding code (byte 207501200) shows the string belongs to `ort()`, the `/autocompact`
*slash-command* handler: it fires `tengu_autocompact_command` and writes
`userSettings.autoCompactWindow`. The environment variable is parsed by a completely
different, generic numeric helper (`tZ`), shared with `BASH_MAX_OUTPUT_LENGTH`.

The defect is therefore *one grammar recovered from one code path and applied to another* —
a far more instructive lesson, and one that generalises to every other claim in that
research file.

The post-mortem instead pins the blame on a sentence in `docs/ccy.md`, a document
*downstream* of the research; calls the research's own method "never made"; and thereby
leaves the actual generator of the error unexamined and uncorrected. Given that the entry's
whole purpose is honest accounting of one's own error, this is the finding that matters
most.

### 4. `research/auto-compact-window-facts.md` and `PLAN.md`'s "Established facts" still assert the falsified equivalence, unannotated

`research/auto-compact-window-facts.md:30-40,124-125`; `PLAN.md:42-43,47,59,97,105,114,151`.

`PLAN.md:42-43` still reads, inside the section headed *"Everything in the tasks below rests
on measurements already taken, not assumptions"*:

> **The unit is tokens.** Accepted grammar is `auto`, or `100k..1M` tokens, with `600k`,
> `600000` and `600` all equivalent. This plan specifies **`600k`**.

That directly contradicts Phase 4 of the same file, 140 lines lower.

`research/auto-compact-window-facts.md:37-40` is worse: it is the document every task cites,
it is titled "established facts", and it says "**`600k` is the clearest of the three**". A
reader who follows the plan's own evidence trail re-derives the bug.

Both need correcting: the recovered string is real but belongs to `/autocompact`; the
environment variable's grammar is plain integers, scientific notation and group-separated
digits, with a hard 100,000 floor and a 1,000,000 cap.

## Should fix

### 5. Task 4.3's closure is a redefinition, and the new evidence is weaker than what it replaced

`PLAN.md:186-194`.

Original task: *"`/config` labels the window's source explicitly, and should attribute it to
the environment variable."* Replacement: behaviour. `PLAN.md:191-194` claims behaviour is "a
stronger witness than the `/config` label here". It is not, and the binary shows why.

`zv` returns a `source` discriminant and `/config` renders it verbatim. Strings extracted at
byte offset 95463974:

```
${er(o)} tokens (from CLAUDE_CODE_AUTO_COMPACT_WINDOW)
${er(o)} tokens (from settings)
${er(o)} tokens (default for this model)
```

`/config` prints **both the source and the resolved number**. On 3.58.0 it would have read
`100,000 tokens (from CLAUDE_CODE_AUTO_COMPACT_WINDOW)` — one line exposing the entire
defect, source *and* misparse, which is exactly the distinction `PLAN.md:192-194` claims a
source label "would not have exposed". The task as originally written would have caught
this.

The replacement would not. "Sessions run to normal length" is the null observation: a
rejected variable also gives normal-length sessions, so does the model default, so does
`auto`. On `600000` the evidence is symmetric between "in force" and "silently ignored" —
an absence of evidence read as evidence of absence, on the very task written to forbid that.

**Fix**: untick 4.3, restore the `/config` requirement, and record that the 3.58.0 *failure*
was in-force evidence while the 3.58.1 *success* is not. Behaviour was a stronger witness in
exactly one direction, and that direction is gone.

### 6. Success criterion 2 is ticked on the same non-evidence, and repeats the 600-token claim

`PLAN.md:210-213`:

> "Sessions running to normal length on `600000` are the evidence that the figure now means
> what it says."

They are not — see finding 5. Untick, or re-tick after a `/config` read showing
`600,000 tokens (from CLAUDE_CODE_AUTO_COMPACT_WINDOW)`.

### 7. Success criterion 1 claims a 3.58.1 host re-confirmation the commit could not yet have had

`PLAN.md:184-185`, `PLAN.md:208-209`.

`PLAN.md:185` says "Re-confirmed on 3.58.1: no override, live environment reads
`CLAUDE_CODE_AUTO_COMPACT_WINDOW=600000`." But 3.58.1 *is* `8c87c646`; deployment via the
owning playbook happens on the host after the commit. Task 4.2's own standard is "read the
live environment, do not infer it from the launcher source".

The `JOURNAL/` records the 3.58.0 read first-hand (`:163-173`, "That session is this one")
but records no equivalent capture for 3.58.1 — the only 3.58.1 statement is the unsourced
"sessions run to normal length" at `:189`. Either cite the capture or drop the claim.

### 8. `PLAN.md:222` ticks "`./scripts/qa-all.bash` passes" in the same commit whose message states it is red

I re-ran it: exit 0, `QA passed: 993 files checked`, and
`docs: VENDORED: 8 verified, 0 unverifiable (repo absent)`. The peer-worktree condition the
commit message described was genuine and was fixed two commits later by `2149af0e`. So the
tick is true *now*; it was not true when written, and the commit message said so.

### 9. `CLAUDE/Plan/README.md:37` still advertises `=600k` as the plan's deliverable

Reader-facing index row, and the first thing anyone browsing plans sees.

### 10. `PLAN.md:235-240` Delivery and Milestones has no 3.58.1 line

It still reads "Phase 2 delivered — `389d4104`: CCY sets the window to `600k`". The
correcting commit `8c87c646` is absent from the milestone list entirely.

### 11. Task 3.2's settled decision still specifies a ceiling "defaulting to `600k`"

`PLAN.md:148-154` — unchanged, and it is the text the public issue quotes.

## Nits

- `docs/ccy-changelog.md:46-51` (the 3.58.0 entry) still says "CCY now sets it to `600k`
  tokens" and "the accepted grammar is `auto`, or `100k`..`1M`". Historical entries are
  legitimately frozen, but nothing marks this one superseded and the wrong grammar is
  copyable. One "**Superseded by 3.58.1**" line resolves it.
- `docs/ccy.md:566-570` sources the corrected rule solely to an upstream web page. The
  repo's own Phase 1 method — extracting from the shipped binary — is local, reproducible,
  stronger, and yields the precise rule *plus* the 100k floor. Cite the binary; a doc page
  can change under you and cannot be re-run.
- `docs/ccy.md:569` "a bare 100-1000 meaning thousands" is an inference from "200 as
  shorthand"; the recovered string does not state that range.
- `.claude/hooks-daemon.yaml` is modified and uncommitted in the working tree (worktree-seed
  `optional: true`). Unrelated to 00133, but dangling.

## Checked and clean

- **Launcher correctness**: `files/var/local/claude-yolo/claude-yolo:3166` —
  `${CLAUDE_CODE_AUTO_COMPACT_WINDOW:-600000}`. Correct value, correct idiom, both override
  paths preserved. This is the part that actually fixes the bug.
- **Version-bump discipline**: `CCY_VERSION` 3.58.0 to 3.58.1 with an updated one-line
  comment (`claude-yolo:17`); `docs/ccy-changelog.md` carries the matching entry; no
  `lib/*.bash` touched; no image content touched, so leaving `REQUIRED_CONTAINER_VERSION`
  and the Dockerfile LABEL alone is correct.
- **`docs/ccy.md` agrees with the launcher**: table row `600000` (`:555`), prose `600000`
  (`:564`), override section `600000` (`:668`) — all match what `claude-yolo` passes.
- **IaC placement**: no playbook touched; no Ansible run in the container; no new play; the
  work sits in the file that owns the concern.
- **Public-repo safety**: no usernames, paths, hostnames, IPs or secrets in the diff. The
  filed issue body carries no identifying detail, so `PLAN.md:165-167` holds.
- **Fail-fast**: no `failed_when`, `ignore_errors` or skip-and-continue logic introduced.
- **Remaining `600k` sweep**: repo-wide grep. Every surviving instance is enumerated in
  findings 2, 4, 9, 10, 11 and the changelog nit. None in the launcher; none in
  `docs/ccy.md` except the deliberate "do not tidy the suffix back in" warning at `:573`,
  which is correct and should stay.

## Mechanical gates

- `scripts/qa-all.bash`: **PASS** (exit 0, 993 files). Shellcheck and semgrep warnings are
  pre-existing and unrelated to this diff.
- `hooks-daemon plan-qa --sweep`: exit 1, 8 advisories, **none against Plan 00133**.
- `ansible-playbook --syntax-check`: **N/A** — no playbook, task, vars or environment file in
  this diff. Stated rather than skipped silently.
- `qa-helper-tests.bash`, `helpers.gnome.check_extension_compat`, extension ESLint: **N/A** —
  no `helpers/`, `tests/helpers/` or `extensions/` change. All three ran inside
  `qa-all.bash` regardless and passed (1616 helper tests, 5 extensions).
