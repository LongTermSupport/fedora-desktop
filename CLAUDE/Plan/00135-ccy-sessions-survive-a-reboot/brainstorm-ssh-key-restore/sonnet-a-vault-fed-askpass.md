# Brainstorm: restored ccy session stuck at SSH key passphrase

Agent: sonnet-a. Independent brainstorm per BRIEF.md — no other agent files read.

## Grounding in the actual code

- `discover_and_select_ssh_keys` (`files/var/local/claude-yolo/lib/ssh-handling.bash`)
  picks a key interactively unless there is only one candidate (the project-remote
  alias key, alone), in which case it auto-selects with no prompt.
- `build_ssh_mounts_and_validate` then unlocks every selected key into a **private
  probe ssh-agent** via `ssh-add` (`_probe_agent_start` / `_probe_agent_add_key`) —
  this happens only when `[ -t 0 ]` and `HEADLESS_MODE != true`. `HEADLESS_MODE` is
  set only by `--headless` (which also requires `--prompt`, i.e. a one-shot
  non-interactive Claude run, not an attended session) — restore does not set it.
- Restore (`ccy-sessions restore`, `session-registry.bash` `ccy_registry_restore`)
  replays the original launch args, keeping `--ssh-key` if it was given
  (`CCY_REGISTRY_KEEP_VALUE_FLAGS`). If the session was started via the interactive
  picker without `--ssh-key`, the replayed args carry nothing, so
  `discover_and_select_ssh_keys` runs again on restore, unless a saved
  `.claude/ccy` "Quick Launch" config exists — restore auto-accepts that
  (`SESSION_RESTORE=true` short-circuits the Y/n prompt), which fixes prompt #1
  in the *common* case. It does **not** fix prompt #2: whatever key is chosen, it
  is still passphrase protected, so `_probe_agent_add_key` always calls
  interactive `ssh-add`, which reads from the controlling terminal — inside a
  systemd-launched, unattended tmux pane, that terminal has nobody attached.
- **The project already has a working answer to almost this exact problem**,
  just not on this code path: `run.bash`'s headless provisioning
  (`hl_ssh_agent_start`, around line 510) loads the same passphrase-protected
  login key (`~/.ssh/id`) into an agent completely non-interactively, using
  `SSH_ASKPASS` + `SSH_ASKPASS_REQUIRE=force` with a **0600 file** holding the
  passphrase (never argv, never stdin) and an askpass helper script that reads
  that file at runtime. `scripts/gh-account-setup.bash`'s `decrypt_passphrase()`
  already knows how to pull `github_ssh_passphrase` out of the vault using
  `vault-pass.secret`, either directly or via an already-set env var. That is
  the missing link for restore: an unattended path already exists in this
  repo, for this exact secret, for this exact purpose (unlocking the signing
  key into an agent) — it just isn't wired into `ccy-sessions restore`.

## Ideas

### 1. Vault-fed `SSH_ASKPASS` helper, reused from `run.bash`, invoked only by restore

