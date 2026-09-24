# qa-reviewer: Plan 00109 Task 0.2, evdi source-tree cleanup (16a08a46..a4c547ab), opus-5, 2026-09-24

Saved by the coordinator; the reviewer is read-only.

**Verdict: PASS WITH NITS.** The cleanup cannot delete the tree that is owned or registered.
One assumption about rpm was untested, but if it had been wrong the play would stop rather
than delete anything.

## Should fix

1. **Nothing showed which output stream rpm writes "is not owned by any package" to.** The
   `failed_when` accepted the message on stdout only. The host evidence came from
   `triage.bash`, which merges both streams. If the assumption was wrong the play would
   stop at the probe on every run, before the dock recovery deploys.
   - **Resolved:** the message is accepted on either stream, in the probe and in the
     removal's condition. Any other rpm answer still fails the play.

## Nits

2. **The two loops were paired by index only.** It was correct, because both loop over one
   list. **Resolved:** the removal also requires the paired stat's path to equal the tree's.
3. **PLAN wording**, "rpm owns it not". **Resolved:** "no package owns it".
4. **`docs/playbooks.md`** didn't mention the cleanup. **Resolved.**

## Checked and clean

- **Deletion safety.** Both conditions are required. The DKMS version is the directory name
  minus `evdi-`, which matches the host evidence exactly.
- **Symlinks.** `find` does not follow them, and a dangling DKMS link still counts as
  registered.
- **Ordering.** The cleanup runs after the RPM install and `dkms autoinstall`.
- **Fail-fast.** The `failed_when` is a real probe-then-check.
- **`--check`** and idempotence are correct.
- **Placement** is the play that owns the package.
- **Ansible 2.19.** No task-name colons and no `shell:` blocks.
- **Public-repo safety.** No identifiers.
- **`--syntax-check`** exits 0.
