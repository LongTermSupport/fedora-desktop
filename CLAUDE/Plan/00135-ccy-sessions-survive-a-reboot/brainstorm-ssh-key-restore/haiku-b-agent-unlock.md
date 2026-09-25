# Brainstorm: Restoring Passphrased SSH Keys After Unattended Reboot

## Ideas

### 1. Pre-Unlock Agent Before Restore

**How it works:**
Before the ccy session starts, a systemd --user service pre-starts ssh-agent and uses `ssh-add` with the passphrase from vault to unlock the key into the agent's memory. The agent socket is shared with the subsequent ccy session. By the time ccy asks for a key, the key is already unlocked in the agent—no passphrase prompt.

**What it needs:**

- A systemd user service that starts before `ccy-sessions restore`
- Access to vault-pass.secret and github_ssh_passphrase from vault (already available in `scripts/gh-account-setup.bash`)
- A simple script: `decrypt_passphrase | ssh-add -` piped to the key file

**Headless server:**
Works perfectly. The service runs at boot (systemd linger), unlocks the key, the ccy session picks up SSH_AUTH_SOCK and never hits a prompt.

**Desktop:**
Works identically. On graphical login, the service starts, key is unlocked into the agent, user's ccy session uses it. The agent persists across the user's session.

**Security cost:**
The passphrase is briefly in the process memory of ssh-add during the pipe. The decrypted key then lives in ssh-agent's memory until the next reboot or explicit lock/kill. This is the same trust model as interactive `ssh-add` at login—widespread practice, no new exposure beyond agent compromise.

**What could go wrong:**

- Vault decryption fails at boot (vault-pass.secret unreadable, vault corrupt): service fails and blocks the restore. Needs explicit fail-fast handling.
- Agent socket collision or race if multiple ccy sessions start before agent is ready. Solved by `After=ssh-agent.service` in the restore systemd unit.
- On reboot, the key is unlocked but the agent is transient; systemd restart would require re-unlock (acceptable, infrequent).

**Prior art:**
Fabianlee.org covers systemd --user SSH agent setup with manual key loading. The webfactory/ssh-agent GitHub Action uses the same pattern for CI/CD. GitHub Actions documentation covers persistent agents for unattended CI/CD workflows.

---

### 2. SSH_ASKPASS with Vault-Decrypted Passphrase

**How it works:**
Set SSH_ASKPASS and SSH_ASKPASS_REQUIRE environment variables in the ccy session launcher. SSH_ASKPASS points to a small script that decrypts the vault passphrase and writes it to stdout. When ccy's launcher prompts ssh-add for the passphrase, SSH falls back to the ASKPASS script instead of the terminal.

**What it needs:**

- A `ssh-askpass.bash` script that: decrypt vault, output passphrase to stdout, consume no stdin
- Export SSH_ASKPASS and SSH_ASKPASS_REQUIRE="force" in the ccy launcher
- DISPLAY variable set (even to a dummy value) so SSH recognizes the non-interactive context

**Headless server:**
Works. The ASKPASS script runs at boot with no TTY, decrypts vault, provides passphrase silently. SSH never prompts the console.

**Desktop:**
Identical behavior on desktop (no GUI prompt shown; script runs silently). User workflow unchanged.

**Security cost:**
The passphrase is in the ASKPASS script's environment and stdout briefly. The script must handle stderr carefully (diagnostics to stderr per CLAUDE.md rules). The script itself must be secured (not world-readable). If the script is killed mid-passphrase, the passphrase is briefly in process memory (same as ssh-add).

**What could go wrong:**

- Vault decryption fails silently in ASKPASS: ssh-add would hang waiting for output. Needs explicit error path with guaranteed stderr message and exit code.
- DISPLAY not set: SSH may ignore ASKPASS on some systems. Needs explicit export in launcher.
- Multiple ccy sessions all spawning ASKPASS simultaneously: vault-pass.secret must be readable in bulk (it is, already encrypted). No blocking risk.
- Script stderr leaks into ccy logs if not redirected carefully.

**Prior art:**
SSH_ASKPASS and SSH_ASKPASS_REQUIRE documented in OpenSSH manual. Used in Moderne CLI and other tools for non-interactive SSH key operations. The ADHDecode article (2026) covers ASKPASS exec errors and GUI passphrase prompts.

---

### 3. systemd-creds LoadCredential with TPM Encryption

**How it works:**
Encrypt the vault passphrase at boot using systemd-creds and TPM2 (or host key as fallback). Store it in a systemd credential. When ccy-sessions restore runs, load the credential via LoadCredential=, decrypt it with systemd-creds read, and pass it to ssh-add. The TPM seals the secret to this specific boot (PCR-bound).

**What it needs:**

