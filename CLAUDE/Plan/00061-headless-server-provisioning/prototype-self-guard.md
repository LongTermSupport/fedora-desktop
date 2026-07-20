# Prototype: self-guarding play (`meta: end_play`/`end_host` + `when:`) — standalone auto-gating

**Purpose**: the owner requires every play to be runnable **standalone**
(`ansible-playbook playbooks/imports/play-X.yml`) *and* still auto-gate to the
detected profile. The converged `when:`-on-import design (Decision 4) only
gates during the `playbook-main.yml` batch run — a standalone core play has no
import-site `when:` and would run unconditionally. This prototype de-risks the
replacement: an in-play scope guard.

**Setup** (scratchpad, throwaway): `ansible.cfg` pins `inventory=./env` (mirrors
the real repo); `env/group_vars/desktop.yml` computes `provisioning_profile`
via the same ternary as the real design; `imports/play-firefox.yml` is a
gnome play whose FIRST task is a guard:

```yaml
    - name: Scope guard — gnome play, skip on headless server
      ansible.builtin.meta: end_play        # or end_host
      when: provisioning_profile == 'server'
```

## Results (ansible-core 2.19.11, run STANDALONE — not via playbook-main.yml)

| Invocation                              | Guard     | Real task            | Meaning                                                            |
| --------------------------------------- | --------- | -------------------- | ------------------------------------------------------------------ |
| `play-firefox.yml` (no `-e`)            | skips     | **runs** (`desktop`) | `group_vars` auto-loads standalone → profile computed → gnome runs |
| `play-firefox.yml -e ...profile=server` | **fires** | not reached          | `meta: end_play` honors `when:` → play ends → gnome skipped        |
| same with `meta: end_host`              | **fires** | not reached          | `end_host` variant works identically (single-host repo)            |

## Verdict — two load-bearing assumptions CONFIRMED

1. **`group_vars/desktop.yml` loads on a standalone single-play run** (because
   `ansible.cfg` pins the inventory), so `provisioning_profile` auto-computes
   even when a play is run by itself — no batch context needed.
2. **`meta: end_play`/`end_host` honors `when:`** under 2.19, so a play can
   cleanly end itself when its scope doesn't match the detected profile.

**Implication**: replace the import-site `when:` (Decision 4 §3.2) with an
in-play guard. This unifies core + optional plays (both gate themselves
identically), makes every play self-describing and standalone-runnable, and
lets a single boilerplate guard referencing a per-play `scope:` var cover all
three buckets. The `group_vars` detection layer is unchanged. Basis for
Decision 5.
