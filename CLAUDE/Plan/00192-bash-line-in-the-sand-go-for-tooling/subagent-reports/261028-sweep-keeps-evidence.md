# Task 3.10: the sweep keeps evidence

Source: the host agent's report, pingbus `$7zLrx0PlxkebT2F8RqwaGSEf4cRO3g9cH6A7vOGvjrA`.

## Finding

`vmtest sweep` (run by `vmtest nightly` from `vmtest-nightly.timer`) removed the whole
directory of every passing run past the newest ten, so the transcript, console,
`response.json` and signature were lost, including runs that plans cite. A pass's teardown
already removes `overlay.qcow2` (`run.go`, `teardown`), so those removals freed almost no
disk. Runs that did not pass keep their overlays until `prune-runs` removes them (E61).

A second defect turned up while fixing it: the sweep ordered passing runs by the directory's
mtime. Removing a file inside a directory changes that mtime, so once the sweep kept
directories, a run it had just evicted read as the newest and the next sweep evicted another
run. The sweep now orders by the start time in the run id, and leaves alone any directory
whose name is not a run id, as `prune-runs` already does.

## Fix (`src-go/internal/vmtest/retention.go`)

- `evictDisks` removes only the `*.qcow2` in a directory (the same `overlays` glob that
  `prune-runs` uses), and never the directory itself. A directory with no `*.qcow2` is left
  alone and nothing is printed or logged for it, so a second sweep evicts nothing.
- What it prints: `VMTEST-EVICTED run-overlay <run-id> <files>` and
  `VMTEST-EVICTED build-disk <name> <files>`. What it logs: `<epoch> evicted <kind> <name> <files>: <why>`.
- If a removal fails, the sweep stops before the builds and the spool and says what it had
  removed: `cannot remove <path>: <err>; removed so far: <paths|nothing>`.
- `runDirs` now returns `[]runFacts`, because the mtime field is gone. `prunePlan` is
  simpler for it.

## Readers of the output

A grep of the tree for `VMTEST-EVICTED` and `VMTEST-SWEEP-DONE` found no play, unit, timer
or helper that parses the sweep's run and build lines. Their readers are the Go tests,
SPEC-vmtest E51, `docs/vm-acceptance-testing.md` and `usage.txt`, and all four are updated.
`VMTEST-SWEEP-DONE` does not exist. The spool's own lines (`VMTEST-EVICTED quarantine|responses`,
`VMTEST-SPOOL-SWEEP-DONE`) come from `helpers/vmtest/retention.py` and are unchanged.

## Failed builds

The sweep had the same problem for failed builds. It removed a failed fast-base build's
whole `bases/<name>.build/`, and the installer's `console.log` in it is the only diagnosis
of that failure. It now removes only the build's `disk.qcow2` and keeps the console log
until the next build of that base clears the directory (`build.go` does this at the start
of every build). A failed desktop build is kept whole, as before, disk included. That was
not changed, because the brief named only logs.

## Tests (red, then green, both journalled)

- `TestE51Sweep`: every run directory, transcript and response is kept, only the overlays of
  the passing runs past the cap go, the fast build loses its disk and keeps its console, a
  directory not named as a run is left, and a second sweep prints nothing.
- `TestE51SweepRemovalFails`: a removal that fails stops the sweep, names the file it failed
  on and what it had already removed, and neither the builds nor the spool are swept.

## Docs and deploy

- SPEC-vmtest: E51 is rewritten, and B19 is added as a departure.
- PLAN.md: the Task 3.10 bullet is updated.
- `docs/vm-acceptance-testing.md` and `usage.txt` are updated.
- `meta-deploy.list`: the changed-plays entry's description now expects
  `play-vm-test-lab.yml`. After the play runs, the host agent checks read-only that the next
  timer run's journal shows overlays evicted and every run directory still there, without
  running `vmtest nightly` by hand.
