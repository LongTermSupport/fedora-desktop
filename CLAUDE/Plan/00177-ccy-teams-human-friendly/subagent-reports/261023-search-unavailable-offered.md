# Search: an action offered while the tool itself calls it unavailable

Worked from the class description only; the dispatch did not contain the rule or its pattern. Detector directories were not opened; the detector was not run. Paths are relative to src-go/internal unless stated. The Write tool was unavailable, so this file was written with printf.

## Forms expected before searching
1. A ui.Action or menu line always added while a header, heading or footer says the same action is unavailable (the originating shape).
2. A menu line offered unconditionally and refused after choosing (Refuse, Tell, sayf) on a precondition the tool already computed for the screen.
3. A choice shown with a reason (house shown-with-reason pattern): not the hazard.
4. A row hidden when unavailable (house hidden pattern): not the hazard.
5. A label computed by the same function the action uses (freeze): not the hazard.
6. Shell fzf/gum menus under files/ and scripts/; Python Textual apps.

## Instances

### Confident: carries the hazard

1. teams/picker/picker.go:187-197 against teams/picker/menu.go:115 (kindActions start, returned for team, human and member menus at 119-134). Found by text search (the word unavailable), then reading menu.go. pickerFrame sets newLine = "Ctrl-N unavailable: ..." for three cases: Checkouts.Find error, not a project folder (here.Top empty), not a host checkout (!here.Host()); r.header (picker.go:173-176) puts it on every list screen. kindActions adds actNew "Start a ccy session in <team>, in this folder" with key ctrl-n unconditionally to the team, person and member menus, and footerFor (menu.go:255) repeats it in the row footer. The refusal happens only after choosing, in teams/seats/seats.go:155-164 (NewSession). The same switch (Find error / Top empty / !Host) is written in pickerFrame and in NewSession; kindActions consults neither. This is the originating instance.

### Probable: same shape; precondition known to the tool, but no line says the word unavailable

2. teams/picker/menu.go:115 with teams/picker/picker.go:216-227 (listHeading, "not up: <teams>") against teams/seats/seats.go:166-172. A team whose homeserver is not up is named in the list heading and its row State shows it. Its menu still offers "Start a ccy session in <team>"; NewSession then refuses "<team> homeserver is down, so no session can join it." The state is in r.snap (StateOf). Found by reading. Unsure whether "not up" counts as the tool calling the action unavailable.

3. teams/picker/menu.go:190 (seatOwnActions: actAttach always added for Kind ccy) against teams/seats/seats.go:77-85 (AttachSeat refuses when row.Dir is "-" and when snap.Live[row.Session] is absent: "has no running session to attach. Ctrl-N starts one."). memberActions (menu.go:169) already tests Dir != "-" for its seat flag; seatOwnActions does not; snap.Live is available to the menu. Found by reading. Confident availability is decided twice; unsure a screen says unavailable (the row State and Dir columns show it).

4. teams/picker/sessions.go:134 (Attach always added in sessionChoices) against teams/seats/seats.go:94-95 (AttachSession refuses when clients != 0: open in another terminal). The same function hides Add, Change role, Move and Remove when !s.Controllable (121, 140) and its question says it cannot be changed while it runs (155-157); Attach is the one line not gated. Found by reading. Moderate. Contrast sessions/picker.go:187-189, which hides "Attach it here" when r.elsewhere().

### Weak: an offered no-op

5. teams/settings/settings.go:155-162 and 202-203: "Discard changes" is always listed; with no pending changes, choosing it sets the notice "Nothing to discard." Same for Save (388, "Nothing to save: the host_vars already had these values."). The pending count is already computed for the header (124). Found by reading. Probably outside the class (no unavailable statement); listed for completeness.

## Checked and found NOT to carry the hazard

