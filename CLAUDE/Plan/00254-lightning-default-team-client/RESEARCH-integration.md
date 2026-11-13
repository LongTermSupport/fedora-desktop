# Lightning as the team Matrix client: research for issue #253

Source read: `github.com/Mizerd/lightning` cloned at `e6ea127a4975ec87f76d21723fecd9fb9b2f27d5`
(main, 2026-11-12; latest tag `v0.10.5`, 2026-11-08), into `untracked/repos/lightning`.
Dependency sources were read from the crates.io tarballs that `rust/Cargo.lock` pins, unpacked in
`untracked/scratch/lightning-crates/`: matrix-sdk 0.18.0, matrix-sdk-ui 0.18.0, reqwest 0.13.4,
rustls-platform-verifier 0.7.0 and rustls-native-certs 0.8.4.
Nothing was run on a host and no Flatpak was started. Every claim about runtime behaviour inside
the sandbox is marked as not tested.

---

## 1. A profile or window per team; several accounts

**Answer: there is no `--profile` flag. Each team can still get its own instance, with its own
data, settings and window, by giving each launch its own XDG directories. Several accounts in one
instance are supported, but only one is active at a time.** Confidence: high for the code paths,
medium for the Flatpak `--env` mechanics (not tested).

Evidence:

- **The CLI has no profile option.** `src/main.cpp:2410-2461` registers `--mock`, `--console`,
  `--log-file`, `--backend` and some demo-only flags. The preflight flags (`src/main.cpp` ~500-680)
  are status and self-test commands plus `--reset-crypto-store`. `QCommandLineParser::process`
  rejects an unknown option and exits. The text "`lightning-matrix --profile ...`" in
  `docs/network-profiles.md` §5 describes a hypothetical vopono workaround. No such option exists.
- **The data root comes from XDG.** `src/storage/AppDataPaths.cpp:177-213`:
  `resolveAppDataBase()` prefers `$XDG_DATA_HOME`, else `$HOME/.local/share`. `primaryRoot()` is
  `<base>/MatrixClient/matrix-client`, and it holds the account stores (the matrix-sdk sqlite and
  crypto store per account).
- **Settings come from QSettings with org `MatrixClient` and app `matrix-client`**
  (`src/main.cpp:2187-2189`; `SettingsManager.cpp:212` uses a plain `QSettings()`). On Linux that
  file is `$XDG_CONFIG_HOME/MatrixClient/matrix-client.conf`. In the Flatpak it is
  `~/.var/app/org.lightning_matrix.Lightning/config/MatrixClient/matrix-client.conf`
  (`docs/message-keyboard-navigation.md:136-137`). The cache uses
  `QStandardPaths::CacheLocation`, which follows `$XDG_CACHE_HOME` (`AppDataPaths.cpp:215-220`).
- **One instance per data root, not one per user.** `src/main.cpp:2278-2318` and
  `src/app/SingleInstanceGuard.cpp:62-66,177-189`: the lock file is
  `<primaryRoot>/instance-matrix-client.lock`. The activation socket name is a SHA-256 of
  (canonical data root, app name, user). A second launch on the same root raises the first
  window. A launch with a different `XDG_DATA_HOME` gets its own lock and socket, so it is a
  separate process and window. That is the per-profile behaviour Element's `--profile` gives.
- **Keyring items do not collide between instances.** `LibSecretStore.cpp:27-80` stores each
  token with the attributes `user_id`, `key` and `install`. `install` is a per-settings-file UUID
  (`SettingsManager::keyringInstallId`, `SettingsManager.cpp:979-999`), so two XDG_CONFIG_HOMEs
  use separate keyring scopes. The Flatpak has `--talk-name=org.freedesktop.secrets`.
- **Portable mode does not help in the Flatpak.** `LIGHTNING_PORTABLE=1`
  (`src/storage/PortableMode.cpp:122-191`) puts the data beside the executable, and `/app/bin` is
  read-only there.
- **Several accounts in one instance.** `docs/feature-contracts.md:146-153` says "Persistent
  multi-account support … Only the active account syncs". You switch between accounts in one
  window. "Live accounts" (background sync of pinned accounts, `feature-contracts.md:158-178`) is
  a Labs feature, off by default and marked "Live: NOT TESTED". It still shows one window with one
  active account. "A window per team" therefore needs one instance per team.

