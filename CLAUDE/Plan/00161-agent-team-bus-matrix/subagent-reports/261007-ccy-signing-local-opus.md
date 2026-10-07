# ccy signing: a checkout's own setting first (U20 host-run finding)

## The finding

U20's leg of `acceptance.bash` starts three headless ccy sessions with `--no-ssh`, each in a
throwaway checkout. All three were refused at launch:

```
ERROR: Commit signing is on in ~/.gitconfig, but the session has no SSH identity to sign with.
  Every commit in the container would fail. Choose an SSH key at launch, or pass --ssh-agent with an agent holding one.
```

`configure_git_signing` (`files/var/local/claude-yolo/lib/ssh-handling.bash`) read
`commit.gpgsign` and `tag.gpgsign` only from the copy of `~/.gitconfig`. Inside the container
git also reads the project's `.git/config`, which outranks the global config, so a checkout
that turns signing off makes no signed commits and needs no key. The refusal was a false
positive for it.

## Fix 1: ccy (CCY 3.85.1 to 3.85.2; container version unchanged at 2.45)

For each of `commit.gpgsign` and `tag.gpgsign`, `configure_git_signing` now:

- reads `git -C <project> config --local --type=bool --get <name>`: exit 0 uses that value,
  exit 1 (unset) falls back to the gitconfig copy as before, any other exit fails the launch
  naming the setting and the project (for example a value git cannot read as a boolean);
- does this only when the project is in a git repository. `git config --local` exits 128
  outside one, which would otherwise fail every non-repository call; that is told apart from
  other `rev-parse` failures by git's "not a git repository" message (run under `LC_ALL=C`),
  and any other failure fails fast. ccy itself refuses to start outside a repository root,
  but the unit tests call the function on plain directories.

A local `true` with a global `false` now counts as signing on and gets the same key
requirements. The refusal's first line names where signing is on: "in this project's git
config" when the project's local config turned it on, otherwise "in ~/.gitconfig" as
before. Everything else in the function is unchanged.

Not a widening of what the container can do: the project's config is writable from inside
the container, but git there already honours a local `false`, so ccy launching without a
key for it grants nothing new; a local `true` only adds a requirement.

Also: `ssh-handling.bash` header version 1.7.0 to 1.7.1, `docs/ccy-changelog.md` 3.85.2
entry, and one sentence in `docs/ccy.md`'s signing bullet saying where "signing on" is read.

## Fix 2: U20's checkouts

`u20_make_checkouts` in `_acceptance-u20.inc.bash` sets `commit.gpgsign false` and
`tag.gpgsign false` in each checkout's local config after `git init`, failing on either
error. `--no-ssh` stays. The function's header comment says so. The "M2 NEEDS from the
owner" comment in `acceptance.bash` does not mention SSH, so it is unchanged; the CCY
3.85.2 launcher reaches the host through `deploy.bash`'s `play-claude-yolo.yml` leg, and
`meta-deploy.bash` already lists this plan.

## Tests

`scripts/test-ccy-git-signing.bash`, new section "the project's own config", six checks,
four of them red before the fix:

- local commit and tag `false`, global `true`, no identity: launches, copy untouched;
- a repository that sets neither: refused (falls back to the copy);
- local `true`, global `false`, no identity: refused, naming the project's config;
- the same with a key-file identity: accepted, the copy names the mounted key;
- local commit `false`, global tag `true`: refused;
- a local value that is not a boolean: fails, naming the setting.

Results: `test-ccy-git-signing.bash` 66/66, `test-ccy-ssh-handling.bash` 86/86,
`test-ccy-restore-askpass.bash` 87/87, `test-ccy-lifecycle.bash` 213/213,
`test-ccy-session-registry.bash` 157/157, `test-ccy-restart-request.bash` 159/159; the plan's
Python tests 231 passed. `test-ccy-ssh-probe.bash` fails here because it needs the host's
real `github_<alias>` keys, which the container does not have; it is not in `qa-all.bash` and
does not exercise `configure_git_signing`. Shellcheck is clean on the touched files.

## Open points

- `--local` reads `.git/config` only, not `config.worktree` or its includes. A checkout that
  turns signing off only there is still refused: conservative, as before.
- Host rerun pending: `./CLAUDE/Plan/meta-deploy.bash` (deploy installs the 3.85.2
  launcher, then acceptance runs U20 again).
