# The QA gate cannot pass in a git worktree of this repository

Found while doing Plan 00123's work in a worktree. It is not caused by that work and is not
fixed by it; it is written down here so the decision sits with the owner rather than
evaporating.

## What happens

`./scripts/qa-all.bash` — which `CLAUDE.md` requires before **every** commit — runs green in
a worktree except for one stage, `qa-ansible-syntax.bash`, which fails on every playbook:

```
[ERROR]: The vault password file <repo>/vault-pass.secret was not found
```

Every other stage passes: `qa-bash`, `qa-python`, `qa-patterns`, `qa-ansible`, `qa-js`,
`qa-docs`, and every `test-ccy-*` / `test-planlib` suite, including this plan's own
`scripts/test-ccy-session-registry.bash`.

## Why

`ansible.cfg` points at the vault password file by a **relative** path. That file is a real
secret: gitignored, and therefore absent from any clean checkout. `--syntax-check` is
parse-only and never decrypts anything, but ansible still refuses to start unless the file
exists — so the gate dies before parsing a single playbook.

This bites in two places, and only one of them was noticed:

- **CI** hit it and grew a workflow step that writes a throwaway placeholder
  (`.github/workflows/qa.yml`), with a comment explaining exactly this.
- **A worktree** hits the identical wall and has no equivalent.
  `.claude/hooks-daemon.yaml`'s `worktree_create.seed` seeds one entry and states plainly
  that the vault file is deliberately **not** seeded, because it is a protected path and
  playbooks never run inside a worktree. That reasoning is correct for *running* a playbook;
  it does not cover *parsing* one, which is what the gate does.

A precondition each caller has to remember is a precondition that gets forgotten. CI
remembered; worktrees never had the chance to.

## Why it is not fixed here

The file is covered by the daemon's `secret_file_guard`, whose denial says that only a human
may lift it and that no workaround should be sought. The worktree exclusion is a recorded
decision, not an oversight. Changing a shared QA gate from a feature branch, to reach around
a guard that exists specifically to keep agents out of that area, is the kind of workaround
this repository bans — and it would be the wrong person making the call.

## How this plan's Ansible work was verified instead

With the form the guard explicitly permits: the password file in flag position, pointed at a
throwaway file that can decrypt nothing.

```bash
PW=$(mktemp); printf 'syntax-check-only-never-decrypts\n' > "$PW"; chmod 600 "$PW"
ANSIBLE_CONFIG="$PWD/ansible.cfg" \
  ansible-playbook --vault-password-file "$PW" --syntax-check playbooks/imports/play-claude-yolo.yml
rm -f "$PW"
```

That exercises the same parser the gate does, on the playbook this plan changes.

## The options, for the owner

1. **Make the gate provide its own precondition.** When the configured file is absent,
   `qa-ansible-syntax.bash` creates a throwaway placeholder in a temp file and points ansible
   at it through the environment — nothing written into the checkout, the real file untouched
   where one exists. This makes the gate work in every worktree and on every clean checkout,
   and **retires the CI step**, leaving one mechanism instead of two. This is the option that
   removes the footgun rather than documenting it.
2. **Seed the file into worktrees.** Rejected already, for good reasons, and this note does
   not reopen that.
3. **Declare the stage out of scope in a worktree.** Cheapest, but it means the mandatory
   pre-commit gate quietly means something different depending on where it runs — which is
   the "could not tell" / "nothing to do" collapse this repo keeps having to fix.

## Two smaller gaps of the same shape, dealt with in this plan

Both were within reach, so they were handled rather than listed:

- **`extensions/node_modules`** is gitignored and absent, so `qa-js.bash` failed. Installed
  from the lockfile with `cd extensions && npm ci`, which is the command that gate's own
  failure message prescribes.
- **`.claude/hooks-daemon/`** is a gitignored per-checkout tool install and is absent, so
  `qa-docs` reported eight missing-link findings that were artefacts of the worktree and
  would have masked a real one. Symlinked in, the same mode the seed config declares.

That second one exposed a genuine bug, fixed in this plan's branch: `.claude/.gitignore`
matched `/hooks-daemon/` **with a trailing slash**, and a trailing slash matches a directory
only. A worktree's symlink is not a directory to git, so it showed up as untracked — one
`git add -A` away from committing an absolute checkout path into this **public** repository.
The rule is now `/hooks-daemon`, which matches both.
</content>
