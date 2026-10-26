# setup-rclone: specification of the Go port

Programme P6 of [INVENTORY.md](INVENTORY.md) ("Interactive account/setup tools"), its fifth tool
after [`open`](SPEC-open.md), [`rclone-cache-status`](SPEC-rclone-cache-status.md),
[`gh-account-setup`](SPEC-gh-account-setup.md) and [`rclone-tail`](SPEC-rclone-tail.md):
`setup-rclone`, which runs `rclone config`, vaults the result into `host_vars`, edits the
`rclone_mounts` list and offers to run the rclone play. **Method: the legacy bash is not an
oracle.** This document states the intended behaviour as numbered, testable expectations (`E*`).
The bash (`scripts/setup-rclone.bash`, 442 code lines, the `data` flag, at F44 `98884b06`) was
read only to find the requirements; where it was wrong, the expectation states the correct
behaviour and section 5 lists the departure (`B*`). The proof is the Go tests in
`src-go/internal/setuprclone` (every flow over `execx.Fake`, scripted answers on a pipe and
temporary directories) and `src-go/cmd/setup-rclone`. What only the host with a real rclone
remote and the vault can prove is section 7.

## 1. Why this one next

The programmes ranked above P6 are ported (P3, P5, P7, P8), owned by another plan (P1, the ccy
launcher, Plan 00191), on a branch waiting for the owner's root run (P2, Task 3.2) or waiting
on a decision (P4, the `run.bash` shrink: how Go reaches a fresh host). P6 lists
`gh-account-setup` and then `setup-rclone`; the first is merged, so this is the next. It carries
two embedded Python programs (the `data` flag) and a regular-expression edit of the host's
configuration file, which dropped every per-mount option the play reads (`rc_port`,
`cache_mode`, `memory_high`, `read_only`...) whenever the mount list was written (B4). Its one
credential, the rclone configuration with its OAuth tokens, only ever travels on
`ansible-vault`'s stdin. It had no test suite, so no suite was converted.

## 2. Scope decisions

- **D1. Run from the checkout, built on each run.** Like `gh-account-setup` (its D1), it edits the
  checkout's `host_vars` and runs the checkout's play, so no play installs it.
  `scripts/setup-rclone.bash` stays at its path as the build-and-run wrapper (`run_go_command` in
  `scripts/lib/go-tool.bash`), giving the command the checkout as `SETUP_RCLONE_ROOT`. Its row
  leaves `scripts/bash-limits.allowlist`; `scripts/go-binary-suites.tsv` gets
  `setup-rclone	qa-go`. No play changes, so nothing is queued in `meta-deploy.list`.
- **D2. External programs stay the tool.** `rclone` configures and lists remotes, `ansible-vault`
  encrypts, `ansible-playbook` deploys. Done in Go: the YAML read of `host_vars` and of
  `vars/fedora-version.yml` (`go.yaml.in/yaml/v3`, already allowlisted), the edit of the two
  top-level blocks, the check that the edit changed nothing else, and every prompt (`ui.Lines`:
  plain line questions, no gum, answers can be piped). No Python is left.
