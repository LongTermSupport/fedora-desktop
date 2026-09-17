# `CLAUDE_CODE_AUTO_COMPACT_WINDOW` — established facts

Everything below was **measured**, not assumed. Each claim names how it was
established so a later reader can re-run the check rather than trust this file.

> **This title over-promises, and §2 is the proof.** Measuring a string is not the same as
> establishing what it governs. §2's string was recovered correctly and attributed to the
> wrong code path, and because this file is the evidence trail every task cites, the error
> propagated into a shipped default. "Established" here means *a string was recovered*;
> it does not mean *the claim about the variable was tested*. Re-verify against the
> function that emits the string before relying on any section.

## 1. The variable is real and is honoured

**Established.** The name appears as a literal string in the shipped Claude Code
CLI, alongside the code paths that read it and the user-facing text that
describes it.

- Binary: `/usr/local/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`
- Version: `2.1.274` (from the adjacent `package.json`)
- Method: `grep -abo` for the literal name to get byte offsets, then `dd` to
  extract the surrounding bytes at each offset.

It is not an inert or speculative name: the strings recovered around it are the UI copy
for the setting, the parse-error message, and the precedence notice. A variable with a
bespoke parse error and a precedence notice is a variable that is read.

> **Corrected**: this paragraph said `/config` UI copy. It is not — those strings belong to
> the `/autocompact` command module. `/config` carries only the `autoCompactEnabled` on/off
> toggle. The same mistake as §2, made about a different surface: a string was located and
> the module owning it was not read.

## 2. The unit is **tokens** — and this section's grammar belongs to a different code path

> **CORRECTION — this section generated the plan's central defect. Read it before the
> text below.** The recovered string is genuine and is still quoted verbatim here. It
> describes the **`/autocompact` slash command**, not the environment variable. The
> section applied one code path's grammar to another, and everything downstream — the
> launcher default, `docs/ccy.md`, `PLAN.md`, and the body of upstream issue 46 —
> inherited it.
>
> The two paths, both re-extracted from `claude.exe` 2.1.274:
>
> - **the slash command** is `ort()` at byte 207501200. It parses with `Flt`, writes
>   `userSettings.autoCompactWindow`, and emits the string below on failure. `auto`,
>   `500k` and the `200`-means-200,000 shorthand are *its* grammar;
> - **the environment variable** is read by `zv` at byte 196882539, which parses with the
>   generic helper `tZ` (byte 194168301) — the same one used for
>   `BASH_MAX_OUTPUT_LENGTH`. `tZ` calls `zl` (byte 189515398):
>   `zl(e){let n=String(e).trim();return N(n)??parseInt(n,10)}`. `N` accepts plain
>   integers, scientific notation (`6e5`) and group separators — **no `k`/`M` suffix**.
>   So `zl("600k")` falls through to `parseInt("600k",10)` = `600`.
>
> `zv` then applies `Math.max(Z1e, effective)` with `Z1e=1e5` (byte 189727644), so the
> environment variable **cannot produce a window below 100,000 tokens**. `600k` therefore
> gave a 100,000-token window — not 600,000 as intended, and not 600 either. An invalid
> value is different again: `status:"invalid"` skips the whole branch and the variable has
> no effect at all.
>
> The lesson generalises to every other claim in this file: a string recovered from the
> binary is evidence about **the code path that emits it**, and nothing else. Each section
> below names its string but not the function around it, so each is open to the same error.

Recovered parse-error string, verbatim — **emitted by `ort()`, the `/autocompact` slash
command**:

> `Couldn't parse '…'. Expected 'auto' or 100k..1M tokens (e.g. 500k, 200000, or 200 as shorthand)`

So, **for the slash command and the `--autocompact` flag only**:

| Form     | Meaning                        |
| -------- | ------------------------------ |
| `auto`   | Let Claude Code pick per model |
| `500k`   | 500,000 tokens                 |
| `200000` | 200,000 tokens                 |
| `200`    | Shorthand for 200,000 tokens   |

**For the environment variable**: a plain integer, scientific notation, or group-separated
digits. Floored at 100,000, capped at 1,000,000. `600000` is the only spelling of this
plan's intended figure that the variable actually accepts.

