The new rule `regex-class-escape` is in and blocking, but it is red on only two of the three lines you named. The third, `podman-store-repair.yml:52`, was not actually broken.

**Why line 52 is not an instance:** it sits in an assert `that`, which is a bare expression with no `{{ }}`. Plan 00191 proved with Ansible's own Templar (commit 4977c0fc5) that Ansible keeps backslashes as written only inside `{{ }}`. Elsewhere Jinja halves them, so `'\\s'` there reached the regex as `\s`, which is correct. The fixed `'\s'` is also correct, because Jinja keeps the backslash on an escape it does not know. A rule that flagged line 52 would also flag correct code, such as `play-speech-to-text.yml:753`. So the clean fixture holds both forms of line 52.

To settle it on the host's Ansible, `CLAUDE/Plan/00235-podman-images-cleaned-up-automatically/regex-escape-proof/render_three.py` renders all three regexes before and after the fix, read-only. It needs ansible-core, which this container lacks. I did not queue it, because `meta-deploy.list` only takes `.bash` scripts, plays and plan folders. If it shows line 52 failing before the fix, both this rule and `regex-backref-escape` count backslashes wrongly and need revisiting.

- **Branch:** `agent-a48affba89defcfeb-c3247d22`, not pushed or merged.
  - b091de34a: the rule, fixtures, wiring and docs.
  - 974864a66: Task 4.10 ticked in `PLAN.md` and journalled (`JOURNAL/2026-10-28/140058Z-action-56ec7a.md`).
- **Where the rule lives:** `src-go/qa/ansible/regexescape.go`, beside `regex-backref-escape`, and run by `scripts/qa-ansible.bash`.
  - It counts backslashes the same way as `regex-backref-escape`; I moved the shared walking code out of `regexbackref.go` without changing that rule.
  - It reads the pattern of `regex_search`, `regex_replace` and `regex_findall` and of the tests `match`, `search` and `regex`, including when passed by name to `map` or `select`. Each piece of a pattern joined with `~` is checked.
  - It also flags `\b`, which Jinja turns into a backspace in bare expressions.
  - Fixtures are `tests/fixtures/ansible-regex-class-escape/flagged.yml` (22 marked cases) and `clean.yml`.
  - Docs: `CLAUDE/QA.md#regex-class-escape` and its gate-table row, plus updates to `src-go/CLAUDE.md`, `scripts/qa-ansible.bash` and `scripts/qa-routing.tsv`.
  - No `GoProjectRules.md` entry, since that catalogue is for Go-code rules.
- **Red/green proof:** I extracted `playbooks tasks vars environment` with `git archive` into scratch git repos at `0d1a61321^` and `0d1a61321`.
  - `QA_JSON_OUT=untracked/scratch/qa-before.json ./scripts/qa-ansible.bash --root untracked/scratch/dbf-before` reports 2 findings: `tasks/podman-store-ownership-probe.yml:27` (the subuid regex) and `tasks/podman-store-repair.yml:130` (the held-lock regex).
  - The same command with `--root untracked/scratch/dbf-after` reports 0.
  - Both runs also show three unrelated sudoers errors, because the archive leaves out `files/`.
  - On the live tree `./scripts/qa-ansible.bash` passes with 0, and `go test -run RegexEscape ./ansible/` passes.
  - `./scripts/qa-changed.bash` passed before both commits.
- **Instances:** the independent searcher, run before the rule existed, found nothing new. Every other doubled backslash is a backreference (`regex-backref-escape` covers those), in a double-quoted scalar, in bash, or the correct speech-to-text case. Nothing was left to fix beyond 0d1a61321.
- **Conformance review not done:** the reviewer agent could not start, because the daemon's worktree hook refused three seed entries that resolve outside the repo root. Someone should reproduce the red/green runs from the commands above before merging.

The full report is at `<worktree>/untracked/scratch/261028-dbf-regex-report.md`, and the searcher's findings are beside it in `261028-dbf-regex-search.md`.