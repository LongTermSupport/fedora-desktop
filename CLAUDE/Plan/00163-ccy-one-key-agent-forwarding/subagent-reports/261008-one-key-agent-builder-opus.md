# Plan 00163 — one-key agent builder report (Tasks 1.1, 1.2, 2.1–2.4)

Built in one branch, one commit series; CCY 3.87.0 (container version unchanged: no
Dockerfile or entrypoint change).

## What was built

- **The one-key agent** — `helpers/ssh_agent_filter/ssh_agent_filter.py`, stdlib only, run by
  path. Listens on `--listen` (refuses an existing path; binds under a staging name, chmod
  0600, `listen()`, then hard-links into place so a client never meets a socket that is not
  yet accepting). Relays to `--upstream` (default `SSH_AUTH_SOCK`), one upstream connection per
  client, opened on first need, one thread per client. REQUEST_IDENTITIES (11) is relayed and
  the IDENTITIES_ANSWER (12) filtered to the allowed key(s); SIGN_REQUEST (13) is relayed only
  for an allowed key blob; every other type, extensions (27) and session-bind included, is
  answered FAILURE (5). Keys are allowed by SHA256 fingerprint (`--allow`, repeatable,
  validated). Messages are bounded at 256 KiB (OpenSSH's own limit); bad framing closes that
  client only. Logs name message types and errors, never a blob. Stops on SIGTERM/SIGINT/
  SIGHUP and when `--parent-pid` is gone, removing its socket.
- **Tests** — `tests/helpers/ssh_agent_filter/test_ssh_agent_filter.py` (34 tests): protocol
  units, and the filter run as a process in front of a REAL `ssh-agent` holding two generated
  keys: only key a listed (`ssh-add -l`/`-L`), `ssh-add -T` and `ssh-keygen -Y sign` (public
  half only, so it can only sign through the agent) work for a and verify, fail for b;
  `ssh-add`, `-d`, `-D`, `-x` fail and the upstream still holds both keys; extensions, an
  unknown type and a malformed sign request get FAILURE; an oversized length closes only that
  connection; concurrent clients; socket mode; no blob in the log; SIGTERM and parent-pid exit;
  existing path, no `--allow`, bad fingerprint and a dead upstream. Missing `ssh-agent`,
  `ssh-add` or `ssh-keygen` raises (no skip).
- **Lifetime (1.2)** — journal decision entry, T1.2: a child of the launcher. Started by
  `ccy_agent_filter_start` after the early EXIT trap and before `build_ssh_mounts_and_validate`;
  stopped by `ccy_agent_filter_stop` from that trap and from `cleanup` (which a restart runs
  before its `exec`); `--parent-pid $$` is the SIGKILL backstop. Directory: `mktemp -d` on
  `XDG_RUNTIME_DIR`. Installed by `play-claude-yolo.yml`'s existing "Install Shared Helper
  Libraries" loop to `/var/local/claude-yolo/lib/ssh_agent_filter.py`; the library finds it
  beside itself. No new play.
- **Launcher (2.1)** — `lib/ssh-handling.bash` 1.8.0: `ssh_key_fingerprint` (from the `.pub`
  or the key file itself, no passphrase), `ssh_key_needs_passphrase`, `ccy_agent_forwards_key`
  (needs a passphrase AND `ssh-add -l -E sha256` lists its fingerprint), `ccy_agent_filter_start`
  (exactly one selected key file; verifies the key is listed through the filter before going
  on; prints the one line), `ccy_agent_filter_stop`. In `build_ssh_mounts_and_validate` that key
  is mounted as `--ssh-agent` mounts the agent (`/run/ccy/ssh-agent`, `SSH_AGENT_FORWARDED=1`,
  `label=disable`), not as a file; the probe agent skips it; the GitHub probe uses `-i <key> IdentitiesOnly=yes IdentityAgent=<filter> BatchMode=yes` (can never prompt); the
  `SSH_ASKPASS` supply skips it. The account identity stays the key file, so `github_<alias>`
  token lookup works as for a mounted key. `ccy_restart_keys_unattended`
  (`lib/restart-request.bash`) accepts a key `ccy_agent_forwards_key` accepts, so the headless,
  restart-relaunch and restart-request checks agree with what the launch then does; their
  refusals now say to `ssh-add` the key instead of `--ssh-agent`. A banner variant says only
  that key is offered. SSH_KEYS, Quick Launch, labels and session records still name the file.
- **Signing (2.2)** — `configure_git_signing` takes a sixth argument, the filter socket; with
  it the copy names `key::<type blob>` as the filter offers it (`_agent_public_for_key`, matched
  by fingerprint), and refuses with "Load it into your ssh-agent with: ssh-add <key>" if not
  offered. The launcher passes it and no mounted path for that key.
- **Docs (2.3)** — `--help` (`--ssh-key`, `--headless`), `docs/ccy.md` (a paragraph under SSH,
  the restart paragraph, the residual-risk bullet, two flag-table rows),
  `docs/ccy-changelog.md` 3.87.0.
- **Plan 00161 (2.4)** — `_acceptance-u20.inc.bash`: `u20_agent_forwards_key` sources the
  INSTALLED `/var/local/claude-yolo/lib/ssh-handling.bash` in a subshell and calls
  `ccy_agent_forwards_key`, so the prerequisite is ccy's own decision. Accepted keys print a
  `==>` line; otherwise the OWNER line says to `ssh-add <key>` in the terminal running
  meta-deploy; an installed ccy without the function says to run play-claude-yolo.yml.

## Tests run (all green)

- `python3 -m unittest tests.helpers.ssh_agent_filter.test_ssh_agent_filter` (34), `ruff check`.
- `scripts/test-ccy-ssh-handling.bash` (90; new: filter probe argv, `build_ssh_mounts_and_validate`
  driven for the filter case: socket mounted, no key file, no prompt; askpass skip).
- `scripts/test-ccy-git-signing.bash` (75; new: the launcher's `ccy_agent_filter_start` in front
  of a real agent holding two keys, the listing through it, the copy's `key::`, a real commit
  signed through the filter and verified, a not-offered key refused, stop removes dir and
  process, a passphrase-less key starts no filter).
- `scripts/test-ccy-restart-request.bash` (166; now sources ssh-handling.bash as the launcher
  does; new: a real agent, held passphrase key accepted with and without `.pub`, unheld refused,
  no agent refused, the refusal names `ssh-add`).
- `scripts/test-ccy-restore-askpass.bash` (trap-line wiring updated; new: filter start between
  guard and probe, cleanup stops it), `test-ccy-lifecycle`, `test-ccy-teams`,
  `test-ccy-session-registry`, `test-ccy-info-flags`, `test-ccy-token-mode`.
- shellcheck on every changed bash file (warnings left are pre-existing in the launcher).
- `ansible-playbook --syntax-check` of play-claude-yolo.yml (syntax only; the worktree has no
  vault file, the main checkout's was named).

`scripts/test-ccy-ssh-probe.bash` fails in this container ("no ~/.ssh/github\_<alias> keys") as it
did before; it is not a qa-all gate. `qa-all.bash` not run (the coordinator's).

## Open points

- **Exactly one key file only.** Several selected keys, or a key file beside `ssh-agent`, are
  unchanged (each file mounted and unlocked). The helper already takes several `--allow`.
- **SELinux:** the filter is an unconfined process like the agent, so its container also runs
  with `label=disable`, as `--ssh-agent` does. Docs say so. A confined filter would need a
  policy; not attempted.
- **The key menu** still suggests the whole-agent row when the agent holds an account key; a
  file it holds is now the narrower choice. Left unchanged (not in the tasks).
- **A container that outlives a SIGKILLed launcher** loses its agent when the filter stops.
- **Host deploy:** no meta-deploy change made: Plan 00161's `deploy.bash`, already in `PLANS`,
  runs play-claude-yolo.yml, which installs the helper and CCY 3.87.0. Task 3.2 (HOST) then
  needs the saved key `ssh-add`ed in the terminal running meta-deploy.
- Plan 00161 DESIGN D38/D57/D59 describe the prerequisite in words that still fit ("naming
  `ssh-add`", "through a reachable ssh-agent"); not edited.
- Task 3.1 (qa-reviewer) is not done here.
