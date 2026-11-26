# Research: a placeholder-swapping HTTPS proxy instead of a base-URL forwarder

The owner's ask, restated: the ccy container never sees a real Claude token, and a session moves to
another token when one runs out of allowance, without being torn down and restarted.

The plan's current design (a forwarder behind `ANTHROPIC_BASE_URL`) delivers both. The watch-list
baseline run (Plan 00258, `CLAUDE/WatchlistRuns/baseline-20261120-1050/`) found two costs in that
design. The same placeholder idea, carried by an HTTPS proxy rather than a base URL, avoids both.

## What the base URL costs

1. **1M context drops to 200K.** Claude Code budgets every natively-1M model at 200K once
   `ANTHROPIC_BASE_URL` names any host but `api.anthropic.com`. The window is decided at startup
   from the base URL's host, so nothing at the forwarder can undo it. Measured by
   DevDock-AI/claude-unlimited (MIT) on Claude Code 2.1.278, in its `docs/adr/0008`: five native-1M
   models read 200,000 through the gateway, and the two non-1M models were unchanged. With the
   variable unset, the check passes. The only fix they found is an internal, undocumented
   environment variable. Source: `usage-observability.md` (claude-unlimited, takeaway 1).
2. **Some traffic ignores the base URL.** Several Claude Code features call `api.anthropic.com` by
   name: bootstrap, policy limits, the fast-mode check, the WebFetch domain preflight, the MCP
   registry, Remote Control. They would carry the placeholder straight to Anthropic and fail. This
   plan's decision 8 already meets the two it knew of. Source: `gateways.md` (KarpelesLab/teamclaude,
   takeaway 2).

## The alternative: a CONNECT proxy that intercepts one host

Claude Code honours the standard proxy variables and a custom CA. Official docs, "Enterprise
network configuration" (code.claude.com/docs/en/network-config): `HTTPS_PROXY`/`HTTP_PROXY`,
`NO_PROXY`, basic authentication in the proxy URL, and `NODE_EXTRA_CA_CERTS`. SOCKS is not
supported.

- `ANTHROPIC_BASE_URL` stays **unset**, so the 1M window is kept and every host Claude Code
  calls by name goes through the same path.
- The container gets `CLAUDE_CODE_OAUTH_TOKEN=<per-session placeholder>`,
  `HTTPS_PROXY=http://<session>:<proxy secret>@127.0.0.1:<relay port>`, and
  `NODE_EXTRA_CA_CERTS=<this session's CA certificate>`.
- The host proxy, one per session as decision 2(a) already places the forwarder, answers
  `CONNECT`:
  - **`api.anthropic.com:443`** (and any other Anthropic host the spike shows carrying the token):
    TLS is terminated with a leaf certificate from a per-session CA. The `Authorization` header
    is replaced only when it carries this session's placeholder, matched in constant time. The
    request is re-sent over real TLS. SSE is streamed through, and response and rate-limit
    headers are left unchanged.
  - **Every other host**: blind TCP passthrough, with no TLS termination, so the proxy cannot
    read it.
- The real token stays on the host. The container holds a placeholder that is worthless
  anywhere else, a proxy secret that works only on this session's socket, and a CA certificate
  (public, and name-constrained to the intercepted hosts, so it cannot vouch for anything else).
- Transport is as already designed: a unix socket in a 0700 runtime directory, bind-mounted into
  the container, and the in-container byte relay on loopback. A CONNECT proxy needs no more from
  the relay than the forwarder did.

**A switch is still an atomic write of the `current` file.** The next request after it carries
the new token: no restart, no `--resume`, and no lost turn, MCP server or sub-agent.

**It is also Plan 00252's first binding.** The same proxy later swaps a GitHub placeholder (`GH_TOKEN`) for
`api.github.com` and git over HTTPS, which closes the baseline's finding S2 (live tokens in the
container) for both credentials. This answers 00252's open question of whether the two belong
together: yes, with the Claude swap first.

What it costs over the forwarder:

- A per-session CA: created at launch, its key kept on the host, never reused, and deleted
  with the session.
