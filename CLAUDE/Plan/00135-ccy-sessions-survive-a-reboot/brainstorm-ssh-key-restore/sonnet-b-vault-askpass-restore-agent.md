# Brainstorm: a restored ccy session is stuck at its SSH key passphrase

Independent agent, Sonnet, brief B. Ideas below range from "small, boring, ships this
week" to "questions why the container needs a key at container-start time at all."

## Question the premise first

Two assumptions are baked into "the restored session stops at the passphrase prompt":

1. **The key must be unlocked at container-start time.** Not true for most of a
   session's life. `git push` and commit signing are the only operations that need the
   identity; editing, testing, reading, and (per Plan 00137) most of a self-update cycle
   (pull, build, run plays, verify) do not. The prompt only *has* to block the one
   operation that needs it, not the whole session.
2. **A human must type the passphrase.** The passphrase already lives decrypted-at-rest
   in the vault, and this repo already has a proven non-interactive unlock path for it —
   `run.bash`'s `hl_ssh_agent_start()` (`/workspace/run.bash:511-538`) loads
   `~/.ssh/id` into an agent with **`SSH_ASKPASS` + `SSH_ASKPASS_REQUIRE=force`**, feeding
   it `HL_GITHUB_SSH_PASSPHRASE` via a transient 0600 file and a throwaway askpass
   helper script, for exactly the unattended `run.bash --headless` case. That is the
   same shape of problem this plan has, already solved once, on the host side. ccy's own
   `ssh-handling.bash` even already runs a **private, throwaway probe ssh-agent**
   (`_probe_agent_start`, `~/lib/ssh-handling.bash:598-678`) whose entire reason to exist
   is "unlock a passphrase key without a human watching the GitHub connection." It stops
   one step short of automatic — `_probe_agent_add_key` still calls plain `ssh-add "$key"`,
   which reads its passphrase from the controlling terminal. Point `SSH_ASKPASS` at it and
   the mechanism this plan needs already exists in the file the brief points at.

Both observations lead to ideas below that don't require inventing new machinery, only
wiring existing machinery to a new caller.

## Ideas

### 1. Restore-scoped `SSH_ASKPASS`, reusing `run.bash`'s exact pattern

**How:** When `CCY_SESSION_RESTORE=1`, and only then, `_probe_agent_add_key` (or a
restore-only twin) decrypts `github_ssh_passphrase` from the vault (the same
`decrypt_passphrase()` `scripts/gh-account-setup.bash` already has), writes it to a
`mktemp`'d 0600 file, points a throwaway askpass script at it, and calls
`SSH_ASKPASS="$helper" SSH_ASKPASS_REQUIRE=force ssh-add "$key"` instead of prompting.
The temp files are unlinked immediately after, same as `hl_cleanup`.
**Needs:** `vault-pass.secret` readable by the `systemd --user` unit's context (it already
must be, for this to be the same host account that ran `run.bash`); `ansible-vault` on
PATH; the passphrase decrypted only in memory/tmpfs, never logged, never in argv (the
existing helper-script trick already satisfies "never in argv").
**Desktop:** invisible improvement — restore now completes without the interactive prompt
even before someone walks up. **Headless:** this is the fix — restore fully unattended.
**Security cost:** the vault passphrase becomes reachable by anything that can act as the
`ccy-sessions-restore.service` user at boot, same trust boundary `run.bash --headless`
already accepts for the identical secret. It does *not* create a new passphrase-free key —
the same passphrase-protected key is used, just unlocked by a script instead of a person.
**What could go wrong:** vault-pass.secret and the passphrase both being on the same disk
is "decrypt everything with root" — no worse than today, but a good moment to ask whether
the passphrase should get its own systemd credential (see idea 4). A bug that logs
`$out`/`$_add_out` verbatim (as several nearby comments warn against) would leak the
secret into the journal — needs the same "never echo raw ssh-add output" discipline
`ssh-handling.bash` already applies elsewhere.

