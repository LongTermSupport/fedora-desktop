# Fable review: Phase 4, post-process with the token that has the most allowance left

Branch `feat/stt-token-auto` at bee06ac3, diffed against F44 (`git diff F44...bee06ac3`).
Read-only review; nothing on the branch was changed but this file.

## Verdict: FIX-FIRST

One blocker, in `CLAUDE/Plan/meta-deploy.list`. Everything else passes; two should-fix items
and a few notes follow.

## Blocker

**`CLAUDE/Plan/meta-deploy.list` carries a stale queue and conflicts with F44.**
`git merge-tree --write-tree F44 bee06ac3` reports `CONFLICT (content): Merge conflict in CLAUDE/Plan/meta-deploy.list`; every other file auto-merges (play-speech-to-text.yml,
bash-limits.allowlist, src-go/CLAUDE.md, src-go/qa/rules/table.go).

The branch's queue (comment lines removed) is six entries: play-agent-bus.yml,
play-md-viewer.yml, play-claude-yolo.yml, the 00203 triage script, play-speech-to-text.yml,
the 00170 clear-ledger script. F44 emptied all of that at c7718d5d ("queue emptied") and now
holds exactly one entry, `playbooks/imports/optional/common/play-speech-to-text.yml` with the
Phase 3 note. The branch was cut before that commit and never rebased its list.

Required: make the branch's list F44's one-entry list plus the Phase 4 paragraph on that same
entry. The Phase 4 paragraph itself is right in content (what the chooser does, the switch is
off by default, the cost of a read, what happens when every token is out). Two words to add to
it: the extension changed again in this phase (extension.js, prefs.js, the schema and the new
tokenWarning.js), so say so beside the Phase 3 "LOG OUT AND BACK IN" line rather than relying
on the reader to join the two paragraphs. The same class of defect was the Phase 3 verdict
("MERGE once the queue is resolved"); this time it is a hard conflict, so FIX-FIRST.

## Should fix (not blocking)

1. **The 170 s promise is no longer kept** — `files/home/.local/bin/wsi-claude-process:31-32`.
   The comment says "Below the callers' own limits (180 s), so this script reports the
   timeout itself". The chooser now runs first and can take up to 20 s on a cold cache
   (`ccy` fans out one curl per token in parallel, `CCY_USAGE_TIMEOUT=20`,
   `token-management.bash:154`; wsitoken's own cap is 90 s, `app.go:66`). 20 + 170 > 180, so
   on a slow read the callers (`wsi-stream:876-901`, `wsipost/app.go:51`,
   `wsi-article-window:46`) kill the script and report "wsi-claude-process could not run:
   timed out" instead of the script's own reason. Still a loud failure, but the diagnosis is
   wrong. Fix: take the chooser's elapsed seconds off `CLAUDE_TIMEOUT_SECONDS`, or lower the
   constant to 150 and say why.

2. **ccy's stderr is dropped from the K5 reason** — `src-go/internal/wsitoken/app.go:244-262`.
   On an exit other than 0/1, or an exec failure, the message is
   `cannot read the tokens' usage (<ccy> --token-usage --json: exit status 2; re-run play-claude-yolo.yml)`. The `stderr` buffer is captured and never shown, so ccy's own
   sentence (e.g. its print_error for an unknown option on an old ccy) is lost. Append the
   first line of stderr when it is non-empty.

## Attack results

### 1. Secrets

- Token values are never in argv, logs, notifications or the panel. The chooser reads the file
  only to validate its shape (`app.go:174-187`, K4) and prints only the path. The tests pin
  this (`noTokenValueSeen`, `app_test.go:101-108`, used by K3/K4/K6; the Python test asserts
  `TOKEN` is absent from stdout+stderr in two cases).
- `wsi-claude-process` reads the path, `basename`s it for the account label, and exports the
  value as `CLAUDE_CODE_OAUTH_TOKEN` (environment, not argv) — the same mechanism Phase 2 used.
  The debug log gets the chooser's stderr (`using token 'NAME' (FILE)`): names only.
- `ccy --token-usage --json` prints `name` and `file` (file name, no directory), never the
  value; the token reaches curl via `--config` on stdin (`token-management.bash:428`). Its
  help says so explicitly (`claude-yolo:315`).
- Printing the path to stdout is safe: the only reader is `wsi-claude-process`, which checks
  the exit code, treats empty as the desktop login, and otherwise reads the file. The K6 path is
  bounded to the store: ccy's `file` must contain no `/` and end `.token` (`app.go:224-227`,
  tested K6 `../x`), and K3's name is matched against directory entries, not joined as a path.

### 2. Usage measurement

- Failure of the reader is loud, not a silent pick. K5 covers exit 64, non-JSON, wrong or
  missing schema, and exec failure; each exits 1 with "cannot read the tokens' usage ... re-run
  play-claude-yolo.yml" and leaves the warning file untouched (right: a broken reader is not a
  verdict on the tokens). Exit 1 with a schema 1 document is treated as figures, which matches
  ccy's contract (exit 1 = no token's usage was read; the document says why per token).
