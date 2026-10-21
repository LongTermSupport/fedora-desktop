# Handoff: review finding F2. Move the status-line oracle's recorder into a Go golden test

Branch `agent-ac068362476b47038-2c58bb45`. The previous agent stopped at its context budget.
F1, F3, F4, F6, F7, F8, the S24 decision and the F5 note are fixed and committed.
F2 is left, together with deleting `golden-capture/capture.py`.

## What exists

- `src-go/internal/pingbus/statusline/golden/testdata/scenarios.json`: one scenario per line.
  The format is described in capture.py's module docstring and in SPEC S40.
  - Each scenario has `files`. Each file entry has `path` and a `kind` of `file`, `dir` or
    `symlink`. A `file` has `content` (with `{world}` expanded) or `content_b64`, a `mode` as
    an octal string, and an optional `age_s`, which sets mtime = now − age. A `symlink` has a
    `target`.
  - Each scenario has `env` and stdin, given as `stdin`, `stdin_b64`, or
    `stdin_repeat {text, count}`.
  - Each scenario has `steps`. A step can carry `files` (placed when the step starts, at that
    moment's time), `remove`, `env` (merged in), `stdin*`, `signal` (sent once
    `{world}/inner-started` exists), and `gone` (pid files whose process must be dead, or a
    zombie, within 5 s).
  - The step key `sleep_s` is no longer used by any scenario (F7). Drop it.
- `testdata/golden/<scenario>/<n>.{stdout,stderr,exit}`: recorded from the Python zipapp.
  `{world}` replaces the world root in both outputs.
- `CLAUDE/Plan/00213-.../golden-capture/capture.py` holds two things: the scenario
  builders, which are Python and produce the literal JSON, and the runner. Its runner
  semantics are the reference for the Go port of the runner.

## What to build (coordinator's F2)

1. `src-go/internal/pingbus/statusline/golden/golden_test.go`, test only, on the model of
   `internal/freeze/golden`:
   - The default run compares an implementation over every scenario and step: stdout and exit
     byte for byte; stderr byte for byte except under the S24 shape rule (SPEC S24). Before
     the Go port exists, the implementation is the Python zipapp. Build it with `python3 -m helpers.pingbus.bundle --source <repo> --out <tmp>/pingbus.pyz pingbus`, which prints
     `BUNDLE-CHANGED`. Find the repo by walking up from the package directory to
     `helpers/pingbus/bundle.py`, not by a fixed hop. Run it as `<pyz> status-line`.
   - A flag such as `-impl` (or an env var) selects another binary, the Go draft. Once Task
     2.1 lands, the default becomes the Go `run`.
   - `-update` re-records `golden/` by running the Python implementation.
2. The scenario source has to move out of the plan folder. Either port the builders to Go
   (a `scenarios_test.go` that writes `scenarios.json` under `-update`), or keep
   `scenarios.json` as the hand-edited source of truth and delete the builders. The first
   keeps the fixtures readable. Then delete `capture.py`, and update `src-go/CLAUDE.md` (the
   paragraph after the freeze golden) and SPEC S40, which name it now.
3. Project rules that will bite:
   - `project_testclock`: no `time.Now` in `_test.go`. To place `age_s`, read a reference
     time from the filesystem (stat a file you just created) or go through a clock in a
     non-test helper.
   - The process seam: check whether `execx` is required for test-run processes (the freeze
     golden test imports `os/exec`, so see how it is allowed there).
   - The 90% coverage floor applies only to non-test code. A test-only package is fine, but
     `project_package_tests` needs a `_test.go` file, which is satisfied.
4. Run time is about 45 s over the Python, with one 10 s timeout scenario. Consider
   `testing.Short()` to skip the timeout scenario, but the full run must stay in go-qa or
   qa-all. Check that `qa-changed`'s diff-mode go-qa copes with that time.
5. Proof: the default run passes over the Python; a stand-in impl (`/bin/echo hi`) fails
   every step; `-update` reproduces `golden/` byte for byte (`git diff --exit-code`).

## State

The goldens were recaptured after F1 (inner-environment now prints a fixed list of names)
and F7 (cache-down-ages-out re-dates the mark in step 2, with no sleep). `capture.py check`
was green before this commit.