- **D3. One seam for processes.** Every program runs through `internal/execx`: the wizard and the
  play in the foreground (`execx.Foregrounder`, the terminal's own stdin and Ctrl-C), the rest
  through `execx.Runner`. The play runs as `ansible-playbook <play>` in the checkout, the
  documented bare form, so the program's name is a constant.
- **D4. One rclone configuration file.** Every rclone call gets
  `--config <HOME>/.config/rclone/rclone.conf`, the file the play deploys and the mount units
  read, so a `RCLONE_CONFIG` in the environment cannot make the wizard edit one file while another
  is vaulted.

## 3. Expectations

### Command line and where things are

- E1. `setup-rclone` takes no argument. `-h` or `--help` prints the usage on stdout, exit 0. Any
  other argument is `setup-rclone: this command takes no arguments — see 'setup-rclone --help'.`
  on stderr, exit 64.
- E2. `SETUP_RCLONE_ROOT` (set by the wrapper) is the checkout, an absolute directory; unset,
  relative or not a directory is exit 1 telling the user to run `scripts/setup-rclone.bash`.
  `HOME` must be absolute (exit 1). The files are
  `<root>/environment/localhost/host_vars/localhost.yml`,
  `<root>/playbooks/imports/optional/common/play-rclone.yml`, `<root>/ansible.cfg`,
  `<root>/vars/fedora-version.yml` and `<HOME>/.config/rclone/rclone.conf`.

### Preflight

- E3. In a CCY container (`ccyenv.InContainer`) it stops before anything else: exit 1, saying it
  runs on the host.
- E4. `ansible-playbook` and `ansible-vault` not on `PATH` is exit 1 naming the missing program.
  `host_vars`, the play and `ansible.cfg` must exist (exit 1 naming the path), and `host_vars`
  must read as E14 says (exit 1 with the reason) before any question is asked.
- E5. The vault is proven usable before the wizard:
  `ansible-vault encrypt_string --encrypt-vault-id localhost --stdin-name setup_rclone_probe`
  with `probe` on stdin, run in the checkout with `ANSIBLE_CONFIG=<root>/ansible.cfg`. A non-zero
  exit (its stderr shown), or output that is not one `setup_rclone_probe` key holding a `!vault`
  value, is exit 1 saying the vault password `ansible.cfg` names cannot be used. This tool never
  names or opens that file.
- E6. `rclone` not on `PATH`: `⚠ rclone not found — running the rclone play to install it` and the
  play runs (E21); a play that fails is exit 1; rclone still missing afterwards is exit 1.
- E7. The release file (`/etc/redhat-release`) must begin `Fedora release <N>`; missing or another
  distribution is exit 1. `fedora_version` in `vars/fedora-version.yml` must be an integer
  (exit 1 otherwise). A different `<N>` is a warning naming both and the branch `F<N>`, then
  `Continue anyway?` (default no; no is exit 1, `Aborted: Fedora version mismatch`).

### Step 1: remotes and the wizard

- E8. The remotes are `rclone --config <conf> listremotes`, one `<name>:` per line; a line without
  the trailing colon, or rclone failing, is exit 1. With none, the wizard runs without a
  question. With some, they are listed and `Open the rclone wizard to add or edit remotes?` is
  asked, default no.
- E9. The wizard: the short guide (n new, e edit, q quit, s skip), `Press Enter to open rclone config`, then `rclone --config <conf> config` in the foreground. Ended by a signal: the run
  ends with 128 + n and nothing written. A non-zero exit: `⚠ rclone config exited <code>` and
  `Vault the rclone configuration as it is now?` (default no; no is exit 1 with nothing written).
  Then the remotes are read again; none is exit 1.

### Step 2: the vault

- E10. Only after the wizard ran. `<conf>` must be a regular file (exit 1). It is given on stdin to
  `ansible-vault encrypt_string --encrypt-vault-id localhost --stdin-name rclone_config`, run as
  E5 runs. A failure is exit 1; the output must be one `rclone_config` key holding a `!vault`
  scalar (exit 1 otherwise). The configuration and the vault text are never printed and never in
  an argv.

### Step 3: mounts

- E11. The mounts are `rclone_mounts` from `host_vars` (E14). The menu (`ui.Lines.Choose`, heading
  `setup-rclone › Mounts`) lists, numbered from 1, each mount as
  `Change <name>: <remote> at <mountpoint>` (with `(remote not configured)` when its remote is not
  one of E8's), then each remote as `Add a mount of <remote>`, then `0) Done`. It is shown again
  after every change until Done.
- E12. A mount chosen: `e) Edit`, `r) Remove` or `c) Cancel`. Remove drops it and says the play
  neither stops nor deletes `rclone-<name>.service` (section 6). Edit asks E13 with the mount's
  values as the defaults and replaces those three values in place, keeping every other key of the
  entry and its position.
- E13. A remote chosen to add (or a mount to edit): first
  `rclone --config <conf> --contimeout 15s --timeout 30s lsd <remote>:/`. It fails: rclone's log
  lines (date prefix stripped), one hint (the Drive API not enabled, with the console URL and the
  project number when given; authentication, naming `rclone config reconnect <remote>:`; the
  network; or another error, naming the `lsd` to run), and back to the menu. It succeeds: the
  top-level folders (or `(remote root is empty)`), then three questions, each with its default,
  Enter taking it: `Folder to mount` (`/`, or the edited mount's path after the colon),
  `Mount name` (the edited mount's, or E15's), `Local mountpoint` (the edited mount's, or
  `<HOME>/mnt/<name>`). A new mount is appended.
- E14. `host_vars` is read as YAML (tags such as `!vault` are data; nothing is decrypted). It must
  be a mapping in which no top-level key is given twice. `rclone_mounts` absent or null is no
  mount; otherwise a sequence of mappings, each with `name`, `remote` and `mountpoint` plain
  strings, `remote` holding a colon. Anything else is exit 1 naming the entry by its number.
- E15. An answer that fails is said and asked again, at most three tries (then exit 1):
  - a name is `[a-z0-9][a-z0-9_-]*`, at most 64 characters, not another mount's (a unit file
    name, a `<name>-push` helper and a bookmark label are built from it); the default is the
    remote's name in lower case with each run of other characters made `-`, trimmed of `-` and
    `_`, or `mount` when nothing is left;
  - a mountpoint is absolute, already clean, not `/`, and neither inside nor around another
    mount's mountpoint, nor equal to it;
  - the remote spec `<remote>:<folder>` and the mountpoint hold no whitespace or control
    character and none of `'`, `"`, `\`, `$`, a backtick, `%`, `{`, `}`: the play writes both
    unquoted into the unit's `ExecStart` (where `%` is a specifier) and inside quotes into the
    push helper, and Ansible templates `{{`. A remote whose own name breaks this cannot be added.

### Step 4: host_vars

- E16. Nothing changed (no wizard, the mounts as read): `i host_vars unchanged` and the file is not
  written.
- E17. Otherwise the file's text is edited: a top-level block is its key line at column 0 and every
  line after it up to the next line that starts with a character other than a space or a tab
  (blank lines at the block's end stay outside it). `rclone_config` (when the wizard ran) is
  replaced by the vault text, `rclone_mounts` by the mounts encoded as YAML (two-space indent,
  `remote` and `mountpoint` double-quoted in a new entry; removed when no mount is left); each in
  its block's place, or appended after a blank line when the key was absent. Nothing else in the
  file is touched.
- E18. The edited text is read back before it is written: every other top-level key, in order and
  with its value, must be as it was; `rclone_mounts` must be the mounts intended; `rclone_config`
  the vault output. Otherwise exit 1 with the file unchanged and the reason. The file (through a
  symlink, its target) is replaced atomically, its mode kept; `✓ host_vars updated`.

### Step 5: deploy

- E19. `Run the rclone play now?` (default no). No: the command to run later,
  `<root>/playbooks/imports/optional/common/play-rclone.yml`.
- E20. Yes: the play runs (E21); then `✓ Mount services started and enabled`, with
  `systemctl --user status rclone-<name>.service` for each mount.
- E21. The play is `ansible-playbook playbooks/imports/optional/common/play-rclone.yml` in the
  checkout, in the foreground (the terminal's stdin, for a become prompt), its output shown as it
  comes and read for `skipping: no hosts matched`: that line is exit 1 (the inventory matched
  nothing, so nothing was deployed), as is a non-zero exit (`Playbook failed`) or one ended by a
  signal (128 + n).

### Signals and output

- E22. SIGINT, SIGTERM, SIGHUP and SIGQUIT at a question end the run with 128 + n; before E18's
  write nothing is changed. The end of input at a question is exit 1, nothing changed.
- E23. Questions, progress and diagnostics are stderr (`i `, `✓ `, `⚠ `, `✗ ` lines, the headings
  `━━━ <step> ━━━`); only the usage is stdout. Colour only when stderr is a terminal and `NO_COLOR`
  is unset or empty. A failed write to stderr is exit 1.

## 4. Kept as it was

The steps in order, no arguments, the wizard asked only when remotes exist, the vault id
`localhost` and the key `rclone_config`, the `rclone_mounts` entry shape, the defaults (`/`, the
lower-cased remote, `<HOME>/mnt/<name>`), the connection check before the questions with its
hints, the no-hosts-matched guard and the closing `systemctl --user status` lines.

## 5. Departures from the bash

- B1. Usage errors exit 64 (the bash: 1); `--help` exists.
- B2. Questions and progress are stderr (the bash: stdout).
- B3. `host_vars` is edited in Go and read back (E17, E18); the bash's regular expression ran on
  the whole text, then appended, and nothing checked the result. A top-level key given twice is
  refused instead of being edited twice.
- B4. Editing a mount keeps its other keys and its place (E12, E17). The bash rebuilt every entry
  from `name`, `remote` and `mountpoint`, so any run that wrote the list silently dropped
  `rc_port`, `cache_mode`, `memory_high`, `read_only` and the rest from every mount, and moved
  both blocks to the end of the file.
- B5. Answers are validated (E15). The bash wrote any text unquoted into YAML (a `#` or `: ` broke
  the file) and let through a name or path with a space, `%` or `{{`, which breaks the unit or
  the play; two mounts could share a name or a mountpoint.
- B6. The menu lists mounts and remotes apart (E11), so a remote mounted twice and a mount whose
  remote is no longer configured can each be reached; the bash found a mount by its remote's
  name, the first only.
- B7. The vault is proven before the wizard (E5), so a missing password fails before the person
  spends time in it; the bash checked a fixed password file path and learned the password was
  wrong only after the wizard.
- B8. A failed `rclone config` asks before vaulting (E9); the bash vaulted whatever was there.
- B9. A wizard ended by a signal ends the run (E9); the bash read it as "exited non-zero" and went
  on.
- B10. Every rclone call names the one configuration file (D4); the bash's wizard and listing
  used rclone's default, which `RCLONE_CONFIG` moves, while it vaulted the fixed path.
- B11. The container check is `ccyenv.InContainer` (the bash: the checkout being `/workspace` and
  the user root); python3 is no longer needed.
- B12. Nothing is written when nothing changed (E16); the bash rewrote the file on every run.
- B13. The connection check has bounded timeouts (`--contimeout 15s --timeout 30s`); the bash's
  `lsd` waited on rclone's defaults (one and five minutes).
- B14. The play runs as `ansible-playbook <play>` in the checkout (D3; the bash ran the play by
  path through its shebang, which does the same).

## 6. Stated limits

- Removing a mount here removes it from `host_vars` only. The play has no task that stops or
  deletes the unit of a mount no longer listed, so `rclone-<name>.service` keeps running until
  the play gains one; the tool says so when a mount is removed.
- The read-back (E18) compares the YAML values, not comments: a comment inside a replaced block
  goes with the block.

## 7. Proof and host checks

`go test -race -shuffle=on` over `internal/setuprclone` and `cmd/setup-rclone`: every expectation
has a test written first and seen to fail (journal). The host checks are the owner's, by hand at
the desktop, since they need a real remote and the vault:

- H1. `./scripts/setup-rclone.bash` with remotes configured: answer no to the wizard, Done at the
  menu: `host_vars unchanged`, no play run.
- H2. Edit an existing mount's mountpoint (take a copy of the gitignored `host_vars` first):
  against the copy only that `mountpoint` line changed, every other key of the mount kept.
- H3. Open the wizard, reconnect a remote, quit: the new `rclone_config` vault block replaces the
  old one in place, and the play run at the end deploys it (`rclone listremotes` lists the
  remote; the mounts come back up).
- H4. Ctrl-C at the mount menu: exit 130, `host_vars` unchanged.

None is a scripted host run, and no play changed, so nothing is queued in `meta-deploy.list`.