- Partial data: a token is "ok" only if ccy says `status: ok` and at least one bucket is
  reported; a reported bucket at or over 100 removes it; an unreported bucket counts as 0 in
  `used()`. ccy nulls a bucket whose raw value its scale cannot produce (scale conflict) and
  adds a warning; wsitoken does not read `warnings`, so a token with a nulled 7-day bucket and
  a low 5-hour one ranks on the 5-hour figure alone. The research names this (F37) as "treated
  as unusable", which is only true when both buckets are nulled. Edge case, noted for D4-style
  owner awareness; not a blocker.
- Stale cache: ccy itself marks an entry older than the TTL whose refresh did not publish as
  `unavailable` ("this run's fetch did not complete, and the cached figures are out of
  date", `token-management.bash:779-780`), so wsitoken never ranks on figures a dead fetch left
  behind.
- Cache race: each worker writes a part file named by its own PID and publishes by a single
  `mv -f` (`token-management.bash:531-538`); two concurrent `wsi-claude-process` runs cannot
  interleave an entry. The chooser's own warning file is written with `fsx.WriteAtomic`.
- "Most room" is one rule, not a fallback chain: room = 100 − max(5-hour, 7-day); ties go to
  the lower 7-day figure, then the name (`app.go:217-219`, tested). The desktop login is never
  a fallback in auto mode (correct: fail fast). The rule weights the two windows equally; the
  write-up says so.

### 3. Panel warning

- It is a visible state: an amber `dialog-warning-symbolic` in the panel box and a menu item
  whose text is the file's text, both shown while the file exists, driven by a
  `Gio.FileMonitor` on the path plus a read at enable (`extension.js:1243-1298`). A file that
  exists but cannot be read still warns (the error text becomes the label). A control, not a
  key: clearing is the menu item's `activate`. It clears two ways: K6 removes the file on a
  successful auto choice (`app.go:232`), or the item deletes it; the monitor then hides both.
- Null guards cover a disable mid-read (`_showTokenWarning` returns if the icon or item is
  gone); the monitor is cancelled on disable. `_buildMenu` runs before `_watchTokenWarning`,
  so the item exists on the first refresh.
- ESLint passes (`scripts/qa-js.bash`: 34 files OK, 20 linted). APIs are current GNOME Shell:
  `Gio.File.monitor_file`, `load_contents_async`/`_finish`, `delete_async`, `TextDecoder`,
  `GLib.Error.matches`.
- Pure logic is in `tokenWarning.js` with no GNOME imports and a Node test (4 cases, run by
  `test-panel-sections.bash`).

### 4. Bash and Python limits, stderr hygiene

- `wsi-claude-process` shrank 125→105 code lines, 3→2 functions, 138→113 statements; the
  ratchet row was tightened, not loosened. `scripts/qa-bash-limits.bash`: BASH-LIMITS-OK.
- stdout of `wsi-claude-process` is Claude's text only; every reason goes to stderr via
  `error_exit`. The chooser's stderr is captured to a temp file so a failed run's reason stays
  separate from claude's; the chooser's `info` line goes to the debug log only in debug mode.
- No new Python; `test_claude_process.py` is stdlib unittest, run by the stop-grace suite.

### 5. Tests (run with a clean environment, no PINGBUS\_\*)

- `go test ./internal/wsitoken/... ./cmd/wsi-claude-token/... ./internal/wsi/... ./internal/wsipost/...` — ok; `go vet` ok; `golangci-lint run` on the two new packages: 0
  issues; `src-go/qa` module `./rules/...` ok (the two new dynamic-name allowances).
- `pytest tests/speech_to_text` — 311 passed.
- `bash scripts/test-wsi-stop-grace.bash` — passed: 121, failed: 0 (includes the schema check
  for `claude-token-auto` default false and the rewritten `test_claude_process.py`).
- `bash scripts/test-panel-sections.bash` — passed: 287. `scripts/qa-js.bash` — OK.
- Real path: the Go tests drive `App.Run` through `execx.Fake` for `wsi-setting` and `ccy`,
  with real token files and a real warning file on disk, and check mode 0600. The bash test
  runs the real script against a stub chooser and a stub claude. Red-first is recorded in the
  journal (undefined `App`/`run`, missing `tokenWarning.js`), which I accept as the record.

### 6. meta-deploy

See the blocker. F44's queue is exactly `play-speech-to-text.yml`; Phase 4's paragraph is
attached to that entry on the branch, which is right, but the branch also resurrects five
entries F44 has run. A log-out is stated only in the Phase 3 paragraph of the same entry.

### 7. Public repo

No hostnames, usernames, checkout paths or install names in the diff. Token names in tests and
docs are placeholders (`work`, `idle`, `busy`, `week`, `hour`, `gone`, `a`/`b`/`c`). The
research write-up cites file:line in this repository only.

## Notes (no action required to merge)

- Research write-up heading "Design built (Phase 2)" means Phase 4
  (`261016-stt-token-auto-research.md`, the section before the decisions).
- D2 (which file of a name: auto uses ccy's earliest-dated, named uses newest unexpired) is
  correctly surfaced as an owner decision rather than silently aligned.
- The Settings switch greys out the token row while on
  (`prefs.js:174-175`, `INVERT_BOOLEAN` bind) — a good touch; the schema description says the
  same.
