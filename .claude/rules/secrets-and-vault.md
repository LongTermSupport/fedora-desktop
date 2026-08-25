---
paths:
  - "environment/**"
  - "vault.bash"
  - "**/host_vars/**"
  - "**/group_vars/**"
---

# This is a PUBLIC repository — you are near the secrets

Full rules: [CLAUDE/SecurityRules.md](../../CLAUDE/SecurityRules.md) ·
placeholders: [CLAUDE/ExampleValues.md](../../CLAUDE/ExampleValues.md)

## Never commit

Personal names, emails, usernames, real hostnames or private IPs, tokens, keys,
account IDs, or paths under a real home directory.

## Use the reserved placeholders — nothing else passes the scanner

| Kind     | Use                                                            |
| -------- | -------------------------------------------------------------- |
| IPv4     | `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24` (RFC 5737) |
| IPv6     | `2001:db8::/32` (RFC 3849)                                     |
| Hostname | `example.com` / `.org` / `.net`, `*.test`, `*.invalid`         |
| Email    | `name@example.com` — **any other domain is rejected**          |
| Username | `{{ user_login }}`, `<user>`, `$USER`                          |

The private ranges (`10/8`, `192.168/16`) are **not** placeholders — they name real LAN
hosts and the scanner blocks them.

## Vault: variable-level, not file-level

`environment/localhost/host_vars/localhost.yml` is a **regular YAML file** with individually
encrypted values. Edit it with a normal editor — **never** `ansible-vault edit`.

```bash
./vault.bash set <varname>       # NEW variable — refuses if it already exists
./vault.bash replace <varname>   # EXISTING variable — encrypts in place
```

Reach for `replace` whenever the variable is already present; `set` will refuse and tell
you so.

## The gap the git hooks do not cover

The pre-commit scanner only fires on `git commit`. It does **nothing** for `gh issue create`,
`gh pr create`, gists, or web pastes. Scrub identifiers **before** any external post — and
GitHub keeps prior bodies in its edit history, so a later scrub does not redact the original.

Treat `CLAUDE/Plan/` as public too: it lives in this repo.
