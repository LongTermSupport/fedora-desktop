I recommend matching the tab by its title. It can be done, but only best-effort: Ptyxis gives no outside way to read which tab is active.

**1. How the window is remembered today.** When Insert is pressed, `_launchFor()` stores `global.display.focus_window`, a Meta.Window object (`extensions/speech-to-text@fedora-desktop/extension.js:1038`). `PasteTargetPin` pins it and listens for the window closing (`extensions/speech-to-text@fedora-desktop/pasteTarget.js`). Before each paste, the Go recorder (`src-go/internal/wsi/paste.go`) calls the panel's D-Bus `PasteKey` method (`extension.js:1213`). If the pinned window has lost focus, the extension calls `Main.activateWindow()` on it and the recorder asks again. The keys are then sent with `ydotool`. Nothing about tabs is recorded, so the pin is the whole Ptyxis window.

**2. What Ptyxis exposes from outside.**
- **Select a tab by UUID: yes.** Ptyxis has an app action `focus-tab-by-uuid(s)` (`src/ptyxis-application.c`). Standard GApplication behaviour would make it callable through `org.gtk.Actions.Activate` on `/org/gnome/Ptyxis`; I did not test that call.
- **Read the active tab or list tabs: no.** Ptyxis exports no D-Bus interface of its own. `ptyxis_window_get_active_tab()` and `ptyxis_window_list_pages()` are internal only. The tab UUID is random (`g_uuid_string_random`) and I found nothing that puts it in the window title. Whether it reaches the shell's environment I could not see: the spawn code was not in what I fetched.
- **Select a tab by number:** `tab.focus(i)`, `page.next` and `page.previous` are widget actions inside the window. They are not exported on D-Bus, so they can only be reached through keyboard shortcuts.
- **Window title: yes.** It is bound to the active tab's title (`ptyxis-window.c`). That title is the terminal title the program sets, or the command name, plus " — <command>" while a foreground program runs (`ptyxis-tab.c`). The extension can read it with `Meta.Window.get_title()`.
- **Accessibility tree: probably no good.** libadwaita gives each tab the TAB role, but I found no accessible "selected" state or title on it (`adw-tab.c`, `adw-tab-box.c`).

**3. Cheaper routes.** Typing straight into the tab's terminal is closed: modern kernels disable TIOCSTI injection and Ptyxis has no way to send text in. What remains is matching titles. At Insert, record the window title (the active tab's). At paste, if the pinned Ptyxis window is back in focus but its title differs, press next-tab (Ctrl+PageDown, default not confirmed from source) and compare titles. Do one full round of the tabs, and paste only if exactly one tab matches.

**4. Versions.** `vars/fedora-version.yml` pins `fedora_version: 44`. Fedora 44 ships GNOME 50. Ptyxis is 50.1-2.fc44 in stable updates. The extension's metadata already covers shell 45–50.

**Recommendation:** title matching, only for `org.gnome.Ptyxis`, inside the existing `PasteKey` loop. The tab keys could be sent from the extension through Clutter's virtual input device, or by the recorder with ydotool.
- **Cost:** small. About one new JS module with a unit-testable matcher, a new "tab" answer from `PasteKey`, and a setting to turn it on.
- **Risks:**
  - Tabs with the same title are ambiguous, so the safe answer is not to paste and leave the text on the clipboard with a notification.
  - Claude Code changes its terminal title while it works (spinner glyph, a new topic). Matching should ignore a leading non-alphanumeric prefix, but a topic change mid-dictation will still miss.
  - The user sees the tabs flip, and could type during the round.
  - Changed default shortcuts would break the tab keys.

**The reliable fix is upstream:** ask Ptyxis to expose the active tab's UUID, for example as an environment variable or a D-Bus property, then restore it with `focus-tab-by-uuid`. That is worth filing alongside the title-matching work.

Sources:
- [ptyxis-window.c](https://gitlab.gnome.org/chergert/ptyxis/-/raw/main/src/ptyxis-window.c)
- [ptyxis-application.c](https://gitlab.gnome.org/chergert/ptyxis/-/raw/main/src/ptyxis-application.c)
- [ptyxis-tab.c](https://gitlab.gnome.org/chergert/ptyxis/-/raw/main/src/ptyxis-tab.c)
- [libadwaita adw-tab.c](https://gitlab.gnome.org/GNOME/libadwaita/-/raw/main/src/adw-tab.c)
- [libadwaita adw-tab-box.c](https://gitlab.gnome.org/GNOME/libadwaita/-/raw/main/src/adw-tab-box.c)
- [Fedora ptyxis package](https://packages.fedoraproject.org/pkgs/ptyxis/ptyxis/)
- [Fedora 44 / GNOME 50 (debugpoint)](https://www.debugpoint.com/fedora-44-features/)
- [ptyxis(1) man page](https://man.archlinux.org/man/ptyxis.1.en)