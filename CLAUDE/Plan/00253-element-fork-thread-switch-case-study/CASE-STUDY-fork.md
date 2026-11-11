# Case study: forking Element to fix and adapt it

Read against element-web `develop` at `7ec1460387a3bcd79125265e7b2e90aef0040bbe` and the Flathub
manifest in `flathub/im.riot.Riot` at `c582b67b9f`. Companion to
[RESEARCH-thread-switch.md](RESEARCH-thread-switch.md), which holds the bug itself.

## Licence

`README.md` of element-web ("Copyright & License"): the software is multi licensed by New Vector
Ltd. It may be used (1) under the GNU AGPL, version 3 or later, or (2) under the GNU GPL, version 3
or later, or (3) under a paid Element Commercial License. The repository carries
`LICENSE-AGPL-3.0`, `LICENSE-GPL-3.0` and `LICENSE-COMMERCIAL`; `apps/desktop` carries the same
three. The `package.json` field says "SEE LICENSE IN README.md".

What a fork must do (a reading of the text, not legal advice):

- Using it privately, with no one outside the owner's own organisation receiving a copy or using it
  over a network, carries no source obligation under either licence.
- Handing the built app to other people is distribution: they are entitled to the corresponding
  source of the version they received, under the same licence, with the copyright and licence
  notices kept.
- Serving a modified web app to users over a network triggers AGPL section 13 (offer those users
  the source). Choosing the GPL option instead removes that clause. Because the licence is a
  choice of "AGPL or GPL", a fork may declare which one it takes.
- A public fork and a private one differ only in who may ask for the source; keeping the fork in a
  private repository is fine until a copy leaves the organisation.
- Upstream contributions are under a CLA, so a patch we send is relicensable by Element; a patch we
  only carry stays ours under the fork's licence.
- Trademarks and branding (the name and logo) are separate from copyright. I did not find or read a
  trademark policy, so treat renaming the app in a distributed fork as unverified and to be
  confirmed with the owner.

## Repository layout today

One pnpm and nx monorepo, `element-hq/element-web`:

| Path                                                                       | Holds                                                                       |
| -------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| `apps/web`                                                                 | Element Web (webpack build, vitest unit tests, Playwright end-to-end tests) |
| `apps/desktop`                                                             | Element Desktop (Electron, electron-builder, `src/`, `scripts/`)            |
| `packages/shared-components`                                               | The UI library `@element-hq/web-shared-components`; new views go here       |
| `packages/module-api`, `shared-utils`, `shared-types`, `playwright-common` | Published support packages                                                  |
| `modules/*`                                                                | Optional runtime modules (banner, restricted-guests, widget toggles, ...)   |

The standalone element-desktop repository is archived ("merged back into element-web"), and
matrix-react-sdk is gone (its code is `apps/web/src`). The one dependency that is still a separate
repository is matrix-js-sdk, pinned in `apps/web/package.json` as
`github:matrix-org/matrix-js-sdk#develop` (the lockfile fixes the commit). The thread fix in
RESEARCH-thread-switch.md touches only `apps/web/src`, so a patch carries no js-sdk work.

## The build

- Toolchain: `engines.node` is `>=22.18` at the root; Desktop pins `apps/desktop/.node-version`
  (26.13.0); `packageManager` is pnpm (version in the root `package.json`; the desktop package
  pins its own). Electron is 44.10.2 in `apps/desktop/package.json`.
- Web: `cd apps/web && pnpm dist` (`scripts/package.sh`) runs the webpack build into `webapp/` and
  tars it. Unit tests: `pnpm vitest run <path>`.
- Desktop: `apps/desktop/README.md` documents `pnpm install`, then `pnpm run fetch` (downloads the
  signed official web build into `webapp.asar`) or linking a local `../web/webapp`, then
  `pnpm run build` which runs electron-builder (Linux targets `tar.gz` and `deb`; also a Docker
  route `pnpm run docker:build`). `electron-builder.ts` refuses to build without `webapp.asar`.
- Native code: the only native module is Seshat (message search), consumed as a prebuilt npm
  package (`docs/native-node-modules.md`); building it yourself needs a Rust toolchain and is
  needed only for an unsupported platform.
- The point that matters for a web-side fix: the desktop app loads the web app out of
  `resources/webapp.asar` (`apps/desktop/src/asar.ts`). A web-only patch needs the webpack build and
  a repacked `webapp.asar`, not a rebuilt Electron.

## Flatpak of a fork

Flathub's `im.riot.Riot.yaml` does not build Element. It uses base `org.electronjs.Electron2.BaseApp`
(runtime 26.08) and one `simple` module that unpacks the official prebuilt
`element-desktop-<version>.tar.gz` from `packages.element.io` (a pinned URL and sha256 per
architecture, with a `x-checker-data` block for the bump bot), installs `element.sh`, the desktop
file, metainfo and icon, and runs `patch-electron-desktop-filename` on `app.asar`. So a Flatpak of a
fork needs only: the same manifest, with the two archive `source` entries replaced by our own
tarball (the `tar.gz` target of electron-builder, or the official tarball with its
`resources/webapp.asar` swapped for our build), built with `flatpak-builder` and installed from a
local or hosted repository. It must use a different application id or be installed in place of
the Flathub one, because the two cannot both own `im.riot.Riot` in one installation. Our existing
play (`tasks/element-desktop.yml`) installs `im.riot.Riot` from Flathub and adds the keyring
override that Flathub's manifest lacks; a fork Flatpak would repeat that override, and the team
profile and CA plays that depend on the app id (`play-agent-bus-element.yml`) would need the new
id.