- TLS termination for one host. Go's standard library covers it: `crypto/tls`, plus
  `x509.CreateCertificate` with `PermittedDNSDomains` for the name constraint.
- `NO_PROXY` must not list the Anthropic hosts, and the spike must show nothing bypasses the
  proxy. ccy's network policy can refuse direct egress to Anthropic's addresses, so a bypass
  fails loudly rather than leaking the placeholder.

## What the spike must measure (it replaces Task 0.3's P1-P4)

| #   | Question                                                                           | Pass                                                                                                                                                                                                                                   |
| --- | ---------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Q1  | Does `claude` start and authenticate with only the placeholder, through the proxy? | `claude -p` succeeds, `/status` shows a subscription login                                                                                                                                                                             |
| Q2  | Is 1M kept?                                                                        | the status-line payload's `context_window.context_window_size` reads 1,000,000 for a native-1M model, at no cost (no request)                                                                                                          |
| Q3  | Does anything bypass the proxy?                                                    | proxy log lists every host contacted; a capture with direct Anthropic egress blocked shows no failure                                                                                                                                  |
| Q4  | Which hosts carry the token?                                                       | every request bearing the placeholder, by host and path; each such host is intercepted                                                                                                                                                 |
| Q5  | Hot switch mid-session                                                             | turn on A, `switch B`, the next turn succeeds and the log names B                                                                                                                                                                      |
| Q6  | Does the request body name the account?                                            | `metadata.user_id` carries an `account_uuid`, set at startup. After a switch it names A while B pays. Does the API accept it, and does anything (rate limits, `/usage`) misbehave? teamclaude rewrites it; measure before copying that |
| Q7  | Does the client keep stale state?                                                  | after the switch: `/status`, `/usage`, and the account shown                                                                                                                                                                           |
| Q8  | No secret anywhere in the container                                                | no token value in any `/proc/*/environ`, argv, log or file                                                                                                                                                                             |

## Running out of allowance

The proxy sees every response, so it knows when a token has hit its limit. That shows up as a
rejection carrying the `anthropic-ratelimit-unified-*` headers. It can classify the response as
the gateways study advises: quota exhaustion, a short rate-limit 429, overload (529), or a
refusal. Each needs a different answer, and only quota exhaustion should switch accounts.

Three levels of help, in rising order:

1. **Report**: the proxy records the usage headers per token name (decision 5), and the
   limit is shown on the session (status line, `ccy-sessions`, team seat status from Plan 00251).
2. **Offer**: on a quota rejection, the session's pane or the team room says which token ran out,
   when it resets, and which pool tokens have room, with the one-key switch.
3. **Automatic**: the proxy switches to the next token with room, and retries the rejected request
   once.

When this was written, the plan's Non-Goals and decision 3 excluded automatic rotation on terms
grounds. The owner has since decided (Q65, restated 26 Nov) that a usage-limited session moves
to another permitted sign-in automatically, after a fuzzy cooldown; see PLAN.md Task 2.4. claude-unlimited's ADR 0001 records how one project reasoned about
it. The other cost is the prompt cache: it does not carry across accounts, so the first turn after
any switch re-reads the whole context at full price. Source: `usage-observability.md`, the
usage-analysis takeaway 3.

## Code we may lift (permissive, notices kept)

| Source                                                                 | Licence          | Use                                      |
| ---------------------------------------------------------------------- | ---------------- | ---------------------------------------- |
| jingkaihe/matchlock `pkg/policy/engine.go`                             | MIT              | placeholder detection and swap           |
| kstenerud/yoloai `internal/broker/injector.go`, `internal/credential/` | MIT              | the forwarder's injection and its tests  |
| maximhq/bifrost header deny list (`core/providers/utils/utils.go`)     | Apache-2.0       | headers never forwarded upstream         |
| DevDock-AI/claude-unlimited `router.py`, `observation.py`              | MIT (port to Go) | limit classification and switching rules |

agentgateway (Apache-2.0) is the reference for the intercepting design: SNI passthrough by
default, intercepting only bound hosts, identity on CONNECT, and fail closed. It is a
specification, not a dependency.
