# Plan 00068 — the CI credential: where it lives, and how it is updated

The assumed process was *"update the vaulted string, then run ansible"*. Checked against source, that
process **does not exist for this credential** — not for CI, and not for the desktop either. This
document records what is actually there, and specifies what to build.

## What exists today — checked, not inferred

| Fact                                                              | Evidence                                                                       |
| ----------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| No vaulted Claude credential of any kind                          | `host_vars/localhost.yml` has 18 variables, 8 vault-encrypted; none for Claude |
| `play-claude-yolo.yml` deploys **no** token — only directories    | `:311-327` create `~/.claude-tokens/ccy/{tokens,projects}`                     |
| Tokens are **files**, created by an interactive OAuth device flow | `ccy --create-token`; `token-management.bash:199`                              |
| The filename encodes an **expiry date**                           | `token_file="$token_dir/${token_name}.${expiry_date}.token"` (`:199`)          |
| ccy passes the token **by name**, never by value                  | `claude-yolo:2777` (`-e CLAUDE_CODE_OAUTH_TOKEN`), `token-management.bash:107` |
| Nothing anywhere pushes a credential to GitHub Actions            | no `gh secret` occurrence in `playbooks/`, `scripts/`, `docs/`                 |

The absence of a vaulted Claude credential is a **checked** absence: the file exists, is 139 lines,
holds 8 `ANSIBLE_VAULT` values, and its variable list contains no claude/anthropic/oauth entry.

## The constraint that determines the whole design

**The real vault passphrase must never be placed on the runner.** A CI job that could decrypt the
estate vault would hold the keys to everything. Therefore:

> The runner **cannot** obtain the credential by decrypting vault. It must arrive as a **GitHub
> Actions secret**, pushed there from a trusted host.

This rules out the assumed shape — "runner runs ansible, ansible decrypts the vaulted string" — on
security grounds rather than convenience. Ansible's role is to push the secret *to GitHub* from a
host that legitimately holds the passphrase, never to hand the passphrase to CI.

## The specified process

### 1. A dedicated CI token — never the operator's daily-driver

Interactive, on the host: `ccy --create-token`, choosing a CI-specific name.

A GitHub Actions secret is readable by every workflow run in its scope. The blast radius of a leak
must therefore be a **machine identity that can be revoked without disrupting a human's work**, not
the token the operator uses in daily sessions. This is the same reasoning that gives the estate its
other machine-account credentials.

### 2. Vault it — by path, never by value

The vaulted ciphertext is IaC and belongs in the tracked tree; the plaintext never does.

```
ansible-vault encrypt_string --vault-id localhost@./vault-pass.secret \
  --name 'vault_ci_claude_code_oauth_token' \
  < ~/.claude-tokens/ccy/tokens/<ci-token-name>.<expiry>.token
```

Read **from the file**, so the value is never typed, echoed, or placed in shell history or argv.
Paste the resulting `!vault |` block into `host_vars/localhost.yml`.

### 3. Push to GitHub — the missing IaC

A play that writes the decrypted value to a temp file with `no_log: true` and mode `0600`, runs
`gh secret set` reading from that file, and removes it in an `always:` block:

```
gh secret set CLAUDE_CODE_OAUTH_TOKEN --repo <owner>/<repo> < <tmpfile>
```

`gh secret set` reads the value from **stdin**, so it never appears in argv or the process table —
the same by-path discipline ccy already uses. The value is never printed, never registered as a
changed-value diff, and never logged.

**Vault is the source of truth; the GitHub secret is a derived deployment target.** One direction
only, so there is no two-store sync problem: re-running the play re-derives GitHub from vault.

### 4. The workflow maps it

```yaml
env:
  CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}
```

### 5. The preflight asserts it

Already specified as **J5** in [ci-required-config.md](ci-required-config.md) — gated on the declared
`--claude` mode, never on whether the variable happens to be set. Missing ⇒ `EX_CONFIG` with the
`[JOB]` layer tag and the exact `env:` snippet above.

## Expiry — the failure mode this design must not ship with

Token filenames carry an expiry date (`token-management.bash:199`), and there is a
`feature/ccy-token-expiry-colours` branch, so expiry is a live operational concern on the desktop.

In CI it is worse: **a GitHub secret is an opaque string with no expiry metadata.** When a CI token
expires, every workflow fails with an authentication error raised deep inside `claude`, at job time,
with nothing naming expiry as the cause. That is precisely the "diagnose quickly" failure the
preflight exists to prevent, and the preflight cannot catch it — the runner cannot see the filename
the expiry was encoded in.

Two places it *can* be caught, and both belong in the design:

| Where                 | Check                                                                                                                         |
| --------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| **The push play**     | Refuse to push a token already expired, or expiring within a threshold. The filename is available there, so the check is free |
| **A scheduled check** | Warn before expiry, on the host that holds the token files                                                                    |

Recording the expiry alongside the secret (as a second, non-sensitive GitHub *variable*) would let
the preflight assert it at job time. Proposed, not decided — it adds a value that can drift from the
secret it describes.

## Open — needs the owner

1. **Create the dedicated CI token.** Interactive OAuth device flow; a human action. Blocks
   everything else here.
2. **Confirm the target scope** — repo secret on `LongTermSupport/fedora-desktop`, or an org-level
   secret shared by several repos. Org-level widens the blast radius; repo-level multiplies the
   rotation work.
3. **Decide the expiry-visibility question** above.

## What must never happen

- The vault passphrase reaching the runner in any form.
- The token value passed as an argument (`-e VAR=value`, `gh secret set … "$value"`) rather than by
  name or stdin — argv is visible in the process table, and CI logs capture process listings.
- The operator's personal token used as the CI secret.
- A value, prefix, suffix or length printed by any diagnostic. Report `set` / `unset` /
  `set but empty` only.
