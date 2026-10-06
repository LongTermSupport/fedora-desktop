# qa-reviewer: ccy writes ccy.env.local.dist (CCY 3.83.0)

Reviewer: qa-reviewer (Opus 5.5), 2026-10-06. Saved by the coordinator; the reviewer has no
Write tool.

**Verdict: FIX-BEFORE-MERGE.** Four should-fix findings, all fixed before commit.

## Should fix

1. **A failed write to the dist went unnoticed.** `ccy_env_local_dist_sync || exit 1` runs
   the function with `set -e` off, so a failing `printf … >"$dist"` returned 0 and the
   launch went on. Fixed: each write is `|| { print_error …; return 1; }`. The test puts a
   directory where the dist goes (a failure even for root) and expects exit 1.
2. **A newer ccy's dist didn't flag an older local file.** The "based on" check compared
   against ccy's own version, not the one in the dist file. Fixed: it compares against the
   newer of the two. Test case added.
3. **`deploy.bash` still named CCY 3.81.0 and container 2.43**, and `meta-deploy.bash` had
   no 00160 entry. Fixed in the same commit.
4. **The `CLAUDE/Plan/README.md` row** still said the template came from the hooks daemon.
   Fixed.

## Nits

- `ccy --help` writes the file too, because help is parsed after the sync. The `.gitignore`
  check already behaves this way. Left as is.
- A "based on" number too large for bash makes `[` fail with no warning. Harmless; left.
- A dead line in the test. Removed.
- Nothing asserted that stdout stays empty. A case now does.

## Checked and clean

- Every launch path reaches the call: restart relaunch, session restore, `--supervise`.
- Writing a tracked file into each project is consistent with the generated `.gitignore`.
- Version handling: a non-numeric or missing first line is rewritten; leading zeros are fine.
- Stderr hygiene, the version bump (no image change, so container 2.44 stays), test realism
  (the real function), public-repo safety, and docs/QA.md/plan agree with the behaviour.
