## 12:23 · action · —

Task B.4 built by Plan 00265 unit U8 (issue #425), branch v2-smoke-u8-xhost-lxc.

- Found and fixed first: `teamchat run --kind lxc --key` ignored --key in the keeper's production config, so every v2 LXC session (the same-host unit included) was refused 78 "a lxc launch needs --desired". RED was TestProduction showing the home layout's roots.
- A seat's url now follows who places it: the lxc apply's joins get the bridge, every member verb a person runs gets the team's url.
- New verb `teamchat seat place-lxc --user U --checkout DIR`: an issued LXC token seat piped from the homeserver host is placed in the keyed layout with a one-seat desired membership; stdout is the key.
- Docs: agent-bus-lxc-members.md (off-host route, hand-run line, launcher contract, sign-in, K15), agent-bus-cross-network.md status note.
- Open: who installs the kit and unit in an off-host container; rotate-ca's hand-reissue list; live proof. All in Plan 00265 reports/U8.md.
