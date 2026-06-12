# Security Audit

> **Redaction note**: real identifiers quoted as evidence are replaced here with
> `<email-a>`-style placeholders, per the same public-repo rule this audit enforces
> (and to avoid re-leaking the SEC-01 data in a new tracked file). The cited
> file:line references locate the actual values in the repo.

## Scope & Method

Defensive security audit of the public `fedora-desktop` Ansible IaC repository (branch `F43`), authorised by the repo owners. Excluded: `.git/`, `node_modules/`, `untracked/`, `.claude/hooks-daemon/`, `roles/vendor/` (skim only).

Method was systematic, not sampled:

1. Enumerated all tracked files (`git ls-files`, `rg --files`) — 326 in-scope files.
2. Swept tracked content for secrets/PII: email regexes, `/home/<user>` paths, private IP ranges, key material (`BEGIN ... PRIVATE`, `sk-ant-oat01-`, `gh[ps]_`, `AKIA…`), and personal account identifiers (`joseph`, `<gh-username-a>`, `<domain-b-name>`, `<org-c>`, `ltscommerce`, hostnames).
3. Confirmed which hits are in **tracked** vs **gitignored** files (`git ls-files`, `git check-ignore`).
4. Read the git-hooks secret scanners (`scripts/git-hooks/pre-commit`, `commit-msg`) in full and compared their pattern coverage against the live PII found.
5. Reviewed CCY container security posture (`files/var/local/claude-yolo/claude-yolo` run invocation, `entrypoint.sh`, `lib/ssh-handling.bash`, `lib/token-management.bash`, `Dockerfile`).
6. Reviewed Ansible secret handling (`no_log`, vault usage), `disable_gpg_check`, `curl|bash` installers, download checksums, sudoers, and SSH host-key verification.
7. Inspected `fedora-install/ks.cfg` credential lifecycle (LUKS/user/WiFi passwords) and `vault.bash`.

### Verified clean

- `environment/localhost/host_vars/localhost.yml` is **gitignored and untracked** (`git check-ignore` confirms). The real secrets it holds (`nordvpn_username`, `qobuz_username`, `user_email`, plus `!vault`-encrypted `lastfm_api_*`, `qobuz_password`, `mok_password`, `nordvpn_password`, `rclone_config`, `github_ssh_passphrase`, `ftp_camera_password`) are local-only and never committed. Vault hygiene for the live config file is correct.
- No private keys, OAuth tokens, GitHub tokens, or AWS keys found in any tracked file.
- `vault-pass.secret` is gitignored.
- The non-breaking-space-named file and empty `localhost`/`loclahost` files are **not tracked** (a directory-listing artefact, not committed content).
- CCY token files are written `chmod 600`, token dirs `chmod 700` (`lib/token-management.bash:256,360,726-729`).
- `ks.cfg` handles passwords carefully: read with `read -s`, hashed via `openssl passwd -6`, persisted to `chmod 600` files, root account `--lock`ed.

## Summary

The most serious issue is **committed personal data in tracked planning documents** (SEC-01): real email addresses, a personal/company username, account-to-organisation mappings, and machine hostnames sit in `CLAUDE/Plan/**` markdown that is part of the public repo. This is a direct, repeated violation of the repo's own "Public Repository — Never Commit secrets/PII" hard rule. The committed git-hook secret scanner (SEC-02) has coverage gaps that let exactly this class of leak through. Lower-severity findings concern CCY container mount breadth (SEC-03), plaintext password echo to stdout (SEC-04), and supply-chain hardening of installers (SEC-05/06).

---

## SEC-01: Real PII and account mappings committed in tracked plan documents

**Severity: high · Area: docs (CLAUDE/Plan)**

Multiple tracked markdown files under `CLAUDE/Plan/` contain real personal/company identifiers in a public repository. Confirmed tracked (`git ls-files`).

Evidence — `CLAUDE/Plan/018-fedora-kickstart-install/codebase-analysis.md`:

```
551 user_login: "joseph"
553 user_email: "<email-a>"
557   <alias-a>: "<email-b>"
558   <alias-b>: "<email-c>"
563   joseph: "joseph-uk"
```

(lines 137-145 of the same file also show `joseph@example.com` placeholders, but 551-563 are real values copied verbatim from a live `localhost.yml`.)

Further real identifiers in tracked docs:

- `CLAUDE/Plan/00035-gh-multi-account-hardening/PLAN.md` — `<gh-username-a>`, `LTSCommerce`, `<org-c>`, `joseph-uk` (real GitHub orgs/usernames and account mappings).
- `CLAUDE/Plan/00046-localhost-yml-leak-guard/PLAN.md:35,234-235` — `joseph`, `joseph-uk`, `LTSCommerce`, `LongTermSupport`.
- `CLAUDE/Plan/00045-project-personas-multi-tool-accounts/PLAN.md:134-135` — `lts: "LTSCommerce"`, `joseph: "joseph-uk"`.
- `CLAUDE/Plan/028-fedora-screen-sharing/PLAN.md:52-53,74` — hostnames `<hostname-a>`, `<hostname-b>`.
- `CLAUDE/Plan/Archive/ccb-browser-automation.md` — `/home/<user>/...` paths, `<token-name-a>` token names.
- `CLAUDE/Plan/Completed/00044-laptop-health-audit/research-02-systemd-boot.md:53` — `home-joseph-mnt-lts-photo.mount`.
- `playbooks/imports/play-basic-configs.yml:12-14` — example hostnames `<hostname-a>` / `<hostname-b>` in a comment.

This is a direct violation of `CLAUDE/SecurityRules.md` ("Never commit … Names, email addresses, usernames … Account mappings … hostnames"). Plan 00046's own background note records that these exact identifiers were previously leaked into a public GitHub issue — confirming the data is genuinely sensitive, not placeholder.

**Impact**: Permanent exposure of an individual's email addresses, employer/organisation associations, GitHub account-to-org mapping, and machine hostnames in public git history. Aids targeted phishing and account enumeration. Already in history, so deletion alone does not remediate.

**Recommendation**: Replace real values with placeholders (`{{ user_login }}`, `example.com`, `<gh-username-a>` as Plan 00046 itself does) across all tracked `CLAUDE/Plan/**` docs. Because the data is in history, treat per `SecurityRules.md` "If accidentally committed": purge with `git filter-repo`/BFG if the exposure warrants it, and rotate nothing security-critical here (no tokens leaked) but accept the identifiers as burned. Add a CI/QA grep gate that fails on the known real identifiers in tracked files.

---

## SEC-02: Committed pre-commit secret scanner has coverage gaps that miss the SEC-01 leak class

**Severity: medium · Area: scripts (git-hooks)**

`scripts/git-hooks/pre-commit` (and `commit-msg`) are the repo's automated leak defence, deployed by `play-git-hooks-security.yml`. Reading them in full reveals gaps that let the SEC-01 data through:

- Email pattern is domain-suffix-restricted: `"@[a-z0-9-]+\.(dev|internal|corp|local)"` (`pre-commit:18`). It catches `<email-a>` but **not** `<email-b>` or `<email-c>` (`.co.uk` is not in the list). Real personal `.com`/`.co.uk`/`gmail` addresses pass.
- No detection of a bare personal username (`joseph`) or org names (`LTSCommerce`, `<gh-username-a>`) — there is no owner-specific identifier allowlist/denylist.
- Token coverage omits current GitHub formats `gho_`/`ghs_`/`github_pat_` beyond `ghp_`/`gho_` and omits the long-lived Anthropic OAuth format `sk-ant-oat01-` (only `sk-ant-` generic is matched at `pre-commit:27`, which does match — but `commit-msg:47` only lists `sk-ant-` too; acceptable, noting for completeness).
- `commit-msg` skips all `Merge branch` messages entirely (`commit-msg:25`), so identifiers in merge commit text are unscanned.
- Both hooks are bypassable with `--no-verify` and operate only on the local clone; they are not a server-side gate.

