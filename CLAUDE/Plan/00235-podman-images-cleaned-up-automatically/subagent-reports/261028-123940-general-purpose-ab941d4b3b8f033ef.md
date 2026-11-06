The timer gate is built on branch `fix/00235-timer-gate`, based on 966c40827, as one commit, **f0ec211a7**. It is not pushed or merged.

**Checks:** `./scripts/qa-changed.bash` passes everything except ansible-syntax, which could not run because the vault password file is not in the container. So `--syntax-check` has not run on this change, and the host run will be its first real parse. Both YAML files load with `yaml.safe_load`. I could not try the subuid regex here because jinja2 is not installed; I wrote it the same way as the repair file's regexes.

**What it does**
- **New file `tasks/podman-store-ownership-probe.yml`**, imported by `playbooks/imports/play-podman.yml` after the store repair and before the timer task. It reuses `podman_store_home` from `tasks/podman-store-repair.yml`.
  - It reads `/etc/subuid` and finds the user's line, by name or by uid. If the user does not have exactly one range, the play fails.
  - As root it runs `find <store>/overlay -xdev ! -uid 0 ! -uid <user uid> ! ( -uid +<start-1> -uid -<start+count> ) -print -quit`. It stops at the first match. If the overlay directory does not exist, the search is skipped and counts as nothing found.
  - It sets `podman_store_foreign_owner`.
- **Nothing found:** the timer is enabled and started, and the first prune runs, both as before.
- **Something found:**
  - The timer task stops and disables the timer (`state: stopped`, `enabled: false`).
  - The first-prune block is skipped.
  - A final `fail` task, after the post-prune repair, ends the play with your message, plus the first file found.
  - No `failed_when: false` was needed, because the probe never ignores its own exit code. No extra variables were added. Run again, it changes nothing.
- **The rule is the broad one:** any uid other than 0, the user's, or the current range trips it, not only uids of 100000 or more. That matches your search definition; the message still says "(old range)".
- **Host notes updated:** the deploy.bash header and usage text, and the 00235 entry in `CLAUDE/Plan/meta-deploy.list`, now say the play may fail on purpose with that message. If it does, the store is repaired and only the daily prune is held off.

**Things to know**
- **The play now fails on every run until Q6 is fixed.** This is by design. If the old-range layers are still there, as the previous worker expected, tonight's deploy will hit this failure.
- **When nothing is found, the search covers the whole overlay tree.** On a store this size that can take some minutes.
- **A file deleted while `find` runs** (for example by a running podman) makes `find` exit non-zero, and the play then fails. This is fail-fast.
- **acceptance.bash still expects the timer to be enabled.** It is skipped when the deploy fails, so it does not conflict today.

**PLAN.md text** (in `CLAUDE/Plan/00235-podman-images-cleaned-up-automatically/PLAN.md`; this goes on top of the previous worker's changes)
- After their Task 4.6 line, add:
  `- [x] ✅ **Task 4.8**: tasks/podman-store-ownership-probe.yml, imported by play-podman.yml after the repair and before the timer: as root, find -xdev -quit looks in the store's overlay for a file owned by a uid other than 0, the user's, or the current /etc/subuid range; if one is found, the timer is stopped and disabled, the first prune is not run, and the play fails at its end ("the podman store holds files owned outside your subuid range (old range); the daily prune is held off until they are re-owned (Plan 00235 Q6)"), with the store already repaired`
- In their Risks row, old text: `P6a stops at once; play-podman.yml repairs as root before and after; Q6 is the lasting fix`
  New text: `P6a stops at once; play-podman.yml repairs as root before and after, and holds the timer off (stopped, disabled) and fails while any such file remains (Task 4.8); Q6 is the lasting fix`
- In their Status line, old text: `the repair, Tasks 4.5-4.6, is built`
  New text: `the repair and the timer gate, Tasks 4.5-4.6 and 4.8, are built`

**What the host agent does:** run `./CLAUDE/Plan/meta-deploy.bash`. If the play fails with the Q6 message, that is expected: podman works and the timer is off. Before the 00:47 run, this branch has to be merged into the checkout the host runs from, and that deploy has to have run.