- sessions/picker.go:114-143, 166-175, 205-211, 258-265 (newPickState, newAction, heading, noSessions, newHere): the new-session row is relabelled "Start a new ccy session here: unavailable, <why>" (136), the heading says no new session can start in X or X is not a project folder, noSessions does not offer the start. One computed unavailable/refusal drives all of them; newHere is the backstop. House shown-with-reason pattern.
- sessions/picker.go:184-201 sessionMenu: Attach hidden when open elsewhere. House hidden pattern.
- teams/picker/menu.go:167-182 memberActions (role action and Park hidden for a handleless member), 186-200 seatOwnActions (restore line only when recorded), 137-147 askAgainActions (only when the never marker is set), 151-159 humanActions: hidden pattern.
- teams/picker/sessions.go:168-211 option.refused, 272-299 teamOptions/cannotBeJoined, teams/picker/addrunning.go:52-110: a refused answer stays listed and is refused with the reason when chosen (addrunning.go:81 appends the reason to the line; sessions.go:200-205 says it and asks again, bounded by maxNoOps). Shown-with-reason pattern. teamLine (sessions.go:302) shows the homeserver state but the not-installed reason appears only on choosing.
- teams/chooser/list.go:161-165 blocked, 201-202: a down team is listed as (down) and refused with the reason. Shown with reason.
- teams/chooser/chooser.go:266-270, teams/find/find.go:65, teams/create/wizard.go:1044, teams/picker/offers.go:359-371 (offerJoinHere makes no offer in a non-host checkout): the check runs before anything is offered.
- teams/choose/choose.go:253-260 offerRole: a new seat gets no role question; gated before offering. teams/settings/settings.go:525-550: No coordinator row only when a holder exists.
- freeze/table.go:62-90 menu and screen: labels carry TargetEffect / RowVerb computed by the function the action runs ("so the two cannot disagree"). The best single-decision example. freeze/chooser.go: no mismatch.
- open/choose.go, ftpcamera/args.go, teams/ops/takedown.go (192, 397), teams/ops/playlock.go:237, teams/lxcadd/adder.go (316, 489, 567), teams/create/wizard.go:459, teams/chooser/widen.go:51, teams/picker/offers.go:88: every listed option was runnable and no header said otherwise (read as far as their choice lists).
- convert, ccylaunch, non-picker parts of open: no interactive menu.
- Shell: files/var/local/claude-yolo/lib/tmux-session.bash:215-262 lists only detached sessions as rows (open ones appear only in header text) plus a create row: no mismatch. files/home/bashrc-includes/history-search.bash and files/home/.local/bin/wsi-model-manager: plain lists. files/var/local/claude-yolo/lib/agent-bus-seats.bash (gum role chooser): grep found no unavailable/offered text; not read line by line.
- Python: helpers/qa_environment/textual_bindings.py and tests/fixtures/hidden-bindings are QA tooling, not product screens; helpers/github_signing/cli.py has no menu. No product Textual app found.

## House patterns for an unavailable action

- Hidden: build the menu from what applies (sessions/picker.go:187; teams/picker/menu.go memberActions; teams/picker/sessions.go:121 Controllable). Used for properties of the row.
- Shown with reason and refused on choosing: sessions/picker.go:134-136 plus heading plus newHere; teams/picker/sessions.go option.refused; chooser blocked. Used for properties of the folder or environment. In sessions/picker.go the reason is computed once (newPickState) and the label, heading, noSessions and newHere all read it.
- Single source: freeze/table.go label from TargetEffect, the function the action runs.
- The originating instance is where header and menu are built from different code.

## Attribution of findings to technique

Text search found: Ctrl-N unavailable (picker.go:190,192,194); unavailable/refusal in sessions/picker.go; every Host()/Refusal( call site (hostcheckout.go:46; sessions/picker.go:130; chooser.go:268; wizard.go:1044; find.go:65; offers.go:368; picker.go:193; seats.go:162), showing the same precondition tested in seven places; every ui.Action{ / Menus: / Choose( / PickSpec{ site, which gave the list of screens to read; the shell fzf/gum files and the Textual files. Text search could not check whether the menu agrees with the header.

Reading found (text search could not): instance 1 mismatch (the action list is unconditional while the unavailable text is built in another function with no shared string); instances 2, 3, 4 (preconditions are snap.StateOf, snap.Live, clients, never worded unavailable, and the refusal is in seats.go far from the menu); that memberActions seat flag tests Dir != "-" but seatOwnActions does not; Attach being the only ungated line in sessionChoices; instance 5; and that the freeze, chooser-list and option.refused screens are agreement patterns.

## Searches run

- grep -rniE "unavailable|not available|disabled|greyed|grayed|cannot|cant|unable" over src-go (first-party, vendor ignored)
- grep for ui.Action{, ui.Menu, Menus:, .Choose(, PickSpec{, Actions:
- grep for refused|Refuse(|.Refusal(|Host() over src-go/internal
- grep for gum choose/filter/confirm, fzf, whiptail, dialog, select, textual over files, scripts, helpers
- grep for textual|ListView|OptionList|Binding( over *.py
- Read: teams/picker/{menu,picker,sessions,offers}.go; sessions/picker.go; teams/seats/seats.go; teams/chooser/{list,chooser}.go; teams/choose/choose.go; teams/settings/settings.go; freeze/table.go; tmux-session.bash 215-262.

Saturation: the last three searches (Refuse/Refusal sites, Choose/PickSpec sites, shell menus) turned up nothing not already listed or ruled out. Not read in depth: teams/lxcadd, teams/ops, teams/create menu bodies, open/choose.go option lists, agent-bus-seats.bash.

Count: 1 confident, 3 probable, 1 weak (5 total).
