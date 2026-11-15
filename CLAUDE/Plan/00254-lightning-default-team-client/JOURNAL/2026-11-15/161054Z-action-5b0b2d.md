## 16:10 · action · —

Task 5.5 written, not yet run on a host. `acceptance.bash` (read-only, as the desktop user, no
sudo) runs `acceptance_lightning.py check`, which reads the host vars through the play's own
readers (`helpers.agent_bus.declare` for the lists and agent_bus_teams states,
`element.parse_profiles` for the entries and the client resolution, `element.check_user` for the
pingbus-bundle refusal; `agent_bus_matrix_client` read line by line in declare's subset) and
checks each lightning team's folders (0700), settings file (0600, url the base_url or its
canonical form), launcher (0644, the exact Exec line of lightning.go execLine), no Element
launcher, and for an https team its CA PEM and the tls-trust-check binary; each element team
has no Lightning launcher. The script then runs the handshake from Lightning's sandbox in the
play's `flatpak run --command=env` form. Exit 0/1/2; 2 when no team opens in Lightning.
Unit tests: 21 in `test_acceptance_lightning.py`, green. The handshake and verdict section was
dry-run in the container against a fake flatpak (one pass, one fail: exit 1). Queued in
meta-deploy.list as a plan-script entry after the play's, not as a plan-folder entry: that form
would also run `triage.bash` (the Task 3.1 window probe) and there is no `deploy.bash`.
Listing faults of `meta-deploy.bash --list` met here are logged on issue #259.
