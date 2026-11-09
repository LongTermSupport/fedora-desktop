# Task 1.5: why changed-plays re-ran play-agent-bus.yml and play-network-tools.yml

## Cause

`changed_plays.judge` takes a play's inputs from `helpers/self_update/affected_plays.play_inputs`.
That mapper read a rooted path whose tail is a template as a prefix input on its literal head.
It did this on purpose, because a regex cannot evaluate a loop. These are the inputs it gave
the two plays for the wsi scripts:

| Play                                                       | Input                           | Source line                                                        |
| ---------------------------------------------------------- | ------------------------------- | ------------------------------------------------------------------ |
| `playbooks/imports/play-agent-bus.yml`                     | prefix `files/`                 | line 144, `path: "{{ root_dir }}/files/{{ item }}"` (a `stat`)     |
| `playbooks/imports/optional/common/play-network-tools.yml` | prefix `files/home/.local/bin/` | line 112, `src: "{{ root_dir }}/files/home/.local/bin/{{ item }}"` |

Neither play had either script as an exact input. The loops are literal:
`agent_bus_swap_paths` lists three paths, and network-tools loops over `lan-scan`,
`qnap-finder` and `speedtest-isp`. This came from wrapping `_Walker._reference` and
`_classify` from Python in the worktree, which recorded which line added each input.

## Fix

`affected_plays` now defers each templated rooted reference until every file is walked
(`finish_templates`). Then it fills the reference in when the tail is one template plus a
literal suffix:

- `{{ item }}` / `{{ item.<key> }}`: from the task's own `loop:` / `with_items:`. That is a
  block list or a flow list of literals, or `"{{ var }}"` naming a list (dotted paths too, so
  `gnome_shell_extensions.custom`) that a walked file sets.
- `{{ var }}`: every literal a walked file sets `var` to.

Anything else keeps the old prefix: a filter-built loop, a `loop_var` rename, a non-literal
item, or a variable no walked file sets. Each line it fills in is recorded in `Inputs.filled`.
The rule play-input-dropped (`tests/helpers/self_update/test_dropped_input_rule.py`) accepts
a templated reference only when THAT line's filled paths all lie under its head and are
inputs. An input under the head that comes from another line does not cover it.

## Tests (red first)

- `tests/helpers/play_ledger/test_changed_plays.py` `TestRealPlayInputs` uses the real
  checkout through `judge`. A wsi change makes speech-to-text stale and leaves agent-bus and
  network-tools out of the run, but `lan-scan` and `tuwunel.pin` still re-run their plays.
  Before the fix it was red, with agent-bus `stale`.
- `test_affected_plays.py` `TestLoopedReferences` has 9 fixture cases: 5 forms were red, and
  4 fallback cases stay on the prefix.
- `test_dropped_input_rule.py` has 3 rule fixtures (1 was red).

## Sweep: every templated rooted reference in a host play

There are 24 references. 22 are now narrowed, across 13 plays:

- collaboration:41
- container-watch:45, :96
- fedora-desktop-panel:48
- host-health-login-report:155, :263, :271
- network-tools:112
- remote-desktop-toggle:25
- self-update:756
- speech-to-text:24, :713, :792
- vm-test-lab:234, :248, :539
- displaylink:393
- agent-bus:144
- claude-yolo:769
- github-cli-multi:114
- gnome-shell-extensions:127

The prefixes these plays lost: `extensions/` (6 plays), `files/home/.config/systemd/user/`
(4), `files/home/.local/bin/` (2), `files/`, `files/etc/systemd/system/`, `scripts/`,
`fedora-install/` and `files/var/local/claude-code/`.

These are left as a directory, and why:

- vm-test-lab:150 and :208 loop over a `fileglob` of `helpers/vmtest/*.py` and
  `files/home/.local/share/vmtest/*.bash`. The play deploys every match, so the directory is
  what it copies. helpers/vmtest/ is a whole-package input anyway.
- The joined forms are unchanged. vm-test-lab:159 and :193 are those same fileglobs.
  speech-to-text:788 is `+ item.item` over a registered result, inside the play's own prompt
  directory. self-update:354 is a vault path that may be absolute.

Against HEAD's mapper, every play's UNRESOLVED list is identical. Every input the new mapper
reports was an input before, and Go inputs are unchanged. The only difference is the narrowed
prefixes above.

Ran:

- `tests.helpers.self_update.test_affected_plays`
- `test_dropped_input_rule`
- `test_program_read_rule`
- `test_gate`
- `tests.helpers.play_ledger.test_changed_plays`
- `test_check_freshness`

All are green.
