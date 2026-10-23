// Package failpoint is the member's named crash points (Plan 00209 SPEC-n3 gap 9, PT1): Hit
// at a point in the keeper or the link, and, in a build with the busfailpoints tag, a point
// named in TEAMCHAT_FAILPOINTS (a comma list) ends its own process with SIGKILL there, as a
// crash would. Without the tag Hit is an empty function the compiler removes, and a gate test
// holds that a release build of teamchat holds no point's name.
//
// Each slice adds the points of the code it builds; this file is the one list.
package failpoint

// Name is a failpoint's name.
type Name string

// n3c's points: PT1's list, and n3c-D2's crash after a judged batch and before its write.
const (
	KeeperLinkWriting           Name = "keeper-link-writing"
	LinkInRemove                Name = "link-in-remove"
	LinkAfterRemoveAnswered     Name = "link-after-remove-answered"
	LinkAfterHandOut            Name = "link-after-hand-out"
	LinkWhileHanded             Name = "link-while-handed"
	KeeperBeforeStoreDelete     Name = "keeper-before-store-delete"
	LinkMidEndSends             Name = "link-mid-end-sends"
	KeeperBeforeEnd             Name = "keeper-before-end"
	LinkAfterJudgeBeforePersist Name = "link-after-judge-before-persist"
)
