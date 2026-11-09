# Re-review: fix/goodbye-acceptance-signing at 20bb7e98 (Plan 00194 Task 2.0b)

Narrow re-check of the signing fix on top of the earlier MERGE of acceptance.bash
(261019-goodbye-acceptance-review-fable.md). Branch fast-forwards from F44 4f4cc87a (checked
with merge-base). Nothing changed.

## What was checked

**Signing.** `configure_git_signing` (lib/ssh-handling.bash) reads `commit.gpgsign` and
`tag.gpgsign` from the project's `--local` config first; a value of `false` there (git exit 0)
means the `~/.gitconfig` copy is not consulted for that name, `signing` stays false and the
function returns 0 at line 1933, before any key, agent or `gpg.format` lookup. With `--no-ssh`
the launcher's `SSH_KEYS` is empty, so `signing_primary` is empty: no key file is mounted, no
agent is forwarded (claude-yolo:2420-2438). `gb_signing_off` runs `git -C <scratch> config --local` only; the owner's `~/.gitconfig` is never written. The fake leg
`gb_fake_signing` runs the real `configure_git_signing` under a fixture HOME /
`GIT_CONFIG_GLOBAL` / `GIT_CONFIG_NOSYSTEM=1`, and the `inherited` leg reproduces the exact
refusal the host run printed, so the fake exercises the code path the fix vouches for.

**Prompt detection.** `gb_waiting_prompt` matches only the last non-blank line of the visible
pane against `ccy_known_prompts` as a prefix, and only while the container is not yet up. The
constants (common-pure.bash lines 39-66) are used solely at `read -rp` sites (verified for the
zombie menu, existing-containers, relabel-fix, token-expired, token-recovery); none is printed
as a status line. The prompts that could plausibly share a prefix with ordinary output
(`Select SSH key`, `Select token`, `Enter passphrase for`, `Connect to this network?`, `Start services with`) are all answered by the flags given (`--no-ssh`, `--token`, `--no-network`,
`--compose skip`) so they are never printed at all. No false positive found.

**`--compose skip`.** Valid flag (claude-yolo:812); `ccy_compose_answer` answers N without
asking. The scratch checkout has no compose file, so it is a no-op there.

**pane-errors.** Only lines that start with `ERROR:` after terminal-sequence stripping, the
last 5. Every `print_error` in token-management.bash and claude-yolo near token handling
prints a token *name*, a *path* or a fixed sentence, never the token value; `--token` on the
launch line is the token name. No route for a secret into the FAIL text.

**Cleanup.** The diff touches no cleanup function (`gb_cleanup`/`plan_on_cleanup` hunks
absent); a detected prompt or an early-ended launcher returns 1 from `gb_launch` into the same
cleanup as before.

**meta-deploy.list.** Lines 1-21 byte-identical to F44 (the only hunk is `@@ -30,0 +31,12`).
Order: play-agent-bus.yml, play-claude-yolo.yml, then the 00194 plan folder; its description
comment is present as the header requires.

## Runs

- `python3 test_goodbye_check.py`: Ran 31 tests, OK (includes the fake run).
- `acceptance.bash --fake`: exit 0, FAKE ACCEPTED; the five new launch legs pass, the
  `inherited` leg prints the real refusal.
- `shellcheck -x acceptance.bash _acceptance-goodbye.inc.bash`: clean.
- `ruff check` on both Python files: clean. (`ruff format --check` reports pre-existing
  line-length formatting in both files; QA does not run `ruff format`, and the fix did not
  introduce it.)

## Notes, not blockers

- The fake legs read the checkout's `files/var/local/claude-yolo/lib`, while the real run
  launches the deployed `/var/local/claude-yolo`. The queue runs play-claude-yolo.yml first,
  so the two agree at acceptance time.

## Verdict

MERGE
