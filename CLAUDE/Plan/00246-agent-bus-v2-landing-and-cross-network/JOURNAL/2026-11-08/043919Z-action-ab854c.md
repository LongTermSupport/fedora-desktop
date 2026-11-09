## 04:39 · action · —   — PR 128 review code items: tlsverb read error, tlsterm race, S6, S8

The rest of PR 128's review, the code items, test first, one commit each on fix/crossnet-review-rest:

- fbd8af113: teamchat tls refuses a credential it cannot read with the read's reason on stderr (the dropped error at tlsverb's credential()).
- 25e1ef2ef: tlsterm.logf no longer races: whether the log failed is a field under logMu; the configuration is never written after Start. Red under -race, green after. Swept teamchat for the class (shared log or writer field without one lock): no other instance.
- 0ba2ec9ca: S6, a relay idle for tlsterm.IdleLimit (2 minutes, above the client's 30 s sync and one-minute pool idle) is closed and its slot freed, on the Clock seam; SPEC X18 says it.
- 309255e01: S8, the team CA key must be root's alone, mode 0600, judged on the opened file (state.ReadPrivateFile); every reader refuses with the chmod or chown fix; owner injected through host.Deps.StateOwnerUID and the four public CA functions. The leaf key, the class's second instance, is made again when loose. SPEC X13 and the member doc say it.

S14 (feat/crossnet-s14-int) must pass an owner to loadCA in rotate.go and leaf.go; details in subagent-reports/261108-crossnet-review-code-opus.md.
