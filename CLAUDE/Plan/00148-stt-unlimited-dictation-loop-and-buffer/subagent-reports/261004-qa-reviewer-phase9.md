# QA review: Plans 00156 (T1.2, 1.4, 1.5, 2.2) and 00148 Phase 9, uncommitted tree on 6515dd7c

Reviewer: `qa-reviewer`. Verdict: **FIX-BEFORE-MERGE**. Nothing blocking: the ChunkPaster
offsets, the `PasteKey` out-args, the playbook copy of `focusOutline.js` and the download
task's idempotence were all confirmed against the code.

## Should fix, and what was done

1. **Batch mode (`wsi`) never asks `PasteKey` or saves, though the docs said every
   paste.** The docs now say streaming mode; the code change is Task 9.9.
2. **00148's status line did not mention Phase 9.** Fixed.
3. **The journal understated what is untested.** No test covers the server's
   `progress(with_text=…)`, or the `run_server_mode` chunk wiring through the real stop
   path. `ChunkPasterTest` compares against a literal. Recorded in Task 9.9.
4. **acceptance.bash did not check what this deploys.** Fixed: it now checks
   `focusOutline.js`, `dictation-paste-interval-seconds` and the `auto` model's
   `model.bin`.
5. **The prefs Auto subtitle ignored the no-GPU case.** Fixed.
6. **The research file named the machine's GPU.** Generalised.
7. **The download probe caught every exception.** Narrowed to `LocalEntryNotFoundError`.

## Nits, and what was done

- **A missing `text_so_far` was silent.** It now logs a WARN.
- **Undelivered and failed reports overstate what is owed after chunks.** Task 9.9.
- **A failed Ctrl+S re-pasted a chunk.** Fixed: the save is outside the paste's result.
- **A chunk paste blocks the KEEPALIVE loop.** Worst case stays inside the 15 s
  heartbeat. Not changed.
- **The outline draws over the overview; `shown` getter unused.** Task 9.9.
- **gi-stubs docblock separated from its method; 9.7 sits before 9.5; 9.3 says
  "every 120 s".** Not changed.

## Mechanical gates

qa-all green except `deployed-drift` (expected before deploy; some drift predates this
diff). plan-qa sweep: 0 block. Ansible `--syntax-check` passes. ESLint clean. Targeted
tests: 73 Python tests OK; `test-stt-prefs-installed-model.mjs` 3/3.