### 2. `systemd-creds`-wrapped passphrase, decrypted only inside the restore unit

**How:** Instead of the restore unit reading `vault-pass.secret` + running
`ansible-vault` at boot, provision the passphrase once (via Ansible) as a systemd
credential: `systemd-creds encrypt` the plaintext passphrase to a `.cred` file, reference
it in the restore unit as `LoadCredentialEncrypted=github-ssh-passphrase:...`, and have
the restore script read it from `$CREDENTIALS_DIRECTORY` instead of touching the vault at
all at boot time.
**Needs:** a systemd new enough for `LoadCredentialEncrypted=` (systemd 250+; Fedora has
it), and the credential re-encrypted whenever the passphrase rotates (an Ansible task, not
a manual step — keeps this IaC).
**Desktop/headless:** identical either way; systemd handles the decrypt before the script
runs, so there's no behavioural difference from idea 1 from ccy's point of view — this is
a swap of *where the secret sits at rest*, not of the unlock flow.
**Security cost:** lower than idea 1 at rest — the encrypted credential is useless without
the *host's* TPM or machine ID (see idea 3 for the strongest version), and it's no longer
`ansible-vault`'s single shared passphrase gating this one automation path, so a leaked
`vault-pass.secret` alone doesn't also hand over the SSH passphrase.
**What could go wrong:** systemd version skew across machines this plays on; a credential
that silently fails to decrypt (wrong host) must fail loudly, not fall through to a normal
prompt that then hangs anyway.

### 3. TPM2-sealed passphrase, PCR-bound to an unmodified boot

**How:** Go one step further than idea 2 — `systemd-creds encrypt --with-key=tpm2` (or
`host+tpm2`) seals the passphrase so it can only be decrypted on *this* TPM, optionally
gated to specific PCR values (kernel + initrd + secure-boot state unchanged). This is the
same primitive Fedora/systemd already uses for unattended LUKS unlock at boot
(`systemd-cryptenroll --tpm2-device=auto`), just pointed at this secret instead of a disk.
**Needs:** a TPM2 chip (virtually all Fedora desktops/servers built since ~2016 have one);
`systemd-creds` with TPM support; care that a routine kernel update doesn't change the
sealed PCRs and brick the auto-unlock (usual TPM/LUKS caveat — Fedora's `dracut`/`systemd`
already handle PCR re-sealing on `kernel-install` for disk encryption, and the same hook
would need to cover this credential too).
**Desktop/headless:** same as idea 2, just harder to extract the secret if the disk is
imaged and read on different hardware.
**Security cost:** best of the bunch *for data at rest*; does nothing about the process
that receives the decrypted value at runtime, which idea 1's `SSH_ASKPASS` file still is.
**What could go wrong:** the single most-cited TPM footgun industry-wide — PCR drift after
a firmware/kernel update silently reseals nothing and the box that "auto-unlocks" stops
doing so at the worst time (an unattended reboot with nobody there to notice). Needs a
`triage.bash`-style probe that resealing actually ran.

### 4. Lazy identity: don't unlock anything at restore; unlock on first push

