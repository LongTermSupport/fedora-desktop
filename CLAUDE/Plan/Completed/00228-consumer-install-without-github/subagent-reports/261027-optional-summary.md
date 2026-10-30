# Optional plays past a failure, and one password per run

Branch `fix/00228-optional-summary`, cut from F44 at 6192e52fc. Not pushed, not merged.

## Commits

- `514a3bd83` Task 2.6: a failed optional play no longer ends the consumer run (and Task 6.6
  recorded).
- `3577ede59` Task 2.7: the password is asked once per run (CCY 3.100.35, because the shared
  `execx` and `ui` packages are built into the launcher's Go front).
- A third commit holds the journal entry and this report.

`./scripts/qa-changed.bash` passed before each commit, with its exit status checked. One run
before the second commit failed in `test-wsi-stop-grace.bash` ("Recording file empty") while the
host's load average was 45. The suite passed alone (121/0) and on the next qa-changed run. qa-all
was not run.

## Task 2.6: as built (`src-go/internal/consumer/app.go`)

- The chosen plays run in menu order. A failure is recorded and the next play runs.
- A signal stops the run at once and is not counted as a play failure. That covers Ctrl-C and
  TERM (the signal context), and also a play ended by a signal (exit above 128).
- A summary always goes to stderr, under "Optional apps: what happened", one line per chosen
  play:
  - `✓ <app>: installed`
  - `✗ <app>: failed (exit N); its output is above, in this terminal (it is not saved to a file)`
  - `✗ <app>: could not run (<error>)`
  - `✗ <app>: stopped part way (exit 130)`
  - `- <app>: not run`
- The retry advice follows:
  - one line per play that did not install, `~/Projects/fedora-desktop/run.bash playbooks/imports/optional/.../play-x.yml`;
  - then the menu, `run.bash --optional-only`;
  - then the issues URL, if a play failed.
    The single-play form works on a consumer box: run.bash's own play runner handles it, and
    `scripts/test-run-bash-consumer.bash` covers that. The path goes through `term.AbbrevHome`
    and `shellword`.
- Exit codes:
  - 1 with "N of M optional apps failed";
  - 128+n on a signal;
  - 0 only when every chosen play passed. Only then is the setup marker written.
- The main playbook still fails fast before the menu.

## Task 2.7: as built

- **Before:** each `ansible-playbook` ran with `--ask-become-pass`. Ansible therefore asked
  once for the main play and once more for every optional play. `sudo.Ready` could also prompt
  before dnf.
- **Now:** if `sudo -n -k true` passes, nothing is asked. Otherwise:
  1. The password is typed once with the echo off: `ui.Lines.Secret` with
     `ttyecho.Off(stdin)`. Only ECHO is cleared, so Ctrl-C still works, and the echo is put back
     however the read ends.
  2. `sudo.CheckPassword` checks it straight away with `sudo -k -S -p "" -v`. The password goes
     in as an `execx.Feed` on stdin. A refusal is asked again, up to three tries; after that the
     run exits 1 before anything is installed.
  3. `execx.Secret.WriteFile` writes it through `os.Root`, with O_EXCL and mode 0600, to
     `become-password` in an `os.MkdirTemp` directory (0700) under `XDG_RUNTIME_DIR`, which is
     tmpfs and this user's alone. If `XDG_RUNTIME_DIR` is unset, the run stops before asking.
  4. Every `ansible-playbook` gets `--become-password-file <file>`.
  5. `App.Run` removes the directory after `main` returns. That covers success, a failed play,
     the input ending, and Ctrl-C, TERM or HUP through the signal context.
     The check leaves sudo's credential cached, so the dnf step does not prompt.
- The password never appears in argv, in an environment or in the output. The tests assert
  this.
- **Why not a pipe:** `--become-password-file -` reads the password from stdin, so the plays
  could not use stdin. A `/dev/fd/N` path is resolved by Ansible to a pipe name that does not
  exist (run.bash's headless note says the same).
- **Limits:**
  - SIGKILL leaves the file behind, in tmpfs, until logout.
  - Ansible strips the file's surrounding whitespace, and `ui.Lines` trims the typed line, so a
    password that starts or ends with a space cannot work. Before this change, Ansible's own
    prompt had the same limit.

## Tests (red, then green)

| Area                    | Tests                                                                                                                                                                                                                                                                                                                                                                                 |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Task 2.6                | Six new tests failed against a stub, then passed.                                                                                                                                                                                                                                                                                                                                     |
| Task 2.7, shared pieces | One test each for `WriteFile`, `CheckPassword` and `ttyecho.Off`, and three for `Lines.Secret`. They failed to build until written.                                                                                                                                                                                                                                                   |
| Task 2.7, consumer      | Once the world typed one password, 13 tests failed. The new ones are: asked once across the main play and three optional ones, with the file 0600 in a 0700 directory as each play finds it; a wrong password and an empty line asked again; three refusals stop the run; no runtime directory; the file gone after a main failure, an optional failure, Ctrl-C and the input ending. |
| VM lab driver           | It now refuses the new question, red then green.                                                                                                                                                                                                                                                                                                                                      |

Coverage of the consumer package is 91.3%.

## Developer path (Task 6.6, not changed)

- Headless `RUN_BASH_OPTIONAL_PLAYBOOKS` stops at the first failed play (`hl_abort`).
- The attended menu goes on after a failure but prints no summary.
- `--rerun` stops at the first failure by its documented design.