**Impact**: The primary committed safeguard would not have blocked the SEC-01 commits, and will not block future commits of the same identifiers. False sense of security.

**Recommendation**: Add a project-specific identifier denylist (sourced the same way Plan 00046 derives one from `localhost.yml`, with a public-token allowlist) to the pre-commit hook; broaden the email pattern to any non-allowlisted real domain; add `gho_|ghs_|github_pat_|sk-ant-oat01-`. Keep the scanners but treat them as defence-in-depth, not the only line.

---

## SEC-03: CCY container bind-mounts the entire host `$XDG_RUNTIME_DIR` read-write

**Severity: medium · Area: ccy**

`files/var/local/claude-yolo/claude-yolo:2524-2530` mounts the whole runtime dir into the container under Wayland:

```
GUI_MOUNTS+=(
    "-v" "$XDG_RUNTIME_DIR:$XDG_RUNTIME_DIR"
    ...
)
```

`$XDG_RUNTIME_DIR` (`/run/user/<uid>`) contains far more than the Wayland socket: the user's `pipewire`/`pulse` sockets, `bus` (the session D-Bus socket), `keyring`, gnupg/ssh-agent sockets, the rootless `podman/podman.sock`, systemd user-manager sockets, etc. It is mounted read-write, and the container runs Claude with `--dangerously-skip-permissions` (`claude-yolo:2549`). A compromised or prompt-injected agent in the container can reach the host session bus and the podman socket, which is an escape/lateral-movement path back to the host user session — undermining the rootless isolation that is the stated reason for using Podman for CCY.

By contrast the X11 branch correctly mounts only `/tmp/.X11-unix:ro` (`claude-yolo:2533`).

**Impact**: Broad host-session attack surface exposed to an agent explicitly run without permission prompts. Defeats much of the container's isolation benefit on Wayland sessions.

**Recommendation**: Mount only the specific Wayland socket read-only, e.g. `-v "$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY:$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY:ro"` (plus the pipewire socket if audio is required), rather than the entire runtime directory. Bump `CCY_VERSION` per `ContainerRules.md`.

---

## SEC-04: MOK enrollment password echoed in plaintext to the terminal/logs

**Severity: low · Area: playbooks (hardware-specific)**

`playbooks/imports/optional/hardware-specific/play-nvidia.yml` and `play-displaylink.yml` print the vault-encrypted `mok_password` in cleartext via `ansible.builtin.pause`/`debug`:

- `play-nvidia.yml:232` `Your MOK enrollment password is: {{ mok_password }}` and `:241`.
- `play-displaylink.yml:169,178` — identical pattern.

The `expect` tasks that actually use the secret (`play-nvidia.yml:208-220`, `play-displaylink.yml:148-156`) do **not** set `no_log: true`, so the password also appears in Ansible task output if `-v` is used. A secret deliberately stored in the vault is rendered to stdout (and any captured terminal log / CI transcript / screen-share).

**Impact**: A vault-protected secret is exposed in console output and logs, partially negating the point of encrypting it. Limited blast radius (MOK enrolment password, local Secure Boot), hence low.

**Recommendation**: The user genuinely needs to read this password at the MMA enrolment screen, so the on-screen display is intentional — but add `no_log: true` to the `expect` import tasks so it does not leak under `-v`, and consider directing the user to read it from the vault (`./vault.bash get mok_password`) rather than echoing in `debug`.

---

## SEC-05: Unpinned `curl | bash` installers without checksum or signature verification

**Severity: low · Area: playbooks**

Several playbooks pipe a remotely-fetched installer straight into a shell with no integrity check:

