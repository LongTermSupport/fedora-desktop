## 15:44 · finding · —

Upstream: text selection cannot cross a message boundary, reported as Mizerd/lightning#78 with the owner's go. The owner met it in a thread; the code shows it is general: every message body is its own read-only TextEdit inside MessageDelegate, which both ThreadPanel and TimelinePane use as their delegate, so a drag selection stays inside one message. The issue suggests a range "copy messages" action on the existing message selection mode as the cheap fix. Watched beside #75, #76 and PR #77.
