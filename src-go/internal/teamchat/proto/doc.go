// Package proto is teamchat's protocol: every rule of area A of the v2 design (Plan 00209,
// DESIGN-v2-A-protocol.md, cited "A n.n"), as pure functions. It does no I/O and reads no
// clock: a time it needs is an argument.
//
// What it owns, by file:
//
//   - strictjson.go: the strict decoder every read path uses (A 2.1) and the compact form
//     sizes are measured over.
//   - ids.go: identifiers and handles (A 2.2, 2.3).
//   - roster.go, limits.go: the roster event, its trust and limits blocks (A 5.1, 11.1), and
//     the rate rules over a given now (A 8.2-8.5, 11).
//   - rooms.go, verify.go, filter.go: the room specs C's engine applies (A 4, 5.2, 5.3), the
//     room-trust check (A 6.3, 6.4) and the sync filters (A 6.10).
//   - refs.go, verbs.go: references, the verb table and a reference's trust class (A 8.1, 9).
//   - content.go, send.go, secret.go, forms.go: the events a member sends, their send-side
//     refusals, the secret check and the body forms of a report, a question and a thread
//     (A 7, 10, 10.1-10.3).
//   - verdict.go: the one verdict over an event (A 7.6).
//   - questions.go: the question form and the question index (A 6.5, 10.2-10.5).
//   - escape.go, envelope.go: the body escaper over one Unicode version and an entry's lines
//     (A 13.2-13.4).
//   - delivery.go, membership.go: a delivery's shape, caps, order and trailing lines (A 13.1,
//     13.7), the MEMBERSHIP notice (A 13.3) and the frame text (A 13.5).
//
// The corpus in testdata/ is A 14's: JSON cases of (receiver, room, event) to their outcome
// in routing/, judged in the world of routing-world.json (the roster, rooms and store facts),
// a case's floodBefore being its sender's earlier admissions in the receive window. Three drop
// reasons have no rows: forge-rate, unresolved and provenance are judged by B, after the
// verdict, over the forge check proto hands it;
// the envelope golden files in envelope/; the escaper's character classes and cases in
// escape-classes.json; and the pinned sync filters. The Go test tables are its authoring form
// (`go test -update` rewrites it); every file is also walked and judged on its own. There is
// no second implementation to agree with; every case is written from the design.
package proto
