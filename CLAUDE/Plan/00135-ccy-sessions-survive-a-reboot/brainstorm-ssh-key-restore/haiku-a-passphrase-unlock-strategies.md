# Brainstorm: SSH Passphrase-Protected Key Unlock for Unattended ccy Session Restore

Agent: Haiku 4.5\
Problem: Restored ccy sessions block at SSH key passphrase prompt on reboot, breaking unattended restore on headless servers.

---

## Ideas

### 1. Pre-Unlock into Dedicated ssh-agent Socket Before ccy Starts

**How it works:**\
Before launching ccy, a systemd user service runs `ssh-add -k` with the vault passphrase (from vault-pass.secret + vault decrypt) to pre-load the signing key into a dedicated ssh-agent socket. ccy then launches with `--ssh-agent <socket-path>` pointing to this already-unlocked agent, skipping the passphrase prompt entirely.

**What it needs:**

- Vault passphrase reading at restore time (vault-pass.secret already on disk)
- A systemd user service or helper script that runs before ccy-sessions restore
- ssh-agent socket at a known path
- Pass the socket path to ccy via `--ssh-agent`

**Desktop vs headless:**

- Desktop: Works; agent stays unlocked in the background
- Headless: Works perfectly; no graphical login needed

**Security cost:**

- Passphrase must be readable at restore time (already true: vault-pass.secret sits on disk)
- Signing key unlocked into agent memory at restore time (same as current manual unlock)
- Agent socket must be protected by filesystem permissions (typical ssh-agent behaviour)
- If restore runs before login, the agent persists with no user session; daemon visibility of the key material depends on process isolation

**What could go wrong:**

- ssh-agent might fail to start or accept the key; need fallback
- Socket path must be stable across restarts for ccy to find it
- Agent process must outlive the ccy session for the entire duration
- If ssh-add fails, restore fails; no user to retry

---

### 2. SSH_ASKPASS Script Reading Passphrase from Vault

**How it works:**\
Set `SSH_ASKPASS` environment variable to a script that reads the passphrase from the vault (via vault-pass.secret) and echoes it. When ccy's SSH key selection prompts for passphrase, openssh calls this script instead of waiting on a terminal. The script reads the vault at passphrase time, decrypts, and provides the answer.

**What it needs:**

- A custom shell script that decrypts the vault passphrase and outputs it
- `DISPLAY` variable set (or `SSH_ASKPASS_REQUIRE=force` in newer ssh) to force SSH_ASKPASS even in non-interactive mode
- Script must run with access to vault-pass.secret and the vault data

**Desktop vs headless:**

- Desktop: Works; script runs silently in background
- Headless: Requires `DISPLAY` to be unset or `SSH_ASKPASS_REQUIRE=force` to force the script call (openssh default is to skip SSH_ASKPASS if a terminal exists)

**Security cost:**

- Passphrase is read from vault at prompt time (same as pre-unlock approach)
- Passphrase briefly exists in the script's memory/environment before being consumed by ssh
- Script output goes to ssh, not visible on terminal (good for unattended)
- No secrets in argv or logs if the script is careful

**What could go wrong:**

- SSH_ASKPASS behaviour varies across openssh versions (old versions ignore it if a terminal exists)
- If `DISPLAY` is not set and SSH_ASKPASS_REQUIRE is not available, openssh falls back to terminal prompt
- Script failure (vault unavailable, vault-pass.secret missing) leaves the session stuck
- The passphrase script itself must be protected and not world-readable

---

### 3. systemd-creds with TPM or Host Key Encryption

**How it works:**\
Encrypt the SSH passphrase using `systemd-creds encrypt` at provisioning time, binding it to the TPM (if available) or the host's key. At restore time, use `systemd-creds decrypt` to retrieve the passphrase, inject it into the environment, and pass it to ccy or an ssh-add script.

**What it needs:**

- systemd >= 248 (credentials feature)
- TPM 2.0 (for TPM binding) OR host key (fallback, less secure)
- A provisioning step to encrypt the passphrase with `systemd-creds encrypt`
- A restore service that decrypts with `systemd-creds decrypt --user` and passes the result to ccy

**Desktop vs headless:**

- Desktop: Works; no graphical login needed, credentials available to systemd user services
- Headless: Ideal; credentials persist across reboots and unattended restore

**Security cost:**

