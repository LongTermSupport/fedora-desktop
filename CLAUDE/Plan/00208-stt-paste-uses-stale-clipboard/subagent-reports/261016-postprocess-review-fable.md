# Fable review: feat/stt-postprocess-button at c748364f (Plan 00208 Phase 3)

Verdict: **FIX-FIRST**, one blocker (the meta-deploy queue), then MERGE. Reviewed against local
F44 (c1fd701e). Nothing but this file was changed.

## Blocker

**B1. `CLAUDE/Plan/meta-deploy.list` conflicts with F44 and would roll the queue back.**
`git merge-tree --write-tree F44 c748364f` reports one conflict, this file (the other three
auto-merged files, `play-speech-to-text.yml`, `src-go/CLAUDE.md`, `src-go/qa/rules/table.go`,
merge clean). The branch's copy is baad4539's list with the Phase 3 paragraph added, so against
F44's current list it:

- lacks `playbooks/imports/play-basic-configs.yml` (F44 line 65) and its description;
- carries the ccy 3.99.12.9 paragraph where F44 has the 3.99.12.10 one (F44 lines 44-49), and
  the agent-bus paragraph without the umask text F44 added (F44 lines 24-30);
- re-adds three entries F44 removed because they have run: `play-md-viewer.yml`,
  `00203-teams-hot-control-and-robustness/triage-hs.bash`,
  `00170-unifi-network-tooling/clear-ledger-broken.bash`;
- F44's speech-to-text paragraph (lines 50-59) says "No extension change, so no log-out is
  needed" and adds the wsi-model-manager sentence; the branch's paragraph says the opposite about
  the log-out and lacks that sentence.

Fix: resolve the conflict by taking F44's file and replacing only its speech-to-text paragraph
with one that keeps F44's wsi-model-manager sentence and adds the branch's Phase 3 text, including
"THE EXTENSION CHANGED: LOG OUT AND BACK IN after this run". Every other F44 entry stays, nothing
removed by F44 comes back. The merge must then be `--no-ff` with that resolution in the merge
commit (or a resolve commit on the branch first).

## Attack results (no blockers)

**1. F1's scope: which notifications now show despite --no-notify.** The gate is the one
function in each recorder: `files/home/.local/bin/wsi-stream:449-453` (`desktop_notification`,
`expire_ms == 0` passes), `src-go/internal/wsi/dbus.go:101-104` (`ms == notifyStaysMS`).
wsi-article is not in the diff but calls `stream.desktop_notification` (wsi-article:82, :334),
so the claim "all three recorders" holds. Every 0 ms site, read in context:
wsi-stream 706 (not pasted), 718 (Enter not sent), 734 (clipboard never took the text), 922
(Claude failed), 1305 (final transcription did not finish), 1336 (dictation failed), 1370
(untranscribed audio kept), 1719 (did not complete), 1744 (nothing transcribed), 1753 (part may
be missing); wsi-article 82 (`fail`), 334 (`Stopped: <reason>`, a stop the user did not ask for);
Go paste.go 245, 292, 333 and transcribe.go 179 (Claude failed). All report a dictation lost,
cut short or not delivered; no success or progress notice is 0 ms, so at most one per dictation
and only on a fault. The line "stays = must be seen" is the right one. Note for the owner: an
article stopped by its silence or duration limit (wsi-article:334) now notifies with Show
Notifications off; that is new and, I judge, wanted.

**2. wsi-postprocess (`src-go/internal/wsipost/app.go`).**

- How Claude is called: `wsi-claude-process --model M --style S [--debug] <text>` from the
  binary's own directory (app.go:170-178; dynamic-name allowance table.go:148-152). That script
  passes the prompt on **stdin** and the OAuth token in the **environment**
  (`CLAUDE_CODE_OAUTH_TOKEN`, wsi-claude-process:107); no secret is in any argv. The transcript
  itself is argv to wsi-claude-process, as the recorders have always passed it; not a secret, and
  not new here.
- Timeouts: 180 s (`claudeTimeout`, app.go:54) over the script's own 170 s, so the script's
  timeout message wins. The panel notifies "Post-processing the last transcription (<style>)
  with Claude (<model>)" at start (postProcess.js:93-94), so a 3-minute wait is not mistaken for
  nothing. The spawn is `Gio.Subprocess.new` + `communicate_utf8_async` (extension.js:1456-1470):
  argv array, no shell, no main-loop block.
- Concurrency: `_running` in `LastTranscriptPostProcessor` (postProcess.js:78-82). It is checked
  and set in one synchronous step on the shell's single main loop, so there is no window for two
  clicks to pass; a lock file would guard nothing more and would need stale-lock handling. It does
  not guard against a concurrent Ctrl+Insert dictation's own wsi-claude-process, nor need to: the
  two write different files (last-transcription.txt vs last-postprocessed.txt). A disable/enable
  during a run makes the old run's result be dropped (`destroy`, postProcess.js:99-101) and allows a
  new one; acceptable.
