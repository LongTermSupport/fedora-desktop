VERDICT: MERGE

The play is right, the pin and checksum check out, the meta-deploy entry and the docs are right, and both earlier FIX-FIRST rounds are fixed. I found no wrong fact, no broken play behaviour, no unmet requirement and no internal contradiction. What is left is notes only. I changed no file.

**Checks I ran myself**

- **Gates:** `ansible-playbook --syntax-check` passes on ansible-core 2.19.14. `qa-version-pins.bash` prints `VERSION-PINS-OK 14 … 14 of 14`. The play file has mode 100755.
- **sha256:** the release API digest for `opencode-linux-x64.tar.gz` at v1.19.3 is `sha256:13a8fb29…536c9b`, the same as `opencodeSha256`.
- **Upstream facts at the tag:**
  - `config/managed.ts`: the Linux managed config directory is `/etc/opencode`.
  - `config/config.ts`: that managed config is merged after the global, `OPENCODE_CONFIG`, project, `.opencode`, `OPENCODE_CONFIG_CONTENT` and account configs. So "no user or project config can override" (docs/playbooks.md) holds on Linux.
  - `core/v1/config/config.ts`: `share` accepts `manual|auto|disabled`, and `autoupdate` accepts a boolean or `notify`.
  - `prompt_async` (`handlers/session.ts:311-327`) forks `prompt`. `prompt.ts:1052-1070` stores the user message (`createUserMessage`) before the turn runs, and returns early on `noReply`.
- **Plan 00209 references:** v2 B 2.4.1 steps 4, 5 and 7 (the `launch` field, `launch-failed` exit 78, `TEAMCHAT_RUNTIME`) and B 2.7.3/2.7.4 on `feat/bus-v2-n3` match what the design cites. On F44, 00209 Task 3.2 is N3 and n3e is its Claude Code slice. Tasks 3.3, 4.1 and 5.1 are LXC, the ccy kit and seam 56 with Gate B. PLAN.md Tasks 3.1-3.4 depend on these correctly, and none on v1.
- **Consistency:** section 7's H-OC1 to H-OC5 match sections 3.1 and 3.2, and the section 4 table and section 5's capabilities agree with section 3. The Gate A paragraph in 5.3 adds no `teamchat` name or path; only the launch-mode argument goes to the owner (O4).
- **Hygiene:** the diff has no install-specific names, paths or emails. `Owner: joseph` is the standard plan field.

**Earlier FIX-FIRSTs**

- Round 1, item 1 (LXC is 00209 Task 3.3, N3): fixed at PLAN.md:81-84 and design.md:298.
- Round 1, item 2 (resume across runs via `conversation.json`): fixed at design.md:83-92 and table row :172.
- Round 2, item 1 (`session.error` must not cause a redelivery): fixed at design.md:125-131 and H-OC3 :314-316. A `session.error` now only prompts a needle check per entry, and a stored message is proven. Round 2's notes 2-5 are also addressed: launch failure at :80-82 with H-OC1, CORS at :65-66 with H-OC5, the relaunch kept as a parity choice at :97-100, and `launch` added to `conversation.json`.

**Findings (all NOTE)**

1. **NOTE**, design.md:129-131. Declaring an entry `missing` "without the 180 s wait" changes B 2.7.4's missing rule. That rule is the link's own, and 5.2:267 puts item states outside the seam. Section 5.3 does not list this among the v2 text changes. **Fix:** add a bullet to 5.3 saying the missing rule gains an "agent-reported error, then idle" trigger, or drop the shortcut and keep the 180 s rule.

2. **NOTE**, design.md:121-131 and 141-144. B 2.7.4's late find (missed nonces searched for 24 h) is not carried over. With an API it is just a re-read of the message list. **Fix:** one sentence saying `missed` nonces are checked against `GET /session/:id/message` for 24 h, with B's late-find outcomes.

3. **NOTE**, design.md:66-75. At v1.19.3, `opencode serve` defaults to `--port 0` (`cli/network.ts:10`). `server.ts:118-121` then tries 4096 and falls back to any free port. The `/proc` ownership lookup the design already uses could find CMD's listening port after the bind. That would remove the window between the keeper's port check and opencode's bind, and the "port taken" restart case. Explicit `--port 0` still tries 4096 first, which is harmless with the ownership check. **Fix (optional):** state it as an alternative for the implementation to choose.

4. **NOTE**, design.md:116-131. `prompt` accepts a client-supplied `messageID` (`prompt.ts:661`, `input.messageID ?? MessageID.ascending()`). The link could set one per delivery and check `GET /session/:id/message/<id>` as well as the needle. **Fix (optional):** mention it in 3.2.

5. **NOTE**, play-opencode.yml:151-159. The version assert runs the symlink, so it proves the link points at the pinned binary. A `~/.opencode/bin` copy earlier in PATH would not be caught; version-pins.yml:83-86 and docs/playbooks.md already say so. No change needed.