The launch form, proven on the host (Plan 00254 Tasks 3.1 and 3.2, Lightning 0.10.5, runtime
`org.kde.Platform` 6.11). Paths must be absolute, because flatpak does not expand `$HOME`. Inside
the sandbox, `~/.var/app/<id>/` is always writable.

```
flatpak run --command=env org.lightning_matrix.Lightning \
  XDG_DATA_HOME=/home/<u>/.var/app/org.lightning_matrix.Lightning/teams/<team>/data \
  XDG_CONFIG_HOME=/home/<u>/.var/app/org.lightning_matrix.Lightning/teams/<team>/config \
  XDG_CACHE_HOME=/home/<u>/.var/app/org.lightning_matrix.Lightning/teams/<team>/cache \
  lightning-matrix
```

Established on the host:

- `flatpak run --env=XDG_*_HOME=…` does **not** reach the app: inside the sandbox the variables
  still name the app's default `~/.var/app/<id>/{data,config,cache}`, so a launch in that form
  only raises the running instance. `env` run as the sandbox's command sets them after Flatpak
  has, and the app sees the given paths.
- In the `--command=env` form, two instances on two roots both stay running, each holding its own
  `instance-matrix-client.lock` under its own root. A second launch on a root already in use
  exits within a second and leaves that root's instance running. That each window showed its own
  login page and that the instance on the default root was left untouched is the owner's
  observation, not the script's: its `flatpak ps` counts (5 instances before the probe, 3 with
  both probes up) are unexplained and are not evidence either way.
- The runtime has `/etc/ssl/certs/ca-certificates.crt` (a link to `tls-ca-bundle.pem`) and
  Python 3.13.

Not established:

- `setDesktopFileName(resolvedAppId())` (`main.cpp:2368`) gives every instance the same app id,
  so the windows group under one taskbar entry. Element has the same limitation.

## 2. Pre-filling the homeserver

**Answer: yes, through the settings file. There is no CLI flag, and `matrix:`/`matrix.to` links
do not choose a server for login.** Confidence: high (code read; not run).

- `qml/LoginScreen.qml:520-533`: on creation the server field is filled from
  `app.settings.loginHomeserverPrefill`, and if that is non-empty it immediately runs
  `app.auth.discoverAuthMethods(text)`.

- `SettingsManager.cpp:35` defines `kHomeserver = "homeserver/url"`. `:228-229` writes
  `https://matrix.org` only if the key is absent, and `:1413-1416` reads the prefill from that key.
  A play can therefore pre-write, per team config dir:

  ```ini
  [homeserver]
  url=https://<team homeserver>
  ```

  in `<XDG_CONFIG_HOME>/MatrixClient/matrix-client.conf`. A launcher with that profile opens on
  the login page with the server already discovered. The user name and password, or SSO, are
  still typed. The app later overwrites the key with the canonical URL (`:5161`), which is
  harmless. The file is made 0600 by the app (`:233-235`).

- Matrix links: `SingleInstanceGuard::isOpenableLink` (`SingleInstanceGuard.cpp:237-247`) accepts
  argv `matrix:` and `https://matrix.to/#/` links. `AppController.h:723-729` holds a link "until a
  signed-in MainScreen takes it". The link opens a room or user after login. It does not select a
  login server.

- Pre-writing a session, the equivalent of Element's config with an access token, is not viable.
  Tokens live in the keyring and the account record in QSettings, with crypto-store invariants
  (`CLAUDE.md` §6 of that repo). Out of scope.

## 3. TLS stack, root store, and a private CA in the Flatpak

**Answer: Matrix traffic goes through the Rust SDK's reqwest 0.13 with rustls and
rustls-platform-verifier. On Linux that loads roots through rustls-native-certs, which honours
`SSL_CERT_FILE` and `SSL_CERT_DIR`. It is not webpki-roots and not NSS. Qt's own TLS stack is used
only for app-side traffic such as the updater.** Confidence: high for the stack (code and lock
file); medium for the sandbox paths (not tested).

Evidence:

- `rust/Cargo.toml:38`: matrix-sdk 0.18 with `default-features = false`. `:49`: reqwest 0.13.4
  with features `rustls,…`. `Cargo.lock`: reqwest depends on `rustls-platform-verifier` 0.7.0,
  which depends on `rustls-native-certs` 0.8.4, which depends on `openssl-probe`.