- systemd >= 252 (supports crypt, LoadCredentialEncrypted)
- TPM2 hardware (or fallback to host key encryption)
- An Ansible play to encrypt vault passphrase at boot: `systemd-creds encrypt --tpm2-device=/dev/tpm0 <passphrase> github-ssh-passphrase.cred`
- The ccy-sessions restore script reads the credential and pipes it to ssh-add

**Headless server:**
Excellent. TPM seals the secret to PCR state (firmware, bootloader, kernel hashes). If someone reboots the machine, the secret unseals only if PCR matches. If the disk is stolen, the secret cannot be decrypted on another machine. No passphrase file on disk in plaintext.

**Desktop:**
The same TPM sealing works on desktops with TPM2. On machines without TPM, systemd falls back to host-key encryption (symmetric encryption with a host-derived key). Still better than plaintext vault on disk, but less isolated than TPM.

**Security cost:**
High security gain: secret is encrypted with TPM, not stored in plaintext vault-pass.secret. The credential is ephemeral (lives in /run, cleared on shutdown). However, during the brief window when systemd decrypts it, the passphrase is in kernel memory. This is acceptable: systemd runs as root, the credential is not logged, and it is immediately consumed.

**What could go wrong:**

- TPM not available or not initialized: systemd-creds falls back to host key, which is less isolated but still encrypted.
- systemd version mismatch: creds format may be incompatible. Requires systemd >= 252 on both deploy and runtime.
- TPM locked/unsealed state wrong: credential decryption fails, ccy-sessions restore fails, sessions don't resume. Needs explicit fail-fast diagnostics.
- Disk stolen but TPM mitigated: attacker cannot decrypt credential without the physical TPM. Strong threat model.

**Prior art:**
systemd.io/CREDENTIALS/ documents the credential system. The paper "Protecting SSH authentication with TPM 2.0" (SSTIC 2021, iooss) covers practical TPM sealing of SSH secrets. Fedora's cryptfs-tpm2 and tpmseal projects implement seal/unseal workflows. systemd-creds with LoadCredentialEncrypted is production-ready in recent systemd versions.

---

### 4. Pre-Session Unlock Hook with Cached Credential

**How it works:**
Add a hook to the ccy launcher that runs before the session starts. The hook decrypts the vault passphrase once, pipes it to ssh-add to unlock the key, then immediately discards the plaintext passphrase from the process. The key now lives in ssh-agent memory, unlocked. When ccy's internal key selection runs, the key is already in the agent—no passphrase prompt.

**What it needs:**

