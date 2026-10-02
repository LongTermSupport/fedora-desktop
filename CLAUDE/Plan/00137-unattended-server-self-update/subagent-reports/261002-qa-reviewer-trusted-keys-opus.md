**Verdict: FIX-BEFORE-MERGE.** I found no way to abuse the list to trust keys beyond the owner's, but there are three should-fix items.

**BLOCK:** none.

Trust model, checked and clean:
- **Every entry is checked or refused, never skipped** (`helpers/self_update/signers.py:33-51`). The match must cover the whole stripped line, starting with a non-certificate key type, so `cert-authority`, `namespaces=` and other options cannot get in.
- **Principal and line breaks.** The principal cannot contain whitespace (`signers.py:36,83`). Embedded newlines are refused (`test_signers.py:75`).
- **Empty input.** An empty list, `""`, `null`, a string where the list belongs, or a non-string entry all exit 2 with nothing on stdout. The play then fails before the copy at `play-self-update.yml:380`.
- **A bad key blob fails closed.** If the base64 is invalid, ssh-keygen fails to parse that line, so the key is not trusted.
- **The old single var cannot be lost.** `default(none)` → `null` → appended after the list (`signers.py:61`). No other code reads it (git grep).
- **Several lines under one principal work as `gate.judge` assumes.** git's `%GS` comes back as the principal, which `gate.judge` compares by equality (`gate.py:58-66`). `test_update.py` `TestSeveralTrustedKeys` proves the second key deploys and an unlisted key still waits.
- **Tests use the production path.** The cycle test builds allowed_signers with the same `python3 -B -m helpers.signers` call the play makes.

**FIX-BEFORE-MERGE**
1. **`deploy.bash` lists every account's key, not just the ones that push this repo.** `CLAUDE/Plan/00139-commit-signing-everywhere/deploy.bash:217-222` globs every `~/.config/git/github-signing-*.gitconfig`. The docs say to list only the keys "ccy sessions push this repository with" (`docs/configuration.md:278,293`). An owner who copies the output will trust unrelated accounts' keys, which widens trust. Filter by the accounts whose alias this repo's remote uses, or label each key with its account.
2. **The acceptance script never checks the file's contents.** `CLAUDE/Plan/00137-unattended-server-self-update/acceptance.bash:268` only checks the file's owner and mode. Nothing checks the deployed file has one line per declared key (e.g. a `COVERAGE: n of m keys` line). The open HOST step for Task 4.8 has no verifier.
3. **Stale trust premise in a paragraph this commit edited.** `helpers/self_update/gate.py:6-8` still says agents "never hold the key". After Plan 00139 D4 and this task, ccy agents sign deployable commits with the account key.

**ADVISORY**
- `deploy.bash:218`: a `github-signing-*.gitconfig` with no `user.signingkey` makes `git config --get` exit 1, and `set -e` kills the script with no message. Handle rc=1 the way line 207 does.
- `deploy.bash:219`: removing duplicates by matching `" ${signers[*]} "` breaks on key paths with spaces. Today's paths are absolute under `/home/{{ user_login }}`, so this is low risk.
- A trust consequence for the owner to accept, not a code defect: the account key is also the GitHub login key. Anything inside a ccy session started with that account can now both push and sign a commit the server will run as root. D3/D4 accept this for "the owner's agents", but the plan doesn't spell it out.
- The play has no `check_mode: false` on the build task, so `--check` would leave `.stdout` undefined. The rest of the play has the same gap already.

**Checked clean:**
- **Ansible style.** argv form, `chdir`, `changed_when: false`, no inline shell, and the copy is gated on `when: self_update_on`.
- **Public-repo safety.** The diff has only placeholders and `example.com` addresses.
- **Docs and `localhost.yml.dist`** are updated.
- **Plans.** Task 4.8 is ticked with its HOST step left open.

**Gates:**
- `test_signers` + `test_update`: OK.
- `scripts/test-self-update-cycle.bash`: passed 164, failed 0, including "a commit the second listed key … signed is deployed".
- `--syntax-check` of `play-self-update.yml`: passed.
- `qa-all.bash` and `plan-qa`: not run, as you asked (the coordinator is running the full gate).

**I broke the read-only rule once:** a stray redirect created an empty untracked file, `/workspace/untracked/scratch/.qa-cycle-err`. Please delete it.