- `playbooks/imports/play-python.yml:70` — `curl -L https://github.com/pyenv/pyenv-installer/raw/master/bin/pyenv-installer | bash` (tracks `master`, no pin, no checksum).
- `playbooks/imports/play-claude-code.yml:45` — `curl -fsSL https://claude.ai/install.sh | bash -s {{ claude_code_version }}` (version defaults to `stable`).
- `playbooks/imports/optional/common/play-rust-dev.yml:44,97` — rustup and cargo-binstall installers piped to `sh`/`bash` (binstall tracks `main`).
- `files/var/local/claude-yolo/Dockerfile:103` — `wget -qO /usr/bin/yq …/releases/latest/download/…` (`latest`, no checksum); `:117` uv installer downloaded then run without checksum.
- `playbooks/imports/play-nvm-install.yml:24` — pinned to `v{{ nvm_version }}` (good, contrast example).

The repo demonstrates the correct pattern elsewhere: `play-photography.yml:64`, `play-markless.yml`, `play-toolbox-install.yml`, `play-nvidia.yml:105`, and `play-darktable-ai-gpu.yml:148` all use `get_url` with `checksum: "sha256:…"`. The installer-pipe tasks deviate from this established standard.

**Impact**: Supply-chain risk — a compromised upstream or MITM serves arbitrary code that executes with the invoking user's (and in the Dockerfile case, build) privileges. Mitigated by TLS and the trust already placed in these vendors, hence low, but it is a rule deviation (`InfrastructureAsCode.md` lists "downloads piped to shell" as prohibited manual actions, and the repo otherwise pins+checksums).

**Recommendation**: Pin pyenv/binstall to a tag instead of branch; where the vendor publishes checksums, download with `get_url` + `checksum` then execute; pin `yq` to a versioned release with a checksum in the Dockerfile. At minimum pin versions so the fetched artefact is reproducible.

---

## SEC-06: `disable_gpg_check: true` on several third-party RPM installs

**Severity: low · Area: playbooks**

Package installs that bypass GPG signature verification:

- `play-nvidia.yml:30` — rpmfusion free/nonfree release RPMs installed with `disable_gpg_check: true`.
- `play-displaylink.yml:128` — DisplayLink GitHub-release RPM, `disable_gpg_check: true`.
- `play-darktable-ai-build.yml:331` — locally-built RPMs (acceptable, self-built).
- `play-photography.yml:73` — RapidRAW; annotated "Community release, no GPG key", and the download is checksum-verified at `:64` (good compensating control).
- `play-ddev.yml:90` — `gpgcheck=0` in the DDEV yum repo definition.

`play-nvidia.yml:83` and `play-vscode.yml:14` correctly keep `gpgcheck`/GPG on, showing the project knows the right pattern.

**Impact**: RPMs are installed without signature verification, trusting only TLS to the source. For DisplayLink and rpmfusion-release this is a real (if low-probability) supply-chain gap; for RapidRAW the sha256 checksum compensates.

**Recommendation**: Where the vendor publishes a GPG key (rpmfusion does — import the rpmfusion key and drop `disable_gpg_check`), verify signatures. For keyless community RPMs (DisplayLink, RapidRAW), require a pinned `get_url` + `sha256` checksum before the `dnf` install (RapidRAW already does this; replicate for DisplayLink). For the DDEV repo, enable `gpgcheck` with the DDEV signing key.

---

## Positive Observations

