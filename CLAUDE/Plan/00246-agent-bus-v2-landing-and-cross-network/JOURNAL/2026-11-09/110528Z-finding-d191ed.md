## 11:05 · finding · —

Two spec points from the first real bus server host arrived on #193 for stage A.

X3 stays as written. Its host has one NIC on its own segment and reaches each member network by
one explicit route through the segment's gateway, on the NIC that also holds the default route.
The existing tests covered it only as a row of a table (two networks on one interface). Two named
tests now hold it, each red against a mutation made for the purpose: the route reader does not
mark an explicit route as the default's because the interface is the same
(`TestAnExplicitRouteThroughTheDefaultInterfaceIsNotTheDefaultRoute`), and `team apply` serves such
a network by the NIC's address, which is not inside the network, and refuses the same network
when only the default route covers it (`TestAMemberNetworkRoutedThroughTheDefaultInterfaceIsServedByIt`).
Mutations: the reader marking every route of `eth0` as default failed the first; `serve` refusing
every network on `eth0` failed the second. Both reverted; the tree was checked clean after.

X21 is inverted. Nothing ssh-es out of the bus server's host, and its owner allows no standing
NOPASSWD, so the document is `member add`'s stdout through an ssh into the host, from the member
machine (pull) or the owner's machine (relay), with the sudo password on the ssh's stdin
(`sudo -S -p ''`), no tty. SPEC X21 and its section 7 question 5, `docs/agent-bus-cross-network.md`
and the lab scenario of INSTALL-PATH.md carry the forms. The lab proves the relay form with the
guest's outbound ssh blocked and a password sudo; the guest-prepare fixture sets the throwaway
password. No code changed: `member add` reads no stdin (read in `admin/`: only `team apply` and
`team approve`, `lxc apply`, the element verbs and the control child read the admin's stdin) and
`seat place-issued` reads one issued document, so both forms work as written by the code; this is
a spec and docs change plus a scenario.

Not proven here: the forms themselves, which need a real ssh and sudo (the lab scenario).
