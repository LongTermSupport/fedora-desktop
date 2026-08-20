# QA — Quality Assurance Scripts

## Primary Rule

**ALWAYS run QA before committing changes to Bash, Python, or Ansible files.**

**ALWAYS and ONLY use this single command:**

```bash
./scripts/qa-all.bash
```

**NEVER use individual scripts directly** (`qa-bash.bash`, `qa-python.bash`, `qa-patterns.bash`) — always use `qa-all.bash`.

---

## What qa-all.bash Runs

`qa-all.bash` runs six stages and merges their JSON into `/tmp/qa-results.json`. A
missing **required** tool makes a stage (and the whole run) exit `2`; a real
analyser crash (e.g. ruff/shellcheck exit ≥ 2) is a hard failure, never silently
treated as "0 issues".

| Script                   | Checks                                                                                                                                                                                                                                                                                                                           | Files                                                                                              |
| ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `qa-bash.bash`           | `bash -n` (always) + shellcheck (**required** — exits 2 if absent, Plan 00075). Exits 2 if discovery finds **0 files**, and (Plan 00076) if it misses **any tracked shell script**. **shellcheck `error` AND `warning` findings GATE** (raised in Plan 00075 — SC2155 is this repo's own defect class); `info`/`style` advisory. | Repo-owned bash (excludes `roles/vendor`, `.claude/hooks-daemon`, `.claude/ccy`, `.claude/skills`) |
| `qa-python.bash`         | `python3 -m py_compile` + ruff (ruff exit ≥ 2 = hard fail; no `--fix` mutation in the check path). Exits 2 if discovery finds **0 files**, and (Plan 00081) if it misses **any tracked Python file**                                                                                                                             | Repo-owned Python files — discovered by extension **or shebang, regardless of file mode**          |
| `qa-patterns.bash`       | Semgrep rules from `.semgrep/bash-conventions.yml` (`\|\| echo` and other error-hiding patterns). Scans a temp mirror so coverage does not depend on file mode, and exits 2 if any discovered file is absent from `.paths.scanned` (Plan 00076)                                                                                  | Repo-owned bash                                                                                    |
| `qa-ansible.bash`        | Fail-fast grep (`failed_when: false`/`ignore_errors` without same-line `# FAIL-FAST-OK:`, case-insensitive), **self-default vars** (`x: "{{ x \| default(…) }}"` — the 2.19 recursive-loop footgun `--syntax-check` can't see), **plus** playbook shebang + exec-bit hygiene                                                     | `playbooks/ tasks/ vars/ environment/ roles/` (excludes `roles/vendor`), `*.yml`/`*.yaml`          |
| `qa-ansible-syntax.bash` | `ansible-playbook --syntax-check` on every playbook (files with a top-level `- hosts:`). Parse-only — safe in the CCY container                                                                                                                                                                                                  | `playbooks/playbook-main.yml` + standalone `playbooks/imports/**`                                  |
| `qa-js.bash`             | `node --check` on repo JS + `eslint .` in `extensions/`                                                                                                                                                                                                                                                                          | Repo-owned `.js` (excludes vendor/node_modules) + `extensions/`                                    |

Two further gates run inside `qa-all.bash` as **hard, non-structural** checks —
they are deliberately not jq-merged stages, so they cannot disturb the positional
`.[0]..[5]` JSON merge. Either one fails the whole run immediately:

| Gate                            | Checks                                                                                     |
| ------------------------------- | ------------------------------------------------------------------------------------------ |
| `qa-nokill-containerwatch.bash` | the container-watch watchdog has gained no process-termination call site                   |
| `qa-deployed-drift.bash`        | every repo-owned `files/home/.local/bin/` script matches its deployed `~/.local/bin/` copy |

### All three source gates assert their own coverage (Plans 00076, 00081)

`qa-bash.bash`, `qa-patterns.bash` and `qa-python.bash` share one discovery
library, `scripts/qa-discovery.bash` — one mechanism, one "is this a shell
script / is this Python" predicate. Change discovery there, not in a gate.

The two languages carry **separate exclusion lists** over that one mechanism,
and the difference is deliberate: `QA_PY_EXCLUDE_DIRS` excludes only
`.claude/ccy/plugins` and `.claude/ccy/file-history`, not the whole `.claude/ccy`
tree, because `.claude/ccy/claude-supervise.py` is tracked and repo-owned.
Unifying the lists would have *dropped* a real file from the Python gate — this
section's own defect, committed inside the fix for it.

`qa-python.bash` joined them in Plan 00081. It had the identical defect and had
not learned from 00076: it discovered by extension **or the execute bit**, so six
tracked Python programs — mode 0644 with a `#!/usr/bin/env python3` shebang,
deployed 0755 by their plays — were never compiled or linted. Widening discovery
took it from 35 files to 41 and surfaced **31 real ruff findings** in ~4,000
previously-unread lines, while the old gate printed `✓ python: 35 files OK`. This
document's own "For Python files that use external libraries" note named
`wsi-stream` as *the* example of Python needing care; it was one of the six.

This exists because the two gates identified bash by **filename extension or file
mode**, and 27 of this repo's scripts have neither a `.sh`/`.bash` extension nor
an execute bit — including the 140 KB ccy launcher. They were never opened by
`bash -n`, shellcheck, or semgrep, while the gates printed `125 files OK`. Zero
coverage was already treated as a broken gate; *partial* coverage was not.

Two things changed, and neither is a file-mode change:

- Discovery keys on a **shell shebang, regardless of mode**. A `read` builtin, not
  `head`, because this runs against every file in the repo.
- Each gate **asserts its own coverage** and exits `2` on a shortfall, naming the
  files. `qa-bash.bash` compares its discovered set against every tracked shell
  script; `qa-patterns.bash` requires every file it handed semgrep to come back in
  `.paths.scanned`.

Semgrep needs the owner execute bit before it will read a shebang, so
`qa-patterns.bash` **copies** each discovered script into a temp mirror at its own
repo-relative path (appending `.bash` where it has no shell extension) and scans
that, mapping findings back to real repo paths. `chmod +x` was rejected as the
fix: `/var/local/colours` and `/var/local/ps1-prompt` are deployed `0644` because
they are sourced libraries, so an execute bit would be a lie told to a linter and
would still miss every future sourced library.

**If a gate reports a shortfall, widen the discovery — never exclude the file.**

> **This is one instance of a general defect class**, and treating it as a
> local quirk of these two gates is why it kept recurring elsewhere — four more
> instances surfaced in Plans 00079/00080. See
> [AgentNotes.md → *A partial result read as a complete one*](AgentNotes.md#a-partial-result-read-as-a-complete-one--guard-the-empty-case-miss-the-partial).

#### `.paths.scanned` is not proof a file was analysed

Semgrep lists a file it could not **parse** as scanned, returns zero findings for
it, and exits `0` — the reason appears only in `.errors[]`. Three of this repo's
scripts were in that state, `ftp-camera` (2,475 lines) among them, and two of
them were being scanned by the *old* gate too. All three pass `bash -n`.

`qa-patterns.bash` therefore checks the error list, and treats its two classes
differently because they mean different things:

- **`Syntax error`** — the file did not parse, and **every rule with a match in
  it lost that match** → **gating**. `SEMGREP_CANNOT_PARSE` is the exception
  list; it is **empty today** and self-expiring in both directions: an unlisted
  unparseable file fails the gate, *and* a listed file that starts parsing fails
  it until the entry is removed. Both former entries were the same construct —
  see below.

- **`PartialParsing`** — parsed apart from named ranges. Reported, not gating,
  and measured to cost **nothing** for this ruleset.

Both counts print on **every** run, pass or fail, above the `✓ patterns:` line.
A summary consisting only of a tick and a file count is the format that let this
sit unnoticed.

#### Semgrep parses a file only when a rule's regex has already matched

This is the fact that explains everything else in this section, and it was
established with a probe rule whose regex matches on every line, so the parse is
always attempted. Across ten measured (file, rule) cells the correlation is
exact:

| rule's raw regex matches in the file | parse attempted | error reported |
| ------------------------------------ | --------------- | -------------- |
| 0                                    | no              | none           |
| ≥ 1                                  | yes             | surfaces       |

So a rule that appears to "parse a file fine" may simply never have been asked.
An earlier revision of this document concluded from that appearance that
**parseability is a property of (file × rule)**; it is not, and the reasoning was
this section's own defect class one level up — an *absence of an error* read as
evidence of coverage.

#### `Syntax error` costs everything; `PartialParsing` costs nothing (today)

Both measured, not argued:

- `rclone-tail` carried a whole-file `Syntax error` and reported **0** findings
  while containing **3 real `|| echo` violations**. `rclone-cache-status` hid a
  fourth. All four were invisible for as long as the files were on the exception
  list.
- `ftp-camera` reports a `PartialParsing` range of lines 585–2486, and a probe
  for `echo` returned **293 findings against 293 raw occurrences** — 262 of them
  on distinct lines *inside* that range.

The asymmetry is because every rule in `.semgrep/bash-conventions.yml` is
`pattern-regex`, and the regex engine reads raw text — the parse tree is never
consulted for a match. A `PartialParsing` range is therefore a note about a
**future** cost: the day an AST `pattern:` rule is added, those lines stop being
covered. The report keeps the magnitude visible so that day is noticeable:

```
files/home/.local/bin/ftp-camera — 1902 of 2486 lines outside the parse tree (first gap from 585, last to 2486)
files/var/local/claude-yolo/claude-yolo — 1 of 3021 lines outside the parse tree (first gap from 1252, last to 1252)
```

Those two lines describe situations three orders of magnitude apart, and a bare
list of filenames rendered them identically. The number is the union of the
skipped ranges, so overlaps are not double-counted. The wording is
"outside the parse tree", not "not analysed" — the earlier phrasing was false in
the *alarming* direction, which is no better than false in the reassuring one.

#### The construct that defeats the bash grammar

Both former `SEMGREP_CANNOT_PARSE` entries reduced to one shape — a heredoc fed
directly to an `if` condition, with `then` on the line after the terminator:

```bash
if ! python3 - "$json" <<'PY' 2>/dev/null
...
PY
then
    echo "ERR|parse failed"
fi
```

That is valid bash (`bash -n` passes) and tree-sitter-bash cannot parse it. Feed
the heredoc to a **command substitution** instead and it parses cleanly:

```bash
if ! parsed=$(python3 - "$json" <<'PY' 2>/dev/null
...
PY
); then
    echo "ERR|parse failed"
else
    printf '%s\n' "$parsed"
fi
```

The rewrite is also better bash — the command's status and its output are
handled separately instead of the output flowing straight through the condition.

#### Two files may not claim one mirror path

The mirror appends `.bash` to a file with no shell extension, so `dir/foo` and
`dir/foo.bash` would both land on `dir/foo.bash`: the second `cp` overwrites the
first, and the mirror→repo map — merged from one object per file — keeps a single
value for that key. The overwritten script is then neither scanned **nor** named
by the coverage assertion, which reads the map's *values*. It would simply cease
to exist, and the gate would report a pass over it.

That is this section's own defect committed inside the fix for it, so a collision
is a hard failure (exit 2) naming both files rather than a silent rename. No
collision exists in the repo today; the check is there so one cannot appear
quietly.

### `qa-deployed-drift.bash` — the repo and the host must agree

This is the one QA check whose subject is the **host** rather than the source
tree. It exists because Plan 00067 fixed `files/home/.local/bin/ftp-camera` in the
repo and never ran the play that deploys it — the repo said "fixed", the machine
ran the old build, and it surfaced weeks later as a camera session that would not
copy. No source-reading check can see that: the source was correct.

It compares each repo-owned script against its deployed copy and, on a mismatch,
names the play to run — derived by searching the playbooks for the file's `src:`
path, not from a hand-maintained table. A file is checked **only when a deployed
copy already exists**, so a machine that never installed a feature is never
nagged. It self-skips in the CCY container and in a clean CI checkout, where
there is no deployed state to compare against.

**This changes when you run QA.** The table below says "before every commit", and
that is still right — but on the HOST this gate makes the repo's documented
`edit → playbook → deploy → test` order (see
[InfrastructureAsCode.md](InfrastructureAsCode.md)) **enforced** rather than
merely recommended: a changed script must be deployed before `qa-all.bash` will
pass. That is the intended sequence, not an obstruction — QA is a pre-*commit*
gate, and the workflow already puts deploy and test ahead of commit. If you are
mid-edit and want the other stages, run the deploy first; do not work around the
gate.

Run it alone with:

```bash
./scripts/qa-deployed-drift.bash
```

---

## GNOME Shell Extension JavaScript

Run ESLint via the binary directly (NOT `npm run lint` — blocked by hooks):

```bash
cd /workspace/extensions && node_modules/.bin/eslint speech-to-text@fedora-desktop/extension.js
```

---

## Helper Unit Tests + Extension Version Compatibility

Helper packages under `helpers/` are stdlib-only (`helpers/CLAUDE.md`). Their unit
tests are namespace-package modules, so `unittest discover` cannot collect them —
run them with the dedicated runner, which enumerates `tests/helpers/**/test_*.py`
and runs them by explicit module name:

```bash
./scripts/qa-helper-tests.bash
```

A separate **static** gate confirms every `extensions/<uuid>/metadata.json`
declares support for the GNOME Shell major that this branch's Fedora release ships
(`vars/fedora-version.yml`). It is session-free (unlike the runtime
`helpers.gnome.verify_extension`), so it runs in CI on the repo source:

```bash
python3 -m helpers.gnome.check_extension_compat
```

The Fedora→GNOME-Shell map lives in `helpers/gnome/fedora_compat.py`
(`FEDORA_TO_GNOME_MAJOR`). When cutting a new `F<N>` branch, add that release's
GNOME major there — an unmapped Fedora version fails the gate by design, forcing a
human to confirm the GNOME version. Both run automatically in the `helpers` CI job
(`.github/workflows/qa.yml`).

---

## CCY ctrl+z Patch

For changes to `ccy-ctrl-z-patch.js`, run the dedicated patch QA script:

```bash
# Always reinstalls the LATEST Claude Code into scripts/qa-ccy/node_modules/,
# applies the patch, and (for native builds) executes the patched binary to prove
# the same-length edit did not corrupt the embedded JS blob. Requires network.
./scripts/qa-ctrl-z-patch.bash

# `--update` is accepted for back-compat but is a no-op — every run pulls @latest.
```

---

## When to Run What

| Changed files         | QA command                                                                  |
| --------------------- | --------------------------------------------------------------------------- |
| Bash or Python files  | `./scripts/qa-all.bash`                                                     |
| Extension JavaScript  | `cd /workspace/extensions && node_modules/.bin/eslint <file>`               |
| `ccy-ctrl-z-patch.js` | `./scripts/qa-ctrl-z-patch.bash`                                            |
| Ansible playbooks     | `./scripts/qa-all.bash` (runs `qa-ansible.bash` + `qa-ansible-syntax.bash`) |

---

## What QA Catches

- ✅ Bash syntax errors (`bash -n` validation)
- ✅ shellcheck `error` **and `warning`** findings (`info`/`style` advisory). **shellcheck is required** — absent, the stage exits 2 rather than reporting a pass it did not earn (Plan 00075). The bar was raised to `warning` because **SC2155** (`local x=$(cmd)` — `local` becomes the command whose status is reported, discarding the substitution's) **is the discarded-failure-signal class**, and it was sitting in the advisory bucket nobody reads. `info`/`style` stay advisory on purpose: they are dominated by SC2016 and SC2012, where gating would trade signal for noise
- ✅ **Discarded failure signals** (Plan 00075, `.semgrep/bash-conventions.yml`) — the class where a command's failure is silently turned into data that is then trusted:
  - `bash-status-after-block` — `$?` read after `fi`/`done`/`esac`/`}`, which is the *block's* status and so is always 0 after a successful `if`. **shellcheck does not catch this even with `--enable=all`** (verified)
  - `bash-capture-discards-status` — `var=$(cmd 2>/dev/null)` with the status thrown away, so an error written to stdout becomes the value. Scoped to `files/var/local/claude-yolo/**` for now; widening is tracked in Plan 00075. Genuine cases carry a same-line `# FAIL-FAST-OK: <reason>`
- ✅ Python syntax errors (`python3 -m py_compile`)
- ✅ Common Python issues (via `ruff` — **required**; `qa-python.bash` exits 2 with an error if ruff is absent; a ruff crash, exit ≥ 2, is also a hard failure)
- ✅ Error-hiding bash patterns (`|| echo` — Semgrep, `.semgrep/bash-conventions.yml`)
- ✅ Ansible fail-fast violations (`failed_when: false` without `# FAIL-FAST-OK:` annotation)
- ✅ Ansible playbook **syntax** errors (`ansible-playbook --syntax-check`, catches the 2.19 parse hazards)
- ✅ Playbook hygiene (every `- hosts:` playbook has the `ansible-playbook` shebang + exec bit)
- ✅ JavaScript syntax (`node --check`) + ESLint across `extensions/`

## What QA Does NOT Catch (Known Limitations)

- ❌ **Runtime API incompatibilities** — e.g., calling a library method with parameters it no longer accepts
- ❌ **Import errors** — missing dependencies only fail at runtime
- ❌ **Logic errors** — code that runs but produces wrong results
- ❌ **Everything judgement-shaped** — work put in the wrong place in the IaC graph, a
  new playbook that should have been an edit to an existing one, names that describe a
  mood rather than a behaviour, a missing version bump, plan/docs drift, a self-test
  that does not exercise the code path it vouches for, or a real identifier about to be
  posted to a public surface. **Use the `qa-reviewer` agent for these** — see below.

---

## The `qa-reviewer` Agent — Required Before Marking a Plan Complete

`qa-all.bash` is mechanical: syntax, lint, greps, playbook parsing. It passes green on
changes that are structurally wrong. `.claude/agents/qa-reviewer.md` is the holistic
gate for that class of defect.

**Run it as the final step of every plan, and to review any PR or branch diff:**

> Use the qa-reviewer agent to review this plan's changes before I mark it Complete.

It is **read-only** — it reports findings with `file:line` evidence and a verdict
(BLOCK / FIX-BEFORE-MERGE / PASS WITH NITS / PASS); it never edits. Its checklist is
built from this repo's own rules and the mistakes already made here (recorded in
`CLAUDE/AgentNotes.md`), so it grows as new ones are found — when a defect gets past
it, add that case to the agent rather than only fixing the instance.

**For Python files that use external libraries** (like `wsi-stream` using RealtimeSTT):

- After editing, **manually test the script** to verify it works
- Library APIs can change between versions
- Syntax checking alone is not sufficient for integration code

---

## Example Workflow

For a file under `files/home/.local/bin/`, deploy BEFORE running QA — the
deployed-drift gate compares the repo against the host, so it fails by design
while a changed script is still undeployed:

```bash
# 1. Make changes
vim files/home/.local/bin/wsi-stream

# 2. Deploy and TEST the actual script (on HOST, not in CCY container)
ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
~/.local/bin/wsi-stream --help  # Verify it imports/runs

# 3. Run QA — now the repo and the host agree
./scripts/qa-all.bash

# 4. Only then commit
git add files/home/.local/bin/wsi-stream
git commit -m "fix: update wsi-stream"
```

For changes that deploy nothing to `~/.local/bin/` (playbooks, `scripts/`,
`helpers/`, extension JS), the order does not matter — run QA whenever, as long
as it passes before the commit.

## Rules Summary

1. **Run `./scripts/qa-all.bash` before EVERY commit** that touches Bash or Python files
2. **Run ESLint before EVERY commit** that touches extension JavaScript
3. **Run `./scripts/qa-ctrl-z-patch.bash` before EVERY commit** that touches `ccy-ctrl-z-patch.js`
4. **Fix all errors** before committing — QA failures indicate broken code
5. **Do not skip QA** — even for "small" changes
