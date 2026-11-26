**QA Review: Plan 00265 R79 (the play-written pinned list and the guard now failing closed)**

**Verdict: FIX-BEFORE-MERGE.** Nothing here blocks. The design is sound and the tests are real. I found one gap where a name could reach play output, one stale instruction that would lock the user out, one gap in which names get covered, and one deploy step that will fail on this machine.

**I did not write the report file.** This review is read-only and redirects are forbidden, so `CLAUDE/Plan/00265-teamchat-v2-smoke-test-register/subagent-reports/261126-r79-pinned-writer-qa-review.md` does not exist. The full report is below for you to save.

### Blocking
None.

### Should fix
1. **The play claims no team name is ever printed, but one can be.** `playbooks/imports/play-git-hooks-security.yml`, task "Read the Teams This Bus Host Serves", relies on `no_log: true`. In ansible-core 2.22, `no_log` keeps `warnings` (`_internal/_task.py:36-44`, the `PRESERVE` set), and the callback prints them (`plugins/callback/__init__.py:379`). If `find` fails to `lstat` an entry it warns `Skipped entry '/var/lib/teamchat-ctl/<team>' due to access issue.` (`modules/find.py:587`), which prints the team name. As root this only happens in a race with a team being removed, but it breaks the stated guarantee. The Python test never checks this task's warnings.
   - Fix: list the directory with a `command` task under `no_log`, or have `pin` take the directory and read it itself.
2. **The guard still tells users to hand-edit the pinned list, which `pin` then refuses forever.** `src-go/qa/privatenames/main.go:237` says "put the team's name in <git dir>/private-terms-pinned (one per line)". `pin.go:233` refuses any list without the generated header, and SecurityRules now says the file is "never hand-edited".
   - Fix: point that message at `private_name_terms` and the play, as line 234 already does.
3. **Only some name-bearing inventory fields are pinned, and that is not stated anywhere.** `teamEntry` (`pin.go`) reads only `team`, `name`, `coordinator` and `duties`. `lxcEntry` reads only `container`. Not pinned:
   - `agent_bus_teams[].org`, `repos[].repo` and `exclude` (the GitHub owners and client repos: `localhost.yml.dist:152-157`)
   - `agent_bus_lxc_members[].user`, `checkout` (`/home/<agent-user>/<repo>`), `repo` and `host` (`:173-207`)
   - `teamchat_site`

   SecurityRules lists "client projects" as private. `humans` is left out with a reason; these fields are not mentioned.
   - Fix: pin them (the public allowlist already filters public names), or document each exclusion with its reason. Also have `pin` print a count per source rather than one total.
4. **The queued deploy will stop at `pin` on this machine.** `.git/private-terms-pinned` already exists (95 bytes, mode 600). It does not start with `# Generated`, and the header alone is 304 bytes. So the queued `play-git-hooks-security.yml` run will take the "written by hand" refusal. The `meta-deploy.list` entry describes this case in general terms but does not say it will happen here.
   - Fix: tell the owner before they run `meta-deploy.bash` that they must first move those names into vaulted `private_name_terms` and delete the file.
5. **A pin is only refreshed when the main playbook runs.** `play-git-hooks-security.yml` is the right owner (git-hook state, runs on every desktop, `private_name_terms` applies without a bus). But `play-teamchat.yml` is optional and not in `playbook-main.yml`. A team added through it, or through `play-agent-bus.yml` / the element play, is not pinned for other containers until the hooks play runs again.
   - Fix: make `play-teamchat.yml` and the agent-bus plays run the same pin step, through a shared `tasks/` file rather than a copy, or record this lag in SecurityRules and the R79 row.

### Nits
- `src-go/qa/privatenames/main.go:221`: the R79 row itself says the `len(fresh) == 0` check "could never fire", but it is still there. Delete it or say why it stays.
- `CLAUDE/SecurityRules.md:39` still opens with "`localhost.yml` never held them". It now holds team names and `private_name_terms`.
- `scripts/qa-private-names.bash`: the header comment lists the modes but not `pin`.
- `pin.go`: `teamIDGrammar` is a third copy (after `paths/ids.go:71` and `proto/ids.go:43`). It can't be imported because `qa` is a separate module, but no test keeps the copies equal. A served directory with a valid but differently formed id would be dropped silently.
- `replacePrivate` renames without an fsync.

### Checked and clean
- **Fail-fast:** both `failed_when: false` are annotated `FAIL-FAST-OK`, and the next task asserts on the registered result each time (find: `files` defined and `skipped_paths` empty; pin: `rc == 0`). The final assert prints a pass line with the count.
- **`no_log` on the other tasks:** the count task, `find` and `command` are `no_log`. The two printing asserts reference no variable that holds a name (Python test). `find`'s `msg` text is generic (`find.py:561,649`).
- **Bus-client failure handling:** `proc.OS` returns an error only when the process never started (`proc.go:89-90`, `runFailure`). A non-zero exit stays a Result, so `errors.Is(err, exec.ErrNotFound)` correctly tells an absent client from one that cannot start.
- **Tests:** Go `privatenames`, `cmd/qa-private-names` and `installplay` all pass. The Python test passes (6 of 6). It drives the play's own Jinja through the real Ansible Templar with real `!vault` items, so the vault check is tested on the real code path. `TestThePinnedListPinWritesIsTheOneTheGuardReads` reads the written file back through the guard. All fixture names are invented and the IP is from RFC 5737.
- **Placement and ordering:** the hooks play is the right owner (see item 5 for the one gap). Importing `play-golang.yml` matches the other main plays. `root_dir` is defined in the play.
- **Docs and plan:** `vault.bash encrypt <varname>` exists (`vault.bash:142`). The R79 row's commit `55c2f34e6` exists. `plan-qa --sweep` reports nothing for 00265. `__pycache__` is gitignored.

### Mechanical gates
- **qa-all.bash:** not run. A sub-agent is barred from full-suite QA (R-SUBAGENT-FULL-QA), so the coordinator must run it.
- **qa-ansible.bash:** passes.
- **Syntax-check:** could not run here. Both `ansible-playbook --syntax-check` and `qa-ansible-syntax.bash` stop with exit 2 because `vault-pass.secret` is absent in the container. CI or the host must run it.
- **plan-qa --sweep:** no findings for 00265 (32 elsewhere, none in this diff).
- **qa-helper-tests:** this diff requires it. I ran only the new module directly; the full gate was not run.
- **ESLint and extension compatibility:** not triggered by this diff.
- **The private-name guard over this diff:** not run, because it rewrites `.git/private-terms`. Run `qa-changed.bash` before committing.