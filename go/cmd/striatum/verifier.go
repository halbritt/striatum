package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/cli/rpcclient"
	"github.com/halbritt/striatum/go/pkg/verifier"
)

// runVerifier implements `striatum verifier run`: the LANE-SIDE entrypoint of
// the RFC 0134 / D227 executable verification half. It runs INSIDE the
// disposable sandboxed verifier lane (a supervised subprocess off the daemon's
// gate path) — never in the daemon. It:
//
//  1. resolves the named check against the operator-curated, content-addressed
//     allowlist (a workflow NAMES a check; it never AUTHORS the executed bytes),
//  2. verifies the resolved binary's sha256 against the pinned hash,
//  3. runs the check TWICE under the strictest available sandbox envelope (the
//     two-signal independent re-execution agreement), and
//  4. mints a tamper-evident receipt.v1 (argv + binary sha + exit code + stdout
//     digest + cwd tree-sha + seal), writing it to --out for the lane to publish
//     as a `receipt` artifact.
//
// The daemon later READS that sealed receipt to allow a claim's VERIFIED status;
// it never re-runs the check. This command performs the only command execution
// in the whole feature, and it is firmly off the gate path.
//
// Flags:
//
//	--allowlist <path>  operator-curated, git-tracked allowlist JSON (required)
//	--check-id   <id>   the allowlist check to run (required)
//	--cwd        <dir>  worktree the check runs against (default: $PWD)
//	--scratch    <dir>  the single writable scratch dir inside the sandbox
//	--out        <path> where to write the minted receipt.v1 markdown (required)
//	--json              emit a machine-readable summary to stdout
func runVerifier(args []string, stdout io.Writer, stderr io.Writer, repoRootOverride string) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: striatum verifier <run|pin|attest> ...")
		return 2
	}
	switch args[0] {
	case "pin":
		return runVerifierPin(args[1:], stdout, stderr, repoRootOverride)
	case "attest":
		return runVerifierAttest(args[1:], stdout, stderr, repoRootOverride)
	case "run":
		// fall through to the run implementation below.
	case "-h", "--help":
		_, _ = fmt.Fprintln(stdout, "usage: striatum verifier <run|pin|attest> ...")
		_, _ = fmt.Fprintln(stdout, "  run     run an allowlisted/builtin check under the strict sandbox and mint a receipt")
		_, _ = fmt.Fprintln(stdout, "  pin     (in the lane) observe each sanctioned binary's sha and write the per-host pins")
		_, _ = fmt.Fprintln(stdout, "  attest  (operator only) bless pinned bytes so a claim can reach VERIFIED")
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "verifier: unknown subcommand %q (want run|pin|attest)\n", args[0])
		return 2
	}
	var (
		allowlistPath string
		intentPath    string
		pinsPath      string
		attestPath    string
		checkID       string
		cwd           string
		scratch       string
		outPath       string
		jsonOut       bool
	)
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--allowlist":
			i++
			if i < len(rest) {
				allowlistPath = rest[i]
			}
		case "--intent":
			i++
			if i < len(rest) {
				intentPath = rest[i]
			}
		case "--pins":
			i++
			if i < len(rest) {
				pinsPath = rest[i]
			}
		case "--attest":
			i++
			if i < len(rest) {
				attestPath = rest[i]
			}
		case "--check-id":
			i++
			if i < len(rest) {
				checkID = rest[i]
			}
		case "--cwd":
			i++
			if i < len(rest) {
				cwd = rest[i]
			}
		case "--scratch":
			i++
			if i < len(rest) {
				scratch = rest[i]
			}
		case "--out":
			i++
			if i < len(rest) {
				outPath = rest[i]
			}
		case "--json":
			jsonOut = true
		case "-h", "--help":
			_, _ = fmt.Fprintln(stdout, "usage: striatum verifier run --check-id <id> --out <path> [--allowlist <path> | --intent <path> [--pins <path>] [--attest <path>]] [--cwd <dir>] [--scratch <dir>] [--json]")
			_, _ = fmt.Fprintln(stdout, "  builtin:* check ids self-pin and need NEITHER --allowlist NOR --intent (runnable out of the box, capped at ASSERTED).")
			return 0
		default:
			_, _ = fmt.Fprintf(stderr, "verifier run: unknown flag %q\n", rest[i])
			return 2
		}
	}
	if strings.TrimSpace(checkID) == "" || strings.TrimSpace(outPath) == "" {
		_, _ = fmt.Fprintln(stderr, "verifier run: --check-id and --out are required")
		return 2
	}
	if strings.TrimSpace(cwd) == "" {
		cwd = repoRootOverride
	}

	// Resolve the check by source. A builtin id self-pins (RFC 0141 Pillar 2) and
	// needs NO operator JSON — runnable out of the box. An external check resolves
	// against the two-layer intent ⋈ pins ⋈ attest (Pillar 1). The legacy RFC 0134
	// allowlist path remains for a directly-pinned operator allowlist.
	req := verifier.RunRequest{CheckID: checkID, Cwd: cwd, ScratchDir: scratch}
	var (
		result verifier.CheckResult
		err    error
	)
	switch {
	case verifier.IsBuiltinID(checkID):
		result, err = verifier.ExecuteCheck(context.Background(), nil, req)
	case strings.TrimSpace(intentPath) != "":
		intent, lerr := verifier.LoadIntent(intentPath)
		if lerr != nil {
			_, _ = fmt.Fprintf(stderr, "verifier run: %v\n", lerr)
			return 1
		}
		fp := verifier.HostFingerprint()
		if strings.TrimSpace(pinsPath) == "" {
			pinsPath = filepath.Join(filepath.Dir(intentPath), verifier.PinsFileName(fp))
		}
		var pins *verifier.Allowlist
		if loaded, perr := verifier.LoadAllowlist(pinsPath); perr == nil {
			pins = loaded
		}
		if strings.TrimSpace(attestPath) == "" {
			attestPath = filepath.Join(filepath.Dir(intentPath), verifier.AttestFileName(fp))
		}
		attest, aerr := verifier.LoadAttestations(attestPath)
		if aerr != nil {
			_, _ = fmt.Fprintf(stderr, "verifier run: %v\n", aerr)
			return 1
		}
		result, err = verifier.ExecuteIntentCheck(context.Background(), intent, pins, attest, req)
	case strings.TrimSpace(allowlistPath) != "":
		allowlist, lerr := verifier.LoadAllowlist(allowlistPath)
		if lerr != nil {
			_, _ = fmt.Fprintf(stderr, "verifier run: %v\n", lerr)
			return 1
		}
		result, err = verifier.ExecuteCheck(context.Background(), allowlist, req)
	default:
		_, _ = fmt.Fprintf(stderr, "verifier run: check %q is not a builtin; supply --intent <path> (RFC 0141) or --allowlist <path> (RFC 0134)\n", checkID)
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "verifier run: %v\n", err)
		return 1
	}
	doc := result.Receipt.MarshalFrontMatter()
	if err := os.WriteFile(outPath, []byte(doc), 0o644); err != nil { //nolint:gosec // receipt is a non-secret durable artifact written into the lane worktree
		_, _ = fmt.Fprintf(stderr, "verifier run: write receipt %q: %v\n", outPath, err)
		return 1
	}

	if jsonOut {
		summary := map[string]any{
			"check_id":          result.Receipt.CheckID,
			"exit_code":         result.Receipt.ExitCode,
			"passed":            result.Passed,
			"classification":    result.Classification,
			"seal_digest":       result.Receipt.SealDigest,
			"cwd_tree_sha":      result.Receipt.CwdTreeSHA,
			"sandbox_mechanism": result.Receipt.Posture.Mechanism,
			"sandbox_strict":    result.Receipt.Posture.Strict,
			"agreement":         result.Receipt.AgreementSignal,
			"receipt_path":      outPath,
		}
		if result.Receipt.WorkingSubdir != "" {
			summary["working_subdir"] = result.Receipt.WorkingSubdir
		}
		// Surface the stderr tail so a red check is diagnosable from the summary alone
		// (the sealed receipt deliberately omits stderr). Only on a non-pass, to keep a
		// green summary terse.
		if !result.Passed && strings.TrimSpace(result.StderrTail) != "" {
			summary["stderr_tail"] = result.StderrTail
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(summary)
	} else {
		workNote := ""
		if result.Receipt.WorkingSubdir != "" {
			workNote = fmt.Sprintf(" workdir=%s", result.Receipt.WorkingSubdir)
		}
		_, _ = fmt.Fprintf(stdout, "verifier run: check %q exit=%d passed=%t classification=%s mechanism=%s strict=%t agreement=%t%s\nreceipt: %s\n",
			result.Receipt.CheckID, result.Receipt.ExitCode, result.Passed, result.Classification,
			result.Receipt.Posture.Mechanism, result.Receipt.Posture.Strict, result.Receipt.AgreementSignal, workNote, outPath)
		// On a red check, print the stderr tail to OUR stderr so the operator can see why
		// without reproducing the sandbox argv by hand (the sealed receipt omits stderr).
		if !result.Passed && strings.TrimSpace(result.StderrTail) != "" {
			_, _ = fmt.Fprintf(stderr, "verifier run: check stderr (tail):\n%s\n", strings.TrimRight(result.StderrTail, "\n"))
		}
	}
	// The command's own exit code reflects the mechanical verdict so a lane can
	// branch on it, but the DURABLE truth is the sealed receipt, not this code.
	if !result.Passed {
		return 1
	}
	return 0
}

