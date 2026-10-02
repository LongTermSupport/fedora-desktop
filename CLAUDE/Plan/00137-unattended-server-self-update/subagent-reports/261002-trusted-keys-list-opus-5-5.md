# Task 4.8: the self-update server trusts a list of the owner's keys

## The defect

`play-self-update.yml` wrote one allowed-signers line from `self_update_signing_public_key`.
Since Plan 00139 D5, host commits are signed by `~/.ssh/id` and ccy commits by the session's
GitHub account key (`~/.ssh/github_<alias>`). A server pinned to either key never deploys the
commits the other key signs.

## The fix

- `helpers/self_update/signers.py` (new, stdlib only). It takes JSON on stdin:
  `{"principal", "keys", "key"}`. The effective set is the list, then the single key, each
  key once. Keys are matched by type and blob, so a different comment does not make a new
  key. Each key is checked with the regex the play used before. It refuses: a missing list
  plus a missing key, an empty set, a string given where the list belongs, a non-string
  entry, a multi-line entry, and a principal with whitespace. A refusal exits 2 with the
  reason on stderr and prints nothing on stdout. On success, stdout is the file: one
  `<principal> <key>` line per key.
- `play-self-update.yml`:
  - the key checks left the input assert;
  - a new task, "Build the Allowed Signers List From the Owner's Keys", runs the helper
    with `command`+`argv`, its input on stdin through `to_json`, and `default(none)` for an
    undeclared variable;
  - "Write the Allowed Signers File" writes that task's stdout.
- Consumers checked for a single-key assumption:
  - `update.py`, `gate.py`, `cycle.py` and the sbin wrapper hand the file to git and compare
    `%GS` with the one principal. A file of several lines with one principal already worked,
    so only their wording changed ("the pinned key" became "a pinned key" or "a listed key").
  - `self-update.conf.j2` carries only PRINCIPAL, so it needed no change.
  - Plan 00137's `acceptance.bash` only checks the file's owner and mode, so it needed no
    change.
  - Plan 00139's `acceptance.bash` checks a scratch commit against the machine key, which is
    a desktop check, not a server consumer, so it needed no change.
- Plan 00139's `deploy.bash` now prints, in its NEXT note, every key to list: the global
  `user.signingkey` (`~/.ssh/id`), plus the `user.signingkey` from each
  `~/.config/git/github-signing-*.gitconfig` (`~/.ssh/github_<alias>`), each once.
  - A missing `.pub` is FATAL.
  - A box with no identity says that nothing signs there.
  - I tested this block alone, with a fake HOME and global config, in all three cases: both
    keys present, a `.pub` missing, and no identity.
- `localhost.yml.dist` and the `play-git-configure-and-tools.yml` comments now name the list.
- Docs:
  - `docs/configuration.md`, "Commit Signing" and "Unattended Server Self-Update": list
    `~/.ssh/id.pub` and every GitHub account key's `.pub`. The single key is still accepted.
  - `docs/playbooks.md`: "one of the owner's pinned keys".
  - `DESIGN-cycle.md`: the paths table row.
- Plans:
  - 00137: Task 4.8 marked ✅, with a HOST owner sub-item, and an amendment note on D3.
  - 00139: the HOST sub-item of Task 3.1 now points at the list.
  - One journal entry in each plan, added with `mkplan.bash --journal`.

## TDD

I extended `test_update.py` first. It has a third key, "account", and a `TestSeveralTrustedKeys`
class that writes the file through `signers.render`. Its cases:

- a commit the second key signed deploys;
- the first key still deploys;
- a list of one key does not deploy a commit the other key signed;
- a key not on the list waits;
- `verify_head` accepts a HEAD the second key signed.

`test-self-update-cycle.bash` now writes a two-key file through the helper. "a change no play
reads" is signed by the account key, and a new check asserts that the clone moved to it. Every
later cycle then passes the wrapper's HEAD check on a commit the account key signed. Both
suites failed to import before the helper existed (red), and pass now.

## Verification

- `python3 -m unittest` over `test_update`, `test_signers`, `test_cycle` and `test_gate`:
  195 tests, OK.
- `bash scripts/test-self-update-cycle.bash`: 164 passed, 0 failed.
- `ruff check helpers/self_update tests/helpers/self_update`: clean.
- `shellcheck -x` on the changed bash files: clean.
- `ansible-playbook --syntax-check play-self-update.yml` with a dummy vault password: rc 0.
- The play's stdin Jinja, rendered with the venv's jinja2: the list case gives
  `"keys": [...], "key": null`, and the single-key case gives `"keys": null, "key": "..."`.

The full `qa-all.bash` was not run; the coordinator runs it. The play has not been deployed,
which is the HOST step.