## Release cadence and the cost of rebasing

Final releases on 2026-09-29, 2026-10-13, 2026-10-27 and 2026-11-11 (v1.12.30 to v1.12.34), each
preceded by an rc. Pull requests merge daily, and recent repository-wide commits (oxlint rollouts
on 2026-07-29, 2026-08-04 and 2026-08-13, the monorepo move on 2026-02-24) touched `ThreadView.tsx`
without changing behaviour. A patch of one function in two files conflicts only when upstream
rewrites those functions or files: `ThreadView.tsx` has had mechanical churn only since February
2026, and `RightPanelStore.ts` had feature work (document viewers, 2026-09 and 2026-11). Expect a
rebase per release that is mostly a no-op, and an occasional real conflict when the thread or
right-panel code is reworked.

## Signing, update and config.json

- Linux has no auto-update in the Flatpak: the updater starts only when `update_base_url` is set in
  `config.json` (`apps/desktop/src/electron-main.ts`, ~line 231); Flathub updates come through
  Flatpak. A fork Flatpak updates the way our repository updates it, so we would own that channel.
- Signing: Flathub's tarball is the official unsigned-by-us artefact, verified by sha256. Our fork
  tarball would be verified by our own sha256 in our manifest; the GPG key that `pnpm run fetch`
  checks belongs to Element's builds and is not needed when we build everything ourselves. macOS
  and Windows signing is not our concern.
- `config.json`: Desktop reads it from the webapp or the profile directory (`--profile` and
  `--profile-dir`, `apps/desktop/src/args.ts`; see `apps/desktop/src/config.ts` and the existing helper
  `helpers/agent_bus/element.py` which already writes locked-down profile config). Everything in
  `docs/config.md` is available without a fork.

## What "adapt for our needs" can cheaply mean

| Need                                                                                                        | Route                                                                 | Needs a fork?          |
| ----------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- | ---------------------- |
| Default homeserver, hidden settings, feature flags, `UIFeature` toggles, branding strings, custom home page | `config.json` (`docs/config.md`, `docs/custom-home.md`)               | No                     |
| Colours, fonts, a custom theme                                                                              | `custom_themes` (`docs/theming.md`)                                   | No                     |
| Hide the right panel, add dialogs, composer actions, document previewers, widget lifecycle                  | A module (`packages/module-api`, loaded from `config.json` `modules`) | No                     |
| Change how threads, the right panel or the room view behave                                                 | Patch to `apps/web/src`                                               | Yes                    |
| Change window handling, tray, protocol, updater                                                             | Patch to `apps/desktop/src`                                           | Yes (Electron rebuild) |

The module API exposes auth, client, composer, dialog, navigation, settings, stores, widgets and
document preview; a search of it found no thread or room-view-store hook, so it cannot reach the
bug in RESEARCH-thread-switch.md.

## Ranked recommendation, with maintenance cost

1. **Upstream pull request, and carry the same patch locally until it ships.** One patch of a few
   lines is the pull request. Cost: writing the Playwright test and the human-written PR description,
   plus following review; nothing after merge. If we carry it meanwhile, the local build is the
   webapp-only route below and ends the day the fix is in a release we take. Risk: upstream may sit
   on it, as it has on three issues; that only means we carry it longer.
2. **A small patch set on a fork, built by our IaC (web-only, `webapp.asar` swap).** A git
   repository that is a fork of element-web with our patches on top of release tags, built to a
   tarball and a Flatpak by a play. Recurring cost: rebase onto each release we choose to take (we
   need not take all), re-run unit and end-to-end tests, rebuild, and keep the build toolchain
   (node, pnpm, Electron base app) current; plus a channel and checksum to maintain for updates.
   Cost scales with patch count and with how close the patched files are to upstream's active areas.
3. **Module or `config.json` route.** The right choice for branding, theming, defaults and new
   dialogs or previews. Recurring cost: module API versions change between releases, so a module
   needs a compatibility check per release. It cannot fix this bug.
4. **Full fork** (diverging code, own releases, own Electron builds, own signing and update
   server). Recurring cost is a team's: tracking security releases of Electron, js-sdk and
   Element itself, triaging crypto fixes (end-to-end encryption bugs are security bugs), and
   merging upstream's roughly fortnightly releases. Not justified by one bug or by what we adapt
   today.

Verdict for this issue: route 1 with the webapp-only part of route 2 as the interim carrier, and
only if the owner wants the fix before upstream ships it. Everything else we currently want from
Element (profiles, locked-down config, CA trust) is already inside route 3.
