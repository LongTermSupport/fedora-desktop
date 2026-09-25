# QA Review: Plan 00139 D5 (branch plan-00139-d5-login-key-signing)

Saved by the coordinator from the reviewer's reply; the reviewer had no file-writing tool.

**Verdict: FIX-BEFORE-MERGE.** Nothing blocks: the owner can commit through every interim
state, and deletion is fail-fast.

## Should fix

1. **ccy sessions already running keep signing with the retired keys.** Pre-3.70 sessions
   signed with a copy at `/tmp/claude-yolo-*/git-signing-key` (F44 `claude-yolo:2018`,
   `entrypoint.sh:41`). Leg 3 deletes that key's GitHub registration, so their later pushes
   show Unverified. `deploy.bash:30-31` says the opposite. Fix: the play asserts no such file
   exists before retiring, and acceptance checks the same.
2. **A retired key with no `.pub` is skipped silently** (`cli.py:239-241`). The play then
   deletes the private key, and the GitHub registration can never be matched again. Fix:
   derive it with `ssh-keygen -y -P ""`, or refuse; add a test.
3. **The real-key test keeps `key_0.pub`, but production mounts only the private key**
   (`ssh-handling.bash:830,836`). Signing without `.pub` works, but the test does not show
   it. The forwarded-agent `key::` path is only tested against a stub. Add a real-agent case.
4. **Acceptance check 6 only checks that the agent holds `~/.ssh/id`.** It should check every
   `github_<alias>` key too and print `n of m`.
5. **Acceptance check 14 matches titles from every machine.** Another machine not yet on D5
   would fail this one, and this play cannot fix that. Filter on `^<hostname> `.
6. **The leg-1 comment is wrong** (`deploy.bash:14-16`). Signing is already on, so between
   legs 1 and 3, `--ssh-agent`-only and `--no-ssh` launches are refused. The refusal also
   advises loading the key being retired (`ssh-handling.bash:1310`).
7. **The self-update server step (PLAN Task 3.1) does not name `~/.ssh/id.pub`.** The deploy's
   NEXT section does not mention the server either.
8. **Plan drift:**
   - Plan 00137 `PLAN.md:81` and `deploy.bash:9-11` still describe the passphrase-free key the
     play used to generate.
   - This plan's D3 (`PLAN.md:52-53`) still says the launcher mounts the key.
9. **A machine with no `github_accounts` never deletes `id_ed25519_git_signing`**, because the
   retirement sits under `when: github_accounts_configured`.

## Nits

- The retire refusal gives no remedy (remove `git_signing_key` from host_vars).
- A locked agent is reported as "does not hold"; nothing tests `ssh-add -L` rc=1.
- The label for a `key::` literal is garbled (`basename` on base64).
- The templated `argv: >-` is used nowhere else in the repo.
- `playbook-main.yml` imports claude-yolo after the signing plays.
- D5 states "crowded the desktop agent" as fact, but the journal says it was not measured.
- A markdown indent in D5.

## Clean

- Deploy order and host commits: `check-selection` reads the real `~/.gitconfig` before any
  retirement.
- Retirement: every check runs before the first DELETE.
- Every `configure_git_signing` case.
- Old key names appear outside the retirement code only at `configuration.md:233-234`.
- No public-repo leaks.
- IaC placement.
- CCY 3.70.0 bump, and no container bump is needed.
- Docs.

## Gates

- `qa-all.bash` passed up to the js stage, then stopped: the worktree has no
  `extensions/node_modules`, a setup gap, not D5.
- The gates after js, run by hand, all passed: helper tests, test-ccy-git-signing (54),
  ssh-handling (44), gh-scopes (818), docs, session-registry, and the unit tests (48).
- `plan-qa --sweep`: 0 block. Its one 00139 advisory is journal order in entries older than
  D5.
- `--syntax-check` passed for the 3 playbooks.