**How:** Restore starts the session without ever touching SSH_KEYS/agent. The container
comes up fully usable — editing, testing, research, Plan 00137's build-and-verify phases —
with `GIT_SSH_COMMAND` pointed at something that fails loudly and specifically ("no
identity loaded for this session; run `ccy-unlock` or attach and pick one") only at the
moment something actually calls `git push` or a signing hook. `verify-restore` gains a
new state, `OK-NO-IDENTITY`, distinct from `WAITING-AT-PROMPT`, so it isn't reported as
stuck.
**Needs:** a clean seam between "container is up and workable" and "container can push" —
today's launcher couples them (SSH validation runs before the container starts). Also
needs the eventual unlock path to be *something* — idea 1/2/3, or a human attaching — so
this is a complement to those, not a replacement.
**Desktop:** no visible change — a human is usually there before a push is needed anyway.
**Headless:** shrinks the unattended-blocking window from "the whole session" to "the one
git operation that needs identity," which is a much smaller target for automatic
unlocking to hit, and gives Plan 00137's cycle somewhere useful to do while it waits.
**Security cost:** none extra — it's strictly less machinery running with the key loaded,
for less time.
**What could go wrong:** CLAUDE.md's fail-fast rule is explicit that task B must not be
decoupled from task A if B depends on A. A queued/deferred push must FAIL LOUDLY and stay
visibly pending (e.g. `verify-restore` naming it), never silently skip — this idea is only
compliant if the deferral is a first-class, reported state, not a swallowed failure.

### 5. HTTPS + token push instead of SSH, deferring the *signing* key only

**How:** ccy already resolves `GH_TOKEN` per account (`gh-token-<alias>`,
`resolve_token_owner_login`) for the probe step. Nothing stops `git push` itself running
over HTTPS with the token as the password (user `x-access-token`) instead of SSH — only *signing*
commits needs the passphrase-protected key (Plan 00139 D5). Restore could bring the
container up fully push-capable via the token, with signing deferred (idea 4) until the
key is unlocked by whichever of ideas 1-3 is chosen.
**Needs:** confirming the stored `gh-token-<alias>` files don't themselves expire in a way
that makes this just as stuck (they're normally longer-lived than a passphrase prompt
though). A remote-URL rewrite or a git credential helper inside the container.
**Desktop/headless:** same in both; this only ever changes *transport*, not who's present.
**Security cost:** a token with push scope loose in the container is arguably a bigger
blast radius than an unlocked signing key scoped to `ssh-agent` — tokens are bearer
credentials good until revoked/expiry, a la carte for any repo the account can reach.
**What could go wrong:** signed-commit enforcement (if branch protection requires it) means
pushes still fail without the key eventually — this only defers, doesn't eliminate, the
need for idea 1-4 to finish the job.

### 6. Passphrase broker: unlock once per boot, serve every session

**How:** A small long-lived `systemd --user` service (`ccy-ssh-broker.service`) owns one
agent socket. At boot it either auto-unlocks (idea 1/2/3) or — on a desktop, where a human
*is* expected eventually — waits once, quietly, for a single interactive unlock (typed at
a graphical terminal, or via a phone push per idea 8) the very first time any session
needs it. Every `ccy-sessions restore`d session then points `--ssh-agent` at the broker's
socket instead of each session separately hitting its own prompt.
**Needs:** the broker to hold only the identity keys already scoped for this purpose (not
a general-purpose gnome-keyring-style everything-agent); a socket ccy's
SELinux-label-disable path already knows how to bind-mount
(`ssh-handling.bash:806-813`).
**Desktop:** collapses "N stuck sessions after reboot" into "one prompt, once," which
directly matches the owner's steer that "waits for a graphical login is fine for the
desktop."
**Headless:** only useful if paired with an unattended unlock (1/2/3) or idea 8 — a broker
with nobody to ask is just as stuck, only once instead of N times.
**Security cost:** one long-lived unlocked agent is a bigger single point of compromise
than N short-lived probe agents, though ccy's probe agent is already short-lived and this
would be the opposite design choice — worth weighing against "signing keys must not bloat
the agent" (this is explicitly a *shared* agent, so it must hold only what restore needs,
nothing an interactive session's own picker added later).
**What could go wrong:** conflates "the session's own choice of identity" (today, picked
per launch) into "whatever the broker happens to hold," which could silently push as the
wrong account if two projects use different `github_<alias>` keys.

### 7. Out-of-band remote unlock (push notification / one-time link)

**How:** On restore, if no automatic unlock is configured, the restore service sends an
alert (ntfy.sh, Pushover, a plain email — whichever this project's existing notification
plumbing already uses) with a short-lived, single-use link. The owner's phone opens it and
submits the passphrase over TLS to a loopback-bound listener (socket-activated by systemd,
matching the "ssh-agent socket activation" prior art below), which feeds it straight into
`SSH_ASKPASS`, never touching disk.
**Needs:** a notification channel already provisioned (check whether one exists in this
repo before inventing one — the "Related plans" 00137 self-update cycle may already have
alerting); a tiny TLS listener; a timeout after which the attempt is abandoned and reported
(never silently retried forever).
**Desktop:** redundant with just attaching to the tmux session already there.
**Headless:** turns "must be physically at the machine" into "must have a phone" — a real
improvement for the owner's stated "the desktop can wait for graphical login, the server
can't wait for anyone" split, if "anyone, ever" is too strong and "the owner, within a few
minutes, from anywhere" is acceptable.
**Security cost:** a network-reachable (even loopback-only-but-internet-triggered) secret
submission path is new attack surface that doesn't exist today; needs real scrutiny before
being anything but a fallback.
**What could go wrong:** this is the most complex idea here for the least certain payoff —
better as a documented fallback than a first mechanism.

### 8. Hardware-resident key, touch-optional for automation

**How:** A FIDO2/hardware-token-resident SSH key (`ecdsa-sk`/`ed25519-sk`) with
`no-touch-required` set specifically for a machine-automation credential, so the private
material never leaves the token and no passphrase exists to prompt for at all — the
"passphrase" problem is sidestepped by removing passphrases from the picture entirely for
this one identity, while a *human's* interactive key can keep `verify-required` (touch) for
extra assurance during hands-on sessions.
**Needs:** a hardware key permanently attached to the headless box (a real physical
dependency for a server, which is itself an odd requirement) or a virtual `pkcs11`/TPM
implementation providing the same resident-key semantics without a USB dongle.
**Desktop:** fine, if the owner already uses one.
**Headless:** awkward — a "hardware key that must be present in a rack-mounted or remote
box, but never touched" gets most of its normal security benefit thrown away; effectively
becomes "an unremovable key with no passphrase," i.e. the same category the project
already retired for signing keys, just moved to different hardware.
**Security cost / fit:** this is the idea that most directly reopens the "passphrase-free
keys were deliberately retired" decision — flag it as **knowingly bending** that rule
rather than fitting it, and only worth it if scoped to a narrowly-privileged automation
identity, never the human's own signing key.

### 9. Split identity: a scoped bot/automation key, never the human's signing key

**How:** Formalize what idea 8 gestures at without requiring hardware: provision a
*second*, separate SSH keypair (or a GitHub App installation) dedicated to unattended
restore/self-update pushes, scoped to only the repos/branches automation needs, clearly
attributed as a bot in commit metadata (GitHub Apps get their own bot identity
automatically). This key can be passphrase-free *because* it is not the human's signing
identity Plan 00139 D5 was protecting — it's a narrower, revocable, differently-blamed
credential.
**Needs:** GitHub App registration (or a second deploy key) per repo that needs unattended
pushes; a decision about whether unattended commits should be authored/signed as the bot
or as the human (GitHub Apps sign commits made via their API automatically — no local SSH
signing key needed at all for that path).
**Desktop:** irrelevant — desktop sessions keep using the human's own key exactly as today.
**Headless:** removes the prompt entirely for automation, by removing the human passphrase
key from the automated path altogether rather than automating its unlock.
**Security cost:** a new credential and its own compromise surface, but one that is
*intentionally* narrower than the human's account key — arguably safer than any of ideas
1-3, which all end with the human's actual signing key unlocked and loaded on a box with
nobody watching it.
**Fit:** explicitly reopens "passphrase-free keys were deliberately retired" — but the
retirement was about *the human's signing key* bloating the agent; a distinct bot identity
was never that key, so this can be framed as consistent with the *reason* for the
retirement rather than a violation of its letter. Worth putting to the owner as a genuine
re-litigation, not smuggled in.

### 10. GitHub's own "Create commit" API / GraphQL keyless signing

**How:** For the specific case of Plan 00137's self-update commits (not arbitrary agent
work), skip local git signing entirely: use GitHub's GraphQL `createCommitOnBranch`
mutation, which GitHub signs server-side with its own web-flow GPG key — no local SSH key,
no passphrase, no agent, at all, for that one automated commit path.
**Needs:** rewriting the self-update commit step to call the API instead of `git commit && git push`; loses the ability to commit offline or when GitHub is unreachable (already a
requirement for a local git-based workflow, so this may be a step backward for the
"box works even if GitHub is down" property other parts of this project seem to value).
**Desktop:** not applicable — this is a headless-automation-only idea.
**Headless:** total sidestep of the whole problem for the self-update path specifically —
worth scoping narrowly to that one caller, not the general "any ccy session might need to
push" case, which still needs one of ideas 1-4.
**Security cost:** trades a locally-held secret for trusting GitHub's API entirely for that
commit; also means the commit is provably not signed by the human's key, which may or may
not be acceptable for the self-update trail.