- Reading the transcription while a dictation writes it: not safe, and not made worse. All three
  writers truncate in place (`os.WriteFile`, deliver.go:83; `Path.write_text`, wsi-stream:1778,
  2051, 2316), so a reader at the wrong microsecond sees an empty or partial file. Empty becomes
  the loud "The last transcription is empty" (app.go:214); partial would be processed silently.
  Copy Last Transcription has had the same exposure since it existed. Follow-up, not a blocker:
  make `save` in deliver.go use `fsx.WriteAtomic` as wsipost itself does (app.go:188), and the
  Python writers a temp + rename.
- Clipboard: `St.Clipboard.set_text` (extension.js:268). No read-back, and rightly none: the
  stale-paste gate exists because an external `wl-copy` may never be granted the selection by the
  compositor and the paste keys would paste the old text. Here the shell *is* the compositor-side
  owner, and nothing is pasted: the owner presses Ctrl+V later. The diagnosis report says so
  (Part 2 choices); the code comment in postProcess.js:9-11 says "never pastes" but not why no
  read-back; one clause there would close the question for the next reader.

**3. Extension JS.** `scripts/qa-js.bash`: 32 files OK, 19 linted by ESLint (the extensions/
tree, which holds postProcess.js and extension.js), pass. GNOME Shell APIs used: `Gio.Subprocess`,
`St.Clipboard`, `Main.notify`, `Meta.KeyBindingAction.NONE` as `addKeybinding`'s failure value
(extension.js:230); all current. F2 (extension.js:224-237) reads the live `get_strv` for each
record key so an empty setting is named as well as a refused one, and notifies unconditionally,
which is right for a configuration fault. The Node suite (`scripts/test-panel-sections.bash`,
283 passed) covers the ten postProcess.js cases including already-running, cannot-start, no
output, late result after destroy; the GNOME wiring in extension.js is untestable under Node and
is small.

**4. H9 host check** (`261016-stt-postprocess-diagnosis.md`). It runs the production
`wsi-claude-process --model haiku --style corporate "this is a test of the post processing step"`. The only owner data read is the token file, by the script, exported to `claude`, never
printed; H2 and H8 print the token's *name*, not a token. Output is Claude's rewrite of a fixed
sentence. It spends one haiku call on the owner's account. Safe to hand to the host agent. H1-H8
are read-only; H3's `dconf dump /` is filtered through grep to the two key names.

**5. Tests, run here with every `PINGBUS_*` unset.**

- `go test ./internal/wsipost/... ./cmd/wsi-postprocess/... ./internal/wsi/...`: ok.
- Red proof for F1: on a scratch copy of src-go with the dbus.go gate reverted to `if p.noNotify`,
  `TestW10NotificationsReplaceTheLastAndCanBeTurnedOff` and
  `TestW23ClaudeReplacesTheTextOrLeavesTheRawTextOnTheClipboard` fail; with the branch's gate
  they pass. The Python twin `tests/speech_to_text/test_claude_failure_report.py:51-66` asserts
  one notification with `expire_ms == 0` under `no_notify`, and that an expiring one is still
  dropped. wsipost and postProcess.js were red as "package/module missing", which is the only
  red a new unit can have.
- `python3 -m unittest discover -s tests/speech_to_text`: 311 tests OK.
- `scripts/test-panel-sections.bash`: passed 283.
- `scripts/qa-js.bash`: pass.
- `scripts/test-wsi-stop-grace.bash`: exit 0, 120 PASS lines, no FAIL (its stub wl-paste
  reads back the stub wl-copy's text, so the W32 gate is exercised end to end).

**6. meta-deploy.list.** See B1. The branch's own paragraph does say a log-out is needed; the
conflict is that it sits in an out-of-date copy of the list.

## Other checks

- Owner rules: both actions are visible menu items (extension.js:435-440), not key-only; every
  failure path ends in a notification (P4-P8 in Go, `_report` in JS); one code path, no fallback
  to raw text (app.go:183-186 fails loudly on an empty answer); new logic is Go, JS is glue.
- Playbook: `postProcess.js` is in the deployed file list and the binary has its build task
  (play-speech-to-text.yml:357, 393-401); `scripts/go-binary-suites.tsv` gains the suite.
- No `files/var/local/claude-yolo/` change, so no CCY bump is due.
- No install-specific names in the new files; the diagnosis report uses `$USER` and `~`.
- `docs/features/speech-to-text.md` documents the items, the clipboard destination and the
  saved file; PLAN.md Tasks 3.1-3.4 ticked, 3.5-3.7 open, matching the code.
