# Phase 2 + Phase 3 Implementation Review — Plan 00065 (T2.1–T2.3, T3.1–T3.2)

**VERDICT: SOUND-WITH-FIXES**

Reviewed the actual committed code (`8ae3106` Phase 2, `eb14ba2` Phase 3) against a
freshly-pulled `F44` checkout (`git pull --ff-only` → already up to date), not the
pre-implementation design docs. No BLOCKER found that would break the human's planned
HOST test (a plain `run.bash` / `playbook-main.yml` run, no `--tags`). Two MUST-FIX
findings and several NITs — none touch the untagged full-run path the human is about
to exercise, but both are real, reproducible defects worth fixing before/soon after
the HOST test, not just theoretical nitpicks.

---

## Findings

### MUST-FIX 1 — `--tags oomd` on `play-systemd-user-tweaks.yml` breaks itself (undefined fact)

`playbooks/imports/play-systemd-user-tweaks.yml:23-47` (the new getent/enable-linger/
user-manager-start tasks) are tagged `[systemd, linger]`. The tasks that *consume* the
fact those tasks establish — the "Verify systemd-oomd Configuration" task
(`:163-178`), the new "Assert systemd-oomd Override Was Applied" indirectly via
`oomd_verify`, and (more importantly) the `reload-systemd-user-daemon` handler
(`:246-255`) and the verify task's own `environment:` block — reference
`ansible_facts['getent_passwd'][user_login][1]`, and the tasks that *write* it are
tagged `[systemd, oomd]`, not `linger`.

Running `ansible-playbook playbooks/imports/play-systemd-user-tweaks.yml --tags oomd`
(a plausible, tag-system-encouraged surgical invocation — `CLAUDE/AnsibleStyle.md`
"Tagging Strategy" exists precisely to enable this) selects every task carrying
`oomd` regardless of what *other* tags it also carries — so it runs the
blockinfile/verify/assert tasks but **skips** the getent/linger/user-manager-start
tasks (they only carry `systemd`+`linger`, and `--tags oomd` does not imply
`linger`). The verify task's `environment:` block and the handler then reference an
**undefined** `ansible_facts.getent_passwd[user_login]` and fail — under
`any_errors_fatal: true` this aborts the whole run, with a confusing "has no
attribute" error rather than a clean explanation.

This does not affect the untagged `playbook-main.yml` / plain-`ansible-playbook`
invocations the HOST test will actually use (both task groups share the `systemd`
tag, so any selection that includes `systemd` gets everything) — but it is a real,
easily-triggered footgun for the next person who reaches for `--tags oomd` to iterate
on just the oomd config, which is exactly the kind of surgical run this repo's tagging
convention is meant to support.

**Fix**: tag the getent/enable-linger/user-manager-start tasks (`:23-47`) with
`always` (or add `oomd` alongside `linger`) so they run whenever anything else in this
play is selected by tag.

### MUST-FIX 2 — new reconcile script mixes diagnostic output into the parsed stdout stream

`files/usr/local/bin/lxc-docker-user-iptables-reconcile.bash:24`:

```bash
if ! iptables -n -L DOCKER-USER; then
```

