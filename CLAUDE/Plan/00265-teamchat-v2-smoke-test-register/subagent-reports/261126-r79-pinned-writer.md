# R79 follow-up: the pinned-list writer, and the guard failing closed

## Commits (v2-smoke)

- `1461e61b3`: `qa-private-names pin`, the play tasks, the guard failing closed, the tests, the docs,
  and the meta-deploy entry.
- The next commit fixes what the qa-reviewer found (see below).

## What was built

- **Placement.** `playbooks/imports/play-git-hooks-security.yml`, not `play-teamchat.yml`.
  - It runs on every developer desktop (the main playbook), as the login user, in the checkout.
    It already owns the git hooks the guard runs in.
  - `play-teamchat.yml` targets `teamchat_hosts`, which may be remote. By design (and by its
    `TestThePlayReadsNothingTheTargetLacks`) it reads no checkout and no login user on the
    target, and a laptop that only watches teams never runs it.
- **The Go side is `qa-private-names pin`** (`src-go/qa/privatenames/pin.go`).
  - It reads the inventory as JSON on stdin and writes `<git common dir>/private-terms-pinned`:
    mode 0600, replaced whole through an owner-only sibling and a rename, with a generated
    header.
  - The names come from team ids, display names, coordinator and duty handles (in the guard's
    three handle forms), trusted owners and repositories, the LXC container, user, repo, host
    and site, `teamchat_site`, and the bus host's served team directories. Each is held to the
    learned-name rule (specific, not public).
  - `private_name_terms` items are used as written.
  - It refuses:
    - a hand-written list (no header);
    - an empty list on a machine with teams;
    - a blank, multi-line or `#` owner item, named by its position only;
    - an unknown top-level key.
  - It prints only a count.
- **Vault check.** The play counts the plaintext items with `reject('vault_encrypted')` (no_log).
  It then refuses, naming only the variable and the count. A single vaulted string is refused as
  not a list.
- **Served teams.** The play lists `/var/lib/teamchat-ctl` with `find(1)`: `become`, no_log,
  rc-only check. The path is held equal to `paths.CtlStateRoot` by an installplay test.
- **The guard fails closed** (the coordinator's added item, the batch 23 review item 2).
  - Any bus-client start error other than `exec.ErrNotFound` is now exit 2.
  - A container with no bus client installed needs a team or member name, learned or pinned.
  - Departure from the brief: teamchat not on PATH in a container still counts as absent. The
    launcher links teamchat only for a v2 member session (Dockerfile, `optional/teamchat`), so
    making it fatal would break every non-v2 container.

## Tests

- **Go** (`privatenames/pin_test.go`, `main_test.go`). They cover:
  - the exact union, the read-back by the guard, the removal of a team, the unchanged list;
  - the empty-result refusal, the hand-written refusal and the bad owner items;
  - bad input, no name printed, and the start failure and no-client cases.
  - The guard cases were red before the fix and green after.
- **Python** (`tests/helpers/private_name_pin/`). The play's own Jinja, read from the play file,
  is templated by the real ansible Templar over real `!vault` items:
  - vaulted items pass and reach pin decrypted;
  - a plaintext item is refused;
  - a single vaulted string is refused;
  - an absent variable passes;
  - the name-holding tasks are no_log.
- **Gates.** `qa-ansible.bash` and the syntax check on the play pass. The syntax check ran with
  `ANSIBLE_VAULT_PASSWORD_FILE` pointed at a placeholder, because this container has no
  `vault-pass.secret`.

## Faults met (dogfood)

- `qa-changed`'s go gate fails on arm64: ast-grep is pinned for amd64 only. Already issue #426.
  golangci-lint and `go test -race` were run by hand on the changed packages instead.
- `lint_on_edit` fails every Ansible edit in this container, which has no `vault-pass.secret`.
  This is the class of issue #294.
- `./vault.bash set` writes only a top-level scalar, not a list item. The docs therefore say to
  use `./vault.bash encrypt` and paste the block as an item. Not filed.

## qa-reviewer

- **Verdict:** FIX-BEFORE-MERGE, no blockers. Its report is beside this file
  (`261126-r79-pinned-writer-qa-review.md`).
- **Fixed:**
  - the find module's warning leak;
  - the guard message that pointed at hand-editing the list;
  - the unread name fields;
  - the stale SecurityRules wording;
  - the lag after a new team, now documented;
  - the wrapper header.
- **Not done:**
  - counts per source;
  - an fsync before the rename;
  - a test that keeps the three copies of the team-id pattern equal.

## Owed by the owner

- **Remove the hand-written list.** `.git/private-terms-pinned` exists now, written by hand, and
  pin refuses it. Move its names into `private_name_terms` and delete the file.
- **Set `private_name_terms`** for client and project names, one `!vault` block per item.
- **Run `./CLAUDE/Plan/meta-deploy.bash`.** Its new entry is `play-git-hooks-security.yml`.