## 3. Precedence inside Claude Code: the env var wins over the setting

Recovered string, verbatim — returned by the **`/autocompact`** command, not `/config`:

> `CLAUDE_CODE_AUTO_COMPACT_WINDOW is set and takes precedence. Unset it to change this setting.`

The settings-file equivalent is `autoCompactWindow`. `/autocompact` labels its sources
distinctly — `… tokens (from CLAUDE_CODE_AUTO_COMPACT_WINDOW)` vs `… tokens (from settings)`
vs `… tokens (default for this model)` — so a human can always see which source won. The
number is rendered with compact notation, so 600,000 reads back as `600k`: the readback
spelling is not an input spelling.

**Consequence for this plan:** setting the env var means a project can no longer change the
window from `/autocompact`, which reports the override instead of applying it. That is the
intended trade for a declarative default, but it should be documented so it is not
discovered as a surprise.

> **Corrected**: this section attributed all of the above to `/config`, and the plan and the
> user-facing docs inherited it until an operator opened `/config` and found no such row.

## 4. The effective threshold is `min(window, model context window)`

Recovered strings: `… capped to … by model`, and:

> `Auto-compact summarizes the conversation when context usage approaches this limit. The actual threshold is the minimum of this setting and your model's maximum context window.`

**This is the single most important caveat in this plan.** On a model with a
200k context window, `600000` is clamped to 200k and the setting changes nothing —
it is not harmful, but it is inert. `600000` only bites on a 1M-context model,
where it makes a session compact at 600,000 rather than running on toward 1M.

A related recovered string confirms this is the variable's intended use for
exactly that case — it is named as the remedy when a 1M cap is not otherwise
enforced:

> `CLAUDE_CODE_DISABLE_1M_CONTEXT is set, but the …K limit isn't enforced for …, so this session can grow past it. To enforce it, set CLAUDE_CODE_AUTO_COMPACT_WINDOW=… (or the autoCompactWindow setting).`

So the operator's intent is coherent and matches the variable's designed
purpose. The plan should simply **say** that the default is a 1M-context guard
and a no-op elsewhere, rather than implying it changes every session.

## 5. Upstream's own recommendation is `auto`

Recovered strings, verbatim:

> `The auto setting picks a window tuned for your model and is strongly recommended for the best cost and performance.`

> `Overriding auto may result in high token usage, especially when resuming long sessions.`

These strings come from the same `/autocompact` module as §2 and §3, so they describe the
**setting**. `auto` is not a value the environment variable accepts at all — it fails the
numeric parse, and a failed parse skips the branch entirely, leaving the variable with no
effect. "Set it to `auto`" is therefore not available as advice for the env var, which
matters for §7's daemon question.

This does not block the plan — a deliberate, documented override is a legitimate
choice, and the operator has made it. It does mean two things:

1. The CCY default should carry a comment saying **why** we override a setting
   upstream calls recommended, so it is not silently reverted later.
2. It creates a genuine open question for the daemon half, recorded in §7.

## 6. Where CCY would set it — established, with the precedence chain

Two placements were considered. Both were checked against the actual code.

### Chosen: the launcher's `-e` block

`files/var/local/claude-yolo/claude-yolo`, in the `container_cmd run` argument
list, alongside the existing forwarded Claude Code environment. The file already
uses exactly the required idiom one line away:

```bash
-e "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=${CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS:-1}" \
```

The `${VAR:-default}` form means a host export overrides the CCY default.

### Rejected: `ENV` in the Dockerfile

`files/var/local/claude-yolo/Dockerfile` does carry `ENV` lines, so it would
work — but it bakes the value into the image, forces a rebuild to change it,
loses the host-override idiom, and would drag in a
`REQUIRED_CONTAINER_VERSION` bump. The launcher placement needs neither.

### The precedence chain, end to end

The entrypoint sources the project's `ccy.env` **after** the launcher's
environment is already in place, and **before** it `exec`s `claude`:

