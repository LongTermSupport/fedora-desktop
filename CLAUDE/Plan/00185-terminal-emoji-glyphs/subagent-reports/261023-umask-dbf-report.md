# Defence Before Fix report: ansible-umask-pinned

## Specification followed

Method specification 1.1.0, detector specification 1.0.0 and toolchain specification 0.2.0,
all cached copies, as printed by the dbf skill's refresh script (plugin 0.1.1).

## Toolchain

There is no manifest declaration (no `defenceBeforeFix` or `extra.defence-before-fix` key;
`.claude/settings.json` only enables the plugin), and no register toolchain covers Ansible
configuration. The project's own practice for a Python-checked rule is used, as for
`fixture-plain-member-bundle` and `checker-copies-cited-constant`: the rule is a stdlib
`unittest` module that reads files, proves itself on fixtures first, then scans the
repository. It was committed red under `tests/helpers/umask_pin/` (in `qa-helper-tests.bash`)
and moved after review round 1 to `tests/umask_pin/`, run by its own gate
`scripts/test-umask-pin.bash` (a `qa-gates.tsv` row, so in `qa-all.bash`), whose text names
`ansible.cfg` and `callback_plugins/umask_pin.py`, so `qa-changed.bash` also runs it when
either changes.

- **Single-rule harness:** `python3 -m unittest tests.umask_pin.test_umask_pinned_rule`.
- **Listing:** the `###` rule headings in `CLAUDE/QA.md`, plus `scripts/qa-gates.tsv`.
- **Identifier resolution:** the `CLAUDE/QA.md` heading of the same name.
- **Calibration assumed:** blocking, no baseline, no exceptions.

## Defect and class

**Originating instance.** `ansible.cfg:24`, `callbacks_enabled`, through which every run
works under the launching shell's umask; it reached the host in Plan 00185's deploy. The run
started from a `cc` session (`files/var/local/claude-code/cc:26`, `umask 077`, Plan 00098),
so the root handler `fc-cache -f` (`playbooks/imports/play-fonts.yml`) rewrote 40 of 41 files
in `/usr/lib/fontconfig/cache` as 0600 root. fontconfig, as the desktop user, used the user's
own still-valid stale cache, and acceptance failed 6 of 17 checks.

**Class.** Ansible runs with the umask of the shell that started it, and its root tasks
inherit it through sudo (which only adds 022), so a task that writes a file with no explicit
mode takes the caller's umask.

**Hazard.** Started from a shell with a restrictive umask, root tasks leave system files that
other users must read (caches, binaries, configuration, trees a user process loads) readable
only by root, and nothing fails: the play is green and the damage shows later as a stale or
missing read.

## Rule

**Detector.** A stdlib Python checker, `tests/umask_pin/test_umask_pinned_rule.py`.

**Identifier.** `ansible-umask-pinned`.

**Message.** `<file>:<line>: ansible-umask-pinned: callbacks_enabled does not name umask_pin, so Ansible runs with the umask of the shell that started it; see CLAUDE/QA.md "ansible-umask-pinned"` (and two sibling messages for the other clauses).

**Documentation.** `CLAUDE/QA.md#ansible-umask-pinned`.

**Width.** The class is defended where every run passes: the Ansible configuration. The rule
reports (1) any tracked `ansible.cfg` whose `[defaults]` does not enable `umask_pin` or does
not name a `callback_plugins` directory; (2) first-party code (any language, tracked files)
that assigns `ANSIBLE_CALLBACKS_ENABLED` or `ANSIBLE_CALLBACK_WHITELIST` without `umask_pin`;
(3) code that assigns `ANSIBLE_CALLBACK_PLUGINS` without a `callback_plugins` directory;
(4) code that assigns `ANSIBLE_CONFIG` to a file that is not an `ansible.cfg` (added after
the conformance review). Assignments are recognised in the bash, Go, Python and YAML forms
and as `setdefault`/`putenv`/`Setenv` calls; reads, comparisons and name lists are not.
Excluded as not carrying the hazard, each with its sentence in `CLAUDE/QA.md`: vendored and
daemon-owned trees, `untracked/`, archived plans (meta-deploy refuses a folder moved to
`Completed/`), qa-changed's golden renderings `<argv> | NAME=value` (text compared with
text), the rule's own file (its fixtures), and the one line of the end-to-end test's control
run, matched exactly, with a test that the exclusion reaches nothing else in that file. The
first draft also skipped `.md`, `.json`, `.txt`, `.golden` and `.tsv` files; that was dropped
after review, and the full scan still finds nothing there.

