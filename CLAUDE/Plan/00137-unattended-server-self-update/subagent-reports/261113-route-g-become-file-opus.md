# Route G: the become password needs no home in host_vars (Tasks 6.1, 6.2)

The owner chose route G on 2026-11-13 and declined F-full (not deferred). D5 stands: the
cycle's plays run as the login user through `runuser`, with the become password handed
over on a descriptor from the root-only `/etc/fedora-desktop/self-update.become`.

## What changed

- `helpers/self_update/become_file.py` (new; tests first in
  `tests/helpers/self_update/test_become_file.py`). Run as root with the file's path, it
  prints `{"state": "present"|"missing"|"unusable", "reason": ...}` and exits 0. "Present"
  means the cycle's own `read_private(..., secret=True)` accepts the file (regular file,
  not a symlink, root-owned, 0600) and the content is exactly one non-empty line, with an
  optional final newline. The content check reads bytes, because text mode turns CRLF into
  LF and run.bash would keep the CR. The permission rule is imported from `cycle.py`, not
  copied, and a test asserts that. No output ever carries the content (tested).

- `tasks/ask-and-save-secret.yml`: new option `secret_save` (default true). With false,
  the answer is used for this run only and nothing is written to `localhost.yml`. Its two
  failure messages then add that the value is needed for one run only. Other callers are
  unchanged.

- `play-self-update.yml`, before any change:

  1. probe the root-only file with the helper (`changed_when: false`, a read-only probe);
  2. record whether `self_update_become_password` came from host_vars;
  3. ask for it (`secret_save: false`) only when it is undefined and the file is not
     present; the explanation says why (the probe's reason);
  4. assert a defined value is one line (now also no CR);
  5. assert there is a value or a present file, otherwise fail with the remedy (run from a
     terminal, or declare the value in host_vars for one run);
  6. when the value came from host_vars, print a deprecation advice: delete the entry; for
     a changed password declare the new one for one run.

  "Provision the Become Password" now runs only when the run has a value. With none, the
  file the probe found present is kept as it is. The "off" path still removes the file.

- Docs: `docs/configuration.md` (first install, afterwards, value still present, password
  changed), `localhost.yml.dist`, `DESIGN-cycle.md` (table row and a "Become password"
  bullet: the cycle reads only the root-only file, never host_vars), and the plan's
  `deploy.bash` usage text.

## Decision: a value still in host_vars is used, with a deprecation advice

The two choices were to use the value and refresh the file when it changes, or to ignore
it with a warning. Using it was chosen:

- A declared value is the desired state (IaC). Ignoring it would silently drop a rotated
  password, and the cycle would then fail at night on sudo with no sign at deploy time.
- Rotation then needs no new mechanism: declare the new password for one run, then delete
  it. Without this, rotation would need a manual removal of the root file or another flag.
- The downstream's "tolerated, not rejected" item is met either way: a host_vars that
  defines the value runs cleanly, and the copy is idempotent (`changed` only when the
  value differs).

## The downstream request's acceptance list against G

| Item                                          | Under G                                                                                                                                                                                                                                                  |
| --------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Outcome 1: not required                       | Met. The play requires it only when the root-only file is missing or unusable. The cycle never read it.                                                                                                                                                  |
| Outcome 2: not written / no descriptor        | **Not met, by design.** The root-only file and the descriptor stay (D5). The user process still sees the password in flight during a cycle, which D5 accepts as the trusted account.                                                                     |
| Outcome 3: nothing reads it from the clone    | Met. The cycle reads only the root-only file. The clone's host_vars copy is refreshed from the user's `localhost.yml` on each play run, so once the value is deleted there, the next play run removes it from the clone.                                 |
| Tolerated, not rejected                       | Met. It is used and draws a deprecation advice, and never fails a run.                                                                                                                                                                                   |
| No degrade                                    | Met. Units, timer, sudoers, verify and the cycle are unchanged. `cycle.py` is unchanged.                                                                                                                                                                 |
| Existing installs cleaned up (file removed)   | Not applicable. G keeps the file, so it is not removed (that item belongs to option F).                                                                                                                                                                  |
| Docs updated                                  | Met, for G: the docs say the password's one home is the root-only file and host_vars needs it once at most. They do not say the cycle runs as root, because it does not.                                                                                 |
| Upstream QA green, tests updated, not deleted | `qa-changed` passed (ansible-syntax over the play and every other asker caller, the new unit tests, test-self-update-cycle.bash). No test was deleted. `scripts/test-self-update-cycle.bash` (the cycle's file contract) is unchanged and still applies. |
| Public-repo rule                              | Met. Nothing install-specific is in tracked files.                                                                                                                                                                                                       |

R3, the at-rest route (the vaulted value beside its vault key in a user-owned checkout), is
closed once the downstream deletes the value from host_vars.

## What the downstream does, in order

1. Deploy this change: converge `play-self-update.yml` with the value **still** in
   host_vars. The play writes or refreshes the root-only file and prints the deprecation
   advice.
2. Delete `self_update_become_password` from the user checkout's host_vars, and from the
   guest vault if nothing else uses it there.
3. Converge `play-self-update.yml` again. It must not ask, must keep the file, and must
   report the file task as skipped. The same run refreshes the deploy clone's host_vars
   copy, which then no longer carries the value.
4. Let one cycle run (or `fedora-desktop-self-update run --dry-run` through the scoped
   sudo, then a real night) and confirm the plays ran.

A host with no root-only file and no value now fails at the "Has a Become Password"
assert before any change, with the remedy in its message.

## Not proven here

The container never runs plays. A desktop host run proves nothing, because the play's
scope guard ends it on a non-server profile, so no meta-deploy entry was added. Task 6.3
holds the server proof, which is the downstream's step list above.
