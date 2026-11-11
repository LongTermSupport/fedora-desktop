# Element on Android against a private-CA Tuwunel behind WireGuard: research

Research date 2026-11-11. Raw captures are in `untracked/scratch/` (gitignored). Each fact carries its URL. "NOT CONFIRMED" marks what I could not verify.

## Verdict

Element X Android is the right client. It trusts user-installed CAs (explicit opt-in in its manifest config), it logs in with a password, and Tuwunel serves the sliding sync it needs. Installing the private CA as a user CA on the phone is enough, with no public cert required. Push is the only real design question (section 4).

## 1. User-installed CAs, trust prompts, maintenance

| Client                    | Trusts user CAs?                                                                                                                                                                                               | Evidence                                                                                                                                                                                                                                                                           |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Element X Android         | Yes. `base-config` lists both `<certificates src="system" />` and `<certificates src="user" tools:ignore="AcceptsUserCertificates" />`.                                                                        | https://raw.githubusercontent.com/element-hq/element-x-android/develop/app/src/main/res/xml/network_security_config.xml and https://github.com/element-hq/element-x-android/blob/develop/app/src/main/AndroidManifest.xml (`networkSecurityConfig="@xml/network_security_config"`) |
| Element Android (classic) | Release builds: no. `base-config cleartextTrafficPermitted="false"` has no `trust-anchors`, so the platform default applies (system only for targetSdk 24+). `src="user"` appears only in `<debug-overrides>`. | https://raw.githubusercontent.com/element-hq/element-android/develop/vector/src/main/res/xml/network_security_config.xml. The platform default is at https://developer.android.com/privacy-and-security/security-config (see "base-config").                                       |