**Not caught.** An `ANSIBLE_CONFIG` naming an `ansible.cfg` outside the repository, a run
from a directory with no `ansible.cfg` (its plays target `desktop`, which only the
repository's inventory defines, so they match no host), and a value built on another line.

**Next wider rule considered.** A task-level rule: every root `shell`/`command`/file-writing
task must carry an explicit `mode:` or `umask`. Not built because the hazard is the inherited
umask, which the pin removes for every task at once; under the pin a task with no mode is
deterministic 022, as from a terminal, and does not carry the hazard. Such a rule would also
need to know which commands write files, which is not decidable from the task text
(`fc-cache`, `dconf update`, `git`, `ansible-galaxy` all write without saying so). The reason
is that the hazard is absent, not that no instance exists.

## Proof

Red commit `3a9eee25f` holds the rule and the end-to-end probe alone, before any fix. Run
through the project's entry point (`./scripts/qa-changed.bash`, check `unit-tests`):

- `test_no_finding`: `ansible.cfg:24: ansible-umask-pinned: callbacks_enabled does not name umask_pin, so Ansible runs with the umask of the shell that started it`.
- `test_umask_pin_against_real_ansible`: a real `ansible-playbook` from the checkout under an
  inherited umask 077 wrote a file through `copy` and one through `command: touch` (as
  `fc-cache` writes), both 0600: `by-module is 0600: the play ran with the umask of the shell that started it`. The control run (callbacks disabled from the environment) passes at 0600,
  so the probe is shown to see the inherited umask. The probe runs unprivileged with `-c local`; the `become` leg (sudo gives the union of the caller's umask and 022) is reasoned
  from sudo's documented behaviour and the reviewers' reading, and is verified only on the
  host, by acceptance check 18.
- The fixtures (missing pin, no `[defaults]`, wrong plugin directory, a look-alike name, six
  forms of the enabled-list override, three of the directory override) fire; the quiet
  cases (pinned config, settings keeping the pin, reads and name lists) do not.

## Independent search

`defence-before-fix:independent-searcher` was dispatched before the rule existed, with the class
and hazard and the rule's paths kept out of its reach. **Its search finished after the red
commit**, not before the rule was run, as clause 3.1 requires: the rule's width was designed
without it, and the search bore on the width only through the reconciliation below. It did
not open the rule's files or run the detector. Its report is
[261023-umask-independent-search.md](261023-umask-independent-search.md). It looked for every
Ansible launch point (and whether it pins the umask or loads `./ansible.cfg`) and every root
task that writes a file with no mode.

- **Text search** found the launch points by name across bash, Python, Go and units (20 that
  run tasks, 7 that only syntax-check or vault), and that the only process-wide umask on any
  launch path is `cc`'s.
- **Reading** found what text could not: `become` inherited from play and block level; that
  `chmod +x` after a redirect is umask-dependent; that `git` and `ansible-galaxy` create trees
  under the umask with no `mode:` to match on; that the self-update play's world-readable
  assert checks only write bits; that the self-update unit has systemd's 0022; and that the
  Plan 00062 deploy script and its legacy-shebang play neither change directory nor set
  `ANSIBLE_CONFIG`. A YAML parse gave the counts (892 root tasks of 1486).

**Reconciliation.** Every launcher that runs tasks loads the repository's `ansible.cfg`, so
clause 1 covers it. The search's one launcher the rule cannot see, Plan 00062 (A14/A15), runs
a play with `hosts: desktop`; without the repository's configuration no inventory defines
that group and the play runs nothing, so it does not carry the hazard and the rule was not
widened for it (recorded under "Not caught"). The task-level findings are where the hazard
shows, not separate launch paths; the pin removes it for all of them.

## Sweep and count

Scope, recorded as the project's decision: every tracked first-party file in any language
(the rule's clauses 2 and 3 read bash, Python, Go, YAML, Jinja and units alike) and every
tracked `ansible.cfg`; vendored (`roles/vendor`, `src-go/vendor`), daemon-owned
(`.claude/hooks-daemon`), `untracked/` and archived plans excluded. Count: 1 instance,
`ansible.cfg:24`. Clauses 2 to 4 found 0 (the one `ANSIBLE_CALLBACK_PLUGINS` assignment,
`helpers/self_update/cycle.py`, keeps `callback_plugins`; every `ANSIBLE_CONFIG` assignment
names an `ansible.cfg`). Clause 4 first reported 2 golden renderings (`src-go/qa/changed`
`golden_test.go:324`, Plan 00192 `capture.py:276`), which format recorded commands as text and
start no process; they were excluded by their `| ` rendering form, not by file.

Task-level sweep by the searcher: 7 confident places the hazard shows (H1 to H7) and 13
unsure, all listed in its report.

## Fixes

- `ansible.cfg:24`: enables `umask_pin`; `callback_plugins/umask_pin.py` sets 022 in its
  constructor, which Ansible runs before the first worker starts. Every launcher inherits it.
- H1 to H7 and the 13 unsure rows received the pin by pattern: one answer is right for all,
  since each writes under the run's umask. Examined individually for past damage that a
  re-run would not repair:
  - H1 `play-fonts.yml`: rebuilds both caches when a system cache file is unreadable by other
    users, so its next run rewrites the 40 files; it also rebuilds the user's own cache.
  - H2 `play-basic-configs.yml` yq: `chmod 0755`, plus a `file` task setting 0755, because
    `creates:` never re-runs the install.
  - H7 `play-basic-configs.yml` `/etc/vimrc.local`: `mode: "0644"` (an existing file keeps its
    mode otherwise).
  - H3 to H6 (root git clones in `play-claude-devtools.yml` and `play-self-update.yml`,
    `ansible-galaxy` collections, `dconf update`): prevented by the pin; any damage from an
    earlier run started from `cc` is not repaired by the plays. Referred below.
- Nothing was suppressed; no instance satisfies the rule while keeping the hazard.

## Decisions referred to the owner

1. **Past host state** (not the rule): whether earlier runs of `play-claude-devtools.yml`,
   `play-self-update.yml` or `play-suspend-and-lid-policy.yml` started from a `cc` session
   left root-only files (H3 to H6). The pin stops new damage; it does not rewrite existing
   trees. Fixed: 3 of 7 confident sites carry an explicit repair (H1, H2, H7); 4 rely on the
   pin alone. Plan 00185's `triage.bash` now has a read-only leg (`rootonly`) listing what this
   user cannot read under `/usr/bin/yq`, `/etc/vimrc.local`, `/opt/claude-devtools`, the
   self-update deploy copy, its collections directory and `/etc/dconf/db`; it runs in the
   queued meta-deploy entry. Where it finds any, the fix is a `mode` or a repair task in that
   play.
2. **The rule left narrower than it could be, for want of an instance** (section 4): an
   `ANSIBLE_CONFIG` that names an `ansible.cfg` outside the repository, and a value built on
   another line. No first-party code does either today; judging them needs path resolution
   the rule does not do. Owner to decide whether to build it.

The rule is merged at full width otherwise; neither item is a known unfixed instance.

## Permanence

`tests/umask_pin/test_umask_pinned_rule.py` and `test_umask_pin_against_real_ansible.py` run
in `scripts/test-umask-pin.bash`, the `ansible-umask-pinned` gate row of `qa-gates.tsv`
(blocking in `qa-all.bash`), which `qa-changed.bash` runs when the gate, either module,
`ansible.cfg` or `callback_plugins/umask_pin.py` changes. Because a callback that cannot load
only warns, the probe there is what notices a broken pin. Green through the entry point:
`./scripts/qa-changed.bash` on the review round 1 changes (the gate passed).

## Toolchain and detector gaps

- No DBF manifest declaration and no identifier resolver beyond the `CLAUDE/QA.md` heading.
- `qa-changed.bash` routes a changed Python file only to its mirrored test module, so a rule
  over non-Python files needs a suite that names them; worked round with the gate above.

## The original defect

`play-fonts.yml` repairs on its next host run: the pinned umask makes `fc-cache -f` write
0644, the new check-and-rebuild finds the 0600 files the last run left and rebuilds the
system cache and the desktop user's own. The reproducing test is the end-to-end probe (red
0600, green 0644). Plan 00185's `acceptance.bash` check 18 now reports an unreadable system
cache by name, and `triage.bash` lists unreadable cache files. Host verification is the
meta-deploy entry `00185-terminal-emoji-glyphs`.