This executes the chain-listing command directly (unredirected), so its multi-line
table dump lands on the script's **stdout** ahead of the stable
`LXC-IPTABLES-RECONCILE-{CHANGED,NOOP}` marker line that
`playbooks/imports/play-lxc-install-config.yml:316-320`'s `changed_when` substring-
matches against. `CLAUDE/StderrHygiene.md` documents this exact anti-pattern
("stdout = the one thing a caller would `$(capture)` … Mixing chatter into stdout
silently pollutes `$(cmd)` captures") and its own decision procedure ("does any caller
capture this in `$(...)`?") applies directly here: yes — Ansible's `register` +
`changed_when` is exactly such a caller.

It is harmless *today* only because the substring check tolerates the extra noise and
nothing currently does `output=$(lxc-docker-user-iptables-reconcile.bash)` expecting a
clean marker. But it is a written repo standard violation, not a stylistic quibble,
and the whole point of the standard (per its own worked example) is that this class of
bug is "silent until a caller captures the output."

**Fix**: capture the probe instead of letting it write straight through, per the
`CLAUDE/InteractiveScripts.md` "Capturing a probe without an error-hiding redirect"
pattern:

```bash
if ! chain_dump="$(iptables -n -L DOCKER-USER 2>&1)"; then
    echo "$chain_dump" >&2
    echo "lxc-docker-user-iptables-reconcile: DOCKER-USER chain does not exist ..." >&2
    exit 1
fi
```

---

### NIT — `head -n1` after a pipeline, under `pipefail`, is SIGPIPE-fragile on a multi-route interface

`files/usr/local/bin/lxc-docker-user-iptables-reconcile.bash:30`:

```bash
subnet_cidr="$(ip -o -4 route show dev "$bridge" proto kernel | awk '{print $1}' | head -n1)"
```

Under `set -o pipefail`, if `ip route show ... proto kernel` ever emits more than one
matching line (a manually-added secondary address/route on `lxcbr0`, for instance),
`head -n1` closes its input after the first line, `awk`/`ip` get `SIGPIPE` (exit 141),
and `pipefail` propagates that as the pipeline's exit status — aborting the script
under `set -e` *before* the friendly `-z "$subnet_cidr"` fail_msg can run. In the
normal single-route case (the only case exercised so far) `head` hits a natural EOF and
this never fires, so it is latent, not active. It is also **not a new regression** —
this exact construct is carried over verbatim from the Phase-1 inline shell task that
this script replaced (`play-lxc-install-config.yml` pre-`eb14ba2`). Worth fixing
opportunistically: `awk '{print $1; exit}'` gets the same first-match value with no
downstream `head` and no SIGPIPE exposure.

### NIT — design-doc/journal ordinal is wrong (harmless, but worth correcting)

`reviews/2026-07-29-phase2-design-decision-fable.md` and the `JOURNAL/` entries
repeatedly state "`play-podman.yml` is play #23" in `playbook-main.yml`. Counting the
actual `import_playbook` list (`playbooks/playbook-main.yml:5-45`), `play-podman.yml`
is **#19** (`play-docker.yml`=17, `play-lxc-install-config.yml`=18, `play-podman.yml`=19).
The load-bearing claim — that play #7 (`play-systemd-user-tweaks.yml`) runs before
`play-podman.yml` — is still true, so this doesn't change the correctness of the fix,
but it's a factual error a future reader could take on faith from the shipped design
doc.

### NIT — two failure surfaces for one condition in the oomd verify

`play-systemd-user-tweaks.yml:163-178` (`set -o pipefail; systemctl --user show user.slice | grep ManagedOOMMemoryPressure`) can itself fail (grep no-match → pipefail
→ non-zero shell task) *before* the new, well-messaged "Assert systemd-oomd Override
Was Applied" task (`:180-193`) ever runs — meaning there are two possible failure
points for the same underlying condition, only one of which has the crafted
`fail_msg`. In practice `ManagedOOMMemoryPressure` is a standard exposed property on
any modern systemd unit (Fedora 44 qualifies, and this exact grep pattern is what the
pre-Phase-2 code already relied on, just with `failed_when: false` swallowing a
no-match) so this is unlikely to trigger — but if it ever did, the user gets a generic
non-zero-rc dump instead of the intended explanation. Consider dropping the `grep`
entirely and letting the new assert's `'ManagedOOMMemoryPressure=auto' in oomd_verify.stdout` check do the filtering against the full `systemctl show` output,
consolidating on one well-messaged failure point.

### NIT — inconsistent `ansible_facts` attribute-access style

`play-systemd-user-tweaks.yml:43` uses dotted access
(`ansible_facts.getent_passwd[user_login][1]`) while the handler, the verify task, and
`play-podman.yml`'s socket task all use bracket access
(`ansible_facts['getent_passwd'][user_login][1]`) for the identical fact. Both are
valid Jinja2; purely cosmetic, but worth aligning for grep-ability.

---

## Checked clean

- **`getent` fact scope**: `ansible_facts.getent_passwd[user_login][1]` is UID (index
  `[1]` in the `[password, uid, gid, gecos, home, shell]` tuple `getent` returns) —
  confirmed correct. The fact set by the play's own task 1 persists for the rest of
  that play (task 3, the handler, the verify task) — standard Ansible host-fact
  lifetime, not a race. It also persists across **plays** in the same
  `ansible-playbook` invocation (confirmed `play-systemd-user-tweaks.yml` is import #7,
  `play-podman.yml` is import #19, same run) — but `play-podman.yml` additionally
  carries its **own** defensive `getent` task ahead of the socket-enable task
  (`play-podman.yml:30-33`, verified placement), so standalone-run and any
  `--tags`-driven skip of play #7's getent task cannot break play #19 either way.
- **Explicit `user@UID.service` `state: started`**: `ansible.builtin.systemd` invokes
  `systemctl start` without `--no-block`, which genuinely blocks until the job
  completes; for a `Type=notify` unit (which `user@.service` is, shipped by the base
  `systemd` package) that means blocking on `READY=1`, not just job-submission — the
  design's core claim holds. `become: true` with no `become_user` is correct here
  (`user@UID.service` is a **system**-scope unit managed by PID 1, not a `scope: user`
  call), so running it as root is right.
- **`flush_handlers` + assert ordering**: placed after the notifying blockinfile task
  and before the verify — reads the reloaded `user.slice` on a first run. On a re-run
  where the blockinfile is unchanged (handler not notified), `flush_handlers` is a
  no-op and the assert still passes because the drop-in was already applied and
  reloaded by a prior run — verified this is not a false-pass, since the property is a
  durable unit-file characteristic, not something that reverts between runs.
- **`ignore_errors: true` removal**: confirmed fully gone from the handler
  (`play-systemd-user-tweaks.yml:246-255`); grepped both edited files for
  `failed_when: false`/`ignore_errors` — none remain unannotated, and the one
  remaining `failed_when: false` anywhere near this work
  (`play-lxc-install-config.yml`'s `lxc-checkconfig` task) is pre-existing, already
  carries `# FAIL-FAST-OK:`, and untouched by this diff.
- **`play-podman.yml` standalone behaviour**: with the `dbus_session_check`
  probe-then-`when` deleted, a never-lingered standalone run now fails loud on the
  socket-enable task rather than silently skipping — explicitly documented in-play
  (`:23-29`) as the intended, acceptable fail-fast trade-off. Play-level `become: false` (`:5`) with task-level `become: true`/`become_user` overrides on the
  Install/Socket tasks is the correct, standard pattern.
- **T3.1 ostree assert**: `not _ostree_booted_marker.stat.exists` is the correct
  boolean; placement (between the Fedora-version check and the
  `provisioning_profile` check, `play-AA-preflight-sanity.yml:29-54`) matches the
  design and doesn't disturb the existing checks. `stat:`/`assert:` are plain modules,
  not `shell:`, so the Ansible-2.19 quote-balance/`split_args` parser hazard
  documented in `CLAUDE/AgentNotes.md` doesn't apply regardless — and I re-scanned the
  multi-line `fail_msg` for apostrophes/backticks/`: -` patterns anyway; none present.
- **F8 script logic**: `iptables -C` probe-then-insert is correctly idempotent and
  explicitly-checked (not error-hiding); no `2>/dev/null` is needed or used elsewhere
  in the script; `readonly bridge=lxcbr0` matches the *existing, unparameterised*
  `lxcbr0` literal used throughout the rest of this same play (firewalld zone bind,
  NM-unmanaged block, sanity checks) — not a new hardcoding regression.
- **F8 unit semantics**: `PartOf=docker.service` does propagate both stop *and*
  restart actions from `docker.service` onto this `Type=oneshot`/`RemainAfterExit=yes`
  unit (per `systemd.unit(5)`) — the design's central claim is correct systemd
  semantics. `Requires=`+`After=docker.service` orders it at boot with no dependency
  cycle. Deployment idiom (`copy` + `daemon_reload: true` + `enabled: true` +
  `state: started` in one `systemd:` task) matches the repo's existing
  `displaylink-dock-recovery.service` precedent.
- **Lost `lxc_subnet_cidr` register var**: grepped the entire repo — zero references
  remain anywhere (playbooks, scripts, docs); the removal is clean, no dangling
  downstream consumer.
- **`state: started` (not `restarted`) staleness question**: not a bug. The oneshot
  unit `exec`s whatever is currently on disk at `/usr/local/bin/...bash` fresh on
  every trigger (boot / docker-restart) — it holds no cached script content between
  invocations — and the immediate-apply Ansible task already re-runs the
  just-`copy`'d script in the same session regardless of the unit's running state. A
  future script content change is applied both immediately (this run, via the command
  task) and on every future trigger (fresh exec of the updated file); `daemon_reload`
  refreshes any unit-file-level changes too. No live-vs-persisted drift window exists.
- **File modes**: both new `files/` entries carry the right git-tracked mode
  (`lxc-docker-user-iptables-reconcile.bash` = `100755`,
  `lxc-docker-user-iptables-reconcile.service` = `100644`), matching the Ansible
  `copy:` tasks' explicit `owner`/`group`/`mode`.
- **Complex-logic-to-Python-helper rule** (`playbooks/CLAUDE.md`): correctly judged
  not to apply — that rule's stated rationale is Ansible's `split_args` parser
  rejecting non-trivial *inline* `shell:`/`command:` content; a standalone deployed
  script invoked via `command:`/`ExecStart=` is never passed through that splitter,
  and this repo already deploys comparable conditional-bearing bash under `files/`
  (e.g. other `files/home/.local/bin/*.bash` executables) without Python-helper
  extraction.
- **Ordering precedent for T2.1/T2.2's env-var shape**: cross-checked against
  `playbooks/imports/optional/common/play-rclone.yml:155-212`, the cited precedent —
  the getent → enable-linger sequence and the `XDG_RUNTIME_DIR`/
  `DBUS_SESSION_BUS_ADDRESS` `environment:` shape used there matches what Phase 2
  applied, confirming the pattern is genuinely load-bearing prior art, not an
  invented shape.