- Modify the ccy launcher (`files/var/local/claude-yolo/claude-yolo`) to add a pre-session unlock step
- Call `ssh-add < <(decrypt_passphrase && echo)` to unlock the key before the session's own key selection
- Redirect stdout/stderr carefully (diagnostics to stderr, not into the session's stdin)

**Headless server:**
Works. The hook runs at the start of the ccy session, decrypts passphrase, unlocks key, and terminates. By the time ccy's interactive key picker runs, the key is already available in the agent.

**Desktop:**
Identical. No behavioral change for the user.

**Security cost:**
Same as Pre-Unlock Agent (idea 1), but localized to the ccy process rather than a separate systemd service. The passphrase is in process memory briefly during ssh-add invocation.

**What could go wrong:**

- Vault decryption fails inside ccy: the session fails to start (fail-fast is correct). Clear error message required.
- ssh-add hangs waiting for passphrase from stdin: requires explicit EOF after passphrase. The `< <(...)` process substitution ensures EOF is sent after the passphrase.
- Racing with systemd agent startup: if agent is not yet ready, the hook might create a temporary agent instead of using the persistent one. Solved by ensuring ssh-agent service is a dependency.

**Prior art:**
ssh-add with stdin passphrase is standard Unix practice. Documented in openssh manual and widely used in CI/CD. GitHub Actions webfactory/ssh-agent uses a similar pattern.

---

### 5. SSH Certificates Instead of Passphrased Keys

**How it works:**
Replace the workflow of signing commits with passphrased SSH keys with SSH certificates. Generate a key pair without a passphrase, then sign it with a certificate authority (CA). The CA-signed certificate grants a time-limited identity. The agent holds the unsigned key (no passphrase) for signing. When ccy needs to sign a commit, it uses the certificate instead of prompting for a passphrase.

**What it needs:**

- A CA private key (on the desktop/secure machine, not in the container)
- Script to generate certificates with ~1 year TTL before expiration
- Ansible play to install certificates into the container
- Update `files/var/local/claude-yolo/lib/ssh-handling.bash` to prefer certificates over passphrased keys
- Renew certificates annually (or on-demand if one expires)

**Headless server:**
Works. Keys have no passphrase, so ssh-add imports them silently. Certificates grant time-limited identity without prompting. No passphrase questions at boot.

**Desktop:**
Works identically. The desktop's CA issuing authority could be local (e.g., a password-protected hardware key), or a service like Teleport that issues short-lived certs on demand.

**Security cost:**
Eliminates the passphrase entirely (violates the rule "passphrase-free keys were deliberately retired"). However, the certificate TTL provides a different security boundary: compromised key is only valid for 1 year, not forever. Trade-off: convenience vs. key longevity. The brief mentions "signing keys don't bloat the agent"—certificates and unsigned keys are lighter than passphrased key + agent memory overhead.

**What could go wrong:**

- Certificate expires during an unattended session. Ccy session continues but the certificate is invalid for new operations. Requires pre-expiry renewal or explicit alerting.
- CA key compromise: attacker can issue fraudulent certificates. Mitigated by keeping CA key off the headless server entirely (stays on desktop or hardware token).
- GitHub/GitLab validates SSH certificate format. Certs are standard OpenSSH (RFC 8090), widely supported, but requires GitHub Actions/CI to trust the cert CA.

**Prior art:**
OpenSSH certificates documented in RFC 8090 and openssh manual. Teleport uses short-lived SSH certs for unattended access. HashiCorp Vault issues certificates with time-bound auth. Cloudflare uses certificates for zero-trust infrastructure. Google Cloud IAM supports SSH certificates.

---

### 6. GitHub App or Deploy Key Alternative

**How it works:**
Instead of using the user's personal SSH key (passphrased), switch to a GitHub App or repository deploy key. The deploy key is either passphrase-free or tied to a short-lived token. For signing commits, use GitHub App installation tokens (valid for 1 hour) generated on-demand.

**What it needs:**

- Create a GitHub App in the organization (or use an existing one)
- Generate an installation access token at boot (via GitHub API, valid 1 hour)
- Use the token for git push (HTTPS instead of SSH) or SSH-with-token (limited on GitHub)
- For commit signing: generate a signing token from the App instead of using SSH keys
- Update ccy's git config to use tokens instead of SSH

**Headless server:**
Works. At boot, a systemd service calls GitHub API to get an installation token (requires a short-lived GitHub App private key or federated identity). The token is injected into git config. Ccy session uses the token, no passphrase prompted.

**Desktop:**
Same token-based workflow, but the desktop user could also refresh the token interactively via a simple command.

**Security cost:**
Eliminates SSH keys entirely (major shift from the current architecture). Tokens are short-lived (1 hour) instead of long-lived keys. Tokens are HTTP-based (easier to audit and revoke). However, tokens introduce a new dependency on GitHub API availability at boot and during ccy operation.

**What could go wrong:**

- GitHub API is down at reboot: token cannot be generated, ccy session starts without git push capability. Needs fallback to a cached token from the previous boot or explicit failure.
- Token exposure: if the token is logged or exposed, its 1-hour TTL limits damage. Still requires careful handling (never in logs per CLAUDE.md).
- Non-GitHub use case: if the user also uses GitLab, Gitea, or self-hosted git, deploy keys don't work there. Would require per-host token management.

**Prior art:**
GitHub deploy keys documented in GitHub Docs. GitHub Actions uses installation tokens for CI/CD. Teleport uses GitHub Actions for identity federation. AWS Keyspaces and Datadog use similar token-based auth for unattended services.

---

### 7. systemd-vaultd Integration (Vault Agent)

**How it works:**
Run a HashiCorp Vault agent as a systemd --user service. The agent authenticates to Vault at boot (using a stored auth token or AppRole credentials), retrieves the SSH passphrase, and writes it to a socket. The ccy launcher reads the passphrase from the socket and uses it to unlock ssh-add.

**What it needs:**

- Vault server running (locally or remote)
- systemd-vaultd or similar agent running as a systemd service
- Vault auth method configured for the machine (e.g., AppRole, JWT)
- The ccy launcher configured to read from the vault socket

**Headless server:**
Works if Vault is available at boot. Vault agent authenticates and retrieves the secret. Ccy session reads the passphrase and proceeds. If Vault is unavailable, the session fails (fail-fast, explicit error).

**Desktop:**
Same workflow. Vault is accessible from the desktop's network.

**Security cost:**
Introduces a Vault server dependency. The secret is centralized and auditable (Vault logs all access). However, Vault is a complex service—requires running, maintaining, and securing a separate system. The passphrase is stored in Vault instead of locally in vault-pass.secret (shift of responsibility to Vault).

**What could go wrong:**

- Vault unavailable at boot: ccy session cannot retrieve passphrase. Explicit error message required; no silent fallback.
- Vault auth fails (AppRole credentials expired, JWT validation fails): same result as Vault unavailable.
- Vault server compromise: attacker has access to all passphrases. Vault is a single point of failure (but also a single point of audit).

**Prior art:**
systemd-vaultd GitHub project. HashiCorp Vault documentation on agent and auto-auth. Nomad and Consul use Vault agents for secret retrieval at runtime.

---

## Prior Art Summary

- **systemd-creds & TPM:** systemd.io/CREDENTIALS/, Fedora's cryptfs-tpm2, tpmseal, SSTIC 2021 "Protecting SSH authentication with TPM 2.0"
- **SSH Agent & systemd:** Fabianlee.org, Baeldung.com, Arch Wiki, webfactory/ssh-agent GitHub Action
- **SSH_ASKPASS:** OpenSSH manual, ADHDecode (2026), Moderne Docs
- **SSH Certificates:** RFC 8090, Teleport, HashiCorp Vault, Cloudflare, Google Cloud IAM
- **GitHub Deploy Keys & Tokens:** GitHub Docs, GitHub Actions documentation
- **GNOME Keyring & PAM:** ArchWiki GNOME/Keyring, GNOME Wiki, Ubuntu Community Hub
- **Vault Agent:** HashiCorp Vault docs, systemd-vaultd project

---

## Top Three Ideas, Ranked

### 1. **Pre-Unlock Agent Before Restore** (Idea 1)

**Why:**

- Minimal code changes (just a systemd service to add)
- Uses existing vault-pass.secret and decrypt_passphrase (already in use)
- Works identically on headless and desktop
- Passphrase is briefly in memory (acceptable, same as manual `ssh-add`)
- Single dependency: ssh-agent (already standard on every Linux box)
- Clear fail-fast semantics: if vault decryption fails, the service fails before ccy session starts
- Agent socket shared seamlessly with ccy session via SSH_AUTH_SOCK
- Aligns with existing IaC discipline: solve via Ansible playbook, not manual intervention

**Constraint compliance:**

- Fits the "signing keys don't bloat the agent" rule: the key is unlocked into the agent once, reused for all operations
- Does NOT require abandoning passphrased keys (respects the deliberate retirement decision)

### 2. **SSH_ASKPASS with Vault-Decrypted Passphrase** (Idea 2)

**Why:**

- No systemd service required; integrates entirely into the ccy launcher
- ASKPASS script is portable (simple bash, no external dependencies beyond vault-pass.secret)
- Explicit control: the ccy launcher decides when to provide the passphrase
- Works on any system with SSH (no systemd required for core logic, though systemd env vars help)
- Clear error handling: ASKPASS script can fail explicitly with diagnostic stderr

**Constraint compliance:**

- Fits both constraints equally well
- Simple, testable, minimal surface area

**Tradeoff:**

- DISPLAY variable must be set (slightly awkward on headless, but simple to export)
- ASKPASS script must be carefully guarded (world-readable script = readable passphrase, bad)

### 3. **systemd-creds LoadCredential with TPM Encryption** (Idea 3)

**Why:**

- Highest security posture: secret is encrypted by TPM, tied to this specific machine, not stored plaintext on disk
- Future-proof: aligns with modern systemd security (creds, LoadCredential are new systemd features)
- Scalable: if fedora-desktop grows to manage multiple machines, TPM isolation per machine is cleaner than a shared vault-pass.secret
- On desktop with TPM, the secret is physical-machine-bound; on headless without TPM, falls back to host-key encryption (still better than plaintext)

**Constraint compliance:**

- Fits both constraints (maintains passphrased key model, doesn't bloat agent)
- More operational overhead: requires TPM hardware or systemd host-key setup

**Tradeoff:**

- Requires systemd >= 252 (compatibility requirement; Fedora 44 has 255, so fine)
- TPM failure mode requires diagnostic clarity: if unsealing fails, explicit error message
- More complex to test (requires TPM or systemd host-key emulation)

---

## Implementation Notes

**For the coordinator:** The three ideas are viable in order of implementation effort and urgency:

1. **Idea 1 is the fastest path to working restore.** It's straightforward to add a systemd service, low risk, and immediately solves the reboot problem. Ansible playbook + 30-line systemd service file. Ship this first.

2. **Idea 2 is the fallback/auxiliary option.** If Idea 1 has unforeseen issues, ASKPASS provides a different control path (all in the ccy launcher, no separate service). Use as a backup or if systemd --user services prove problematic.

3. **Idea 3 is the long-term hardening.** If the project grows to multiple machines or security requirements tighten, TPM-based encryption of the secret is the cleanest long-term design. Implement after Idea 1 is stable and proven.

**What NOT to do:**

- **Idea 5 (SSH certificates):** Violates the "passphrase-free keys were deliberately retired" decision. Do not pursue unless that decision is reversed by the owner.
- **Idea 6 (GitHub App/Deploy Key):** Major architectural shift. Reserve for a future plan if the owner requests a move away from personal SSH keys.
- **Idea 7 (Vault agent):** Adds unnecessary complexity (Vault is a large service). Use Idea 1's local vault-pass.secret instead.