const (
	defaultIntentPath = "verification/allowlist.intent.json"
)

// runVerifierPin implements `striatum verifier pin --host-here` (RFC 0141 Pillar 1).
// It runs INSIDE the disposable verifier lane (never the daemon): it loads the
// committed, hashless intent, OBSERVES each sanctioned binary's sha256 on THIS
// host, and writes/updates the gitignored per-host pins file — printing a per-entry
// diff, refusing to pin when a binary cannot be resolved or the argv has drifted
// from the intent, and refusing to overwrite an existing real pin (changed bytes)
// without --force. The operator never transcribes a hash; the sandbox observes it.
//
// Pinning produces ONLY PINNED-but-unattested rows: `pin` observes bytes, it does
// not bless them. Promotion to VERIFIABLE requires the separate, operator-token
// `attest` verb — the verified lane can never stand behind its own pins.
func runVerifierPin(args []string, stdout io.Writer, stderr io.Writer, repoRootOverride string) int {
	var (
		intentPath string
		outPath    string
		force      bool
		hostHere   bool
		jsonOut    bool
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--host-here":
			hostHere = true
		case "--intent":
			i++
			if i < len(args) {
				intentPath = args[i]
			}
		case "--out":
			i++
			if i < len(args) {
				outPath = args[i]
			}
		case "--force":
			force = true
		case "--json":
			jsonOut = true
		case "-h", "--help":
			_, _ = fmt.Fprintln(stdout, "usage: striatum verifier pin --host-here [--intent <path>] [--out <path>] [--force] [--json]")
			return 0
		default:
			_, _ = fmt.Fprintf(stderr, "verifier pin: unknown flag %q\n", args[i])
			return 2
		}
	}
	if !hostHere {
		_, _ = fmt.Fprintln(stderr, "verifier pin: --host-here is required (pin OBSERVES this host's bytes; there is no hand-typed-sha form)")
		return 2
	}
	repoRoot := repoRootOverride
	if strings.TrimSpace(repoRoot) == "" {
		repoRoot, _ = os.Getwd()
	}
	if strings.TrimSpace(intentPath) == "" {
		intentPath = filepath.Join(repoRoot, defaultIntentPath)
	}
	intent, err := verifier.LoadIntent(intentPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "verifier pin: %v\n", err)
		return 1
	}
	fp := verifier.HostFingerprint()
	if strings.TrimSpace(outPath) == "" {
		outPath = filepath.Join(filepath.Dir(intentPath), verifier.PinsFileName(fp))
	}

	existing := map[string]verifier.AllowlistEntry{}
	if data, rerr := os.ReadFile(outPath); rerr == nil { //nolint:gosec // gitignored per-host pins path
		var f verifier.AllowlistFile
		if json.Unmarshal(data, &f) == nil {
			for _, e := range f.Checks {
				existing[e.ID] = e
			}
		}
	}

	type pinDiff struct {
		ID     string `json:"id"`
		Path   string `json:"resolved_path"`
		OldSHA string `json:"old_sha256,omitempty"`
		NewSHA string `json:"new_sha256"`
		Status string `json:"status"`
	}
	var (
		diffs    []pinDiff
		result   []verifier.AllowlistEntry
		refusals []string
	)
	for _, id := range intent.IDs() {
		entry, _ := intent.Entry(id)
		bin := entry.Argv[0]
		resolved := bin
		if !strings.Contains(bin, string(os.PathSeparator)) {
			p, lookErr := exec.LookPath(bin)
			if lookErr != nil {
				refusals = append(refusals, fmt.Sprintf("%s: cannot resolve binary %q on this host (%v) — refuse to pin a check whose tool is absent", id, bin, lookErr))
				continue
			}
			resolved = p
		}
		data, derr := os.ReadFile(resolved) //nolint:gosec // resolved from the operator-sanctioned intent argv
		if derr != nil {
			refusals = append(refusals, fmt.Sprintf("%s: read resolved binary %q: %v", id, resolved, derr))
			continue
		}
		sum := sha256.Sum256(data)
		sha := hex.EncodeToString(sum[:])
		prev, had := existing[id]
		if had && !equalArgv(prev.Argv, entry.Argv) && !force {
			refusals = append(refusals, fmt.Sprintf("%s: argv drifted from the intent (pinned %v, intent now %v) — re-pin with --force after reviewing", id, prev.Argv, entry.Argv))
			continue
		}
		if had && prev.BinarySHA256 != "" && prev.BinarySHA256 != sha && !force {
			refusals = append(refusals, fmt.Sprintf("%s: bytes changed (pinned %s, observed %s) — refuse to overwrite a real pin without --force", id, short(prev.BinarySHA256), short(sha)))
			continue
		}
		status := "pinned"
		switch {
		case had && prev.BinarySHA256 == sha:
			status = "unchanged"
		case had:
			status = "updated"
		}
		result = append(result, verifier.AllowlistEntry{ID: id, Argv: append([]string(nil), entry.Argv...), BinarySHA256: sha, PassWhen: entry.PassWhen})
		diffs = append(diffs, pinDiff{ID: id, Path: resolved, OldSHA: prev.BinarySHA256, NewSHA: sha, Status: status})
	}
	if len(refusals) > 0 {
		sort.Strings(refusals)
		_, _ = fmt.Fprintf(stderr, "verifier pin: refused (%d):\n  %s\n", len(refusals), strings.Join(refusals, "\n  "))
		return 1
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	file := verifier.AllowlistFile{SchemaVersion: verifier.AllowlistSchemaVersion, Checks: result}
	body, _ := json.MarshalIndent(file, "", "  ")
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil { //nolint:gosec // pins dir is non-secret operational scratch
		_, _ = fmt.Fprintf(stderr, "verifier pin: mkdir %q: %v\n", filepath.Dir(outPath), err)
		return 1
	}
	if err := os.WriteFile(outPath, append(body, '\n'), 0o644); err != nil { //nolint:gosec // gitignored per-host pins, non-secret
		_, _ = fmt.Fprintf(stderr, "verifier pin: write pins %q: %v\n", outPath, err)
		return 1
	}
	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"host_fingerprint": fp, "pins_path": outPath, "pinned": diffs})
	} else {
		_, _ = fmt.Fprintf(stdout, "verifier pin: host=%s wrote %d pin(s) → %s\n", fp, len(result), outPath)
		for _, d := range diffs {
			_, _ = fmt.Fprintf(stdout, "  %-30s %-9s %s (%s)\n", d.ID, d.Status, short(d.NewSHA), d.Path)
		}
		_, _ = fmt.Fprintln(stdout, "  (PINNED-but-unattested — run `striatum verifier attest` as the operator to reach VERIFIED)")
	}
	return 0
}

