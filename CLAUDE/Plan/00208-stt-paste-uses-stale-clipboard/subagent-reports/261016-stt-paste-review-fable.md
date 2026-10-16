# Fable review: Plan 00208, `fix/stt-paste-stale-clipboard` (16f5a6c4 + a8117ec5) against F44 f01d993a

**Verdict: FIX-FIRST** — one blocker, small and local. Everything else is MERGE-grade.

## Evidence run

- `go test ./internal/wsi/...` — ok.
- `python3 -m unittest tests.speech_to_text.test_clipboard_confirm tests.speech_to_text.test_paste_target` — 46 tests OK.
- `bash scripts/test-wsi-stop-grace.bash` — passed 120, failed 0.
- Probe (not a committed test): `copy_confirmed("hello")` with no `wl-copy` on PATH, wayland session.
- Host (coordinator, read-only): `timeout 3 wl-paste -n` from an unfocused client returned rc 0 three times in 61–73 ms, nothing on stderr; the stuck `wl-copy` from the bad dictation is still alive with fd 0 a pipe.

## Blocker

**B1 — `copy_confirmed` busy-loops when the copy cannot start.**
`files/home/.local/bin/wsi-stream:553-572`. When `start_copy` returns a reason (OSError: binary
missing, exec failure, or `stdin.write` failing because the process died at once), the inner
`while not why` never runs, there is no sleep, and the outer `while time.monotonic() < deadline`
spins. Measured: **32,316 `Popen` attempts and 32,315 WARN `log()` lines in 3.00 s**, then the
right `ClipboardNotTaken`. The outcome is correct (nothing pasted, exit 1), but it is the
opposite of fail fast: a non-transient error is retried at CPU speed and floods the log. The
fix is one branch: a non-empty `why` from `start_copy` raises `ClipboardNotTaken` at once
(after `stop_copies()`); only a copy that *started* and did not take within
`CLIPBOARD_COPY_ATTEMPT_SECONDS` is recopied. Add a case to `test_clipboard_confirm.py` with an
empty PATH asserting one attempt and a bounded log.

## Attack points

1. **Comparison exactness.** `got in (text, text + "\n")` on `wl-paste -n` output decoded with
   `errors="replace"`; `text.encode()` is UTF-8 both ways, so odd unicode and NUL round-trip
   byte-exact (wl-copy stores bytes, no `--trim-newline`). A lone surrogate in `text` would raise
   `UnicodeEncodeError` from `start_copy` (not an OSError) → `auto_paste`'s `except Exception`
   → `False`, no paste: safe. The `+ "\n"` allowance is a loosening with no case that needs it
   in this code path (wl-copy adds nothing); harmless, since the stale text has a *different*
   body. Very large text: `proc.stdin.write` is synchronous with no deadline; a `wl-copy` stuck
   before draining stdin with > 64 KiB of text would block forever. A dictation of 64 KiB is
   not realistic; noted, not a blocker. Go: `got == text || got == text+"\n"` on raw bytes
   from stdout, same reasoning.
2. **wl-paste behaviour.** Python: every `wl-paste` runs under `subprocess.run(..., timeout=)`
   bounded by the attempt end, min 0.1 s; a hang is a `TimeoutExpired`, reported as "nothing
   readable", and the deadline still holds. Go: the read runs on `cctx` (3 s). Host evidence
   says it returns in ~70 ms and needs no focus; `wl-paste` is a plain Wayland client that asks
   the data-control/selection offer, it raises no window and triggers nothing on GNOME. Race
   with a user copy inside the window: a different text never matches → false failure, not a
   wrong paste. The residual race (user copies between a matching read and the key press, tens
   of ms) is inherent to any paste-by-key design and existed before; it is not stated anywhere
   — a one-line note in W32 would be honest.
