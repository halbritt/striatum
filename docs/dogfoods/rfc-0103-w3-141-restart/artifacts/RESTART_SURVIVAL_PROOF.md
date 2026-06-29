# RFC 0103 W3 (#141) — Daemon-Restart Survival Proof

author: author-claude-opus-4.8-001

This note was authored by a Claude lane that bootstrapped under the Striatum
runner, claimed a work packet, and completed the job through the runner's
production daemon handlers on 2026-06-02.

It corroborates RFC 0103 W3 (#141): a supervised lane now survives a
`systemctl restart striatumd` issued mid-run. The `striatumd` systemd unit uses
`KillMode=process`, so restarting the daemon no longer orphans or kills the
lane's supervisor helper and PTY — the lane keeps running and finishes once the
daemon comes back.

The runner moved this job through daemon state transitions (claim → ack →
publish → complete) over the live control plane — not by scraping terminal
output or printing phrases. The artifact's content hash and the daemon's
recorded job state are the authoritative evidence.

_Evidence: run `run_425e58fc11a1e3c86ad97cc96313c212`, session
`sess_9173ec7616e6d48d1bb540af234960c5`, lane `claude`, supervisor
`sup_7b9b981e094f20a363f516c7d80405a6`._