// runVerifierAttest implements `striatum verifier attest` (RFC 0141 Pillar 1, the
// PINNED → VERIFIABLE hinge). It is OPERATOR-ONLY: it REFUSES inside a supervised
// lane/session (where the daemon injects STRIATUM_SESSION_ID / STRIATUM_LANE_ID and
// a session-bound token), because the trust signal must come from a principal the
// verified lane cannot impersonate. It binds an attestation to the EXACT pinned
// sha, so a later re-pin of different bytes silently invalidates the blessing.
//
// RFC 0141 / D243 (#482) — graduation hardening LANDED: this verb now ALSO records
// the AUTHORITATIVE attestation in daemon-owned PG via the operator-token
// `verifier.attest` RPC (recordDaemonAttestations), which the run-completion gate
// consults (mutations.attestationCovers). The repo-file sidecar this verb writes is
// now a CACHE/projection (it still drives the lane-side intent⋈pins⋈attest join at
// `verifier run`), not the trust source — a non-compliant lane that forges the
// sidecar can no longer reach VERIFIED, because the gate reads PG, not the
// lane-writable file.
func runVerifierAttest(args []string, stdout io.Writer, stderr io.Writer, repoRootOverride string) int {
	// The operator-token gate: a supervised lane is exactly a process the daemon
	// launched with these markers. The operator shell has none of them.
	if sid := strings.TrimSpace(os.Getenv("STRIATUM_SESSION_ID")); sid != "" {
		_, _ = fmt.Fprintf(stderr, "verifier attest: refused — STRIATUM_SESSION_ID=%s is set (a lane/session context). attest requires an OPERATOR token; the verified lane can never bless its own pins. Run it from the operator shell.\n", sid)
		return 3
	}
	if lid := strings.TrimSpace(os.Getenv("STRIATUM_LANE_ID")); lid != "" {
		_, _ = fmt.Fprintf(stderr, "verifier attest: refused — STRIATUM_LANE_ID=%s is set (a lane context). attest requires an OPERATOR token, not a lane/session token.\n", lid)
		return 3
	}

	var (
		pinID      string
		intentPath string
		pinsPath   string
		attestPath string
		all        bool
		jsonOut    bool
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--pin":
			i++
			if i < len(args) {
				pinID = args[i]
			}
		case "--all":
			all = true
		case "--intent":
			i++
			if i < len(args) {
				intentPath = args[i]
			}
		case "--pins":
			i++
			if i < len(args) {
				pinsPath = args[i]
			}
		case "--out":
			i++
			if i < len(args) {
				attestPath = args[i]
			}
		case "--json":
			jsonOut = true
		case "-h", "--help":
			_, _ = fmt.Fprintln(stdout, "usage: striatum verifier attest (--pin <id> | --all) [--intent <path>] [--pins <path>] [--out <path>] [--json]")
			return 0
		default:
			_, _ = fmt.Fprintf(stderr, "verifier attest: unknown flag %q\n", args[i])
			return 2
		}
	}
	if pinID == "" && !all {
		_, _ = fmt.Fprintln(stderr, "verifier attest: one of --pin <id> or --all is required")
		return 2
	}
	repoRoot := repoRootOverride
	if strings.TrimSpace(repoRoot) == "" {
		repoRoot, _ = os.Getwd()
	}
	if strings.TrimSpace(intentPath) == "" {
		intentPath = filepath.Join(repoRoot, defaultIntentPath)
	}
	intent, err := verifier.LoadIntent(intentPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "verifier attest: %v\n", err)
		return 1
	}
	fp := verifier.HostFingerprint()
	if strings.TrimSpace(pinsPath) == "" {
		pinsPath = filepath.Join(filepath.Dir(intentPath), verifier.PinsFileName(fp))
	}
	pins, err := verifier.LoadAllowlist(pinsPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "verifier attest: load pins %q: %v (run `striatum verifier pin --host-here` first)\n", pinsPath, err)
		return 1
	}
	if strings.TrimSpace(attestPath) == "" {
		attestPath = filepath.Join(filepath.Dir(intentPath), verifier.AttestFileName(fp))
	}

	// Carry forward existing attestations (best effort), then overlay the new ones.
	byID := map[string]verifier.AttestEntry{}
	if data, rerr := os.ReadFile(attestPath); rerr == nil { //nolint:gosec // gitignored per-host sidecar
		var f verifier.AttestFile
		if json.Unmarshal(data, &f) == nil {
			for _, e := range f.Attestations {
				byID[e.ID] = e
			}
		}
	}

	var targets []string
	if all {
		targets = intent.IDs()
	} else {
		targets = []string{pinID}
	}
	principal := strings.TrimSpace(os.Getenv("STRIATUM_PRINCIPAL_ID"))
	if principal == "" {
		principal = strings.TrimSpace(os.Getenv("USER"))
	}
	now := time.Now().UTC().Format(time.RFC3339)
	attested := []string{}
	for _, id := range targets {
		if _, ok := intent.Entry(id); !ok {
			_, _ = fmt.Fprintf(stderr, "verifier attest: %q is not sanctioned in the intent (%s)\n", id, intentPath)
			return 1
		}
		entry, rerr := pins.Resolve(id)
		if rerr != nil {
			_, _ = fmt.Fprintf(stderr, "verifier attest: %q is not pinned on this host — cannot bless bytes you have not observed; run `striatum verifier pin --host-here` first\n", id)
			return 1
		}
		byID[id] = verifier.AttestEntry{ID: id, BinarySHA256: entry.BinarySHA256, AttestedAt: now, AttestedBy: principal}
		attested = append(attested, id)
	}

	entries := make([]verifier.AttestEntry, 0, len(byID))
	for _, e := range byID {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	out := verifier.AttestFile{SchemaVersion: verifier.AllowlistAttestSchemaVersion, HostArchFP: fp, Attestations: entries}
	body, _ := json.MarshalIndent(out, "", "  ")
	if err := os.MkdirAll(filepath.Dir(attestPath), 0o755); err != nil { //nolint:gosec // non-secret operational dir
		_, _ = fmt.Fprintf(stderr, "verifier attest: mkdir %q: %v\n", filepath.Dir(attestPath), err)
		return 1
	}
	if err := os.WriteFile(attestPath, append(body, '\n'), 0o644); err != nil { //nolint:gosec // gitignored per-host sidecar, non-secret
		_, _ = fmt.Fprintf(stderr, "verifier attest: write sidecar %q: %v\n", attestPath, err)
		return 1
	}
	sort.Strings(attested)

	// RFC 0141 / D243 (#482): record the AUTHORITATIVE attestation in daemon-owned PG
	// via the operator-token `verifier.attest` RPC. This — not the sidecar above — is
	// what the run-completion gate consults. The sidecar is now a cache/projection
	// (it still drives the lane-side intent⋈pins⋈attest join at `verifier run`). A
	// daemon REFUSAL (e.g. a session-bound token) is fatal: the blessing is not
	// gate-authoritative without it. A daemon that is simply UNREACHABLE degrades to a
	// LOUD warning rather than a hard failure, so offline pin-then-attest still writes
	// the sidecar — but the operator is told the attestation is not yet enforced.
	daemonRecorded, daemonRefusal := recordDaemonAttestations(repoRoot, byID, attested, jsonOut, stdout, stderr)
	if daemonRefusal != nil {
		_, _ = fmt.Fprintf(stderr, "verifier attest: daemon refused the authoritative attestation: %v\n", daemonRefusal)
		return 3
	}
	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"host_fingerprint": fp, "attest_path": attestPath, "attested": attested,
			"attested_by": principal, "daemon_recorded": daemonRecorded,
		})
	} else {
		_, _ = fmt.Fprintf(stdout, "verifier attest: blessed %d pin(s) by %q → %s (daemon-recorded: %d)\n  %s\n",
			len(attested), principal, attestPath, len(daemonRecorded), strings.Join(attested, ", "))
	}
	return 0
}

