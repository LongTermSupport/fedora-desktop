# QA Review — Plan 00133 Phases 2–3 (commits 389d4104, 26b64939 vs 4a251dcf)

**Verdict**: PASS WITH NITS

The launcher change is correct, minimal, in the right place, correctly versioned, and the
generated issue body is clean. Everything found is documentation-link and plan-bookkeeping
precision.

## Blocking

None.

## Should fix

1. **Changelog cross-link points at the section that does not contain the example it cites**
   — `/workspace/docs/ccy-changelog.md` (3.58.0 entry, final paragraph):

   ```
   The example in [ccy.md](ccy.md#claude-code-environment-ccy-sets) uses `export` for that reason
   ```

   `### Claude Code environment CCY sets` is `/workspace/docs/ccy.md:545` and contains only the
   table row and the `#### CLAUDE_CODE_AUTO_COMPACT_WINDOW` prose. The `export` example is at
   `/workspace/docs/ccy.md:659`, under `#### Overriding the auto-compact window` inside the
   `ccy.env` section (`### 2. ccy.env — per-project environment`, line 636). The anchor should be
   `ccy.md#overriding-the-auto-compact-window`. Both anchors resolve, so nothing errors — a
   reader just lands somewhere with no example.

2. **Plan drift: a success criterion that is already satisfied is left unticked** —
   `/workspace/CLAUDE/Plan/00133-ccy-auto-compact-window-default-and-daemon-check/PLAN.md:203`
   reads `- [ ] ./scripts/qa-all.bash passes.` while Task 2.7 is `[x]` and the journal records
   the pass. Re-run here: `QA passed: 978 files checked`. Note the count differs from the
   `977 files checked` asserted in 389d4104's commit message; the cause was not established, so
   this is reported as an observation only — it means the number in the message is not a
   reproducible assertion.

3. **Plan drift: `## Delivery & Milestones` still records only Phase 1** — `PLAN.md:216-217`.
   Its own comment says the section carries "curated milestones + delivery commit hashes".
   Phases 2 and 3 have both delivered, with hashes `389d4104` and `26b64939`, and neither is
   recorded. This is exactly the lag the Plan Commit Rule exists to prevent.

## Nits

4. **Task 2.6 and a Success Criterion disagree about the `min()` caveat** — `PLAN.md:118-124`
   vs `PLAN.md:201-202`. Task 2.6 had the "state the `min(setting, model window)` caveat here
   too" requirement deliberately removed; the success criterion still requires it and is ticked.
   No harm (`docs/ccy.md:566-568` does state it), but the two lines now describe different
   contracts.

5. **The `min(setting, model window)` + "`/config` can no longer change it" pair is stated three
   times** — launcher `claude-yolo:3129-3130`, the 3.58.0 changelog entry, `docs/ccy.md:566-568`.
   The launcher copy earns its place (it carries the "do not revert this as an accident" WHY that
   a reader at the call site needs) and changelog entries in this repo are consistently
   self-contained narrative, so this is not called an SSoT violation — just note that a future
   edit has three copies to keep honest.

## Checked and clean

- **Launcher correctness**: `-e "CLAUDE_CODE_AUTO_COMPACT_WINDOW=${CLAUDE_CODE_AUTO_COMPACT_WINDOW:-600k}"`
  at `claude-yolo:3161` sits in the single session-launching `container_cmd run` (line 3134).
  Every `container_cmd run` site in `files/var/local/claude-yolo/` was enumerated — the others are
  `--entrypoint claude --help/--version` probes, an `npm install` temp container, a network probe
  and the token-login run. None runs a session, so none needs the variable. `bash -n` passes.
- **Both overrides genuinely preserved**: host export — `${VAR:-600k}` expands on the host before
  `container_cmd run`, so an exported host value wins. Project override —
  `/workspace/files/var/local/claude-yolo/entrypoint.sh:382-391` sources
  `/workspace/.claude/ccy/ccy.env` unconditionally when present, and `exec "$@"` is at line 541,
  i.e. the source happens after the forwarded `-e` environment exists and before the exec. An
  `export` in `ccy.env` therefore replaces the forwarded value; a bare assignment does not, which
  is exactly what `docs/ccy.md:661-663` says and what the entrypoint's own comment at line 452
  independently records. The docs claim matches the code.
- **Version bump**: `CCY_VERSION="3.58.0"` (`claude-yolo:17`), up from 3.57.0. Minor is the right
  increment for an added default with no removal. The one-line description names the new variable,
  its value and the override route — accurate, current-state only, no appended history. No
  `lib/*.bash` changed, so the launcher-staged rule is satisfied trivially; `entrypoint.sh` and
  `Dockerfile` are untouched, so `REQUIRED_CONTAINER_VERSION` and the image LABEL correctly do not
  move.
- **Comment placement convention**: the banner at `claude-yolo:3121-3133` mirrors the
  `CCY_HOST_HOSTNAME` banner at 3105-3115 exactly, including running straight into the code with no
  trailing separator. The journal's stated reason (a comment inside the backslash-continued
  argument list is fragile) is the right call.
- **Public-repo safety of the generated issue body**
  (`/workspace/untracked/issue-reports/issue-report-20260917-100110.md`, read in full): no
  hostname, username, container/project name, git remote, or absolute path from any real tree; no
  config or log dump. Paths cited are upstream source paths and `untracked/scratch/acw-probe`. The
  platform line is generic (`Linux x86_64, Python 3.11.2`). It carries a `body_sha256`, was
  generated by `hooks-daemon issue-report`, and is untracked — `R-UPSTREAM-ISSUE-UNVERIFIED-BODY`
  will accept it. Nothing hand-drafted exists in the plan folder (`PLAN.md`, `JOURNAL/`,
  `research/`, `subagent-reports/` only).
- **Settled decisions honoured, not reopened**: `PLAN.md:43-47` and 112-116 state 600k as the
  operator's figure with no 200k hedge; the issue body states the warn-on-unset / warn-on-`auto` /
  configurable-ceiling design as a decision rather than a question, and explicitly refuses the
  small-model exemption. Neither commit hedges.
- **Fail-fast**: no `failed_when` / `ignore_errors` / `|| true` introduced; nothing added
  skips-and-continues. An invalid value supplied via `ccy.env` is rejected by Claude Code's own
  parse error (recorded in `research/auto-compact-window-facts.md:46,73`), so the absence of
  launcher-side grammar validation does not create a silent-degradation path.
- **Phase 4 correctly not attempted**: every Task 4.x is marked HOST-run and left unticked; no
  Ansible ran in this container.
- **Plan index**: `CLAUDE/Plan/README.md:37` carries the 00133 row.

## Mechanical gates

- `qa-all.bash`: PASS — `QA passed: 978 files checked`.
- `plan-qa --sweep`: PASS for this plan — 8 findings repo-wide (0 block, 8 advise), all
  pre-existing and in plans 00046/00063/00109/00119/00132 plus a journal-freshness list; zero
  findings against 00133.
- `ansible-playbook --syntax-check`: not applicable — the diff touches no `playbooks/**`,
  `tasks/`, `vars/` or any `.yml`.
- `qa-helper-tests.bash` / `check_extension_compat` / extension ESLint: not triggered — no
  `helpers/`, `tests/helpers/` or `extensions/` files in the diff. (`qa-all.bash` ran the helper
  suites and extension-compat checks anyway; both green.)