- **Classic fingerprint prompt:** classic Element has its own in-app "unrecognised certificate" dialog that shows the SHA-256 fingerprint and lets the user trust it. See `UnrecognizedCertificateDialog.kt` and the SDK's `Fingerprint.kt` and `UnrecognizedCertificateException.kt` in https://github.com/element-hq/element-android (paths: `vector/src/main/java/im/vector/app/core/dialogs/`, `matrix-sdk-android/.../network/ssl/`).
- **Classic prompt, unconfirmed:** I did not run the classic app against a private CA. Whether the dialog still triggers in the current release is NOT CONFIRMED. The code exists on `develop`.
- **Element X prompt:** I found no equivalent in-app "trust this certificate" prompt in Element X. Its path is the Android user CA store, which is a source-level finding rather than a tested one.
- **Element X verifier, unconfirmed:** the Element X config comment says the cleartext CRL domains were imported from rustls-platform-verifier. That crate delegates to the Android trust manager on Android (https://github.com/rustls/rustls-platform-verifier README, "Android System Trust Store / Android Trust Manager"). Whether the Matrix Rust SDK's HTTP client uses that verifier, and so honours user CAs end to end, is NOT CONFIRMED. I did not read the Rust SDK source. A real device test is the proof.
- **Classic maintenance:** still published, in maintenance only. The README says it "is still supported and receives security updates but no new features or usability enhancements are made" and recommends Element X (https://github.com/element-hq/element-android). Latest release v1.6.66, published 2026-09-25 (https://api.github.com/repos/element-hq/element-android/releases/latest).
- **Element X release:** v26.10.1, 2026-10-23 (https://api.github.com/repos/element-hq/element-x-android/releases/latest). Minimum Android 7.0 / API 24 (https://github.com/element-hq/element-x-android).

## 2. Sliding sync and password login

- **Sliding sync:** Element X builds its client with simplified sliding sync ("Creating client with simplified sliding sync", `slidingSyncType = ClientBuilderSlidingSync.Discovered`) in https://raw.githubusercontent.com/element-hq/element-x-android/develop/libraries/matrix/impl/src/main/kotlin/io/element/android/libraries/matrix/impl/auth/RustMatrixAuthenticationService.kt. The server must therefore offer MSC4186. Spec: https://github.com/matrix-org/matrix-spec-proposals/blob/main/proposals/4186-simplified-sliding-sync.md (a Gitea mirror of the same proposal appeared in my search results).
- **Tuwunel, native:** yes.
  - v1.3.0 removed legacy sliding sync in favour of simplified sliding sync.
  - v1.4.3.1 (2025-10-11) is the first release that says "Sliding Sync has been fixed (Element X)" with a new MSC4186 architecture.
  - v1.4.4.0 and v1.4.5 note that Element X and Element Pro deployments must be on the patch.
  - Current stable v1.9.4 (2026-10-10) keeps fixing Element X behaviours. A v2.0.0 notes entry is published as `v2.0.0-assets` (2026-11-10). It is only a prerelease asset tag, so v2.0.0 is not confirmed as a stable release.
  - Source for all of the above: https://api.github.com/repos/matrix-construct/tuwunel/releases (also https://github.com/matrix-construct/tuwunel/releases).
  - Earliest usable version: use at least 1.4.5, and any current release is better. A fresh install should be on 1.9.x or newer.
- **How the server advertises it:** NOT CONFIRMED. `src/api/client/versions.rs` (https://raw.githubusercontent.com/matrix-construct/tuwunel/main/src/api/client/versions.rs) lists spec versions up to v1.19, and I found no sliding-sync flag in it. I did not locate where Tuwunel advertises simplified sliding sync. The release notes state it works with Element X, so test `/_matrix/client/versions` against the real server.
- **Password login without OIDC/MAS:** yes.
  - Element X has a password login path: `login(username, password)` with `LoginType.PASSWORD` in `RustMatrixAuthenticationService.kt` (above), plus `LoginPasswordPresenter` in `features/login/impl/.../screens/loginpassword/`.
  - Tuwunel's password login is the default; OIDC is optional. Registration is off by default, and accounts are created by admin or token (https://raw.githubusercontent.com/matrix-construct/tuwunel/main/docs/authentication/legacy.md).
  - Tuwunel's own OIDC server needs `oidc_native_auth` (default `false`, per v1.9.1 notes).
  - Not tested: a live Element X password login against Tuwunel with OIDC off. Element X's own OAuth doc is https://github.com/element-hq/element-x-android/blob/develop/docs/oauth.md.

## 3. Installing a user CA on Android 14, 15 and 16

- **App trust rule:** apps targeting API 24 (Android 7.0) and up trust only system CAs by default. Apps targeting 23 and lower also trust the user store. An app must opt in with `<certificates src="user"/>` (https://developer.android.com/privacy-and-security/security-config). Element X opts in (section 1). Classic release builds do not.
- **Certificate transparency:** the same page says CT is not checked for user or inline trust anchors by default. It is opt-in on Android 16 and on by default from Android 17, but off for connections using a user CA unless the app enables it. No concern for this setup.
- **Install path:** Google's Pixel page documents only the "Wi-Fi certificate" path, Settings > Security & privacy > More security settings > Encryption & credentials > Install a certificate (https://support.google.com/pixelphone/answer/2844832). The "CA certificate" entry sits in the same menu, but that page does not describe it. That is secondary-source level (web search results from several guides, e.g. https://httptoolkit.com/blog/android-11-trust-ca-certificates/ and https://docs.start9.com/0.3.5.x/device-guides/android/ca-android). Menu names vary by vendor.
- **Install flow, from those secondary sources:**
  1. Put the CA's public cert file on the phone.
  2. Open the install path above.
  3. Choose "CA certificate".
  4. Accept the "Install anyway" warning ("CA certificates can put your privacy at risk" per the search summary of those guides).
  5. Pick the file.
- **Lock screen:** a PIN, pattern or password must be set. Google's page says the device asks you to create one if there is none.
- **"Network may be monitored":** I could not find the exact current wording in a primary Google source. NOT CONFIRMED. Expect a persistent notification or warning that the network may be monitored, and a warning screen at install time.
- **Android 11+:** an app cannot install a CA silently. The system installer refuses unless Settings launched it (https://httptoolkit.com/blog/android-11-trust-ca-certificates/, via search results). The user must do it in Settings.
- **Android 14+:** system CAs now live in an updatable APEX store (https://httptoolkit.com/blog/android-14-install-system-ca-certificates/ was a 404 on fetch). I did not confirm this, and it only matters for rooted system-store injection, which is not the plan.
- **Remove or inspect:** Settings > Security > Encryption & credentials > "Trusted credentials", "User" tab. Not confirmed on all vendors.
- **Limit:** only apps that opt in trust it. Element X does. Chrome and others do (Chrome uses the platform store). A managed (MDM) phone can block user CA installs. NOT CONFIRMED for the owner's phone.

## 4. What Element needs beyond the client API; push

- **Server contact:** the client talks to the homeserver base URL only. Typing the homeserver URL directly (for example `https://matrix.team.internal`) works without `.well-known` if the server name equals the host. If the server name is a bare domain, serve `/.well-known/matrix/client` (Tuwunel `[global.well_known] client = ...`; see https://raw.githubusercontent.com/matrix-construct/tuwunel/main/docs/deploying/root-domain-delegation.md). Element X's exact discovery order was not read, so treat the direct-URL case as likely but NOT CONFIRMED for X.
- **No public reachability needed for messaging.** Federation is irrelevant.
- **Push model, Tuwunel:** Tuwunel implements the homeserver side of the push gateway API and posts events to the pusher's gateway URL. It needs only outbound reach to the gateway (https://raw.githubusercontent.com/matrix-construct/tuwunel/main/docs/pushers.md). The homeserver does not need to be on the public internet inbound. The phone only needs to receive the wake-up from the gateway.
- **Gateways Element X uses by default:**
  - Firebase: pusher URL `https://matrix.org/_matrix/push/v1/notify` (Sygnal run by matrix.org; `FirebaseConfig.kt`).
  - UnifiedPush default gateway: `https://matrix.gateway.unifiedpush.org/_matrix/push/v1/notify` (`UnifiedPushConfig.kt`). Both files are under `libraries/pushproviders/` in https://github.com/element-hq/element-x-android.
- **Payload and privacy:** the default gateways receive only event and room IDs. The app then syncs to fetch and decrypt the event ("Push (FCM) received in background" in https://github.com/element-hq/element-x-android/blob/develop/docs/notifications.md; that document describes Element Android and is stored in the Element X repo). So the phone needs the WireGuard tunnel up at push time. With the tunnel down (no always-on), the push arrives but the notification text cannot be resolved. This is a design inference from that doc, not a tested result.
- **Pushing to a private gateway:** Tuwunel refuses private and loopback destinations by default (`ip_range_denylist`); a self-hosted gateway on a private range needs the denylist narrowed. Plain `http://` gateways are accepted (pushers.md above).
- **UnifiedPush and ntfy:** Tuwunel supports it out of the box. ntfy is a gateway at `/_matrix/push/v1/notify` (pushers.md; ntfy docs https://docs.ntfy.sh/config/#example-unifiedpush). A ntfy topic with no subscriber for 4 days returns 410, and Matrix gets its pushkey rejected (ntfy docs, same page). Element X ships a UnifiedPush provider; the user installs a distributor app (ntfy app) (https://unifiedpush.org/users/distributors/).
- **Classic UnifiedPush:** classic Element supports it too, both F-Droid and Play flavours (https://github.com/element-hq/element-android/blob/develop/docs/unifiedpush.md).
- **With no gateway reachable at all:**
  - Classic, F-Droid flavour: a polling service via AlarmManager. Doze limits it: about every minute at best, about every 15 minutes in low power mode.
  - Classic, Play flavour: a fallback periodic sync when FCM fails. Doze mode stops background sync entirely.
  - Source: https://github.com/element-hq/element-x-android/blob/develop/docs/notifications.md.
- **Element X without a distributor:** NOT CONFIRMED what it does on a phone with neither Google services nor a distributor.
- **Recommendation to verify:** two workable options.
  - (a) Stock Play build with FCM via matrix.org's Sygnal. Needs outbound internet from the homeserver, no inbound.
  - (b) A team-run ntfy as UnifiedPush gateway. Needs an `ip_range_denylist` change if ntfy is on a private range, and a reachable ntfy endpoint for the phone.

## 5. WireGuard Android

- **Split tunnel by AllowedIPs:** the Android app turns each AllowedIPs entry into a VpnService route (`builder.addRoute(addr.getAddress(), addr.getMask())`), so listing only the team's ranges in AllowedIPs sends only that traffic through the tunnel. Source: https://github.com/WireGuard/wireguard-android/blob/master/tunnel/src/main/java/com/wireguard/android/backend/GoBackend.java (line 320). Routing semantics: https://developer.android.com/develop/connectivity/vpn ("addRoute ... Routes filter by destination addresses").
- **App-level split:** the same file calls `addDisallowedApplication` and `addAllowedApplication` (lines 301 to 304), which are the app's excluded or included applications.
- **Always-on:** Android 7.0+ feature set in Settings > Network & internet > VPN. "Block connections without VPN" blocks non-VPN traffic, and Settings warns that you lose connectivity before the VPN connects (https://developer.android.com/develop/connectivity/vpn#always-on).
- **Caution:** with "Block connections without VPN" on and a split-tunnel config, everything not routed through the tunnel is blocked, which breaks general internet use. Use always-on without the block option for a split-tunnel setup. This follows from the Android doc's "Blocked connections" caution and is not tested.
- **WireGuard-specific wording:** the wireguard.com xplatform and quickstart pages did not describe Android always-on or AllowedIPs behaviour (fetched, no mention). NOT CONFIRMED beyond the source and Android doc above.
- **Note on pushes:** with split tunnel, the gateway traffic (FCM or ntfy) goes over the normal network, and only the homeserver traffic goes over WireGuard.

## 6. A publicly trusted certificate instead

- **DNS-01:** Let's Encrypt validates by a TXT record at `_acme-challenge.<name>`, so the web server need not be publicly reachable; wildcards are allowed; IP addresses cannot be validated (https://letsencrypt.org/docs/challenge-types/). That page does not address names resolving to private IPs. In practice the validation never connects to the address (my reading of that page), so a public DNS name pointing at a private IP works. This is the usual practice but is not stated on that page.
- **What it would avoid:**
  - Installing the CA on each phone, the lock-screen requirement and the monitoring warning.
  - Dependence on the app opting in to user CAs. This would make classic Element Android work too, and any other client or browser, with no per-device step.
  - Issuer rotation: trust no longer follows the team CA key.
- **What it would not avoid or what it costs:**
  - The name goes into public Certificate Transparency logs (leaks the internal hostname).
  - It needs a public DNS zone and an ACME DNS credential.
  - It does not change the WireGuard or push questions.
  - Phones still need the tunnel. The CA-based path stays valid for other uses.
- **Not read in detail:** Let's Encrypt's CT policy. Treat the CT leakage as general knowledge, NOT CONFIRMED here.

## Not confirmed (summary)

1. Element X with a private CA on a live device, and whether the Rust SDK HTTP client honours user CAs end to end.
2. The exact current "network may be monitored" text and the "CA certificate" install steps on Android 14 to 16 from a primary Google source.
3. How Tuwunel advertises simplified sliding sync to clients (not in `versions.rs`).
4. Element X's behaviour with no push distributor and no Google services.
5. Whether classic Element's fingerprint dialog still works in v1.6.66.
6. A Tuwunel v2.0.0 stable release (only `v2.0.0-assets` seen).
7. Whether the owner's phone is MDM-managed or restricts user CA installs.