- matrix-sdk `src/http_client/native.rs:187-230` (`make_client`): TLS 1.2 minimum and
  `tls_certs_merge(additional_root_certificates)`. Lightning passes none. The verifier is reqwest's
  default. `docs/network-profiles.md` §7 confirms that Lightning's proxy client copies this with
  "default roots, no certificate leniency".
- rustls-platform-verifier `src/verification/others.rs:55-125`: on Linux the root store is
  `rustls_native_certs::load_native_certs()`. It does not fall back to webpki-roots, and it fails
  only if the store ends up empty.
- rustls-native-certs `src/lib.rs:44-60,191-230,268-306` and `src/unix.rs`: with no env vars it
  uses `openssl_probe::probe()` (the usual `/etc/ssl/certs/ca-certificates.crt`,
  `/etc/pki/tls/certs/ca-bundle.crt`, …). **If `SSL_CERT_FILE` and/or `SSL_CERT_DIR` is set, only
  those are read.** `SSL_CERT_DIR` is a colon-separated list, and every regular file in it is read
  as PEM (no hash names needed). Load errors only produce warnings.
- Exception: the call SFU websocket (`tokio-tungstenite`, `Cargo.toml:67`) uses
  `rustls-tls-webpki-roots`, so MatrixRTC calls through a private-CA LiveKit would fail. That is
  irrelevant to the bus.
- Flatpak manifest `org.lightning_matrix.Lightning.yaml:1-21`: runtime `org.kde.Platform//6.11`.
  The finish-args give no home filesystem access. Only its own `~/.var/app/<id>` is reachable.

How a play could place a team CA (not tested):

- Keep the runtime's public roots and add the team CAs:
  `--env=SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt` (the runtime bundle; confirm the path
  exists in the KDE runtime) plus
  `--env=SSL_CERT_DIR=/home/<u>/.var/app/org.lightning_matrix.Lightning/teamchat-ca`, which holds
  one PEM per team. Both are read when both are set. The play could do this per launcher with
  `flatpak run --env=…`, or once with `flatpak override --user --env=…`.
- Do not set `SSL_CERT_FILE` alone to a file holding only the team CA. That replaces every public
  root, including the one the updater and any non-team server need.
- Writing into the sandbox's `/etc/ssl` is impossible, because the runtime is read-only. Whether
  the runtime's bundle already includes host-added CAs is not established, and I believe it does
  not. Flatpak's p11-kit forwarding serves PKCS#11 consumers such as NSS/GnuTLS. rustls reads PEM
  files and is not one of them. The same distinction is why `helpers/element_ca` (NSS) does not
  reach Lightning, as `play-matrix-clients.yml:56-58` already notes.
- Qt's OpenSSL backend (updater, `QNetworkAccessManager`) also honours `SSL_CERT_FILE` and
  `SSL_CERT_DIR` through OpenSSL. The team CA files are therefore harmless there.

Checking a handshake from the sandbox:

- Lightning has no "test server" CLI. The flags are listed in `src/main.cpp` preflight.
  `--rust-sdk-smoke-test` needs credentials, logs in and sends a message (`docs/build-and-test.md`
  ~259-312), so it is unsuitable.
- Practical check: the same pattern as `helpers/element_ca check`.
  `flatpak run --env=SSL_CERT_FILE=… --env=SSL_CERT_DIR=… --command=python3 org.lightning_matrix.Lightning <script>`.
  The script does an `ssl` handshake to the team URL with `cafile` and `capath` set to exactly
  those env values. Not established: that `python3` exists in `org.kde.Platform//6.11`. It is
  inherited from the freedesktop Platform, which I believe ships it; check with
  `flatpak run --command=python3 org.lightning_matrix.Lightning --version`.
- Caveat: Python verifies with OpenSSL, not webpki. That proves the CA files are readable from
  the sandbox, and that the chain verifies against them. webpki is stricter: it needs a SAN DNS
  name with no CN fallback and v3 CA basicConstraints. A team CA whose certificates pass with
  OpenSSL could still fail in Lightning if they lack a SAN. The check should also assert that the
  server certificate has a SAN for the host.

## 4. Threads and mentions

**Answer: threads yes, with `m.relates_to.rel_type = m.thread`. Mentions yes, with
`m.mentions.user_ids` plus a pill linking to `https://matrix.to/#/<percent-encoded user id>`.**
Confidence: high from code. A live check against pingbus is still owed (not tested).

