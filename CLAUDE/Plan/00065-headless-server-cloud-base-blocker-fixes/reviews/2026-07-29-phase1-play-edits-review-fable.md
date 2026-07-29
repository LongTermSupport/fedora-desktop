# Phase-1 Play Edits Review — Plan 00065 (T1.1–T1.4)

**VERDICT: SOUND-WITH-FIXES**

Reviewed at `becf5a7` (HEAD) and `0e02604`, against a freshly-pulled checkout
(`git pull --ff-only` → already up to date at `becf5a7`). No blocker found. One
MUST-FIX (a defensive gap around enabling firewalld for the first time on a
box this repo may be administered over SSH on) and two NITs (a known,
narrow-window Fedora packaging bug, and a pre-existing style violation
adjacent to the touched markless block).

---

## Findings

### 1. MUST-FIX — no assurance the default firewalld zone still permits SSH before first-time enable

**File**: `playbooks/imports/play-lxc-install-config.yml:102-106` (new task
"Ensure firewalld is running")

**Defect**: This is Cloud Base's **first-ever** firewalld start — the box has
never run a firewall before this task. `AnsibleStyle.md` documents this repo's
target as `hosts: desktop` with **local** connection (the playbook runs ON the
box, typically inside a human's existing terminal/SSH session, e.g. via
`run.bash`). Nothing in this play or the ones before it asserts, sets, or even
queries which zone will apply to the box's admin-facing NIC, or that the zone
permits `ssh`, before the daemon is enabled+started.

In practice the risk is low — stock Fedora ships `DefaultZone=public` in
`firewalld.conf`, and firewalld's stock `public.xml` includes `ssh` and
`dhcpv6-client` by default, and firewalld does not flush conntrack on a first
start (established sessions survive). But this repo's own stated posture is
paranoid-by-design about exactly this class of gap (`CLAUDE.md` "Missing
Dependencies — Fail Fast, Fix in IaC", and the extensive Docker/firewalld
iptables-reconciliation machinery a few tasks later in this same file already
shows the authors don't trust default assumptions to hold across images). A
non-stock cloud image, a prior cloud-init firewalld config drop-in, or simply
a dropped connection immediately after this task runs (common on flaky links
during a long provisioning run) would silently strand the operator with no
prior signal — that is a **hard-to-reverse, high-impact failure mode** (loss
of the only access path to a fresh headless VM) introduced by turning on a
firewall that was, until this change, simply absent.

**Fix**: add an explicit, idempotent assurance immediately before "Ensure
firewalld is running" — e.g. a `community.general.ini_file`/`lineinfile`
confirming/forcing `DefaultZone=public` in `/etc/firewalld/firewalld.conf`
(only if unset — do not clobber an intentional override), or an
`ansible.posix.firewalld` task that explicitly ensures the `ssh` service is
permitted in whatever the box's actual default zone resolves to
(`permanent: true, immediate: true, state: enabled`) run in the same task
batch as the firewalld start. This costs one idempotent task and removes a
plausible self-lockout path on exactly the class of box (fresh, minimal,
remote, headless) this whole plan is written for.

### 2. NIT — `iptables-nft` had a known (since-fixed) Fedora 42 packaging bug that could omit `/usr/bin/iptables`

**File**: `playbooks/imports/play-lxc-install-config.yml:65`

Researched externally: `iptables-nft` is the correct current Fedora package
name and normally does provide a working `iptables` binary via `alternatives`
(confirmed against Fedora Packages + the `Changes/iptables-nft-default`
wiki page). However, a Fedora 42 update in April 2025 shipped `iptables-nft`
without wiring the `/usr/bin/iptables` alternative (an alternatives/usr-merge
interaction bug), later reverted/fixed upstream. If this play ever runs
against a point-in-time-affected image, the later "Assert DOCKER-USER
iptables chain exists" task (`iptables -n -L DOCKER-USER`,
`play-lxc-install-config.yml:277`) fails loudly with "command not found" —
consistent with this repo's fail-fast posture, so **not a silent failure**,
just a confusing one (an operator sees a bare command-not-found with no hint
it's a distro packaging regression rather than an IaC bug).

**Fix (optional, low priority)**: not required to unblock this plan; if it
ever bites, a one-line comment pointing at the known Fedora bug next to the
package declaration would save someone a diagnosis cycle. Not worth blocking
Phase 1 on.

### 3. NIT — `play-markless.yml`'s touched shell block still uses `set -ex`, not the mandated `set -euo pipefail`

**File**: `playbooks/imports/play-markless.yml:25`

`CLAUDE/AnsibleStyle.md` ("External Repository Integration") requires every
multi-command `shell: |` block to begin with `set -euo pipefail` (plus
`args: executable: /bin/bash`, which IS present here). This block still opens
with `set -ex` (pre-existing — not introduced by T1.1's diff, which only
touched the `cd ~/Downloads` / `rm -rf markless*` two lines). It's flagged
here because T1.1 is precisely the commit that opened this block back up;
there is no `|` pipe inside the block whose masked exit code `pipefail` would
protect today, so this is not currently exploitable, but it is one keystroke
away from becoming exploitable the next time someone adds a pipe to this
script, and it's already non-compliant with the documented house style.

**Fix (optional, low priority)**: change `set -ex` → `set -euo pipefail` (`-x`
tracing is optional/additive per the style doc, so drop it or keep it
alongside — `set -euxo pipefail` also satisfies the rule). Cheap, but not
required to land Phase 1 since it is pre-existing and unrelated to the actual
bug being fixed.

---

## Categories checked with no findings

- **Dependency completeness (T1.3)**: read the whole play and enumerated
  every external binary/service/module it invokes — `firewall-cmd`/firewalld
  D-Bus bindings (`ansible.posix.firewalld`), `nmcli`, `iptables` (direct +
  NAT), `dnsmasq` (via lxc-net), `lxc-net`/`lxc-checkconfig`/`lxc-ls`, `ssh- keygen`, `git` (already covered — see below), `sysctl`. All five newly
  declared packages (`firewalld`, `python3-firewall`, `dnsmasq`,
  `iptables-nft`, `NetworkManager`) map to a real consumer in this exact
  file. Verified externally (web research) that `python3-firewall` is the
  correct/current package name for the firewalld Python bindings the
  `ansible.posix.firewalld` module imports, and that base `firewalld` itself
  (no separate subpackage) ships `firewall-cmd` + the systemd unit. No
  missing package found. `git` is NOT newly required by T1.4 — the *previous*
  code already shelled out to `git clone` directly, so this is a pre-existing
  assumption, and it's a safe one: `play-git-configure-and-tools.yml:3-4`
  explicitly documents "Git is already installed as it was used to clone this
  repo" — a property that holds for any host this repo's playbooks run
  against, headless or not.

- **Ordering**: "Ensure firewalld is running" (line 102) is correctly placed
  before its only firewalld-module consumer, "Bind lxcbr0 to the trusted
  firewall zone" (line 110), and before lxc-net is ever started (lxc-net is
  only `enabled`, not `started`, until the "restart lxc-net" handler fires
  later) — so lxcbr0's zone is bound before the bridge/DHCP server exists,
  removing the race the comment describes. No interaction found with the
  later Docker/DOCKER-USER iptables reconciliation block (separate nft
  table namespace; this repo already has extensive tooling built on that
  coexistence working). See Finding 1 for the one ordering-adjacent gap that
  *is* real (no SSH-zone assurance before daemon start).

