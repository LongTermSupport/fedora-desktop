I found two further instances, both in dimension (b), and no further dimension (a) or (c) instances. I read around each match but did not run any suite. I confirmed your three known instances: the `sort -u` at `test-ccy-session-registry.bash` lines 166-172 against `"--opencode -oc"`, the empty `$prefix` in `test-ccy-token-mode.bash:227`, and `test-ccy-token-usage-command.bash`.

**Further instances (all in `<worktree>/scripts/`):**
1. `test-ccy-teams.bash:261-262`, dimension (b). The "agent-bus not installed: refused, 78" case sets `PATH="/usr/bin:/bin"`. That is the host's real directories, not a scratch one. If the owner has agent-bus in `/usr/bin` or `/bin`, the case runs the real binary and stops returning 78.
2. `test-ccy-teams.bash:551-558` and `:600-601`, dimension (b). `run_ccy no-bus` builds `path="$stub_bin:/usr/bin:/bin"` and asserts rc 78 and "names play-agent-bus.yml". It has the same exposure as instance 1. The `lib-run` and `run_ccy` cases both use it.

The case (a) risk in `test-ccy-container-name.bash:129,138` is only theoretical. It compares `sort` output with `proj_yolo`, `proj_yolo_1`, and so on against a fixed string. The order is the same in the C and en_US locales, because the names share a prefix and differ only by a trailing suffix. I judged it not an instance but cannot rule it out without running it in another locale.

**Judged NOT instances:**
- `test-ccy-token-import-cleanup.bash:87-88` sorts both sides in the same locale.
- `test-ccy-image-build-lock.bash:470` sorts both sides in the same locale.
- `test-ccy-image-build-lock.bash:496` sorts one side, but the two expected names are `old-a` and `old-b`, which order the same in any locale.
- Only counted: `test-ccy-image-build-lock.bash:322,361`, `test-ccy-container-version-hook.bash:442`, `test-bash-history-search.bash:88`, `test-ccy-session-registry.bash:795`.
- `test-ccy-relabel-preflight.bash:212` is a single uid.
- `test-ccy-git-signing.bash:420` is a single value.
- Order-insensitive membership: `test-vmtest-prepare-record.bash:422`, `test-ccy-session-registry.bash:806`, `test-ccy-agent-choice.bash:194`, `test-ccy-teams-command.bash:429`.
- `test-planlib.bash:733` and `test-gh-alias-wrappers.bash:196` sort function names against fixed strings. The order is the same in C and en_US: the compared names differ at a letter before any underscore matters.
- `test-vmtest-kernel-selection.bash:115` uses `sort -Vr`, which is version ordering and does not follow the locale for these inputs.
- `test-ccy-agent-choice.bash:215-216`, `test-run-bash-gh-scopes.bash:419` and `test-ccy-sessions-take-over.bash:250` use an empty or scratch PATH, or strip the fake directory on purpose, so no installed copy is reachable.
- `test-ccy-token-mode.bash` and `test-ccy-token-usage-command.bash` use `PATH="$prefix$PATH"`, but the two suites are already in your known list.
- Dimension (c): no `export -f`, `BASH_FUNC` or `BASH_ENV` appears in any suite. Two suites use `env -i` with `LANG=C.UTF-8` (`test-qa-tool-pin-converges.bash:63`, `test-git-signing-declared.bash:59`), which is safe. HOME is overridden to a scratch directory everywhere I read. `test-ccy-restart-request.bash:629` only greps the launcher text for `$HOME`.

**Attribution by technique:**
- Text search found: the `PATH="/usr/bin:/bin"` literals, the sort lines, and the absence of exported-function markers.
- Reading was needed to judge: whether a sort feeds a fixed-string comparison or is only counted, and whether `/usr/bin:/bin` can contain agent-bus.

**Searches run:** `sort|comm|join|uniq`, `LC_ALL|LANG`, `ls`/glob loops, `[[ < ]]`, `printf` widths, `BASH_FUNC|BASH_ENV|ENV=|export -f|env -i`, `PATH=`, `command -v|which`, `not installed`, and `~/|$HOME`. I ran them over the 76 test scripts named in `qa-gates.tsv`. `tests/*.bash` was not searched. I did not search `qa-gates.tsv` for scripts outside `scripts/test-*.bash`, such as `tests/*.bash`.