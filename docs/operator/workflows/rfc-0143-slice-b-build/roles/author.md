# Author Role

You are the implementation author for RFC 0143 Slice B. Build the narrowest
`CapabilityReseal` path that satisfies D261 on top of RFC 0168 per-lane uid
leases, and keep source, tests, docs, and required artifacts inside the declared
write scope.

Favor current source and current docs over older same-uid design text. When a
tradeoff touches credential authority, fail closed and document the constraint
instead of widening the trust model.