// recordDaemonAttestations records the AUTHORITATIVE attestation rows in daemon-owned
// PG via the operator-token `verifier.attest` RPC (RFC 0141 / D243 / #482), one per
// blessed (check_id, binary_sha256). It returns the ids the daemon recorded and a
// non-nil refusal error if the daemon ACTIVELY refused (e.g. a session-bound token,
// capability_denied, or any RPC error) — that is fatal, because the sidecar alone is
// not gate-authoritative. A daemon that is merely UNREACHABLE (no socket / no token)
// is NOT a refusal: it returns (recorded-so-far, nil) after a loud warning, so an
// offline pin-then-attest still writes the cache sidecar and tells the operator the
// blessing is not yet enforced at the gate.
func recordDaemonAttestations(repoRoot string, byID map[string]verifier.AttestEntry, attested []string, jsonOut bool, stdout, stderr io.Writer) (recorded []string, refusal error) {
	config, err := rpcclient.ResolveConfig(os.Environ(), "", "", "", 0)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "verifier attest: WARNING daemon unreachable (%v) — wrote the sidecar CACHE only; the attestation is NOT gate-authoritative until recorded in the daemon (re-run with a reachable daemon + operator token)\n", err)
		return nil, nil
	}
	client := rpcclient.Client{Config: config}
	ctx := context.Background()

	repositoryID := envLookup(os.Environ(), "STRIATUM_REPOSITORY_ID")
	if repositoryID == "" {
		resolved, rerr := client.Invoke(ctx, "repo.resolve", map[string]any{"path": repoRoot})
		if rerr != nil {
			// A failure to RESOLVE the repository (unreachable, no token, or the path is
			// not a registered repo) is "could not record" — warn and keep the sidecar
			// cache. It is NOT the security boundary: only the daemon ACTIVELY refusing
			// the verifier.attest WRITE (capability_denied for a session-bound token) is
			// fatal. The operator re-runs from a registered repo with an operator token.
			_, _ = fmt.Fprintf(stderr, "verifier attest: WARNING could not resolve the repository against the daemon (%v) — wrote the sidecar CACHE only; the attestation is NOT gate-authoritative until recorded in the daemon\n", rerr)
			return nil, nil
		}
		repositoryID, _ = resolved["repository_id"].(string)
	}
	if repositoryID == "" {
		_, _ = fmt.Fprintln(stderr, "verifier attest: WARNING could not resolve repository_id — wrote the sidecar CACHE only; the attestation is NOT gate-authoritative")
		return nil, nil
	}

	for _, id := range attested {
		entry := byID[id]
		res, rerr := client.Invoke(ctx, "verifier.attest", map[string]any{
			"repository_id": repositoryID,
			"check_id":      id,
			"binary_sha256": entry.BinarySHA256,
		})
		if rerr != nil {
			if daemonAttestRefusal(rerr) {
				return recorded, rerr // a security refusal must fail loud
			}
			// Non-fatal "could not record" (unreachable, no token, repo not registered).
			_, _ = fmt.Fprintf(stderr, "verifier attest: WARNING could not record the authoritative attestation in the daemon (%v) — wrote the sidecar CACHE only; the attestation is NOT gate-authoritative until recorded in the daemon\n", rerr)
			return recorded, nil
		}
		if status, _ := res["status"].(string); status == "attested" {
			recorded = append(recorded, id)
		}
	}
	return recorded, nil
}

// daemonAttestRefusal reports whether an RPC error from the verifier.attest dispatch
// is an active daemon-side SECURITY REFUSAL that must fail the verb LOUD — chiefly
// capability_denied (the session-bound-token refusal: the verified lane can never
// bless its own pins). Everything else (daemon unreachable, no operator token wired,
// or the path is not a registered repo) is a non-fatal "could not record" condition:
// the verb warns and keeps the sidecar cache, telling the operator the blessing is
// not yet gate-authoritative. This keeps offline pin-then-attest working while making
// a forged-context attest fail closed.
func daemonAttestRefusal(err error) bool {
	var clientErr *rpcclient.Error
	if !errors.As(err, &clientErr) {
		return false
	}
	switch clientErr.Code {
	case "capability_denied", "forbidden", "permission_denied":
		return true
	default:
		return false
	}
}

// equalArgv reports whether two argv slices are identical.
func equalArgv(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// short trims a sha for human-facing diffs.
func short(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}