## Prior art

- **`systemd-creds` + TPM2**: systemd's own credential system can encrypt a secret so it
  only decrypts on the specific host's TPM (`systemd-creds encrypt --with-key=tpm2`), the
  same mechanism Fedora uses for unattended LUKS unlock via `systemd-cryptenroll --tpm2-device=auto`, and units consume the result via `LoadCredentialEncrypted=`.
  ([systemd.io/CREDENTIALS](https://systemd.io/CREDENTIALS/),
  [smallstep: the magic of systemd-creds](https://smallstep.com/blog/systemd-creds-hardware-protected-secrets/),
  [LINBIT: securing a passphrase with systemd-creds+TPM2](https://linbit.com/blog/securing-the-linstor-encryption-passphrase-by-using-systemd-creds-and-tpm-2-0/))
  PCR-policy binding means an unmodified boot chain is a precondition for decrypt, but PCR
  drift after kernel/firmware updates is the industry's most commonly reported footgun with
  this approach.
- **`ssh-agent` + `SSH_ASKPASS`**: the standard non-interactive unlock mechanism for a
  passphrase-protected key when no TTY is available — exactly what this project's own
  `run.bash` already uses for headless first-install (`hl_ssh_agent_start`,
  `/workspace/run.bash:511-538`). This is the most directly reusable prior art because it's
  already in this codebase and already proven for this exact secret.
- **GNOME Keyring / `gcr-ssh-agent`**: historically wrapped `ssh-agent`, unlocking keys once
  when the login keyring unlocks (via PAM, `pam_gnome_keyring`) and serving them to every
  subsequent process — the desktop-side version of idea 6's broker. As of gnome-keyring
  1:46 the SSH functionality moved to `gcr-ssh-agent`, socket-activated via
  `gcr-ssh-agent.socket`, needing no manual `SSH_AUTH_SOCK` export.
  ([ArchWiki: GNOME/Keyring](https://wiki.archlinux.org/title/GNOME/Keyring),
  [saveman71: gnome-keyring as shared ssh-agent](https://saveman71.com/2019/ssh-agent-gnome-keyring))
  This pattern only solves the desktop case (a human unlocks the login keyring once at
  graphical login) and does nothing for a headless box with no graphical login at all —
  which matches the owner's own framing of the split.
- **`keychain`**: a well-known third-party wrapper that keeps one `ssh-agent` alive across
  logins/reboots by reattaching to a still-running agent process rather than starting a
  fresh one, deferring the *unlock* prompt to whenever a key is first needed after boot —
  the same "lazy unlock" shape as idea 4, but for interactive shells, not systemd services.
- **HashiCorp Vault SSH secrets engine / auto-unseal**: rather than storing a
  passphrase-protected static key at all, Vault issues short-lived signed SSH certificates
  on demand, and separately supports delegating its own unseal to a transit/KMS backend so
  no human types an unseal key at boot.
  ([Vault SSH secrets engine](https://developer.hashicorp.com/vault/docs/secrets/ssh),
  [Vault auto-unseal](https://developer.hashicorp.com/vault/tutorials/auto-unseal)) This is
  the enterprise-grade version of idea 9/10: don't protect a long-lived secret better, stop
  having a long-lived secret.
- **GitHub-side keyless signing (Sigstore/Gitsign, GraphQL `createCommitOnBranch`, SSH
  certificates, GitHub Apps)**: CI systems increasingly avoid holding any long-lived signing
  key at all. Gitsign uses a CI-provided short-lived OIDC token to get a Fulcio-issued
  certificate valid only for the run; GitHub's GraphQL mutation lets GitHub itself sign the
  commit; GitHub Apps get their own bot identity with no local key.
  ([Chainguard: keyless signing with Gitsign](https://www.chainguard.dev/unchained/keyless-git-commit-signing-with-gitsign-and-github-actions),
  [GitHub Community: SSH certificates for signing](https://github.com/orgs/community/discussions/55204),
  [GitHub Community: SSH deploy keys to push](https://github.com/actions/checkout/discussions/1270))
  These map onto ideas 9 and 10: today's design assumes "the container needs *the* signing
  key"; industry practice for unattended pushers increasingly assumes it needs *a* narrower,
  short-lived, differently-scoped one instead.

## Top three, ranked

1. **Idea 1 — restore-scoped `SSH_ASKPASS`, reusing `run.bash`'s proven pattern.**
   Smallest change, reuses code this exact repo already trusts for this exact secret, and
   changes nothing about *which* key is used or how it's later held — it only automates
   the moment of unlock, only during `CCY_SESSION_RESTORE=1`. **Fits both rules
   cleanly**: no passphrase-free key is introduced (the key stays exactly as
   passphrase-protected as it is today), and the agent gains nothing it didn't already
   hold — this just removes the human from one keystroke. The main new risk (vault
   passphrase reachable at boot by the restore unit) is a risk `run.bash --headless`
   already accepts for the same secret, not a new category of exposure.

2. **Idea 3 — TPM2-sealed passphrase via `systemd-creds`, as the delivery layer under
   idea 1.** Once the passphrase is being read automatically at boot, "readable by
   anything that can read `vault-pass.secret` on this disk" is the weakest link idea 1
   leaves behind. Sealing the passphrase to this host's TPM (optionally to an unmodified
   boot chain) is the same primitive Fedora already trusts for unattended disk unlock,
   and it's a delivery-mechanism change, not a design change — it doesn't touch the
   "passphrase-free keys retired" or "agent must not bloat" rules at all, since the key
   itself and its agent lifecycle are unchanged from idea 1. Ranked second rather than
   merged with idea 1 because it adds real provisioning complexity (TPM presence checks,
   PCR-drift monitoring) that idea 1 alone doesn't need to ship first.

3. **Idea 4 — lazy identity: unlock on first push, not at restore.** This is the one idea
   here that changes the *shape* of the problem rather than just how the passphrase gets
   typed, and it directly serves Plan 00137: most of an unattended self-update cycle
   doesn't need git identity at all, so there's no reason restore should block the whole
   session on it. It **fits the fail-fast rule only if built carefully** — the deferred
   push must be a loud, visible, `verify-restore`-reported pending state, never a silently
   skipped one, which is exactly the "probe-then-fail" pattern CLAUDE.md already
   endorses. It doesn't touch the retired-passphrase-free-key decision at all (it's
   orthogonal to *how* the key eventually gets unlocked — idea 1/2/3 still do that part),
   which is why it pairs with rather than competes against the top two.
