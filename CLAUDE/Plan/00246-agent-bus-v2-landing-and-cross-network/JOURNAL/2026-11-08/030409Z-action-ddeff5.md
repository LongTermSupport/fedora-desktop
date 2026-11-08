## 03:04 · action · —   — PR 128 review: S1, S4 built, S5 recorded, the docs items done

The rest of the coordinator's review of pull request 128 (S1, S4 to S8 and the nits), on `fix/crossnet-review-rest`, cut from the head that carried B1 to B3, S2 and S3.

Built with tests first: S1 (X29 runs the unit's own `ExecStart=` and credentials directory through the terminator verb and a member built by `matrix.NewMember`; a mutation that swaps the two credential files turns the new test red and leaves the old one green) and S4 (`project_hostname_read` also flags `Uname`, and the constant names of the hostname's variables and files; red on `503e456b5^`, where the old rule printed nothing; four instances in today's tree fixed).

Recorded: S5. Tuwunel's login rate limits at the pinned v1.9.4 are keyed on the account, not the client address, so the terminator does not merge members into one login bucket; the limits that are keyed on the address (the OIDC throttle and the MSC4388 rendezvous throttle) are not used by teamchat; a loopback peer bypasses `ip_source`, so a member can name any address in Tuwunel's logs. The evidence is in SPEC X18.

Docs: X12's wording now says exactly what `matrix.memberBase` and `declare.URLProblem` do (a canonical loopback literal only, no CA on plain http, the url's port is the team's, the default url is https); X34 returns to its specified text on this branch, because S15 is built in pull request 172, which keeps the "As built" text and gets it back in its own merge; X30 names the policy-routed VPN and the host's own /32; the docs page says to reissue when the ssh step of issuing fails.

Still to do on this batch: S6 (the terminator's idle deadline), S8 (`loadCA` refusing a `ca.key` that is not mode 0600 and root-owned), and two nits (a dropped read error in `tlsverb`, a data race in `tlsterm.logf`).

Two decisions for the coordinator came out of S1 and S4: `matrix.Config.Dial` is a production field that only tests use (a route override, the certificate name and the CA unchanged), and the hostname rule matches whole constant values only, so a substring such as `HOOKS_DAEMON_HOSTNAME=box` inside a longer string in three lxc tests is not caught.
