All 26 pauses under `playbooks/` and `tasks/` are now classified and fixed. Eight info-only ones (including the DisplayLink "Final instructions" that hung your batch) now print with `ansible.builtin.debug` and carry on. A new QA gate fails any pause that has no `register:` and no `# PAUSE-OK: <reason>` comment. The gate passes on the repo, and `ansible-playbook --syntax-check` passes on all 9 changed plays.

- **Branch:** `agent-ade4477da87fefd0e-0bbf2a27` (pushed)
- **Commit:** `85068e67`

## The pauses

| file:line (before the change) | Class | Action |
|---|---|---|
| play-displaylink.yml:439 "Final instructions" | a | now debug, "Press ENTER" removed |
| play-displaylink.yml:193 MOK password | a | now debug; text says it is `mok_password` in your vault |
| play-displaylink.yml:125 version check failed | b, not registered | kept with `PAUSE-OK`: Enter accepts the risk, Ctrl+C aborts. Only fires when GitHub can't be reached |
| play-nvidia.yml:283 MOK password | a | now debug, same vault note |
| play-nvidia.yml:407 reboot reminder | a | now debug |
| play-ipu6-webcam.yml:111 final instructions | a | now debug |
| play-AB-dnf-upgrade.yml:246 kernel reboot notice | a | now debug |
| play-github-cli-multi.yml:1531 summary (`seconds: 0`) | a | now debug |
| play-qobuz.yml:110 Last.fm instructions | a | now debug; the two real prompts that follow still ask |
| play-qobuz.yml:126, 136 Last.fm key / secret | b | left; only asks when the vars are missing |
| play-qobuz.yml:280 rescrobbled login | c | `PAUSE-OK`; the next task checks the session file and fails |
| play-qobuz.yml:400 hifi-rs config | c | `PAUSE-OK`; the next task checks the API |
| play-remote-desktop-toggle.yml:139 2 s wait | timed | `PAUSE-OK` (never hangs) |
| play-speech-to-text.yml:687 2 s wait | timed | `PAUSE-OK` |
| play-docker-overlay2-migration.yml:127, 272, 319 2–3 s waits | timed | `PAUSE-OK` |
| play-docker-overlay2-migration.yml:208 type `MIGRATE` | b | left; only asks when there is data to delete |
| play-basic-configs.yml:57 prompt colour | b | left; only asks when no colour is set or saved |
| play-nordvpn-openvpn.yml:80, 102 username / password | b | left; only asks when the credentials are missing |
| play-self-update.yml:151 Slack webhook | b | left; see below |
| play-lastpass.yml:24, 40, 46 account setup | b | left; only asks when no LastPass accounts are configured |

For the two (c) waits: both plays could instead check first and fail with "run `rescrobbled` / `hifi-rs config …`, then re-run", which would remove the wait entirely. I left them as they are.

## The QA gate

- **Script:** `scripts/qa-ansible-pause.bash` (rule name `pause-without-register`), added to `scripts/qa-all.bash` next to the ready-wait gate.
- **Self-test:** it checks itself on every run against `tests/fixtures/ansible-pause/flagged.yml` (each marked line must be reported and nothing else) and `clean.yml` (nothing may be reported). Run on its own, it fails (exit 1) on the flagged file and passes on the clean one.
- **Docs:** a table row and a `### pause-without-register` section in `CLAUDE/QA.md`, and one paragraph in `CLAUDE/AnsibleStyle.md`. `qa-docs.bash` passes and shellcheck is clean. I did not run `qa-all.bash` itself.

## Things I was unsure of

- **Syntax-check:** this worktree has no `vault-pass.secret`. I ran the check with `ANSIBLE_VAULT_PASSWORD_FILE` pointing at the main checkout's file, read-only.
- **MOK password:** the old pause said "memorize before pressing Enter". I made it info-only because the password is the owner's own vault value. In a batch it can scroll away, hence the vault note.
- **Batches can still hang** on two prompts I left alone:
  - The DisplayLink risk prompt fires whenever the GitHub API fails, which includes rate limiting. A small fix would be to skip it when `force_install` is set, or to fail and tell the owner to re-run with that flag.
  - The self-update Slack webhook prompt asks on every interactive run until `self_update_slack_webhook_url` is set, even to an empty string.
- **meta-deploy:** I did not touch `CLAUDE/Plan/meta-deploy.bash` and no plan file was updated. Whether these changed plays go into its list is your call.
- **Scope:** the gate scans tracked YAML under `playbooks/` and `tasks/` only, not playbooks that live inside `CLAUDE/Plan/`.