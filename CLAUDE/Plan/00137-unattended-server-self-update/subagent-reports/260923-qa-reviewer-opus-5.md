# Plan 00137 — full-diff qa-reviewer (Task 5.2, round 1), opus-5, 2026-09-23

Saved by the coordinator: the reviewer is read-only and could not write this file.

**Verdict: BLOCK**

## Blocking

1. **The first cycle runs an unsigned tip as root.** The play clones the remote tip (`update: false`) and never verifies it. The gate judges only commits above HEAD, and with no deployed record the cycle runs every allowlisted play from that tree. The sbin entry point has already imported `cycle.py`, `update.py` and `gate.py` from the clone as root. So anyone with push access decides what root runs on enable, and on every re-clone. This contradicts the plan's trust model and `docs/configuration.md`. The end-to-end test starts from a signed commit, so it cannot catch this.

## Fix before merge

2. **The D5 hardening claims are false.**
   - The server has no NOPASSWD:ALL, and the become password is copied into a user-owned `mktemp` file for the whole play run (`run.bash`), readable by any process running as the user. That makes the `ptrace_scope=1` rationale moot.
   - The module, action, become, filter and module_utils plugin paths default to `~/.ansible/plugins/*`, and the cycle's environment overrides none of them.
   - "No user-writable code on the path" and "never touches a disk" are both untrue.
3. **The plan index row and the Overview's step order are stale.**

## Should fix

4. No test pins that extra arguments cannot override the entry point's fixed paths. They are rejected today only by how argparse handles subcommands, and sudoers allows any arguments. Narrow sudoers to the exact argument lists.
5. The real-wrapper test has no gate-refusal (exit 20) case and no wrong-remote case.

## Nits

- DESIGN-cycle.md says the result has no paths, but `plays` and `detail` carry repo-relative play paths.
- The cycle does not check that the become and vault files are unreadable by others.

## Checked and clean

- Sudoers is validated with `visudo -cf` and installed 0440.
- The entry point cannot be steered (fixed PATH, `env_reset`, no SETENV, and the units set no environment).
- The lock uses O_NOFOLLOW and O_EXCL, with owner and type checks.
- Gate refusals: bad, revoked and uncheckable signatures refuse, and git settings are pinned on the command line.
- A failed play means no reboot, and a test covers it.
- The published result is 0640 in a 2750 directory, with no identifiers.
- The CCY and run.bash changelogs are complete.
- Placement: an optional play, server scope, off by default.
- No leaks.
