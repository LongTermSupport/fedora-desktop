# QA Review: Plan 00139 (6001a540; range 1382ef08..78d33e43)

Saved by the coordinator: the reviewer is read-only. The review ran before the
implementation journal entry (`ddc77c74`) landed, so finding 5 is half answered already.

**Verdict: FAIL (BLOCK)**

## Blocking

1. **Signing on globally breaks the self-update-cycle test after deploy, on the host and in
   ccy 3.66 containers.** In `scripts/test-self-update-cycle.bash`, the "unsigned" fixture
   commits (lines 494 and 570) go through `git_work` (174-177). That helper sets the trusted
   fixture `user.signingkey` but does not isolate the global config, so a global
   `commit.gpgsign=true` signs them with the trusted key.
   - With `GIT_CONFIG_COUNT` setting `commit.gpgsign`/`tag.gpgsign`: rc=1, 149 passed, 14
     failed.
   - Baseline without it: 163 passed.
   - CI stays green because it has no global config.
   - Fix: set `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1`, as
     `tests/helpers/self_update/test_update.py:38-40` does.
   - Seven other helper test files make commits without isolation: the `play_ledger` ×4,
     `test_affected_plays`, `test_judge_run` and `test_login_report`. They were not run with
     signing on.

## Should fix

2. **The deploy order is backwards for the reason it gives** (`00139/deploy.bash:12-14,66-70`).
   The comment argues the launcher should go first. A 3.66 launcher with signing off is
   harmless (`ssh-handling.bash:1212-1220`). With git-configure first, a failure between the
   two legs leaves containers that cannot commit. Swap the legs.
3. **The ccy docs don't say the signing key is exposed.**
   - `docs/ccy.md:646` and `:1112` say `--no-ssh` mounts no key, but the signing key is now
     always staged.
   - The residual-risk list (`:355-366`) doesn't name that key as release authority for a
     server that runs plays as root.
   - `docs/ccy-changelog.md:26` says the key "never leaves" the temp directory, which is
     false inside the container.
4. **The passphrase assert can give the wrong diagnosis**
   (`play-git-configure-and-tools.yml:94-118`). `creates:` (line 72) accepts a zero-byte or
   corrupt key. The play then reports "has a passphrase" and gives a remedy that doesn't
   fit. Branch on ssh-keygen's stderr.
5. **The JOURNAL has no implementation or handoff entry.** The implementer's "Journal
   bodies" were never filed, and `PLAN.md:120` Delivery is still a placeholder.

## Nits

06. `test-ccy-git-signing.bash:160-170`:
    - It doesn't check that staging comes before `container_cmd run`.
    - The fixture key is fake, so no test signs a real commit through the repointed config.
07. `acceptance.bash:129` (check 8) treats `git config` exits 2 and 128 as "absent".
08. `acceptance.bash:163-177` (check 11):
    - When all lookups fail it blames registration.
    - A key on any account passes, which doesn't prove the Verified badge.
09. `docs/configuration.md:220`: `gh auth refresh` acts on the active account, so switch
    accounts first.
10. `00137/acceptance.bash:97`: the unsigned-commit owner step now needs
    `-c commit.gpgsign=false`.

## Checked and clean

- **Key and config:** key generation is idempotent and 0600. A leftover passphrase key fails
  before any config is written.
- **XDG `state: absent` on a missing file:** a no-op. Checked in community.general 11.4.2's
  source plus a live git exit code; the `>=13` series was not read.
- **Server profiles:** server-signed commits are UNTRUSTED and passed over (`gate.py:40`).
- **The key in the container:**
  - It is staged in a 0700 `mktemp` directory, mounted `:ro` with `,Z` (`claude-yolo:2082`)
    and removed by the EXIT trap (`:1991`).
  - The in-container path exists, and the image has `openssh-client`.
  - There is one run site (`:3216`). Headless, supervise and tmux, and restore
    (`session-registry.bash:746`) all go through it.
- **Refusal:** it only triggers when signing is on.
- **Version bump and changelog:** CCY 3.66.0, lib 1.5.0, changelog present. No container
  bump is needed.
- **Removed references:** no live `sign-deploy` or opt-in wording remains outside history.
  The docs state that a push from that machine is a release.
- **Public-repo hygiene:** clean; the other infrastructure repo is not named.
- **09afdfc7:** no grounded findings. `pipx pin` idempotence couldn't be checked here (this
  container's pipx is 1.1.0).

## Gates

- **qa-all.bash:** fails on toolchain (ruff 0.16.4 against the 0.16.8 pin),
  bash-history-search (no gawk in the container) and helper-counts-reader. All three come
  from the container, not this plan.
- **Passing:** ansible-syntax (82 playbooks), ccy-git-signing (19), self-update-cycle (163,
  only because signing is off in this container), helper-tests.
- **plan-qa --sweep:** 0 block, nothing for 00139.
- **`--syntax-check` on the play:** rc=0.
