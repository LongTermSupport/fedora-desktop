# Element Flatpak "No encryption support" dialog: research

Scope: Element Desktop as Flatpak `im.riot.Riot`, launched with `--profile <team>`, on Fedora GNOME Wayland. Sources are web pages read on the research date; nothing was run on a host.

## 1. What triggers the dialog

Element (since the safeStorage rework, element-desktop PR #2227; the Flatpak breakage starts at 1.11.103) stores secrets such as the session/access-token key through Electron `safeStorage`. In `src/store.ts`, `prepareSafeStorage` -> `reallyPrepareSafeStorage` -> `chooseBackend`:

- On Linux it calls `safeStorage.setUsePlainTextEncryption(true)` (only matters for `basic_text`), then reads `safeStorage.getSelectedStorageBackend()`.
- If `isEncryptionAvailable()` is false, or the backend is unknown, it falls back to `plaintext`.
- In the default `Encrypted` mode, on a first launch where the result is `plaintext` or `basic_text`, it calls `consultUserConsentDegradedMode`, which is the Electron-style dialog the owner sees (translation keys `store|error|unsupported_keyring_use_plaintext` / `..._use_basic_text`).

Electron docs (safeStorage): `isEncryptionAvailable()` on Linux is true only after `ready` and when a secret key is obtainable. `gnome_libsecret` is *selected* purely by desktop-environment detection (GNOME, XFCE, Cinnamon, ...) or `--password-store=gnome-libsecret`; selection does not prove the secret service is reachable. So "backend gnome_libsecret + not available" means: Electron picked libsecret, but the Secret Service call failed. The docs also name the `org.freedesktop.portal.Secret` portal as the preferred provider for sandboxed apps.

Known causes, with evidence:

1. **Flatpak sandbox has no session-bus permission for `org.freedesktop.secrets` (the most likely cause here).** The Flathub manifest at master (read via raw GitHub) has finish-args `--filesystem=xdg-run/keyring` (old libgnome-keyring/keytar era) but NO `--talk-name=org.freedesktop.secrets` and no kwallet names. Flathub PR #530 "Add secret access, as the safeStorage api is now used" (adds that talk-name plus a libsecret module) was still OPEN when read; maintainer stepping down is cited. flathub/im.riot.Riot issue #528 documents the breakage after 1.11.103 and the working fix `flatpak override ... --talk-name=org.freedesktop.secrets` (confirmed on 1.12.7, Ubuntu 24.04; one user reported no effect; KDE users needed `org.kde.kwalletd6`; one user fixed it with `XDG_CURRENT_DESKTOP=GNOME`). Still broken by default on 1.12.8 (SteamOS). Element maintainers confirm Flatpak sandbox blocking is a Flathub packaging matter (element-desktop #2420).
2. **Locked or absent default `login`/`Default_keyring` collection** (gnome-keyring not unlocked at login, e.g. autologin or passwordless login, or no default collection). Generic Secret Service behaviour; Electron would then fail or prompt. UNVERIFIED for Element specifically: no source found tying this dialog to a locked keyring, though the dialog text says "reboot and try again".
3. **Non-detected desktop / no full DE** gives `basic_text` rather than `gnome_libsecret` (element-desktop #2420, Electron issue 39789). Not the owner's case (backend reported as gnome_libsecret).
4. `--password-store` flag: can force a backend; irrelevant to reachability. A forum post suggests `--password-store="gnome-libsecret"` as a workaround for the non-DE case (untested in #2420).
5. **Separate `--profile`:** Element only appends `-<profile>` to the userData path (`app.setPath("userData", ...)`, `src/electron-main.ts`), so each profile has its own `electron-config` store, but the profile does not change the permission situation. UNVERIFIED: whether the keyring item ("<app name> Safe Storage") is shared between profiles (probably yes, same app name). Element records `safeStorageBackend` in the profile's store and shows a "backend changed ... cannot migrate" dialog (yes clears data and relaunches, no throws) if the backend later changes, so decide the backend once before profiles hold real sessions.

Versions: breakage from 1.11.103; seen through 1.12.8. UNVERIFIED which version the owner has: `flatpak info im.riot.Riot`.

## 2. What the buttons do

- "Use no encryption" (key `unsupported_keyring_use_plaintext`): Element switches to `StorageWriter`, so secrets that would go through safeStorage are stored base64-encoded, unencrypted, in the profile's `electron-config` store under keys prefixed `safeStorage.` (location: `userData`, i.e. `~/.var/app/im.riot.Riot/config/Element-<team>/` for the Flatpak). The mode/backend is recorded (`safeStorageBackend`), so the question is not asked again. That store holds what Element protects with safeStorage, notably the pickle/session-key secrets and the access-token-derived material. UNVERIFIED: the exact list of secret names. Chromium's own cookie/local-storage encryption is a separate mechanism and is not covered by this dialog. Practical effect: anything able to read the owner's profile directory (same-user processes, unencrypted backups) can read the secrets; sandboxing and disk encryption are the remaining defence (view of flathub #528 participants).
- "Cancel": the handler throws (`isEncryptionAvailable=false and user rejected plaintext`). No `app.quit()` appears in store.ts; the throw propagates out of `prepareSafeStorage`, and the caller in `electron-main.ts` (`if (!(await store.prepareSafeStorage(...))) return;`) means startup aborts. Whether the process then exits or lingers without a window is UNVERIFIED; treat Cancel as "Element does not start".

## 3. Durable fixes for IaC, and host verification

A. **Flatpak per-user override (recommended).** Grant the sandbox the Secret Service name:
`flatpak override --user im.riot.Riot --talk-name=org.freedesktop.secrets`
Persists in `~/.local/share/flatpak/overrides/im.riot.Riot`, survives Flatpak updates, applies to all profiles. Idempotence: Ansible task that checks `flatpak override --user --show im.riot.Riot` for the name before running (or `community.general` has no override module, UNVERIFIED; use command with a probe-then-act check). The kwallet names are unnecessary on GNOME. Do not use `--socket=session-bus` or broad `--filesystem`.
Verify (read-only):

- `flatpak override --user --show im.riot.Riot | cat`
- `flatpak info --show-permissions im.riot.Riot | cat` (expect `org.freedesktop.secrets=talk` under `[Session Bus Policy]`)

B. **Secret Service provider running and unlocked at login.** Fedora GNOME runs gnome-keyring-daemon via PAM/systemd and creates/unlocks `login` on a password login; autologin leaves it locked. Verify:

- `busctl --user list | grep -i secrets` (name `org.freedesktop.secrets` owned by gnome-keyring or a replacement)
- `secret-tool search --all xdg:schema org.gnome.keyring.Note 2>&1 | head` is NOT a clean lock test; prefer `gdbus call --session --dest org.freedesktop.secrets --object-path /org/freedesktop/secrets/aliases/default --method org.freedesktop.DBus.Properties.Get org.freedesktop.Secret.Collection Locked` (expect `(<false>,)`; an object-not-found error means no default collection).
- Inside the sandbox: `flatpak run --command=sh im.riot.Riot -c 'ls /run/user/$(id -u)/bus'` shows only that a bus exists; permission check A is the real test.
  IaC angle: ensure `gnome-keyring` and `libsecret` are installed (package list in playbook) and the PAM keyring is the default. An unlocked-at-login guarantee on autologin cannot be met by IaC alone (needs a password login or empty keyring password, a security trade-off and an owner decision).

C. **Launch flag `--password-store=gnome-libsecret`** in the .desktop `Exec=`: only forces backend selection, which GNOME already selects; it does not fix reachability. Pass-through also depends on the Flathub `element.sh` wrapper forwarding args to Electron (UNVERIFIED). Not recommended as the fix.

D. **`XDG_CURRENT_DESKTOP=GNOME` override** (`flatpak override --user --env=XDG_CURRENT_DESKTOP=GNOME`): one #528 user reported success; relevant only when DE detection fails. Not needed on GNOME. UNVERIFIED for this setup.

E. **Accepting "Use no encryption"** is a deliberate downgrade; do not automate it.

## 4. Recommendation

1. In the playbook that installs Element (or the ccy-teams/agent-bus provisioning play that owns the Element Flatpak), add idempotent `flatpak override --user im.riot.Riot --talk-name=org.freedesktop.secrets`, probe-then-act, failing fast on error. Ensure gnome-keyring/libsecret packages are present.
2. Have the owner run the read-only checks in 3A/3B first (`flatpak info --show-permissions`, `busctl --user list | grep -i secrets`, the `Locked` property call). If A is missing, that is the cause; if the secret service is absent or locked, fix B before profiles are used.
3. Do this BEFORE a profile ever answers the dialog, because a recorded `plaintext` backend later changing to `gnome_libsecret` triggers Element's "backend changed" path (may clear that profile's data). For a profile that already chose "Use no encryption", plan on resetting it (delete its `Element-<team>` config dir through IaC-managed cleanup, owner decision) or re-login.
4. Optionally keep `helpers/agent_bus/element.py` unchanged: no launch flag is needed.

## Sources

- https://www.electronjs.org/docs/latest/api/safe-storage
- https://raw.githubusercontent.com/element-hq/element-desktop/develop/src/store.ts
- https://raw.githubusercontent.com/element-hq/element-desktop/develop/src/electron-main.ts
- https://github.com/element-hq/element-desktop/issues/2420
- https://github.com/flathub/im.riot.Riot/issues/528
- https://github.com/flathub/im.riot.Riot/pull/530
- https://raw.githubusercontent.com/flathub/im.riot.Riot/master/im.riot.Riot.yaml
- https://forums.opensuse.org/t/flathub-element-no-encryption-support-error/187011/1