3. **Recopy and process handling.**
   - *Fallback chain?* No. A fallback chain substitutes a different mechanism when the first
     fails and masks the failure. This is one mechanism, retried a bounded number of times
     (≤ 3 in 3 s), gated by a verified postcondition (the selection reads back as the text),
     with a hard, loud failure and exit 1 when it does not. That is a bounded retry with a
     proof of success, which the fail-fast rule permits (probe-then-fail). Acceptable; say so
     in the plan.
   - *Reaping.* `stop_copies` does `kill()` then `wait()` on each handle, so no zombies from
     this run. Note that real `wl-copy` forks and the parent exits once the selection is set;
     the handle is the parent, so on the happy path `wait()` reaps an already-exited parent
     and the forked server child (which legitimately holds the selection until replaced) is
     untouched. The stuck process on the host is the *pre-fork* parent (never detached), which
     is exactly the handle — so the kill addresses the observed failure mode. The plan goal
     "no copy process this tool started is left running holding old text" is therefore true
     for the stuck case and *not* for the normal detached server, which is correct wl-copy
     behaviour; reword the goal.
   - *Killing the user's wl-copy.* Only `Popen` handles in `_copies` are killed; nothing is
     matched by name. The test `test_an_earlier_copy_still_running_is_killed_before_the_next`
     proves the handle path. The meta-deploy comment that the old stuck copy is not touched is
     correct.
   - Go has no recopy (deadline only). Asymmetric with Python but acceptable: it fails loud.
4. **Paste target after up to 3 s.** Both recorders ask the panel (W30) *before* the copy and
   press the key after confirmation. The focus check is now up to 3 s stale rather than
   0.4 s. On the happy path the gap is ~70–150 ms, smaller than before. On the slow path the
   window could lose focus and the key would go to another window — the same class of risk
   W30/W31 already accept over the old 0.4 s. New only in degree, and not stated. Non-blocker:
   state it in W32, or re-ask the panel once if confirmation took more than one attempt.
5. **Limits.**
   - *Python CLI.* Deferring the Go port (Task 2.1) is the right call for an owner-visible
     bug: the fix is ~110 lines inside an existing 2,400-line script, and porting first would
     have delayed the fix by a full plan. CLAUDE.md's "ported when next reworked" means a
     rework, not a bug fix. Recommendation: accept, keep Task 2.1 open and do not let a
     *third* substantive change land in wsi-stream without the port.
   - *StderrHygiene.* `report_clipboard_not_taken` prints to stderr; stdout untouched. Good.
   - *Fail fast.* B1 above. Otherwise the error paths are explicit and loud.
6. **Tests.** The Python fakes are real executables on PATH driving the real `copy_confirmed`,
   `auto_paste` (only `press`/`paste_target_now` mocked) and `paste_and_report`; they cover
   takes-at-once, late, newline, first-never (recopy), never (hard fail, all copies dead,
   selection unchanged), handle-kill, and the chunk path. The Go kit's `wl-paste` fake with
   `staleReads` exercises the real `confirmSelection`. RED-first: the journal records the Go
   tests red on the old `wsi` and a one-off red run of the old `auto_paste`; the Go claim is
   mechanically credible (the tests depend on new constants, so they were written against the
   new code and run against the old binary via `k.exec`); the Python red is journal-only. The
   missing-binary case (B1) is untested.
7. **meta-deploy.list.** Every F44 entry is kept (diff of non-comment lines: one addition).
   The comment is correct: the play deploys `wsi-stream` (notifies `restart wsi-stream-server`,
   handler at play line 724) and builds/installs the Go `wsi` (play lines 12, 382); no
   extension change.
8. **Public repo.** No hostnames, paths or container names. `**Owner**: joseph` appears in
   160 PLAN.md files already — existing convention, not introduced here.
9. **Merge.** `git merge-tree f01d993a HEAD`: one conflict,
   `src-go/qa/cmd/qa-helper-tests/main_test.go` — a8117ec5 duplicates the fix already in F44;
   F44's version differs only by one comment line. Resolve by taking F44's side (`ours`).
   `CLAUDE/Plan/README.md` auto-merges. The branch is otherwise far behind F44 (diff stat shows
   many F44 additions as deletions); that is merge-base distance, not reverts.

## Non-blockers

- N1 `wsi-stream:548` docstring and W32: state the residual key-press race and the staler focus check (points 2, 4).
- N2 PLAN.md Goals: reword "no copy process … left running" to the stuck-parent case (point 3).
- N3 Go/Python asymmetry (no recopy in Go) — fine, but note it in W32 so a later reader does not "fix" one to match the other.
- N4 `urgent_notification` exists for a failure the user must see even with `--no-notify`; `report_clipboard_not_taken` uses `desktop_notification`, which `--no-notify` silences. A dictation that pasted nothing is arguably in the same class as "dictation lost". Owner's call.
- N5 Deferred port tracked as Task 2.1 — keep it.
