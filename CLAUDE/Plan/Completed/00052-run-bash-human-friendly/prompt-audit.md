# Plan 00052 — Phase 1 Prompt Audit (run.bash)

Catalogue of every interactive prompt site in `run.bash`, its behaviour before
this plan, and its behaviour after. Line numbers are approximate (post-edit).
Helper contract: prompts/errors → stderr, value → stdout via `printf '%s'`,
infinite re-prompt on invalid input, never exits the run on a typo. Confirm
polarity is visible in casing: `(Y/n)` = Enter accepts Yes, `(y/N)` = Enter
declines.

## Helper family (after this plan)

| Helper                           | Kind                  | Enter behaviour                      | Invalid input                  | Can exit?                                |
| -------------------------------- | --------------------- | ------------------------------------ | ------------------------------ | ---------------------------------------- |
| `confirm <msg> [y\|n]`           | y/n confirm           | takes the `[default]` (Y/n or y/N)   | re-prompt with what to press   | no                                       |
| `promptForValue <item> [v] [d]`  | value entry + confirm | accepts default (if any) or confirms | re-prompt; `n` re-edits value  | no                                       |
| `promptChoice <p> <max> [d]`     | menu choice           | takes `[default]` if given           | re-prompt 1..max               | no                                       |
| `promptSecretConfirmed <label>`  | secret (double entry) | empty allowed (caller decides)       | re-prompt until both match     | no                                       |
| `promptDefault <p> <def> [min]`  | free text w/ default  | takes `<def>`                        | re-prompt until minlen met     | no                                       |
| `prompt_verified_vault_password` | secret + verify loop  | n/a (hidden read)                    | re-prompt until decrypts/abort | no (caller fails after explicit `abort`) |

## Prompt sites