- **HTTPS clone correctness (T1.4)**: confirmed experimentally in this
  container that plain `git clone` creates missing leading directories (`git clone url a/b/c/dest` succeeds even when `a/b/c` doesn't exist) — so
  `~/Projects` not existing yet is not a bug; `ansible.builtin.git` will
  create it. `become_user: "{{ user_login }}"` is set at the parent
  `block:` level (`play-lxc-install-config.yml:213`) and applies to **both**
  the `git` task and the `lineinfile` task inside it — so both the clone and
  `~/Projects` end up owned by the target user, not root, and the
  `lineinfile` write to `~/.bashrc` also runs as that user. `update: false`
  correctly preserves the old `creates:`-equivalent "clone once, never fight
  local edits" semantics. Removing the vault-passphrase assert does not
  orphan `github_ssh_passphrase` — grepped the whole repo; it's still a hard
  requirement of `play-github-cli-multi.yml` and `scripts/gh-account-setup.bash`,
  so the variable and its vault entry remain live and necessary elsewhere.
  The `/tmp/.lxc_ssh_pp` passphrase file and its `always:` cleanup were fully
  removed together with their only producer/consumer — no dangling reference.

- **Fail-fast / suppression**: no new `failed_when: false` or
  `ignore_errors: true` anywhere in either commit. The pre-existing
  `failed_when: false  # FAIL-FAST-OK: ...` probe-then-act lines in the
  iptables reconciliation block are untouched by this diff and were already
  correctly annotated.

- **2.19 parser hazards**: all new/changed text was checked for unbalanced
  quotes/apostrophes, `: -` patterns in unquoted task names, and self-default
  vars. None found. Note the new multi-line comments in T1.3 (package-list
  rationale, firewalld-start rationale) sit outside any `shell:`/`command:`
  module value, so the 2.19 argument-splitter's quote-balance scan (which
  only inspects module content, not arbitrary YAML comments) does not apply
  to them regardless.

- **Scope**: `play-lxc-install-config.yml` stays `scope: general`
  (`line 19`, unchanged) with no scope-guard tasks added — correct, matches
  `AnsibleStyle.md`'s "general play carries no guard" rule. `play-markless.yml`
  likewise stays `general` (unchanged). Neither play gained an inappropriate
  guard or GUI gate.

- **markless mktemp (T1.1)**: `trap 'rm -rf "$work"' EXIT` fires under
  `set -e`/`set -ex` on any exit path, including one triggered by a failing
  command — standard, reliable bash behaviour. `$work` is quoted at every use
  (`cd "$work"`, the trap body). Read the entire rest of the file: nothing
  downstream assumes `~/Downloads` or a shared `markless*` glob — the only
  consumer of the download is the immediately-following `tar -xzf` +
  `mv -f markless "{{ marklessPath }}"` in the same scratch dir, and the
  early `exit 0` (already-current-version) path returns before `mktemp` is
  ever called, so no cleanup is owed there either.

- **fwupd gate (T1.2)**: `provisioning_profile` is a `group_vars/desktop.yml`
  fact auto-applied to every host in the `desktop` group (per
  `AnsibleStyle.md`'s Provisioning Profile Self-Guard section) — it's in
  scope with no explicit import needed. The USB-audio task earlier in the
  same file (`play-basic-configs.yml:194-195`) uses the byte-identical
  `when: provisioning_profile != 'server'` idiom, confirming the cited
  precedent is real and consistent within this file. The gated task's
  `changed_when:` (which references `fwupd_result.stdout`) is only evaluated
  when the task actually runs, so gating it with `when:` introduces no
  evaluation-order hazard on the skip path.

---

## Note on scope not reviewed

SELinux-permissive (F7) and DOCKER-USER reboot-persistence (F8) are called
out in the commit message as untouched, design-laden Phase-3 items — correctly
out of scope for this review, not evaluated here.
