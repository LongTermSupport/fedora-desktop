# Issue 227: play-teamchat.yml follow-ups (items 1, 2, 3)

Items 1 and 3 went through Defence Before Fix (DBF); item 2 is a plain fix. Items 4 and 5 are
not done here (see the end of this report).

# Defence Before Fix report

## Specification followed

DBF method 1.1.0, detector spec 1.0.0, toolchain spec 0.2.0. All three came from the skill's
cache (`refresh-spec.bash`).

## Toolchain

The repo has no DBF manifest declaration. Both rules are Ansible rules in the Go gate
`src-go/qa/ansible`, which `scripts/qa-ansible.bash` runs. That gate is already in `qa-all`,
`qa-changed` and CI, and it blocks: a finding is exit 1. Each finding prints its rule ID with
`Why and how to fix: CLAUDE/QA.md#<id>`, which points at docs shipped in the repo. Semgrep's
generic mode cannot see task structure (a block's `when`, registers, vars definitions). The
existing play-UX rules (`command-always-changed`, `duplicate-managed-block`) already live in
this gate, so the new rules went there too.

## Defect and class

- **A, `install-guard-trusts-record`.** Work is skipped on a record of itself (a stamp, checksum
  or marker the work writes from nothing), and nothing checks what the record vouches for.
  Hazard: if the installed artefact is deleted or damaged while the record stays, the play
  skips the install and reports success.
- **B, `filter-list-unchecked`.** A list a person writes picks items from another list by
  membership, and nothing checks that each of its names is there. Hazard: a name that matches
  nothing is dropped without a word.

## Rule

Each rule is defined in its `CLAUDE/QA.md` section and its Go package doc:
`src-go/qa/ansible/recordguard.go` and `src-go/qa/ansible/filterlist.go`.

- **A** has two shapes:
  - A stat or slurp guard. The rule follows the work it guards through registers (so it also
    catches a record written under `when: x is changed`). It fires when that work makes the
    guarded file from nothing and also changes something its condition does not check. That
    can be a path, or a change whose path cannot be known (a command or a package) when the
    condition reads no independent probe. A probe that the record's content is made from does
    not count.
  - A command whose `creates` marker its own text touches.
- **B** covers `selectattr`/`rejectattr`/`select`/`reject` with `in`, `intersect`, and a loop
  `when: item.. in L`, where L is an input (no name the play, the repo's `vars/`/`group_vars`
  or Ansible supplies). L needs an assert or fail check, either `L | difference(X)` or
  `L is subset(X)`. The check may be inline or in the assert's own vars. X must be the source
  of one of L's filters, or a variable that the source is cut from by no other input list.
  Filters inside checks are not picks.
- **Next wider rule for A, considered and not built:** "a guard that reads one product, or the
  install directory, of work that makes several products". Examples are `creates: id_lab` for
  an ssh-keygen that also writes the `.pub`, the Toolbox data directory, `creates:` on the
  cuDNN or msttcore directory, and `mkcert -install`'s CA file guarding trust-store state.
  Commands do not declare their products, so this has no static answer. The repo also uses
  "creates the install root" as its normal idiom. **This is the owner's decision (SPEC 4).**
- **Next wider rule for B, considered and not built:** a `difference(...)` used as a pick. No
  instance exists, and the method makes leaving it unbuilt the owner's call.

## Proof

- Red commits: `d38db78e0` for A (the gate is red on play-teamchat, play-self-update and
  play-rpm-fusion) and `b2bc4f9f9` for B (red on play-teamchat's approve and only-teams lists).
- Each rule's flagged and clean fixtures are tests in `src-go/qa/ansible`:
  `tests/fixtures/ansible-install-guard-record/` and
  `tests/fixtures/ansible-filter-list-unchecked/`. Each `EXPECT-FINDING` line must be
  reported, and the clean fixture must report nothing.

## Independent search

`subagent-reports/261110-issue227-search.md` was dispatched before either rule existed.

- **A:** it found 2 firm instances (teamchat, self-update), 3 likely (rpm-fusion, toolbox,
  ddev) and 4 unsure.
  - The first rule missed self-update (slurp, a transitive record, a command with no path) and
    rpm-fusion (a self-touched `creates` marker). I widened the rule until it caught both.
  - Toolbox, ddev and the 4 unsure cases are the product-guard class above. They are not
    record guards, so they are not caught and not fixed. They are referred to the owner.
- **B:** it found 2 instances (approve, and the only-teams list, which nothing checked at
  all), plus the covered agent-bus shape.
  - The rule's first sweep also flagged play-speech-to-text's `intersect` inside an assert,
    which is a check, not a pick. Filters inside checks are now excluded; this is confident
    and in the clean fixture.
- What each technique found: text search found every `creates:`, `stat` and membership
  filter. Only reading found the self-update slurp-and-compare guard, that teamchat's approve
  assert compared against the wrong set, and which guards cover products rather than records.

## Sweep and count

The scope is the whole Ansible tree the gate reads (`playbooks/ tasks/ vars/ environment/ roles/`, only what git publishes).

- A: 3 instances.
- B: 2 instances.

## Fixes

Every instance was examined on its own:

- **teamchat Tuwunel:** the install also stats the binary, and runs when the binary is
  missing or not executable.
- **self-update:** the play slurps the clone's `requirements.yml` and stats each collection's
  `ansible_collections/<ns>/<name>/MANIFEST.json`. It reinstalls when any is missing.
- **rpm-fusion:** the marker is gone.
  - The play probes `dnf -q repolist --enabled` and runs `config-manager setopt` only when
    `fedora-cisco-openh264` is absent from that list.
  - `@multimedia` is installed through `ansible.builtin.dnf` with the same `allowerasing`,
    weak-deps and exclude settings.
  - The old marker file is removed.
  - **Host-unverified:** the dnf5 `repolist` output format and the dnf5 group options. Check
    these on the next host run.
- **teamchat lists:** a new assert, "Refuse a Scope or an Approval Naming a Team This Run Does
  Not Apply", names the unknown teams. `x30-lab.bash`'s scoped run (`teamchat_only_teams: []`)
  used to depend on the silent drop; it now approves nothing.

## Decisions referred to the owner

1. Whether to build the wider product-guard rule for A. It has 6 or more candidates: toolbox,
   ddev mkcert, the ssh-keygen pairs in play-lxc-install-config and play-vm-test-lab, nvidia
   kmodgenca, msttcore fonts and cuDNN.
2. Whether to build the `difference`-as-pick extension for B.

## Permanence

Both rules are blocking in `qa-ansible` (exit 1). There is no allowlist and no suppression
route. The tree is green after the fix commit.

## Toolchain and detector gaps

- There is no DBF manifest declaration. This was not changed.
- There is no single-rule harness or listing command for the qa-ansible rules: they run
  together. The Go tests run each rule alone against its fixtures.
- **Dogfood, for the coordinator to log:** the `lint_on_edit` hook blocked an edit to the
  plan-local `x30-lab.bash` on shellcheck *info* findings that were already there (SC2329 on
  functions the script dispatches indirectly, SC2016). The edit itself added none.
- **Dogfood:** `qa-changed` waited more than 17 minutes on the QA lock held by another
  session's `qa-all`.
- **Not done:** the `conformance-reviewer` agent was not dispatched (context budget). Do this
  before merge if the owner wants it.

## The original defect

The teamchat Tuwunel guard is fixed. The rule's flagged fixture reproduces it (its first
play), and the clean fixture's first play is the fix.

# Item 2 (not DBF)

The controller is not managed by this play, and the deploy route may have no `desktop` host.
Importing `play-golang.yml` (`hosts: desktop`) would install Go on the wrong machine or on
none, so the play does not import it. Instead, play-teamchat now:

1. probes `go env GOVERSION` on localhost with `GOTOOLCHAIN=local`, and
2. refuses a missing or older Go before any change, saying what to run.

`golang_min_version` moved into `vars/golang.yml`, which both plays read.

# Items 4 and 5 (not done)

- **Item 4:** nothing is ever pruned (old digest-named binaries and kits, old Tuwunel
  versions, units dropped from `units list`). Still open.
- **Item 5:** nothing in the repo runs the play. Still open, and deliberate per INSTALL-PATH.md.
  The X30 lab run should compare the installed units with `teamchat-admin units print`.
