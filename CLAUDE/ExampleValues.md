# Reserved Example Values

This is a **public repository**. The pre-commit secret scanner
(`scripts/git-hooks/pre-commit`) rejects anything that looks like a real email,
private IP, hostname, or credential.

When you need a sample value in documentation, comments, usage strings, or
playbook examples, **use one of the IETF-reserved placeholders below**. They are
guaranteed never to refer to a real host, person, or network, so the scanner
whitelists them explicitly. Anything outside this schema is treated as a
potential leak and the commit is blocked.

## The Schema

| Kind                       | Use exactly these                                                         | Reserved by         |
| -------------------------- | ------------------------------------------------------------------------- | ------------------- |
| IPv4 address               | `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`                       | RFC 5737            |
| IPv6 address               | `2001:db8::/32` (e.g. `2001:db8::1`)                                      | RFC 3849            |
| Domain / hostname          | `example.com`, `example.org`, `example.net`, and any subdomain of them    | RFC 2606            |
| Reserved TLDs              | `*.example`, `*.test`, `*.invalid`, `*.localhost`                         | RFC 2606 / RFC 6761 |
| Email address              | `name@example.com` (and subdomains, e.g. `admin@host.example.com`)        | RFC 2606            |
| Username (in paths/config) | `{{ user_login }}`, `<user>`, `$USER`                                     | project convention  |
| Generic inline placeholder | `<user>`, `<host>`, `<token>`, `{{ var }}` (anything in `<…>` or `{{…}}`) | project convention  |

## Examples

```bash
# ✅ GOOD — reserved, accepted by the scanner
ssh admin@host.example.com
ssh -p 2222 user@192.0.2.10
curl https://api.example.com/v1/status
PING_TARGET=198.51.100.5
ssh-with-password <user>@<host>

# ❌ BAD — rejected (real-looking values). The forbidden parts are shown as
#         <...> here only so this doc itself passes the scanner; in the wild
#         they would be literal values like a 10/8 IP or a real mailbox.
ssh admin@<10/8-or-192.168/16-ip>   # a private LAN IP  → use 192.0.2.x
ssh me@<real-host>                  # a routable host   → use host.example.com
EMAIL=<you>@<real-provider>         # a real mailbox    → use name@example.com
```

## Why these specific values

- **RFC 5737** reserves `192.0.2.0/24` (TEST-NET-1), `198.51.100.0/24`
  (TEST-NET-2), and `203.0.113.0/24` (TEST-NET-3) for documentation. They are
  not routable, so an example can never accidentally name a real host.
- **RFC 3849** reserves `2001:db8::/32` for IPv6 documentation.
- **RFC 2606** reserves `example.com`/`.org`/`.net` and the `.example`,
  `.test`, `.invalid`, `.localhost` TLDs for documentation and testing.

The private ranges (the `10/8` and `192.168/16` blocks) are **not**
placeholders — they name real LAN hosts, so the scanner blocks them. Use the
RFC 5737 ranges above instead.

## If the scanner blocks you

The rejection message names the offending value, the issue type, and the exact
placeholder to swap in. Replace the value with the matching reserved one from
the table above and re-commit. Do **not** use `git commit --no-verify` to get
real-looking values past the gate.
