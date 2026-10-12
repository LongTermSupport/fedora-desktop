# QA review: Plan 00171 branch against origin/F44 (after commit 39a3c731)

The qa-reviewer agent's report, copied by the builder (the reviewer has no Write tool).
What was done about each finding is in PLAN.md Task 5.9 and the JOURNAL handoff entry.

**Verdict: FIX-BEFORE-MERGE**

The three-dot diff also lists files from earlier F44 merges; the two-dot diff against the
later merge base leaves only the STT work, which was reviewed.

All six fixes from the previous review are in place and tested:

1. TERM/Escape while START is in flight: `StartCancel` waits for START's reply and ABORTs
   a START the server accepted, including a signal between the first check and the new
   handlers. The tests send real `os.kill` signals.
2. UNOWNED after disable/enable: the panel checks for a stray recorder when created and on
   each Insert.
3. QUEUED is separate from FINISHING.
4. Exit before the final signal: the panel waits `EXIT_SETTLE_MS` for it.
5. `Display:` lines are logged.
6. Escape kills the recorder's process group: recorders start through setsid, and `pkill`
   is gone from the extension.

The microphone-shortfall reporting and the red badge are correct.

## Should fix before merge

1. The probe passed check 3 when there was no `Display:` line at all (a panel from before
   Task 5.8). Fail or report "not established" unless the window holds a `Display:` line;
   remove the duplicate `DISPLAY = re.compile`.
2. `acceptance.bash` checked a hand-written list of extension files that missed
   `panelLook.js` and `prefs.js`. Loop over `git ls-files` of the extension minus
   `schemas/`, and print the count.
3. A clean exit before any report was treated as an article hand-off for every kind of
   recorder; an Insert then sent TERM to a reaped PID (also in the MIC_OPEN branch during
   the settle). Limit the hand-off wait to articles, never signal `session.pid` once
   `session.exited`; add the dictation case.
4. Batch `wsi` sent RECORDING after STOPPING when Insert landed during PREPARING (the TERM
   trap runs after `sleep 0.3`). Skip RECORDING when a stop is pending; add a case to
   `test-wsi-stop-grace.bash`.
5. The setsid test checked pid == pgid, which also holds if setsid forked. Assert the
   spawned PID equals the `$$` the child prints.
6. The plan has not reached `meta-deploy.bash`. Add it when merging, with the two-round
   logout note.
7. The README index row was stale ("Not Started", a "blocked" icon).

## Nits

8. An unknown state fell back to the ready look, against Decision 2. Map it to the
   hourglass and log it.
9. `_readPidFile` checked `query_exists` then `load_contents`; a file removed in between
   throws inside GLib callbacks. Call `load_contents` and treat only NOT_FOUND as null.

## Checked and clean

- GJS calls: `GLib.spawn_async(null, argv, null, SEARCH_PATH, null)` is the right form and
  GLib reaps the child; setsid does not fork for a child that is not a group leader;
  `killArgv` refuses anything but a numeric PID and a recorder signal.
- Stale PID file: `_readLiveRecorderPid` checks `/proc/<pid>/cmdline`.
- Python signals: a handler that does not raise lets the socket call retry (PEP 475); a
  failed ABORT is retried at exit, with a test.
- Stderr hygiene, no new silent error swallowing, the play deploys every extension file,
  no install-specific identifiers, docs match the code.

## Gates the reviewer ran

`test-panel-sections.bash` passed; `test-wsi-stop-grace.bash` passed (288 unit tests).
`qa-all.bash` not run, as instructed.