- TPM-binding: Very strong; passphrase cannot be decrypted off the machine
- Host-key binding: Weaker; passphrase encrypted with host key (still on-disk); still better than plaintext but less isolated than TPM
- Credentials stored in `/run/credentials/` (tmpfs, wiped on reboot) or `/var/lib/systemd/` (persistent)
- systemd service must have `LoadCredential=` to access encrypted creds

**What could go wrong:**

- TPM not available; falls back to host key (weaker security)
- TPM sealed in a way that restore cannot unseal (e.g., PCR mismatch after kernel updates)
- systemd credential support not available on older distros
- Credential decryption fails silently if the service unit is misconfigured
- Multiple machines share the same host key; credential not portable between systems

---

### 4. SSH Certificates Instead of SSH Keys

**How it works:**\
Instead of storing a long-lived passphrase-protected SSH private key, obtain a short-lived SSH certificate from a trusted CA (e.g., HashiCorp Vault, step-ca, or custom CA). Certificates have an expiry, eliminating the need for a passphrase and simplifying unattended use. The CA issues certificates programmatically at restore time or on demand.

**What it needs:**

- A certificate authority (self-hosted step-ca, Vault's SSH secrets engine, or custom)
- A client certificate or access token to request certificates from the CA
- Logic at restore time to request a certificate, obtain the signed cert+key, and pass to ccy
- ccy modifications to accept a certificate instead of a key

**Desktop vs headless:**

- Desktop: Works; certificate valid for session duration only
- Headless: Excellent; no passphrase needed, certificates ephemeral and short-lived

**Security cost:**

- Shifts trust from a long-lived key to a CA; CA must be highly available
- Client credential for CA access must still be protected (is it better than a passphrase-protected key?)
- Certificates expire; if restore cannot reach CA, session fails
- Key material in certificate never protected by passphrase (weaker than current model)
- Introduces a dependency: CA availability is now critical

**What could go wrong:**

- CA unreachable at restore time; session fails completely
- Certificate expiration not well synchronized; cert might be valid for agent but invalid for git operations
- CA compromise exposes all current and future certificates
- ccy changes required; not a drop-in replacement

---

### 5. GitHub Deploy Keys + Separate Signing Key

**How it works:**\
Use GitHub deploy keys (limited scope, often passphrase-free) for git clone/fetch/pull operations. Keep a separate, passphrase-protected signing key for commits (Plan 00139). Deploy keys are not suitable for signing, so this separates concerns: a simpler, unattended-friendly key for pushing, and a protected key for signing.

**What it needs:**

- Register a deploy key on the repository
- ccy must route git push through the deploy key, commits through the signing key
- Two separate ssh identities in the session

**Desktop vs headless:**

- Desktop: Works; deploy key simplifies git ops, signing key needs passphrase
- Headless: Partial win; git ops don't need a passphrase, but signing key still blocks if commits needed

**Security cost:**

- Deploy key scope is limited to one repo but often stored passphrase-free (less secure than passphrase-protected)
- Two SSH identities in the session increase complexity and surface area
- Deploy key not tied to user; if compromised, hard to revoke cleanly
- Does NOT solve the signing-key passphrase problem; still blocks on commit signing

**What could go wrong:**

- Deploy key permissions insufficient for the task (e.g., write required but only read granted)
- Multiple repositories accessed; deploy key not sufficient, need user key anyway
- Plan 00139's signing requirement is not met; signing key still needs passphrase
- Increased ssh-agent bloat: two keys instead of one

---

### 6. Pre-Unlock into Temporary Agent, Store State in Session Registry

**How it works:**\
At restore time, read the passphrase from vault, unlock the signing key into a temporary ssh-agent, and record the agent's socket path and unlock time in the session registry. The restored ccy session reuses the already-unlocked agent. If the agent exits, restore detects it and re-unlocks.

**What it needs:**

- Session registry (already exists) extended to track ssh-agent socket paths and unlock state
- A helper script that detects agent death and re-unlocks on demand
- Passphrase read from vault at restore time

**Desktop vs headless:**

- Desktop: Works; agent stays unlocked
- Headless: Works; agent unlocked by restore script

**Security cost:**

- Passphrase read from vault at restore (same as other unlock approaches)
- Agent socket recorded in registry (a data-bearing file on disk, must be protected)
- If registry is readable by other users, they can find the agent socket
- Agent lifetime extends across multiple ccy sessions; key unlocked longer

**What could go wrong:**

- Registry becomes stale if agent dies and restore doesn't detect it
- Agent socket path not cleaned up on shutdown; next restore finds a dead socket
- Session registry becomes a security-sensitive file; must be protected and audited
- Multiple concurrent restore attempts might race to unlock the agent

---

### 7. gnome-keyring Auto-Unlock via PAM (Desktop Only)

**How it works:**\
Configure gnome-keyring PAM integration so the SSH key is automatically unlocked when the session starts. The login keyring is unlocked with the user's login password (or a blank password for autologin), and SSH keys stored in it are automatically unlocked.

**What it needs:**

- gnome-keyring installed and configured
- PAM module `pam_gnome_keyring.so` in `/etc/pam.d/` (usually already present on GNOME desktops)
- SSH key stored in the login keyring (requires manual import once)
- For unattended restore: a blank keyring password (security cost)

**Desktop vs headless:**

- Desktop: Works if user logs in; keyring unlocked with login password
- Headless: BROKEN; no login means no password to unlock the keyring; requires blank password (key stored unencrypted)

**Security cost:**

- Desktop login password used to unlock keyring (trades one secret for another, OK for desktop)
- Unattended mode requires blank keyring password; SSH key stored unencrypted in keyring
- gnome-keyring requires a running dbus session; headless servers typically don't have one

**What could go wrong:**

- gnome-keyring daemon not running on headless servers
- Blank keyring password exposes keys if the local filesystem is compromised
- Works well for desktop but completely unsuitable for headless
- Keyring locking timeout might expire before ccy needs the key

---

### 8. systemd Socket Activation for ssh-agent

**How it works:**\
Configure systemd to socket-activate an ssh-agent for the user session. The socket is created before the session starts, and ssh-agent is started lazily when the socket is first accessed. ccy is launched with `--ssh-agent <socket-path>`, and the socket is pre-loaded with the signing key via a oneshot service that runs before ccy starts.

**What it needs:**

- systemd user session (available on most modern systems)
- A `.socket` file for ssh-agent socket activation
- A `.service` file for ssh-agent in foreground mode
- A oneshot service to pre-load keys (using passphrase from vault)

**Desktop vs headless:**

- Desktop: Works; systemd user session available
- Headless: Works; systemd user session (with linger) can start before any login

**Security cost:**

- Same as "Pre-Unlock into ssh-agent" but using systemd's socket activation
- Passphrase read from vault at key-load time
- Socket activation slightly delays the first connection but is negligible for ccy startup

**What could go wrong:**

- systemd user session not started if systemd linger is not enabled
- Socket activation adds complexity; debugging failures is harder
- Key load service depends on vault-pass.secret being available
- Multiple sessions might attempt to pre-load keys concurrently; race conditions possible

---

### 9. Expect/PTY Automation of Passphrase Entry

**How it works:**\
Wrap the ccy invocation with `expect` or a pseudo-terminal simulator to catch the SSH passphrase prompt and automatically respond with the passphrase from the vault. When ccy prints the passphrase prompt, expect matches it and sends the passphrase as if typed by a user.

**What it needs:**

- `expect` package installed
- A custom wrapper script that spawns ccy in a pty and matches passphrase prompts
- Passphrase read from vault by the wrapper script

**Desktop vs headless:**

- Desktop: Works but feels hacky; simulates a terminal, unnatural for an unattended service
- Headless: Works; no graphical dependency

**Security cost:**

- Passphrase in the expect script's memory (vulnerable if script is core-dumped)
- Wrapper script must be protected (world-executable is dangerous)
- Simulating a terminal creates a fake terminal environment; tools behave unexpectedly

**What could go wrong:**

- expect is a heavy dependency; not typically installed on headless servers
- Prompt matching is fragile; a change in ccy's wording breaks the script
- SSH passphrase prompt might not be recognized in all environments (locale-dependent)
- Script failures crash silently or with confusing errors
- expect adds process overhead and complexity for a "simple" task

---

### 10. Separate, Pre-Loaded Signing Agent Daemon

**How it works:**\
Run a separate, long-lived daemon process that holds an unlocked ssh-agent with the signing key pre-loaded. At restore time, ccy connects to this daemon's ssh-agent socket instead of its own. The daemon is started once at boot time (or by a system service) and persists across all ccy sessions.

**What it needs:**

- A daemon process (systemd service) that starts an ssh-agent and pre-loads keys at boot
- Passphrase decrypted once at boot time (from vault-pass.secret)
- SSH_AUTH_SOCK environment variable set to the daemon's socket
- Mechanism to start the daemon before ccy (e.g., systemd dependencies)

**Desktop vs headless:**

- Desktop: Works; daemon runs in the background
- Headless: Ideal; daemon started by systemd at boot, no user interaction needed

**Security cost:**

- Key unlocked at boot time and remains unlocked in memory until shutdown (long exposure window)
- Daemon socket must be protected; any process that can access the socket can use the key
- Increased attack surface: daemon is a long-running service
- Key material persists across multiple ccy sessions (higher total exposure)

**What could go wrong:**

- Daemon crash leaves all ccy sessions without access to the key
- Daemon socket not cleaned up after shutdown; stale socket causes confusing failures
- Daemon startup timing: if it starts after ccy tries to connect, ccy fails
- Elevated privileges (root) required if the socket needs to be in a system location
- Key persists in memory after logout; if user logs in later, key still loaded (unwanted exposure)

---

### 11. systemd PassEnvironment with Vault-Decrypted Passphrase

**How it works:**\
Decrypt the passphrase from the vault at boot time, set it as an environment variable in the systemd user environment (via a oneshot service), and pass it to ccy via `PassEnvironment=`. ccy or the SSH layer uses this environment variable to unlock the key (though standard openssh doesn't directly consume such a variable; would need custom logic or SSH_ASKPASS).

**What it needs:**

- A systemd oneshot service to decrypt vault and set environment variables
- PassEnvironment directive in the ccy-sessions service
- Custom logic to consume the passphrase environment variable (or SSH_ASKPASS integration)

**Desktop vs headless:**

- Desktop: Works
- Headless: Works; systemd user session available

**Security cost:**

- Passphrase stored in systemd environment (accessible to all processes in the session via `/proc/<pid>/environ`, readable by the user)
- Environment variables are less secure than a script that deletes them immediately
- Passphrase visible in process listing if not carefully managed

**What could go wrong:**

- Passphrase leaked via `/proc/<pid>/environ` if a process is compromised
- systemd environment variables persist across service restarts; potential for accidental reuse
- Custom ccy logic needed to consume the passphrase (non-standard, harder to maintain)
- Passphrase not automatically cleared after use; potential for leaks

---

### 12. Hardware Security Key (Yubikey, etc.)

**How it works:**\
Store the SSH signing key on a hardware security key (Yubikey, Titan, etc.). The key never leaves the hardware; the hardware signs operations when asked. At restore time, ccy connects to the hardware key directly (via USB or another interface). No passphrase needed on the system; the hardware enforces access control.

**What it needs:**

- A hardware security key with SSH support (Yubikey 5, Google Titan, etc.)
- USB port or network connectivity to the hardware key
- A driver/daemon to interface the hardware key to ccy (e.g., `pcscd` for smart cards, Yubikey agent)
- Per-key PIN on the hardware (not the same as the system passphrase)

**Desktop vs headless:**

- Desktop: Works if USB or network-connected
- Headless: BROKEN; hardware keys typically require USB or physical presence, not available on a remote server

**Security cost:**

- Key never stored on the system; hardware provides isolation (very strong)
- Hardware PIN required for each operation (might cause bottlenecks)
- Hardware key is a separate, purchasable device (cost and logistics)
- Hardware key loss = loss of SSH signing ability (no recovery possible)

**What could go wrong:**

- Hardware key not available on a headless server (no USB, no network bridge)
- Hardware PIN prompts are not automated; ccy would still block waiting for PIN entry
- Hardware key driver crashes; ccy loses access to the key
- Hardware key firmware vulnerabilities; security depends on hardware vendor
- Incompatible with distributed/remote scenarios (Yubikey must be physically connected)

---

### 13. SSH Agent Forwarding from a Parent Session

**How it works:**\
The main user session (on the desktop) has an unlocked ssh-agent with the signing key. When a ccy session is restored (potentially on a different machine or container), it connects to the parent session's ssh-agent via forwarding (over SSH or a local socket). The restored ccy session reuses the parent's already-unlocked key.

**What it needs:**

- A parent session with an unlocked ssh-agent (the desktop session)
- A forwarding mechanism: SSH agent forwarding (if remote) or socket forwarding (if local)
- ccy configured to connect to the forwarded agent socket

**Desktop vs headless:**

- Desktop: Works great; ccy on the desktop reuses the user's unlocked agent
- Headless: BROKEN; no parent session with an unlocked agent, no user to authorize forwarding

**Security cost:**

- Depends on the parent session's security; if the parent is compromised, the forwarded key is exposed
- Agent forwarding creates a socket that any process in the session can access
- Requires the parent session to be running; if it closes, the forwarded agent is gone

**What could go wrong:**

- Parent session closes; ccy's agent access is severed
- Agent forwarding socket path unstable; ccy can't reliably find it
- Remote forwarding (over SSH) requires an SSH connection to the parent (chicken-and-egg problem)
- Multi-container scenarios: the forwarded socket might not exist in the container namespace

---

### 14. Keychain Daemon (e.g., `keychain` utility)

**How it works:**\
Use a daemon utility like `keychain` (or macOS Keychain) to manage SSH key passphrases. The daemon asks for the passphrase once and caches it. ccy (and other processes) connect to the daemon to use the cached passphrase. At restore time, if the daemon is running, it serves the cached passphrase; if not, it asks for it once.

**What it needs:**

- `keychain` or similar utility installed
- Daemon startup at boot or session initialization
- Environment variables set to point to the daemon (e.g., `SSH_AUTH_SOCK`)

**Desktop vs headless:**

- Desktop: Works; daemon caches passphrase across multiple terminal sessions
- Headless: Works if daemon is configured to run at boot (but still expects a one-time passphrase entry for caching)

**Security cost:**

- Passphrase cached in daemon memory (same as other daemon approaches)
- `keychain` typically works best with interactive login; unattended caching requires configuration

**What could go wrong:**

- `keychain` expects interactive input for the initial passphrase; doesn't work fully unattended without modification
- Daemon startup timing; if the daemon starts after ccy, ccy can't connect
- Not a standard tool; maintenance burden and potential bitrot

---

### 15. Session Registry with Cached Unlock State

**How it works:**\
Extend the session registry (already in place) to record whether a session was successfully unlocked and at what time. At restore time, if the previous unlock was recent, reuse the cached agent socket; if the unlock is stale, re-unlock from vault.

**What it needs:**

- Session registry extended to track unlock timestamps and agent socket paths
- Logic to detect stale unlocks and re-unlock
- Passphrase read from vault at unlock time

**Desktop vs headless:**

- Desktop: Works
- Headless: Works

**Security cost:**

- Same as "Pre-Unlock into ssh-agent" but with stale-detection
- Session registry becomes sensitive; must not be world-readable
- Unlock state persists across restarts; if the machine is rebooted, the state is stale anyway

**What could go wrong:**

- Registry state corrupted; ccy uses a stale/dead socket
- Re-unlock logic is expensive; every restore re-unlocks (defeats caching)
- Race conditions if multiple ccy sessions restore concurrently
- Registry cleanup incomplete; stale sockets accumulate

---

## Prior Art & Related Work

### systemd-creds & TPM

- [systemd credentials documentation](https://systemd.io/CREDENTIALS/) — official guide to systemd-creds, credential encryption, and TPM binding
- [LINBIT: Securing Encryption Passphrase with systemd-creds and TPM 2.0](https://linbit.com/blog/securing-the-linstor-encryption-passphrase-by-using-systemd-creds-and-tpm-2-0/)
- [ProjectBlueFin: systemd-creds for SSH keys](https://github.com/projectbluefin/server/issues/138) — extending systemd-creds to SSH keys and network config

### SSH Agent Socket Activation

- [OpenSSH PR #502: ssh-agent socket activation](https://github.com/openssh/openssh-portable/pull/502) — upstream effort to add socket-activation support to ssh-agent
- [How to Start SSH-Agent as systemd Unit](https://www.baeldung.com/linux/ssh-agent-systemd-unit-configure) — practical guide to configuring ssh-agent as a systemd user service
- [systemd Socket Activation Concepts](https://sumguy.com/systemd-socket-activation/) — general explanation of socket activation pattern

### gnome-keyring PAM Integration

- [ArchWiki: gnome-keyring](https://wiki.archlinux.org/title/GNOME/Keyring) — keyring setup and PAM configuration
- [GNOME Wiki: Keyring PAM Integration](https://wiki.gnome.org/Projects/GnomeKeyring/Pam) — official GNOME keyring PAM docs
- [Automatic Unlocking with Login Password](https://wiki.gnome.org/Projects/GnomeKeyring/Pam) — automatic keyring unlock on successful login

### SSH Certificates

- [Smallstep: If you're not using SSH certificates, you're doing SSH wrong](https://smallstep.com/blog/use-ssh-certificates/) — case for SSH certificates over long-lived keys
- [Teleport: SSH Certificate-Based Authentication](https://goteleport.com/blog/how-to-configure-ssh-certificate-based-authentication/) — practical implementation and benefits
- [OneUptime: SSH Certificates Setup (2026)](https://oneuptime.com/blog/post/2026-03-02-set-up-ssh-certificates-instead-of-ssh-keys-ubuntu/view) — modern guide to certificate setup
- [step-ca](https://smallstep.com/docs/step-ca/) — step's Certificate Authority for automated certificate issuance

### GitHub Deploy Keys

- [GitHub Docs: Managing Deploy Keys](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/managing-deploy-keys) — official docs on deploy keys vs user keys
- [CircleCI: Deploy Key vs User Key](https://discuss.circleci.com/t/when-to-use-a-deploy-key-vs-a-user-key/46856) — comparison for CI/unattended contexts

### SSH_ASKPASS

- [Nicholas Dille: SSH_ASKPASS](https://dille.name/blog/2005/11/27/ssh_askpass/) — explanation of SSH_ASKPASS mechanism
- [Bash Script SSH Automation Without Password](https://www.exratione.com/2014/08/bash-script-ssh-automation-without-a-password-prompt/) — SSH_ASKPASS for automation

### General Key Management

- [HashiCorp Vault SSH Secrets Engine](https://www.vaultproject.io/docs/secrets/ssh) — Vault's approach to SSH key and certificate management
- [keychain utility](https://www.funtoo.org/Keychain) — caching SSH passphrase across multiple shells

---

## Top Three Ideas Ranked

### 1. **Pre-Unlock into Dedicated ssh-agent Socket Before ccy Starts** (Recommended)

**Why first:**

- Simple, elegant, and proven: uses standard openssh and ssh-agent behaviour
- Unattended operation on both desktop and headless servers
- Vault passphrase already exists on disk (`vault-pass.secret`); no new secret infrastructure
- Passphrase exposure window is minimal and controlled (at restore time, not throughout ccy lifetime)
- No ccy code changes required; use existing `--ssh-agent` flag
- systemd user services provide clean lifecycle management
- Does NOT bend the "no passphrase-free keys" rule or the "no bloat" rule; key is still protected, agent is isolated

**How it fits the constraints:**

- Adheres to fail-fast: if the unlock fails, restore aborts clearly
- No secrets in argv or logs if vault decryption is careful
- Public-repo safe: no install-specific data in the unlock script
- Infrastructure as Code: implement as a systemd service and playbook-driven vault setup

---

### 2. **systemd-creds with TPM or Host Key Encryption**

**Why second:**

- Strong security if TPM is available; passphrase cannot be decrypted off the machine
- systemd-native: aligns with how modern Linux services think about secrets
- Unattended operation: credentials available to systemd user services without user login
- Credentials stored securely (encrypted at rest, never in plaintext in the service)
- Scales well: credentials system can be extended to other secrets in the future

**How it fits the constraints:**

- Does NOT bend the passphrase-protection rule; credential binding is stronger than plaintext storage
- Fails fast: credential decryption failure stops restore immediately
- Requires systemd >= 248 and (ideally) TPM 2.0; some older servers may lack TPM

---

### 3. **SSH Certificates Instead of SSH Keys**

**Why third:**

- Fundamentally different approach: eliminates the passphrase problem by eliminating the long-lived key
- Unattended operation: certificates are short-lived, no passphrase needed for the certificate itself
- Aligns with security best practices: ephemeral credentials over long-lived secrets
- Excellent for both desktop and headless; certificates obtained programmatically

**How it fits the constraints:**

- KNOWINGLY BENDS the "passphrase-protected keys must stay that way" rule; certificates have no passphrase (but they expire, so the tradeoff is different)
- Introduces a CA dependency; availability critical
- Requires ccy code changes to accept certificates
- Shifts security model from "key protected by passphrase" to "key protected by CA certificate expiry"

**Caveat:** This requires more infrastructure investment (a CA) and ccy modifications, so it's lower priority than the above two. However, it's the most elegant long-term solution if commit signing can eventually move to certificates.

---

## Summary

**Immediate recommendation:** Implement idea #1 (Pre-Unlock into ssh-agent socket) for quick unattended restore on headless servers. It requires minimal changes, uses existing infrastructure (vault-pass.secret, ssh-agent, systemd), and solves the problem today.

**Medium-term:** Add systemd-creds (idea #2) as a hardening layer and alternative if TPM is available; this decouples the passphrase from vault-pass.secret and provides stronger binding.

**Long-term:** Explore SSH certificates (idea #3) as the permanent solution, factoring in Plan 00139 (commit signing) refactoring.

All three are compatible; they can coexist or be selected per environment.