| #   | Approx line | Purpose                                                   | Before: Enter                                         | Before: invalid                                         | After: helper / behaviour                                                         | Polarity                     | Could exit before?         |
| --- | ----------- | --------------------------------------------------------- | ----------------------------------------------------- | ------------------------------------------------------- | --------------------------------------------------------------------------------- | ---------------------------- | -------------------------- |
| 1   | ~795        | SSH-protocol acknowledgement                              | bespoke `read (Y/n)`, Enter proceeded                 | re-asked                                                | `confirm "...SSH..." y` — Enter proceeds                                          | Y/n (benign)                 | no                         |
| 2   | ~705        | Main SSH key password                                     | `promptSecretConfirmed`                               | re-prompt                                               | unchanged                                                                         | secret                       | no                         |
| 3   | ~717        | Custom hostname                                           | `promptDefault "" 1`                                  | re-prompt (minlen 1)                                    | unchanged (no default — must type)                                                | n/a                          | no                         |
| 4   | ~964        | Config-source selector (which saved config)               | bespoke loop, Enter=skip                              | re-prompt                                               | `promptChoice` with explicit Skip option as default; Enter=skip preserved         | choice (default=skip)        | no                         |
| 5   | ~1016/1086  | Config-choice menu (pull/selective/merge/keep/push/fresh) | `promptChoice` no default                             | re-prompt                                               | `promptChoice` with recommended default (pull > keep > fresh); Enter takes it     | choice (default=recommended) | no                         |
| 6   | ~1032,1063  | user_login                                                | `promptDefault` `[whoami]` minlen 3                   | re-prompt                                               | unchanged + `(Enter to accept)` hint                                              | n/a (default)                | no                         |
| 7   | ~1033,1064  | Full name                                                 | raw `read -rp`, manual `${:-login}`                   | accepted anything                                       | now `promptDefault "[login]"` minlen 1                                            | n/a (default)                | no                         |
| 8   | ~1035,1066  | Email address                                             | `promptForValue ... email` (buggy confirm)            | re-prompt; **Enter re-typed whole value**               | `promptForValue` — Enter at confirm accepts; bad email re-prompts; `n` re-edits   | Y/n at confirm               | no (but forced re-type)    |
| 9   | ~272        | GitHub username(s) YAML                                   | `promptForValue` (buggy confirm) inside validate loop | re-prompt                                               | same `promptForValue` fix flows through                                           | Y/n at confirm               | no                         |
| 10  | ~1094       | Vault wrong-password recovery                             | raw `read -rsp`, **written unverified**               | none (no validation)                                    | `prompt_verified_vault_password` — verify-before-write, re-prompt, `abort` escape | secret + verify              | no (silently wrote bad pw) |
| 11  | ~1105       | Vault first-entry (values exist, no secret file)          | raw `read -rsp`, written unverified                   | none                                                    | `prompt_verified_vault_password` — same verify loop                               | secret + verify              | no (silently wrote bad pw) |
| 12  | ~1117       | Vault new-vault (brand new)                               | raw `read -rsp`, blank=auto-gen                       | none                                                    | `promptSecretConfirmed` double-entry; blank=auto-gen; now `chmod 600` too         | secret (double entry)        | no                         |
| 13  | ~1144       | GitHub passphrase reuse                                   | raw `read -n 1`, Enter silently = "no"                | re-read (single key)                                    | `confirm "...same password..." y` — Enter reuses (suggested path)                 | Y/n (benign)                 | no                         |
| 14  | ~1154       | GitHub SSH keys passphrase                                | `promptSecretConfirmed`                               | re-prompt                                               | unchanged                                                                         | secret                       | no                         |
| 15  | ~502        | Create PUBLIC GitHub issue                                | `confirm` no default (explicit y)                     | re-prompt                                               | `confirm "...PUBLIC tracker" n` — Enter declines                                  | y/N (destructive)            | no                         |
| 16  | ~550        | Create issue on playbook failure                          | `confirm` no default                                  | re-prompt                                               | `confirm "...PUBLIC tracker" n`                                                   | y/N (destructive)            | no                         |
| 17  | ~1226       | Create issue on main-playbook failure                     | `confirm` no default                                  | re-prompt                                               | `confirm "...PUBLIC tracker" n`                                                   | y/N (destructive)            | no                         |
| 18  | ~1231       | Continue despite main failure                             | `confirm` no default                                  | re-prompt                                               | `confirm "...continue..." y` — Enter continues                                    | Y/n (benign)                 | no                         |
| 19  | ~1242       | Restore projects from manifest                            | `confirm` no default                                  | re-prompt                                               | `confirm "...restore..." y`                                                       | Y/n (benign)                 | no                         |
| 20  | ~1421       | Install optional components                               | `confirm` no default                                  | re-prompt                                               | `confirm "...optional..." y`                                                      | Y/n (benign)                 | no                         |
| 21  | ~1495       | Configure manual-check hardware                           | `confirm` no default                                  | re-prompt                                               | `confirm "...hardware..." y`                                                      | Y/n (benign)                 | no                         |
| 22  | ~1516       | Run UNTESTED playbooks                                    | `confirm` no default                                  | re-prompt                                               | `confirm "...untested..." n` — Enter declines                                     | y/N (destructive)            | no                         |
| 23  | ~1559       | Reboot now                                                | `confirm` no default                                  | re-prompt                                               | `confirm "Ready to reboot now?" n` — Enter declines                               | y/N (destructive)            | no                         |
| 24  | ~1306       | `show_menu` main choice (1-9/A/W/B/S/Q)                   | bespoke case loop, re-prompts                         | re-prompt (`*)` branch)                                 | **kept bespoke** — genuine multi-key menu (Decision 3); not value-shaped          | multi-key                    | no                         |
| 25  | ~1330       | `show_menu` Whitelist number list                         | bespoke `read` + `_parse_number_list`                 | invalid numbers warned + ignored; empty list re-prompts | **kept bespoke** — number-list parser, not a single value                         | list entry                   | no                         |
| 26  | ~1345       | `show_menu` Blacklist number list                         | bespoke `read`, Enter=run all                         | invalid numbers warned + ignored                        | **kept bespoke** — number-list parser; Enter=run-all is meaningful                | list entry                   | no                         |

## Justified bespoke reads (sites 24–26)

These three reads are **not** converted to the helper family, per Decision 3
(extend the family; justify surviving bespoke loops):

- **Site 24 — `show_menu` main choice** is a multi-key menu (`1`–`9`, `A`, `W`,
  `B`, `S`, `Q`) handled by a `case` statement. `promptChoice` only validates a
  single integer range and cannot express the letter actions. The loop already
  re-prompts on any invalid key (`*)` branch prints what to enter and loops), so
  it satisfies the "never exit on typo / say what to enter" contract without a
  helper.
- **Sites 25 & 26 — Whitelist / Blacklist number lists** read a *list* of
  numbers (`1 3 5` or `1,3,5`), parsed by `_parse_number_list`, which warns on
  and skips each invalid token. They are not single-value prompts, so none of
  the value/choice helpers fit. The Blacklist prompt's Enter = "run all" is
  intentional semantics, not a missing default. Both loops re-prompt (W) or
  proceed sensibly (B) and never exit the run.

## Polarity summary

- **Enter = Yes `(Y/n)`** (benign continues / value confirmations): sites 1, 8,
  9, 13, 18, 19, 20, 21, plus every `promptForValue` confirm step.
- **Enter = No `(y/N)`** (destructive — explicit `y` required): sites 15, 16,
  17 (post to PUBLIC tracker), 22 (untested playbooks), 23 (reboot).
- **Choice default** (recommended option / skip): sites 4, 5.
- **No default** (must type): site 3 (hostname), secret entries 2, 12, 14.