| Step | Location                                      | Effect                                     |
| ---- | --------------------------------------------- | ------------------------------------------ |
| 1    | `claude-yolo` `-e` flag                       | CCY default `600000` enters the container  |
| 2    | host export before launch                     | Overrides step 1 via `${VAR:-600000}`      |
| 3    | `entrypoint.sh` sources `.claude/ccy/ccy.env` | Overrides steps 1–2 — **the project wins** |
| 4    | `entrypoint.sh` `exec`s `claude`              | Final value is what Claude Code reads      |

So a project's `ccy.env` genuinely overrides the default, as the brief requires
— because it is sourced later in the same shell.

**Note on the brief's line reference.** The brief cited `entrypoint.sh:288` from
Plan 00098's research. In the current file the `ccy.env` source block is at
**`entrypoint.sh:382-391`** (`_ccy_env_file="/workspace/.claude/ccy/ccy.env"` at
386). The mechanism is exactly as the brief described; only the line number has
moved. Cite the marker, not the line.

**A `ccy.env` override must use `export`.** `entrypoint.sh` already carries a
comment recording that a value set in `ccy.env` without `export` would not
survive the `exec` into `claude`. Any documented override example must therefore
be written `export CLAUDE_CODE_AUTO_COMPACT_WINDOW=…`.

### Documentation homes, both already existing

- `docs/ccy.md` — the **"Claude Code environment CCY sets"** table, which already
  documents this precedence rule in prose and lists the sibling variables. A new
  row belongs here.
- `docs/ccy-changelog.md` — required by the `CCY_VERSION` bump.

`CCY_VERSION` is currently `3.57.0`. Editing `claude-yolo` **requires** bumping
it; the script self-checks the version against a stored hash and refuses
otherwise.

## 7. The daemon half — where it goes, and the one open question

**The daemon is an external upstream project.** This repo vendors a copy under
`.claude/hooks-daemon/` but cannot patch it. The upstream tracker is
<https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon>.

### There is an exact existing home for the check

`src/claude_code_hooks_daemon/handlers/session_start/optimal_config_checker.py`
already audits Claude Code environment variables on session start. Its own
docstring lists what it covers:

1. `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`
2. Effort level
3. Extended thinking
4. `CLAUDE_CODE_MAX_OUTPUT_TOKENS=64000`
5. `CLAUDE_CODE_DISABLE_AUTO_MEMORY`
6. `CLAUDE_BASH_MAINTAIN_PROJECT_WORKING_DIR=1`

Item 4 is the same shape as the requested check: a named Claude Code env var
with an expected numeric value. **The upstream ask is therefore "add a seventh
check to this handler", not "write a new handler"** — a materially smaller and
more acceptable request, and the issue should say so.

Since Plan 00128's lean-SessionStart rework, the handler's full per-setting audit
surfaces through `cli check` rather than on every session start; the session
start itself stays quiet. The issue should ask for the new check to follow
whatever the handler's current convention is, not prescribe a surface.

Per-project override already exists at the handler level:
`.claude/hooks-daemon.yaml` configures `optimal_config_checker: {enabled: true, priority: 52}`. The issue should ask for a **per-check** override key so a
project can set its own threshold, which is the "projects can override this in
their hooks daemon config" half of the brief.

### The open question the owner must settle before the issue is filed

The brief says warn when the window is **not set or greater than 600k**. But §5
establishes that `auto` is upstream's own recommended value, and §4 establishes
that on a 200k model any value at all is clamped. So a literal reading would
have the daemon warn at a setting upstream recommends.

The issue body should present this as the question it is, rather than asserting
one answer:

- Should `auto` count as "set" and pass?
- Should the threshold be configurable per project, defaulting to 600,000?
- Should the check be skipped when the resolved model window is below the
  threshold, where the setting is inert anyway?

### Filing constraint — this is not optional

Rule `R-UPSTREAM-ISSUE-UNVERIFIED-BODY` blocks `gh issue create` against that
tracker with any body a generator did not produce. That tracker is **public** and
an issue cannot be retracted. The body **must** be generated with:

```bash
hooks-daemon issue-report
```

which writes a file under `untracked/issue-reports/`. A hand-drafted body cannot
be filed, and pasting one is the specific thing the rule exists to stop.

**Precedent:** Plan 00075 was closed after discovering its hand-drafted
`upstream-report.md` could no longer be filed that way. Do not repeat it — do
not hand-draft a body anywhere in this plan folder.
