# Research: how devvm puts a proxy in front of the agent

Source: https://github.com/devvmhq/devvm (MIT, Go), read in full at head
`28d7eca6c8e5f5ef18e5b24a587755def2a0a254`, watch-list run `scan-20261111T1210Z`. devvm is a
Lima VM per project; the mechanisms below are VM-specific in their plumbing and portable in their
ideas. All text here describes devvm; nothing was run. Commit SHAs are the last commit that touched
each file at that head.

## 1. How traffic is forced through the proxy

One proxy per VM, a hidden command `devvm _proxy` on the host, listening on a loopback port
(docs/content/docs/internals/network.md, "The proxy", 647ad4bf). Four layers, strongest first:

1. **Confinement outside the guest.** On every tier but `open` the VM is started so that its only
   network path is the proxy (Lima confinement, `backend.NetProxy`; internals/lima.md). This is
   the boundary: root inside the VM can change nothing about it. A backend that cannot confine can
   only be `open` (devvm refuses other tiers).
2. **Proxy variables.** The guest gets `HTTPS_PROXY` and friends naming the proxy, with a
   per-VM random proxy credential in Proxy-Authorization (the loopback port is reachable by other
   host programs, so the port alone does not identify the VM). Programs that ignore the
   variables (apt, sudo, ssh, the Docker and Nix daemons) are pointed at it by systemd drop-ins and
   an ssh ProxyCommand (network.md, "The VM's setup", 665f27dc for the guest side).
3. **Transparent mode** (assets/guest/transparent.sh, 665f27dc; internal/proxy/transparent.go).
   For tools that skip the variables: dnsmasq in the guest answers every A query with a stand-in
   address on loopback, nftables redirects that address's ports to socat listeners, socat opens
   `CONNECT transparent.<reserved-name>:<port>` to the proxy, and the proxy reads the host from
   the TLS SNI or the HTTP Host header. Everything else on the stand-in address is reset.
4. **A self-check at each launch** (internal/netpolicy/selfcheck.go) that proves the VM cannot
   reach out around the proxy, before the shell opens.

