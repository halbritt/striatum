# Logistics Divergence Ideas
author: diverger-reviewer-1-001

- **Cold-chain context crates:** Each campaign stage travels in a sealed context crate with freshness labels for accepted arc, current blockers, deferrals, evidence handles, and expiry; stale crates route to quarantine instead of being opened by the next lane.

- **Returns desk for deferrals:** Every deferral gets a return label with reason, originating stage, owning reviewer, re-entry dock, and expiration; the campaign cannot show delivered while unresolved return labels remain on the manifest.

- **Cross-dock handoff bay:** Design, build, verify, and slice-discovery lanes drop standardized manifests into a transfer bay, where the next fresh-context lane receives only the current crate, exception slips, and required proof handles.

- **Yard-control map for active arcs:** The supervisor views each RFC arc as a trailer in a yard slot: loading, sealed, delayed, damaged, customs-held, or ready for dispatch; movement between slots is a daemon action with an attached evidence ticket.

- **Milk-run slice pickup:** Discovered follow-up slices wait at scheduled pickup stops with small manifests, cutoff times, and acceptance stamps; the coordinator batches pickup into the next arc revision instead of starting ad hoc work immediately.

- **Customs broker for authority crossings:** Any scope expansion, irreversible action, new daemon authority, or ticket substrate change enters a customs lane with declared cargo, required papers, hold reason, and human release stamp before downstream work proceeds.