- Threads: `rust/src/timeline.rs:1355-1410` (`send_thread_text`) sends through a matrix-sdk-ui
  `Timeline` built with `TimelineFocus::Thread { root_event_id }` (`timeline.rs:3184,3756`).
  matrix-sdk-ui 0.18.0 `src/timeline/mod.rs:320-370,405-430`: `Timeline::send` on a threaded
  timeline turns a relation-less message into a thread reply (`make_reply_event` with
  `EnforceThread::Threaded`), and a reply inside a thread uses `ReplyWithinThread::Yes`. That
  produces `rel_type: m.thread`, `event_id: <root>` and the `m.in_reply_to` fallback. The repo's
  rules (`CLAUDE.md` §8) forbid falling back to a plain room send. Attachments use
  `send_thread_attachment`.
- Mentions: `src/models/MentionTokenizer.cpp:82-86` writes a completed pill as a link to
  `https://matrix.to/#/` + `QUrl::toPercentEncoding(userId)`, so `@` and `:` are encoded
  (`%40…%3A…`). Element writes them unencoded. `RichComposerBridge.cpp:50-79` passes
  `mentionUserIds` for both room and thread composers. `timeline.rs:82-117`
  (`mentions_from_ids`, `outgoing_content`) sets `m.mentions.user_ids`, and `room: true` for
  `@room`. `m.mentions` is always present, even as `{}` (`timeline.rs:7966-7996`). Edits carry
  only newly added users at the top level (`:8000-8028`).
- The bus keys on `m.mentions` (`helpers/pingbus/protocol.py`,
  `src-go/internal/teamchat/proto/content.go:588`). Lightning's mentions should therefore reach
  agents. If any of our code ever matches the pill href instead, it must accept the
  percent-encoded form.
- One FFI path, `mx_rust_send_text` (`rust/src/lib.rs:3290-3335`), sends `Mentions::new()` (no
  user ids). I did not establish whether the composer uses it. The composer goes through
  `sendPrepared` with ids, so this looks like a legacy or HTTP-only path. The owner's trial
  should confirm with one real `@agent` message.

## 5. Project health

| Item          | Finding                                                                                                                                                                                                       | Confidence |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
| Age           | First commit 2026-07-04; 2873 commits by 2026-11-12                                                                                                                                                           | high       |
| Releases      | 42 tags. 0.5.14 (2026-07-13) to 0.10.5 (2026-11-08): weekly or faster, often several a week                                                                                                                   | high       |
| Contributors  | Effectively one maintainer, Rokas Smetonis: 2507 + 316 + 2 commits under his names. Next highest is 15.                                                                                                       | high       |
| Process       | Heavily agent-developed (`CLAUDE.md` is an agent guide). Canonical repo is the maintainer's GitLab; GitHub is a mirror where PRs are applied out-of-band and closed. History was rewritten once (2026-08-11). | high       |
| Status        | Self-declared **Beta**; "has not been formally security audited" (README)                                                                                                                                     | high       |
| Licence       | GPL-3.0-or-later (`LICENSE`, README)                                                                                                                                                                          | high       |
| Packaging     | Flathub `org.lightning_matrix.Lightning` (README badge). Since 0.10.0 the release pipeline opens the Flathub PR itself (`CLAUDE.md` §2). Also AppImage, deb, rpm (Fedora 44+), snap and Nix.                  | high       |
| E2EE          | matrix-rust-sdk 0.18.0 (`matrix-sdk`, `-ui`, `-base` exact-pinned); no own crypto. Recovery/key-backup flagged "shipped NOT TESTED" in its own docs.                                                          | high       |
| Store at rest | Message content, encrypted rooms included, is stored unencrypted on disk (local search index)                                                                                                                 | high       |
| Update check  | Anonymous check against the release host, on by default. A Flatpak build's in-app install is presumably deferred to Flathub; not verified.                                                                    | medium     |

## Gaps that need the host or the owner

1. Whether `flatpak run --env=XDG_*_HOME=` takes effect (one `sh -c 'echo'` check).
2. Whether `/etc/ssl/certs/ca-certificates.crt` and `python3` exist in the KDE 6.11 runtime.
3. A live round trip: Lightning sends a thread reply and an `@agent` mention, and pingbus is
   observed to receive both.
4. Whether the team server certificates carry SANs, because webpki is strict.
