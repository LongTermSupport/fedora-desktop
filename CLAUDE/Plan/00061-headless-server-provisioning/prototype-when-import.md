# Prototype: `when:` on `import_playbook` + computed profile (ansible-core 2.19)

**Purpose**: de-risk the load-bearing assumption of the `when:`-based
auto-detect design (Decision 4) before committing to it — does a
`provisioning_profile` variable reliably gate an `import_playbook` under this
repo's ansible-core 2.19, and can the result be verified statically?

**Setup** (scratchpad, throwaway): `main.yml` imports a general play
(ungated) and a gnome play gated by
`when: (provisioning_profile | default('desktop')) != 'server'`; two `debug`
tasks; `localhost` / `connection: local`.

## Result 1 — runtime gating WORKS ✅

- `-e provisioning_profile=server` → gnome play `skipping: [localhost]`,
  `PLAY RECAP ... skipped=1`. Correctly skipped.
- `-e provisioning_profile=desktop` (and the bare default) → gnome play runs
  (`"GNOME ran"`, `skipped=0`).

So `when:` on `import_playbook` consuming a `provisioning_profile` var is a
valid, working mechanism under ansible-core 2.19.11. Zero flags in the common
case (the `| default('desktop')` keeps a bare run = desktop); `-e` overrides.

## Result 2 — `--list-tasks` does NOT reflect `when:` ⚠️ (important)

`ansible-playbook main.yml --list-tasks` lists the gnome task in **all**
profiles, including `provisioning_profile=server`. `--list-tasks` enumerates
what *could* run statically and does **not** evaluate `when:` conditions.

**Implication for verification**: the cheap in-container zero-regression /
skip proof that works for the tag design (`--skip-tags` **is** honored by
`--list-tasks`, per `AUDIT-round-1.md` §4) does **not** work for the `when:`
design. Proving a gnome play is actually skipped on a server requires a real
run or `--check` (host-side per `CLAUDE/ContainerRules.md`; Task 3.8 already
is a host validation). Static in-container verification for `when:` is limited
to: (a) the QA gate greps the `when:` conditions in `playbook-main.yml` and
validates each import carries exactly one recognized profile-gate, and (b) the
detection→profile mapping is unit-testable on its own. End-to-end skip proof
is host-only.

## Verdict

`when:` + auto-detected `provisioning_profile` is viable and gives true
zero-flag auto-detection with no wrapper. Accepted trade-off vs the tag design:
loss of the cheap `--list-tasks` static skip-proof (host verification instead).
This is the empirical basis for Decision 4.
