# qa-reviewer: 00137 Slack alert sink (01ff3387, e7d6b98f), opus-5, 2026-09-24

Saved by the coordinator. Two rounds.

## Round 1, on 01ff3387: BLOCK

1. **BLOCK: a webhook saved by the prompt broke every later run of the play.** The encrypt
   task fed the URL on `stdin:` without `stdin_add_newline: false`, so the vaulted value
   ended in a newline. The next run's assert failed. Without that assert, the cycle would
   have exited 70 on the malformed file. The same defect was in `play-qobuz.yml`.
2. **A redirect could be recorded as a delivery.** The default opener follows a 302 as a
   body-less GET, and a 200 there read as delivered. The test covered a 302 that the real
   opener never returns.
3. **Some HTTP errors escaped the alert step and left the record reading as delivered.**
   `http.client.HTTPException` and its subclasses are not `OSError`.
4. **The entry point's Slack path was untested:**
   - the exit-70 cases;
   - that a dry run never reads the secret;
   - `RealHost.send_alert`.
5. **Nit: with no terminal, the play asks for the webhook and gets only Ansible's pause
   warning.** Nothing names the IaC route.

Checked and clean in round 1:

- **`no_log`** is on every task that touches the webhook.
- **The webhook stays out of argv and error text.**
- **Encryption** uses the repo's vault identity, and only ciphertext is written.
- **The URL check, the timeout and stdlib-only** all hold.
- **The old variable** has no remaining references.
- **Placeholders** are obviously fake, and the private config repo is not named.

## Round 2, on e7d6b98f: PASS WITH NITS

1. **Fixed:** `stdin_add_newline: false` is on the self-update task and both qobuz tasks.
   A new test reads every tracked play; against the parent commit it names exactly the
   three unfixed tasks.
2. **Fixed:** the default opener refuses redirects. With the old opener patched back in,
   the redirect test fails against a local stub server.
3. **Fixed:** a malformed reply returns `not delivered (BadStatusLine)`.
4. **Fixed:** `test-self-update-cycle.bash` passes 163 of 163, including 19 new Slack
   checks. The whole path runs through a local proxy that refuses the connection, so
   nothing leaves the machine.
5. **Fixed:** the headless line prints only when no webhook was given.

Nits:

- Last.fm values saved before the fix keep their trailing newline. A `| trim` where they
  are templated, or a `vault.bash replace`, would repair existing installs.
- The exit-70 checks assert only the exit code, not the reason on stderr.

`qa-helper-tests.bash`: 2106 tests OK.