**How it works.** Add a small helper (e.g.
`files/var/local/claude-yolo/lib/restore-askpass.bash`, generated fresh each
restore into a 0700 tmp dir, deleted after use) that, when `ssh-add` needs a
passphrase, prints it by shelling out to `decrypt_passphrase`-equivalent logic
(ansible-vault decrypt of `github_ssh_passphrase` using `vault-pass.secret`,
exactly as `gh-account-setup.bash` already does). `ccy_registry_restore` sets
`SSH_ASKPASS=<helper>`, `SSH_ASKPASS_REQUIRE=force`, and unsets `DISPLAY`'s
relevance by forcing the askpass path unconditionally (mirroring
`hl_ssh_agent_start`'s technique) for the one `_probe_agent_add_key` call that
restore triggers. `ssh-handling.bash` gains a restore-only branch: when
`SESSION_RESTORE=1`/a new `CCY_RESTORE_ASKPASS=1` marker is set, `ssh-add` is
invoked with `SSH_ASKPASS`/`SSH_ASKPASS_REQUIRE=force` and **no controlling
terminal** (`setsid ssh-add < /dev/null`), so it cannot fall back to a TTY
prompt even if the askpass invocation fails oddly — it fails closed instead of
hanging.

**What it needs.** `vault-pass.secret` readable by the restoring user (already
true — it's how Ansible itself decrypts `localhost.yml` on this box);
`ansible-vault` or `ansible localhost -m debug` available at restore time (it's
already an install-time dependency); a marker so this askpass path is used
*only* during `ccy-sessions restore`, never during an attended launch (an
attended user should keep typing their own passphrase — no regression to the
retired passphrase-free-key decision for the common case).

**Desktop vs. headless server.** Identical on both — this fixes the *headless,
nobody-present* case; on the desktop it just means restore quietly succeeds
too, instead of parking a prompt nobody's watching regardless of whether it's
a laptop or a rack server.

**Security cost.** The vault passphrase secret already exists on disk
(`localhost.yml`, vault-encrypted) and `vault-pass.secret` already exists on
disk (0600) — this adds no new secret at rest. What's new: a **decrypted**
passphrase now transiently exists in a pipe/env at restore time on an
unattended boot, where before a human always had to be present to type it.
That is a real, if narrow, increase in the attack surface for "an attacker
with a live shell as this user, timed to hit the few-second restore window" —
but that same attacker already has `vault-pass.secret` and could decrypt it
themselves. The askpass helper file itself must never contain the passphrase
literally (matches `hl_ssh_agent_start`'s existing comment: "the helper
carries only the non-secret PATH, never the passphrase").

**What could go wrong.** Vault decryption at restore time fails silently if
`ansible.cfg`/`localhost.yml` aren't reachable from the restore unit's
working directory or user (systemd `--user` units have a different, sparser
environment than an interactive shell — `PATH`, `ANSIBLE_CONFIG`, etc. may
not be set the way they are in a login shell). Must fail loud per the
project's Fail Fast rule, not silently skip the key and continue unlocked.
Also: this makes `ccy-sessions restore` newly depend on Ansible/vault tooling
being present and correctly configured, which the launcher itself never
needed before (ccy today never decrypts vault secrets itself — it only
consumes what `gh-token-<alias>` and mounted keys already provide).

---

### 2. Systemd-credential-sealed passphrase, decrypted only inside the restore unit

**How it works.** Use `systemd-creds encrypt` (systemd ≥ 250) to seal
`github_ssh_passphrase` at deploy time into a credential file bound to the
**machine's TPM2** (or, without a TPM, to the host's own randomly-generated
key in `/var/lib/systemd/credential.secret`), and load it into the
`ccy-sessions-restore.service` unit via `LoadCredentialEncrypted=`. At
restore, the unit reads `$CREDENTIALS_DIRECTORY/github-ssh-passphrase`
(systemd decrypts it into a private, non-swappable, 0400 tmpfs file just
before the unit starts) and feeds it to the same `SSH_ASKPASS` mechanism as
idea 1.

**What it needs.** systemd ≥ 250 (TPM2 sealing needs ≥ 248 + `systemd-creds`
tool + a TPM device, widely available on Fedora Desktop hardware); an
Ansible task that runs `systemd-creds encrypt --name=github-ssh-passphrase - /etc/credstore.encrypted/ccy-github-ssh-passphrase` from the *decrypted* vault
value at deploy time (never storing the plaintext on disk itself); a unit
file edit for `ccy-sessions-restore.service` adding
`LoadCredentialEncrypted=github-ssh-passphrase:/etc/credstore.encrypted/ccy-github-ssh-passphrase`.

**Desktop vs. headless server.** Both identical, and this is arguably
*better* than idea 1 for the server case specifically because TPM-sealing
ties decryption to *this specific machine's* TPM state (and optionally PCR
measurements, i.e. "only decryptable if the boot chain looks the same") —
exactly the "unattended service needs a secret, but only on this box, only
in a trusted boot state" pattern systemd credentials were built for. No vault
password file needs to be read at restore time at all — the vault
passphrase is only needed once, at deploy/provisioning, to seed the sealed
credential.

**Security cost.** Genuinely lower than idea 1 at rest: the sealed credential
is only decryptable by systemd running as root on *this* machine (TPM-bound),
not by anyone who merely has `vault-pass.secret` and the repo checkout —
which matters a lot for a laptop that could be stolen with its disk
un-encrypted-at-rest-relative-to-a-live-session. Downside: a second copy of
the same secret now exists in a second at-rest form (vault-encrypted
`localhost.yml` AND the systemd-sealed credential store) — two things to keep
in sync if the passphrase is ever rotated, and IaC has to manage a
non-Ansible-native artefact (`systemd-creds` isn't a stock Ansible module,
so this is a raw `command`/`shell` task with `creates:`/idempotency care).

**What could go wrong.** TPM absence or a TPM in a weird state (common in
VMs, some cloud/headless boxes) silently degrades `systemd-creds` to the
weaker host-key-only sealing — must be surfaced, not silently accepted, given
this project's Fail-Fast rule ("no silent degradation"). A `secure-boot`/PCR
policy that's too strict breaks decryption after *any* kernel/firmware
update, which is exactly the kind of flakiness an unattended self-update
cycle (Plan 00137) can't tolerate — needs a deliberately loose PCR policy
(e.g. seal to PCR 7 only, or no PCR binding at all — machine identity, not
boot state).

---

### 3. Bind restore to a GitHub App / short-lived installation token instead of an SSH key at all

**How it works.** Sidestep "unlock a passphrase key unattended" entirely for
the *push* half of what the key is for. A GitHub App installation token
(1-hour-lived, minted via the App's private key — itself just another secret,
but one `gh`/GitHub's own tooling is built to mint headlessly with no human
passphrase step) authenticates HTTPS pushes. The brief notes the key is *also*
used for **commit signing** (Plan 00139 D5, `ssh-agent`-based signing through
`~/.ssh/id`/`~/.ssh/github_<alias>`) — that half doesn't disappear, so this
idea only removes the SSH-for-push half, and restore would need to fall back
to *not signing* commits made during the unattended window, or defer signing
until a human re-attaches and unlocks the key normally.

**What it needs.** A GitHub App registered for the project (or reuse of an
existing one if `gh-account-setup.bash`'s multi-account tokens are already
backed by one), its private key stored the same way the vault stores the SSH
passphrase today, and new code in ccy's entrypoint to prefer an installation
token over SSH when `SESSION_RESTORE` is set.

**Desktop vs. headless server.** Same on both, but this is really a
headless-server-shaped idea — on the desktop, the existing agent-based signing
flow works fine once the owner is back, so this only pays for itself on a
server plan (00137) where unattended pushes across a reboot matter.

**Security cost.** Trades "occasionally-typed long-lived passphrase-protected
key" for "a private key that mints itself 1-hour tokens with no human
interaction ever" — that's *more* automatable and therefore, if compromised,
more immediately usable by an attacker with host access, though its blast
radius can be scoped per-repository (App installation permissions) tighter
than a personal SSH key ever can be.

**What could go wrong.** This is a much bigger change than the other ideas —
it doesn't just fix restore, it changes the standing identity model
(Plan 00139 chose *personal signing keys through ssh-agent*, deliberately, to
have commits signed as the person, not as a bot/app). Doing this only for the
restore window creates two different commit-authorship stories depending on
whether a push happened before or after a reboot — a subtle, hard-to-explain
inconsistency. Likely over-engineering relative to the actual problem (YAGNI).

---

### 4. Defer the passphrase, not the whole session: bring the session back **unlocked for everything except git push/sign**

**How it works.** Restore starts the tmux session and the container exactly
as today, but skips SSH key selection AND the probe-agent unlock entirely on
restore (`CCY_SESSION_RESTORE=1` short-circuits both prompts to "proceed with
no key for now"), rather than making them silently prompt. The agent working
inside gets a clearly-flagged environment (`CCY_SSH_DEFERRED=1`) so its
`git push`/commit-signing attempts fail with an explicit, actionable error
("SSH key not loaded after reboot restore — attach with `ccy-sessions` and
run `ccy --relock-ssh` [a new small subcommand] to unlock, or ask the owner"),
instead of hanging at a prompt forever. `verify-restore` already has a
`WAITING-AT-PROMPT <which>` state (`ccy_restore_verdict`) — this is exactly
that state, just reached deliberately and reported clearly rather than as an
accidental hang.

**What it needs.** A new `ccy --relock-ssh` (or similar) subcommand that
re-runs just `discover_and_select_ssh_keys` + `build_ssh_mounts_and_validate`
against the *running* container's mounts; plumbing so the entrypoint/agent
knows push/signing are unavailable and can say so instead of failing
mysteriously; no vault/systemd-creds work at all.

**Desktop vs. headless server.** Desktop: the owner attaches, sees the clear
"SSH not loaded" state, runs the one command, unlocks by hand — arguably
*better* UX than today's silent hang at a raw `ssh-add` prompt buried in a
detached tmux pane. Headless server (Plan 00137's self-update cycle): the
agent can keep working — editing, testing, running Ansible dry-runs, opening
local commits *unsigned locally, to be signed/pushed later* — and only the
final "publish" step blocks, which it can surface as an explicit todo/ledger
item rather than a hung process. This does NOT fully solve "sessions must
come back after an unattended reboot" if the self-update cycle *requires* a
push mid-cycle with nobody present — it converts a silent hang into a loud,
recoverable stuck state, which is a strictly better fallback but not full
automation.

**Security cost.** Essentially zero — no new secret handling, no new secret
at rest, no new decrypt path. Purely a control-flow / UX change.

**What could go wrong.** If Plan 00137 genuinely needs an unattended push
(not just unattended edits), this idea alone doesn't get there — it needs to
be paired with 1 or 2 for the server case, and used alone only where "a human
finishes the loop eventually" is acceptable. Also: teaching the in-container
agent to detect "SSH deferred" and behave sensibly (queue the push, don't
retry-loop forever, don't silently drop work) is itself non-trivial logic to
get right under the Fail-Fast rule (must not "skip and continue" past a push
failure without a loud, visible marker).

---

### 5. Split the login key into two: an unattended-restore-only *subkey* scoped to push, still passphrase'd but unlocked from a **user-session keyring** (gnome-keyring / `pam_ssh`), not a static vault secret

**How it works.** Rather than storing the SSH passphrase in Ansible vault at
all for this purpose, rely on the **desktop login keyring**
(`gnome-keyring-daemon` with `pam_ssh`/`pam_gnome_keyring`, or plain
`ssh-agent` socket activation tied to the user's PAM session) to unlock
`~/.ssh/id` automatically the moment the user's session (or systemd
`--user` instance, which linger keeps alive) starts, using the **login
password** as the unlocking factor instead of a separately-vaulted SSH
passphrase. If the keyring is already unlocked (e.g. full-disk-encryption +
autologin, or the desktop session was already unlocked before the reboot in
a suspend/resume scenario — less relevant to a cold reboot), restore's
`ssh-add` calls succeed against the already-populated
`SSH_AUTH_SOCK`/keyring-backed agent with no interactive step at all.

**What it needs.** `pam_ssh` or `pam_gnome_keyring` wired into the login PAM
stack (already common on Fedora Workstation via GDM); the SSH key's
passphrase set *identical* to the login password so the PAM unlock actually
unlocks it (a real constraint — GNOME's `pam_ssh`/keyring auto-unlock only
works when the two secrets match, which is itself a security trade a lot of
guidance warns against: reusing the login password as an SSH key passphrase
lowers the key's effective secrecy to whatever protects the login password).

**Desktop vs. headless server.** Desktop: plausible — GDM autologin (or a
kept-open session) plus `pam_gnome_keyring` genuinely gets an unattended
`ssh-agent` populated across a reboot with no vault involved at all. Headless
server: **does not work** — there's no graphical/PAM login session at all on
a true headless box (that's the crux of the owner's original constraint:
"waits for a graphical login is fine for desktop, not for headless"), so this
idea directly reproduces the exact failure mode the brief is trying to avoid,
unless paired with autologin on a virtual console (`agetty --autologin`),
which itself needs an unlocked disk and is a much bigger security move than
anything else on this list.

**Security cost.** Reusing the login password as the SSH key's passphrase is
a real downgrade widely warned against (see Prior Art below) — a single
compromised password now unlocks both the desktop session and the signing
key. Requires disk to be unencrypted-enough for autologin on the server case,
which is a much bigger hole than a vaulted passphrase.

**What could go wrong.** GDM autologin combined with keyring auto-unlock is
a well-known weakening (see ArchWiki/GNOME docs below) — and it doesn't
generalize to the actual headless case at all. Ranked low mainly for that
reason: it solves the desktop half only, and the desktop half was already
declared fine to leave waiting for a human.

---

### 6. Hardware-backed key (YubiKey / FIDO2 `ssh-ed25519-sk`) with a **PIN-caching resident credential**, unattended via a policy that trusts "this TPM/security-key is physically present at boot"

**How it works.** Move the signing key to a FIDO2 security key
(`ssh-keygen -t ed25519-sk -O resident`) plugged into the server permanently.
Some FIDO2 tokens support a "no user presence required" (`-O no-touch-required`)
policy or a cached PIN via `gpg-agent`/`pkcs11` style daemons, allowing signing
operations without a human touch after boot, as long as the token stays
physically inserted.

**What it needs.** Physical hardware token permanently attached to the
headless box; `ssh` built with FIDO2/U2F support (widely available);
provisioning work to enrol the resident credential.

**Desktop vs. headless server.** Backwards from what's needed: this trades a
software secret for a *physical, boot-order-dependent* one — plausible for an
always-on server in a known rack, actively bad for a laptop (you'd need to
leave a security key permanently plugged into the laptop, defeating the
point of a removable hardware token, and it'd be lost/stolen with the laptop
itself).

**Security cost.** High assurance against remote-only compromise (an
attacker needs physical access to the token), but a *lower* assurance
against "attacker has physical access to the whole machine," which for a
literal physically-present-headless-server is a wash, and for a laptop is
worse than today.

**What could go wrong.** Fiddly, hardware-dependent, breaks the moment the
token needs replacing/re-enrolling, and most FIDO2 "no touch" policies are
explicitly *discouraged* by vendors as a security regression — largely
defeats the reason to use a hardware token in the first place. Mentioned for
completeness; not a serious contender here.

---

### 7. Two-tier trust: a **short-lived, restore-scoped SSH certificate** signed by a project/host CA, instead of unlocking the long-lived personal key at all

**How it works.** Stand up (or reuse, if one already exists elsewhere in the
provisioning for other purposes) a minimal SSH CA. At restore time, a
service — itself unlocked by *something* unattended-safe, e.g. the
systemd-sealed credential from idea 2, but only to sign a **short-lived
certificate** rather than to directly hand out the long-lived login key —
mints a certificate valid for, say, 10 minutes, wrapping a throwaway keypair
generated fresh at boot. GitHub does not natively verify SSH certificates for
personal accounts (that's an Enterprise Server / cert-authority feature, not
available for github.com personal repos), so this only works as described if
paired with a GitHub App/deploy-key model (see idea 3) or self-hosted Git —
worth naming as prior art even though it doesn't map cleanly onto
github.com without idea 3's identity shift too.

**What it needs.** An SSH CA (`ssh-keygen -s`), a way to mint certs
unattended, and (critically) a git remote that accepts certificate auth,
which plain github.com personal-account SSH does not.

**Desktop vs. headless server.** N/A as a github.com-facing solution unless
combined with idea 3; would apply cleanly only to a self-hosted Git remote.

**Security cost.** Short-lived certs are a real security *improvement* over
a standing key when they apply — compromise window shrinks to minutes. But
the long-lived signing key (for commit signing, not just transport auth)
still needs unlocking by *something*, so this doesn't remove the original
problem, only moves it one layer up (now "unattended CA signing key" needs
the same unattended-unlock treatment idea 1/2 already solve).

**What could go wrong.** Doesn't fit GitHub.com's personal-account model at
all without a bigger identity rework; adds a CA to operate and rotate; likely
overkill for this project's actual footprint (one repo, one or a few
personal GitHub accounts).

## Prior art (web research)

- **systemd credentials (`systemd-creds`, `LoadCredentialEncrypted=`)** — the
  modern, purpose-built systemd answer to "give a unit a secret it can use at
  start-up without it living in the unit file or the environment," including
  TPM2-backed sealing so the credential only decrypts on the specific machine
  (optionally bound to a specific measured boot state). Documented in
  `systemd.exec(5)` and `systemd-creds(1)`; TPM2 sealing added around systemd
  248-250. This is exactly the "unattended service needs a decrypted secret,
  once, at start" shape ccy-sessions-restore.service has.
  (freedesktop.org systemd docs; multiple LWN/Fedora Magazine writeups on
  "systemd credentials" from 2022 onward.)
- **`SSH_ASKPASS` + `SSH_ASKPASS_REQUIRE=force`** — the standard OpenSSH
  mechanism (since OpenSSH 8.4) for feeding `ssh-add`/`ssh` a passphrase from
  a program instead of a TTY, explicitly designed for non-interactive/
  headless contexts (originally for GUI toolkits, now also used exactly this
  way for automation). `ssh-add(1)`/`ssh(1)` man pages document
  `SSH_ASKPASS_REQUIRE` forcing askpass use even with a TTY present — which is
  precisely the override needed here, and precisely what `run.bash`
  (`hl_ssh_agent_start`) in this repo already does for the same key.
- **`ssh-agent` socket activation via systemd user units** — systemd/Fedora
  ship `ssh-agent.socket`/`ssh-agent.service` user units that start the agent
  on first connection and keep `SSH_AUTH_SOCK` stable across logins; this
  solves "the agent survives," never "the key gets unlocked without a human,"
  so it composes with (doesn't replace) an unattended-unlock idea.
- **`pam_ssh` / `gnome-keyring`'s SSH agent integration (`pam_gnome_keyring`)**
  — auto-unlocks an SSH key at login when its passphrase matches the login
  password, widely documented (ArchWiki "GNOME/Keyring", "Ssh-agent"), and
  just as widely flagged as a security trade-off precisely because it forces
  the SSH key's effective secret down to the login password's strength — the
  same objection this project already raised when retiring passphrase-free
  keys. Solves the desktop/PAM-session case only; does not exist without a
  login session, so it does not reach true headless.
- **`keychain` (funtoo/gentoo tool)** — a long-standing wrapper that keeps one
  `ssh-agent` alive across logins/reboots and re-attaches shells to it,
  explicitly built for "server administrators [who] need ssh-agent... across
  multiple login sessions." It solves agent persistence across reboots, not
  unattended unlocking — after a reboot the agent is empty again and
  `keychain` still needs *something* to answer the passphrase prompt once,
  which is the exact gap this brief is about.
- **Hardware security keys for SSH (FIDO2/U2F, `ecdsa-sk`/`ed25519-sk`)** —
  OpenSSH 8.2+ native support; GitHub's own docs recommend hardware keys for
  the highest-assurance SSH signing/auth, but their entire value proposition
  (a human must be present to tap/touch) is in direct tension with
  "unattended, nobody present," matching the concern raised in idea 6 above.
- **SSH certificates (`ssh-keygen -s`, a CA)** — used at scale by
  HashiCorp Vault's SSH secrets engine and Netflix/Lyft-style infra
  (BLESS: "Bastion's Lambda Ephemeral SSH Service", a well-known open-source
  example of "mint a short-lived signed cert instead of storing a long-lived
  key") precisely to avoid distributing long-lived keys to unattended hosts
  at all — the host asks a CA for a short-lived cert each time it needs one,
  and the CA's own signing key is the one thing that must be protected (with
  HSM/KMS backing in Vault's case). Doesn't map onto plain github.com SSH
  (no cert-auth support for personal accounts), which is why idea 7 above
  only partially applies here.
- **GitHub Apps / installation access tokens** — GitHub's own recommended
  replacement for long-lived PATs/deploy keys in automation
  (docs.github.com "Differences between GitHub Apps and OAuth apps";
  "Authenticating as a GitHub App installation"): a private key mints
  short-lived (1 hour) installation tokens with no human step, which is the
  direct analogue of idea 3's approach to the *push* half of this problem
  (not the commit-signing half, which GitHub Apps don't do for personal
  commit authorship).
- **HashiCorp Vault SSH secrets engine ("one-time SSH passwords" and
  "signed certificates" modes)** — another widely-deployed pattern for "a
  fleet of unattended hosts needs SSH credentials without storing long-lived
  keys on each host," conceptually the same shape as idea 7, generalized
  beyond git hosting.
- **cloud-init / AWS Secrets Manager + `RUN_BASH_*_FILE` env-var-holds-a-path
  pattern** — the exact pattern `run.bash`'s own header comments already
  reference for headless provisioning
  (`RUN_BASH_VAULT_PASSWORD_FILE=/run/secrets/vault-pass`,
  `aws secretsmanager get-secret-value ... > /run/secrets/vault-pass`) is a
  textbook instance of the general "secret manager injects a file at
  instance boot, application reads the file, never the argv/env" pattern
  documented by every major cloud secrets manager (AWS Secrets Manager,
  HashiCorp Vault Agent, systemd-creds itself). Worth noting because this
  repo has *already chosen* this pattern for the vault password itself — the
  restore problem is really "apply the pattern the repo already uses for the
  vault password, to the SSH passphrase too, at restore time."

## Top three, ranked

1. **Idea 1 — vault-fed `SSH_ASKPASS` helper, reused from `run.bash`.**
   Ranked first because it is the smallest, most consistent change: it
   reuses a pattern (`SSH_ASKPASS_REQUIRE=force` + a passphrase-in-a-file
   read by a helper, never argv/env directly) that **already exists and is
   already trusted in this exact codebase** for this exact secret
   (`decrypt_passphrase`, `hl_ssh_agent_start`). It does **not** bend the
   "passphrase-free keys were retired" decision — the key stays
   passphrase-protected always; only the *restore path* gains a way to
   supply that passphrase without a human typing it, scoped narrowly to
   `SESSION_RESTORE=1`. It does not bloat the ssh-agent (still one unlock
   into the same private probe agent structure the code already has). Its
   only real new dependency is that restore now needs Ansible-vault tooling
   reachable from a systemd `--user` unit's environment, which is a solvable,
   bounded IaC task (fits "fix it in the playbook," not a manual workaround).

2. **Idea 4 — defer SSH, surface the stuck state loudly, add `ccy --relock-ssh`.**
   Ranked second not because it's the best *automation*, but because it is
   the best **fail-fast-respecting fallback** and pairs naturally with idea 1:
   ship idea 1 as the automatic unlock, but keep idea 4's explicit
   "SSH deferred" state and `--relock-ssh` recovery command as the answer for
   whatever idea 1 cannot cover (vault unreachable at restore time, multiple
   candidate keys with no saved default, etc.) — replacing today's silent
   hang with a named, actionable, `verify-restore`-visible state is a strict
   improvement on its own even with zero new secret-handling. Doesn't bend
   any retired decision at all — it adds no new unlock path, just better
   observability of "not yet unlocked."

3. **Idea 2 — systemd-credential-sealed passphrase (TPM-bound).**
   Ranked third: it is the *more correct* long-term answer for the headless
   server case specifically (machine-bound rather than
   anyone-with-vault-pass.secret-bound), and is genuinely purpose-built
   prior art for this exact shape of problem — but it is more IaC surface
   (a non-Ansible-native `systemd-creds` provisioning step, a second at-rest
   copy of the secret to keep in sync with the vault, TPM/PCR-policy
   fragility to get right) for a project whose stated principle is YAGNI.
   Worth keeping in the plan as the natural *upgrade path* from idea 1 if the
   simpler vault-file approach ever proves insufficient (e.g. if
   `vault-pass.secret`'s blast radius becomes a concern), rather than as the
   first cut. It fully respects "keys stay passphrase-protected, no bloat to
   the agent" — arguably even more strongly than idea 1, since it removes
   the need for `vault-pass.secret` to be read at restore time at all.

Ideas 3, 5, 6 and 7 are recorded for completeness but not ranked in the top
three: 3 and 7 knowingly reopen the Plan 00139 identity/signing decision for
a fix that doesn't need to touch it; 5 solves only the half of the problem
the owner already said is fine to leave alone (desktop) and reproduces the
core failure on the half that matters (headless); 6 is a physical-hardware
mismatch for the stated "nobody present after a reboot" scenario.
