## 13:08 · decision · — — Decision 2 settled (host); decision 3 reworded as the owner's own-account ban risk

The owner questioned decisions 2 and 3, and was right about both.

**Decision 2: "where else could it run?"** The only alternative was inside the container, with the token in container tmpfs. That breaks the owner's first requirement: ccy never sees a real token. It was never a real choice, so it is recorded as decided: on the host.

**Decision 3: "the terms? which other account could we be talking about?"** It was badly worded. No one else's account is involved; every token belongs to one of the owner's own subscriptions. The real question is the account-ban risk of one person rotating several subscriptions to get past usage limits:

- the legal page says advertised limits "assume ordinary, individual usage";
- community proxies warn that this "may violate the ToS" and report bans for "unusual OAuth usage patterns".

It is reworded as a risk the owner chooses to take, not a legal reading. It gates only Task 2.4 (automatic `ccy-teams` failover). Phase 0, its spike and the manual front ends do not wait on it.

Decisions 4 to 8 keep their recommendations. They shape the later phases, not Phase 0.