TLS: the proxy does NOT interpret most traffic. A CONNECT to an allowed host is tunnelled
untouched (it only checks that the ClientHello SNI equals the CONNECT host). It decrypts a host
only when that host has a bound credential or a rule narrowed by method or path. For those it
terminates TLS with a certificate from a per-VM CA (ECDSA P-256, 30-day life, a "next" CA
pre-installed a week early so rollover needs no relaunch; internal/trust, network.md "The VM's
CA"). The CA key never leaves the host; the guest trust store gets only the certificates.

## 2. The policy model and where it is configured

Plain-text rule lines, layered, one parser (`proxy.ParsePolicy`, internal/proxy/policy.go,
647ad4bf): `host`, `*.suffix`, `host:port`, CIDR, `!deny`, and the narrowed form
`api.example.com GET,HEAD /repos/org/repo/**`. A deny beats any allow. Files, in order: built-in
per-layer and per-tool policies (compiled into the binary), the user's `~/.config/devvm/policy`,
and a per-VM `policy`. Tiers: `open`, `internet` (allow all, user denies apply), `allowlist`
(default), `none` (only the agents' own hosts). Requests inside a decrypted tunnel are judged one
by one (HTTP/1.1 keep-alive and HTTP/2); the matcher is strict about path forms (no encoded
slashes, no `..`, no `;`), method-override headers and `_method` query parameters, so a rule cannot
be bypassed by a form the origin reads differently. A broken deny line fails closed: every
request is refused until it is fixed. Rule edits apply to a running proxy at its next read.
CLI: `devvm network allow|deny|rm|lock|log`.

**GitHub calls limited to named repos.** devvm does this with the same rules, not a special
engine: one GitHub token is bound to the VM (section 3), and the VM's policy denies or allows
`api.github.com` and `github.com` by method and path, e.g. `api.github.com GET,HEAD /repos/`
(safety/network.md, "Rules by method and path"). A per-repo allow is
`api.github.com * /repos/<org>/<repo>/**` plus the git smart-HTTP paths on `github.com`
(`/<org>/<repo>.git/...`). devvm's own documentation advises "a fine-grained GitHub token, scoped
to the repos your VMs need" as the real boundary and treats the path rules as the second line.
Limits it states: GraphQL reads are POSTs, so a read-only method deny breaks `gh`; and the proxy
refuses (403) the GitHub API calls that would mint a credential with the token (add deploy key,
register runner, add webhook) in `mintsCredential` (internal/proxy/secrets.go, 647ad4bf).
Per-agent control is per-VM: each VM has its own policy file and its own set of bound credentials
(`devvm secret use|unuse <name>`), which take effect at once on the running proxy.

## 3. Credential injection: the VM holds only a placeholder

`internal/proxy/secrets.go` (647ad4bf), `internal/secrets/secrets.go` (01e5d248):

- The host keeps the real value (macOS Keychain, else a mode-600 file store). The guest gets a
  placeholder in the usual variable (`GH_TOKEN`, `CLAUDE_CODE_OAUTH_TOKEN`) ending in a per-VM
  random salt, so one VM's placeholder is worthless to another.
- The proxy holds a *bindings file* (name, kind, hosts, header, placeholder, optional path and
  methods; never the value) in the VM's run folder, mode 600, watched and re-read on change. When
  it changes, the proxy drops its cached values (`changed` hook in `newSecrets`). Values are looked
  up in-process and cached 5 minutes (30 seconds for agent logins).
- On a request to a bound host, only the bound header is touched, and only when it holds the
  placeholder in one of four exact shapes (bare, `Bearer`/`Token`/scheme + space, Basic with the
  placeholder as password, Basic with it as username when so configured). Anything else
  (doubled placeholder, Digest, bytes around it) keeps the placeholder, because an origin may echo
  what it refuses. A binding can carry a path and methods so the value goes in only for matching
  requests.
- git over HTTPS works through a credential helper that supplies the placeholder.

## 4. Claude OAuth: hot swap and refresh behind the proxy

The answer to "can the token change under a running agent without a restart": yes, because the
agent never held the real token, and the proxy looks the value up per request (cache 30 s to
5 min). Mechanism (network.md, "Logins made in a VM"; internal/agentlogin/claude.go, 8f6b30a8;
internal/proxy/capture.go, f5e6eebc):

- Two bound credentials for `api.anthropic.com` `Authorization`: a long-lived token (the output of
  `claude setup-token`, saved as the `claude` credential) and an OAuth login (the host's copy of
  Claude Code's credentials file). Both bindings always exist whenever either login does, and the
  store gives each "the login devvm keeps now". Source comment/doc: "a VM that started with the
  login that the newest replaced keeps working until its next launch gives it the new one."
- devvm keeps ONE Claude login. Saving the token deletes the OAuth credentials file and vice
  versa; the newest wins. `CLAUDE_CODE_OAUTH_TOKEN` in the host environment wins over both. So the
  "swap" is a store write (`devvm secret set claude` or `devvm agent login claude`), applied
  within the cache TTL to every running VM. There is no per-VM account choice and no named pool.
- The VM's credentials file holds a placeholder access token, **no refresh token** (the literal
  `devvm-proxy-managed`), and expiry 2099, so Claude Code in the VM never refreshes. The host
  refreshes the real login itself (JSON POST to the platform token endpoint with Claude Code's
  public client id, written back under a file lock), `claude_refresh_failed` in the proxy log asks
  for a re-login.
- A login made inside a VM is **captured at the token endpoint**: the proxy intercepts
  `platform.claude.com/v1/oauth/token` (and the older console host), reads the request (64 KiB cap,
  duplicate fields refused), reads the reply whole (1 MiB cap), saves the real tokens to the host
  store, and hands the VM a reply with placeholders. Replies with a scope lacking `user:inference`
  (a Console login), or with `org:create_api_key`, are refused 502 (the latter because the injected
  login could mint an API key whose value the VM would see). The access token must start
  `sk-ant-oat01-`. devvm states that Anthropic does not document these requests; it read them from
  Claude Code 2.1's bundle and drives the real client against a fake endpoint in its end-to-end
  suite to catch breakage.
- Stated residual risks: any VM with the agent can replace the single store login, so an
  untrusted VM can point the others at an account of its choosing; the injected login works for
  anything its scopes allow at `api.anthropic.com` beyond the model (profile reads); on the `open`
  tier a program can bypass the proxy.

**Provider terms.** devvm's documentation contains no discussion of Anthropic's terms for
switching or injecting tokens (a search of its docs and SECURITY.md found none). The caveat is
ours: Plan 00146 already records the Claude Code legal page's wording and recommends manual
switching only, between the owner's own accounts, no auto-rotation.

## 5. Fit with ccy and the neighbouring plans

| devvm mechanism                                            | ccy today                                                                                                                                                          | Gap                                                                                               |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------- |
| Confinement outside the guest, proxy as the only path      | Plan 00232: host-enforced netfilter, L3/L4 allow/deny of private ranges and the host; public internet open (Non-Goal)                                              | No L7 view of the public internet: no host allowlist, no method or path rule                      |
| Per-VM policy files, layered, fail-closed on a broken deny | Plan 00232 per-checkout policy file, same enforcement point                                                                                                        | Same file shape could carry L7 lines                                                              |
| Placeholder in the agent, real value injected by the proxy | `CLAUDE_CODE_OAUTH_TOKEN` and `GH_TOKEN` are real values in the container environment (src-go/internal/ccylaunch, src-go/internal/tokens, src-go/internal/ghlogin) | Container holds live credentials                                                                  |
| Switch the Claude token under a running agent              | Full container relaunch (files/var/local/claude-yolo/lib/token-management.bash)                                                                                    | Plan 00146 plans a header-replacing forwarder on `ANTHROPIC_BASE_URL`: no TLS interception, no CA |
| Host-side OAuth refresh, refresh token never in the VM     | ccy uses setup-tokens (no refresh)                                                                                                                                 | Only matters if ccy ever moves to OAuth logins                                                    |
| Org-wide GitHub token narrowed to named repos              | `GH_TOKEN` is a full token; Plan 00116 gives one repo a deploy key over SSH                                                                                        | No HTTPS-API per-repo narrowing                                                                   |

### How devvm's design meets the Plan 00251 trigger

Plan 00251 chose the local change of a seat to `rate-limited` as the trigger for a host-local
token hot-swap proxy (RESEARCH-seat-status.md, Q8; the edge is in the link's `status.json`). In
devvm's design a swap is a write to the store that the proxy watches and re-reads, so the trigger
is only "something writes the store". The trigger consumer would be a host process reading
`status.json` for local seats, choosing a named token from the pool (Plan 00048's) and writing it.
devvm has no automatic trigger of its own and no per-session choice: one login serves every VM,
which is simpler than ccy needs (ccy has a named-token pool and several concurrent seats). Per-seat
placeholders (as in Plan 00146's per-session placeholder) are the closer fit than devvm's single
shared login. devvm's rate-limit-agnostic design also means the first turn after a swap re-reads
the context with a cold cache (Plan 00146 Risks).

Neighbours (rows read in CLAUDE/Watchlist.md only, not scanned): KarpelesLab/teamclaude (OAuth
key hot-swap through a pooling proxy), rudderlabs/devbox (credential proxy, no real secrets in
the sandbox), agentgateway/agentgateway (authorisation at the proxy). All three are on the list
for the same idea; this plan should be informed by their scans when they happen.
