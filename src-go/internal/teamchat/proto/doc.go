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
//   - content.go, send.go, secret.go: the events a member sends, their send-side refusals and
//     the secret check (A 7, 10).
//   - verdict.go: the one verdict over an event (A 7.6).
//   - questions.go: the question form and the question index (A 6.5, 10.2-10.5).
//   - escape.go, envelope.go: the body escaper over one Unicode version and the delivery
//     envelope (A 13).
//
// The corpus in testdata/ is A 14's: JSON cases of (roster, room, receiver, event) to their
// outcome, the envelope golden files and the pinned sync filters. There is no second
// implementation to agree with; every case is written from the design.
package proto