- **Live secrets file is properly excluded**: `environment/localhost/host_vars/localhost.yml` and `vault-pass.secret` are gitignored and untracked; only `localhost.yml.dist` (pure placeholders) is committed. Variable-level vault encryption is used for all real secrets.
- **`no_log` discipline in the sensitive path**: the GitHub SSH passphrase handling (`play-github-cli-multi.yml:265-320`) writes the passphrase to a `0600` temp file with `no_log: true`, cleans it up (`:485`), and uses a probe-then-fail pattern rather than silent failure.
- **CCY token files** are created `0600` in `0700` dirs (`lib/token-management.bash`), and credentials are passed to the container via env vars rather than mounted files (`claude-yolo:2567-2569`).
- **SSH probe isolation** in `lib/ssh-handling.bash:309-315` correctly uses `-F /dev/null -o IdentitiesOnly=yes -o IdentityAgent=none` to prevent false-positive account mapping — a thoughtful security-relevant hardening.
- **Kickstart credential lifecycle** (`fedora-install/ks.cfg`) reads passwords with `read -s`, hashes the user password (`openssl passwd -6`), writes secrets to `0600`/`0700` paths, locks root (`rootpw --lock`), enables SELinux enforcing and the firewall, and uses `set -euxo pipefail` with `--erroronfail` in `%post`.
- **Fail-fast compliance**: every `failed_when: false`/probe in the playbooks I reviewed carries a `# FAIL-FAST-OK:` annotation with a concrete reason (e.g. `play-github-cli-multi.yml:294`, `play-nvidia.yml:203`), and `failed_when` is overwhelmingly used in its correct assert-and-halt form.
- **Checksum-verified downloads** are the norm for binary/RPM artefacts (`get_url` + `sha256:` in photography, markless, toolbox, nvidia-cudnn, darktable-gpu playbooks), showing the secure pattern is well established — SEC-05/06 are deviations from it, not the baseline.
- **sudoers** entries use `0440` mode and scoped files (`/etc/sudoers.d/99-fedora-desktop-setup`), and the main sudoers block uses an Ansible `blockinfile` marker rather than ad-hoc edits.

---

## Adversarial Verification Appendix

### SEC-01 — CONFIRMED (high confidence)

Confirmed by reading every cited file; all are git-tracked (verified via git ls-files). Evidence: (1) /workspace/CLAUDE/Plan/018-fedora-kickstart-install/codebase-analysis.md:549-563 reproduces actual localhost.yml contents including real emails <email-a>, <email-b>, <email-c> and the full github_accounts mapping (balli: <gh-username-a>, lts: LTSCommerce, joseph: joseph-uk), plus /home/<user> paths at lines 195 and 608 — direct violations of SecurityRules.md (no emails, no account mappings, no hardcoded /home paths). (2) /workspace/CLAUDE/Plan/00035-gh-multi-account-hardening/PLAN.md:21,94,387-388 names <gh-username-a>, joseph-uk, LTSCommerce. (3) /workspace/CLAUDE/Plan/028-fedora-screen-sharing/PLAN.md:52-53,74,309 and /workspace/playbooks/imports/play-basic-configs.yml:11-15 contain real hostnames <hostname-a>/<hostname-b> (the playbook only in comments, but still tracked PII per SecurityRules). (4) /workspace/CLAUDE/Plan/Archive/ccb-browser-automation.md:508,745,774-893 contains /home/<user> paths and token name <token-name-a>. (5) Plan 00046 (localhost-yml-leak-guard) PLAN.md:64 records the 2026-05-26 leak incident and explicitly classifies <gh-username-a>'s alias pair as PRIVATE (it was scrubbed to <alias-a>/<gh-username-a> in 00045/00046) — yet <gh-username-a> remains in plaintext in 4 other tracked files (018, 00035, 034-localhost-config-account, Completed/033-ddev-installation), proving the scrub was incomplete. No compensating control found: scripts/git-hooks contain no patterns for these identifiers, and no whitelist/annotation exists. One correction to the claim: 00046 classifies joseph/joseph-uk and lts/LTSCommerce as 'public' aliases (deliberately left unscrubbed), and 'joseph' is the repo owner's visible git identity — so those specific identifiers (incl. 00045 PLAN.md:134-135) are quasi-public by the project's own taxonomy. The high severity stands on the three real email addresses, /home/<user> paths, hostnames, and especially the privately-classified <gh-username-a> account mapping still present in tracked files of a public repo.


