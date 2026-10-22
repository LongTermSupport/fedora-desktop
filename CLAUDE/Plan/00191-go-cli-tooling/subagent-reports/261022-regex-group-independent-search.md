# Independent search: regex filter backreference backslash count

Searched without reading the detector or fixtures (src-go/qa/ansible, tests/fixtures not opened). The dispatch gave the mechanism but not the rule; I searched from the class description.

## Instances (every regex filter call with a group argument or backreference): 7 calls on 5 lines-groups, 1 DEFECT

| # | file:line | filter | scalar style | context | backslashes in source | verdict |
|---|-----------|--------|--------------|---------|-----------------------|---------|
| 1 | playbooks/imports/play-basic-configs.yml:604 | regex_replace(..., '\\1') | double-quoted | `{{ }}` in task vars | 2 (need 2) | CORRECT |
| 2 | playbooks/imports/play-claude-yolo.yml:585 | regex_search(..., '\\1', multiline=True) | block scalar `>-` (key line 583, set_fact) | `{{ }}` in set_fact | 2 (need 1) | DEFECT (confident) |
| 3 | playbooks/imports/play-claude-yolo.yml:692 | regex_search(..., '\\1', '\\2') | block scalar `>-` (key changed_when, line 689) | bare conditional | 2 (need 2) | CORRECT |
| 4 | playbooks/imports/optional/common/play-vm-test-lab.yml:357 | regex_search(..., '\\1') | double-quoted | `{{ }}` in set_fact | 2 (need 2) | CORRECT |
| 5 | same file :364 | regex_search(..., '\\1') | double-quoted | `{{ }}` in set_fact | 2 (need 2) | CORRECT |
| 6 | same file :365 | regex_search(..., '\\1') | double-quoted | `{{ }}` in set_fact | 2 (need 2) | CORRECT |
| 7 | playbooks/imports/optional/common/play-self-update.yml:602 | regex_search(..., '\1') | block scalar `>-` (key line 601, task vars) | `{{ }}` in vars | 1 (need 1) | CORRECT |

Note on #2: a neighbouring task at line 692 in the same file uses the same `>-` style but sits in a bare conditional, where `\\1` is right; #2 is `{{ }}` where `\\1` reaches the filter as two backslashes. Reasoned from the stated mechanism; I did not run Ansible.

No other regex filter has a group argument or a backreference in its replacement. Every other regex_replace replacement is a literal (e.g. '', '--uuid=', '/home/...'), none contains a backslash.

## Pattern escapes (wider class, not counted)
- play-lxc-install-config.yml:452 `\s`, `\S` in `>-`, `{{ }}`, one backslash: correct.
- play-lxc-install-config.yml:455 `is match('iptables(\+firewalld)?$')`: plain list item, bare conditional, one backslash. Jinja unknown escape `\+` is kept, so it works, but it is the wrong count by the stated rule (`\\+`). Unsure.
- play-self-update.yml:85,87 `[^\s]` in `that:` items: same shape as above (bare conditional, one backslash, unknown escape kept). Unsure.
- play-self-update.yml:602 `\[core`, `\]` in `>-` `{{ }}`: correct.
- play-self-update.yml:48 `\.` in single-quoted var value, consumed as plain data by `is match(var)` at :192: correct (no Jinja layer on the string).
- play-basic-configs.yml:229 `[ \t]` in a `that` item (bare conditional, plain): `\t` becomes a TAB under Jinja, still matches a tab in the regex; harmless.
- play-basic-configs.yml:604 comment says `[.]` is used to avoid backslash rules; most other patterns follow it.

## Searches run
- `regex_(search|replace|findall)|regex_escape|\| *(match|search)|is (match|search|regex)` over the whole repo excluding .git, untracked, node_modules, Plan, *.md, *.go, and the detector and fixtures dirs.
- `\\+(g<|[0-9])` over *.yml, *.yaml, *.j2, *.jinja*, *.cfg (no Plan dir): only the 7 hits above plus a printf in fedora-install/ks.cfg (not a regex).
- File list of every file mentioning regex filters; checked .j2/templated files in tests/, scripts/, files/, helpers/: none use regex filters. No roles/ directory exists. No `\g<` or named group forms exist anywhere.
- Plan docs and Python/Go sources were excluded as non-Ansible; Plan folders contain only prose and probe scripts.

## Attribution
- Text search found all 7 hits and all candidate files. Reading was needed to establish scalar style (block key line above), and `{{ }}` versus bare conditional context. Only reading could classify #2 as a defect and #3 as correct, since the text of both is the same shape (`>-` plus `'\\1'`).
- Reading also found the pattern-escape notes (lxc 455, self-update 85/87), which the backreference grep could not.
- Text search could not check mechanism (rendering); no run was done.
