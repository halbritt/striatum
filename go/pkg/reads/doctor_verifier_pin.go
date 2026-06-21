package reads

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/halbritt/striatum/go/pkg/verifier"
)

// RFC 0141 / D252 (#483) — read-only verifier pin-drift legibility for `striatum
// doctor`. These two blocks turn two silent 3am gate failures into a pre-flight
// the operator sees the moment they run doctor, never at run start:
//
//   - builtin_selfpin_drift (slice 1): the running daemon's executable bytes vs
//     the on-disk daemon binary. After `make install` WITHOUT a daemon restart
//     the on-disk binary is the new build while the daemon keeps its old
//     in-memory image (a documented gotcha). A verifier lane that a stale daemon
//     spawns self-pins to a striatum binary built from a DIFFERENT commit than
//     the daemon was, and the BuiltinStriatumVersion sealed into its receipt may
//     not match the daemon's own version — making builtin receipts mysteriously
//     version-skew. This surfaces that the instant doctor runs.
//   - verifier pin-drift (slice 2): the repo's committed, hashless verifier
//     allowlist INTENT vs the per-host PINS. It reports, per sanctioned external
//     check, whether the check is NAMED-but-unpinned on this host, whether the
//     pinned argv has drifted from the intent argv, and whether the pinned bytes
//     no longer match the binary on disk (a run would REFUSE that check). It says
//     "this host would degrade / refuse N checks" before run start, not after.
//
// Both are ADVISORY ONLY — every finding is a `warnings` string, never a hard
// `problems` failure that flips doctor red. They read NO secret and they change
// NOTHING about the allowlist TRUST contract: the gate's builtin→ASSERTED cap
// and the intent⋈pins⋈attest promotion rule are untouched. They only OBSERVE and
// REPORT the host-specific state that already determines a run's rungs.

// defaultVerifierIntentRelPath mirrors the verifier CLI's defaultIntentPath: the
// repo-relative location `striatum verifier pin`/`run` use for the committed,
// hashless intent file. doctor looks there (and at the legacy top-level name) so
// it reports drift for the same file a run would resolve.
const defaultVerifierIntentRelPath = "verification/allowlist.intent.json"

// legacyVerifierIntentRelPath is an alternative intent location some repos use.
const legacyVerifierIntentRelPath = "verification/allowlist.intent.v1.json"

// shaOfFile hashes a file's bytes (sha256, lowercase hex). Used only to OBSERVE
// the running/on-disk daemon binary and the on-disk check binaries; it reads no
// secret material.
func shaOfFile(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // hashing our own executable / operator-pinned check binaries for drift legibility
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// daemonExecutablePath resolves the on-disk path of the running daemon binary,
// stripping a trailing " (deleted)" the kernel appends to /proc/self/exe (via
// os.Executable) when the binary has been replaced underneath the running
// process (exactly the `make install`-without-restart case). A var so tests can
// stub it.
var daemonExecutablePath = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(exe, " (deleted)"), nil
}

// runningDaemonExePath is the stable kernel handle to the running process image.
// /proc/self/exe still resolves the original inode's bytes even after the
// on-disk file is replaced. A var so tests can stub it.
var runningDaemonExePath = "/proc/self/exe"

// verifierSelfpinDriftBlock (slice 1) reports whether the running daemon's
// executable bytes match the on-disk daemon binary. A mismatch means the daemon
// was rebuilt/reinstalled WITHOUT a restart, so the verifier lanes it spawns
// self-pin to a striatum binary built from a different commit than the daemon
// was — the documented `make install`-without-restart gotcha that makes builtin
// receipts version-skew. Advisory only (a warning), reads no secret.
func verifierSelfpinDriftBlock() (map[string]any, []string) {
	block := map[string]any{
		"checked":          true,
		"daemon_version":   packageStriatumVersion,
		"builtin_capped":   "asserted",
		"contract_changed": false,
	}

	runningSHA, runErr := shaOfFile(runningDaemonExePath)
	if runErr != nil {
		// Non-Linux or an unreadable /proc/self/exe: we cannot compare, so we stay
		// quiet rather than emit a misleading warning.
		block["comparable"] = false
		block["reason"] = "running daemon image not readable for self-pin comparison"
		return block, nil
	}
	block["running_sha256"] = runningSHA

	onDiskPath, pathErr := daemonExecutablePath()
	if pathErr != nil {
		block["comparable"] = false
		block["reason"] = "daemon executable path not resolvable"
		return block, nil
	}
	block["on_disk_path"] = onDiskPath

	onDiskSHA, diskErr := shaOfFile(onDiskPath)
	if diskErr != nil {
		// The on-disk binary is gone (deleted-and-not-replaced): we know the daemon
		// is running a now-absent image, which is itself the restart gotcha shape.
		block["comparable"] = false
		block["reason"] = "on-disk daemon binary unreadable (replaced or removed since start)"
		block["drift"] = true
		return block, []string{
			"verifier_selfpin_drift: the running daemon's on-disk binary " + onDiskPath +
				" is no longer readable (replaced/removed since the daemon started); the daemon is running a stale in-memory image. " +
				"Restart the daemon (systemctl --user restart striatumd) so freshly-spawned verifier lanes self-pin to the deployed build.",
		}
	}
	block["on_disk_sha256"] = onDiskSHA

	if runningSHA != onDiskSHA {
		block["drift"] = true
		return block, []string{
			"verifier_selfpin_drift: the running daemon (sha " + short12(runningSHA) + ") differs from the on-disk daemon binary " +
				onDiskPath + " (sha " + short12(onDiskSHA) + "); `make install` ran WITHOUT a daemon restart. " +
				"Verifier lanes this daemon spawns self-pin to a different striatum build, so builtin receipts may version-skew. " +
				"Restart the daemon (systemctl --user restart striatumd) to clear the drift.",
		}
	}

	block["drift"] = false
	return block, nil
}

// verifierPinDriftCheck is one row of the slice-2 pin-drift report.
type verifierPinDriftCheck struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	BacksClaim string `json:"backs_claim,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// verifierPinDriftBlock (slice 2) reports the host-specific verifier pin state
// for the repo's committed intent: per sanctioned external check, whether it is
// NAMED-but-unpinned on this host, whether the pinned argv has drifted from the
// intent argv, or whether the pinned bytes no longer match the binary on disk (a
// run would REFUSE that check). Advisory only (warnings), reads no secret, and
// changes NOTHING about the allowlist trust contract.
func verifierPinDriftBlock(repoRoot string) (map[string]any, []string) {
	block := map[string]any{"checked": false}
	if strings.TrimSpace(repoRoot) == "" {
		block["skipped"] = "repository_id / repo_root required"
		return block, nil
	}

	intentPath := resolveIntentPath(repoRoot)
	if intentPath == "" {
		// No committed verifier intent in this repo: nothing to be pinned/drifted.
		block["checked"] = true
		block["intent_present"] = false
		return block, nil
	}
	block["intent_path"] = intentPath

	intent, err := verifier.LoadIntent(intentPath)
	if err != nil {
		// A malformed intent is a workflow-author problem; surface it as a warning so
		// it is legible at doctor time, but do not flip doctor red over a repo file.
		block["checked"] = true
		block["intent_present"] = true
		block["intent_error"] = err.Error()
		return block, []string{"verifier_intent_invalid: " + intentPath + ": " + err.Error()}
	}
	block["checked"] = true
	block["intent_present"] = true

	fp := verifier.HostFingerprint()
	block["host_fingerprint"] = fp
	pinsPath := filepath.Join(filepath.Dir(intentPath), verifier.PinsFileName(fp))
	block["pins_path"] = pinsPath

	var pins *verifier.Allowlist
	if loaded, perr := verifier.LoadAllowlist(pinsPath); perr == nil {
		pins = loaded
		block["pins_present"] = true
	} else {
		block["pins_present"] = false
	}

	checks := []verifierPinDriftCheck{}
	warnings := []string{}
	unpinned := 0
	argvDrift := 0
	bytesDrift := 0

	for _, id := range intent.IDs() {
		entry, _ := intent.Entry(id)
		row := verifierPinDriftCheck{ID: id, BacksClaim: entry.BacksClaim}

		var pin verifier.AllowlistEntry
		var pinErr error
		if pins != nil {
			pin, pinErr = pins.Resolve(id)
		}
		if pins == nil || pinErr != nil {
			row.Status = "named_but_unpinned"
			row.Detail = "no pin for this check on host " + fp
			unpinned++
			warnings = append(warnings, "verifier_check_unpinned."+id+": sanctioned in "+intentPath+
				" but not pinned on host "+fp+"; a run would cap this check at DESIGNED. Pin with `striatum verifier pin --host-here`.")
			checks = append(checks, row)
			continue
		}

		if !equalStringSlice(pin.Argv, entry.Argv) {
			row.Status = "argv_drifted"
			row.Detail = "pinned argv " + strings.Join(pin.Argv, " ") + " != intent argv " + strings.Join(entry.Argv, " ")
			argvDrift++
			warnings = append(warnings, "verifier_pin_drifted."+id+": the pinned argv has drifted from the committed intent argv; "+
				"a run would REFUSE this check. Re-pin with `striatum verifier pin --host-here --force` after reviewing the intent change.")
			checks = append(checks, row)
			continue
		}

		// The pin records the sha the binary HAD when it was observed. If the binary
		// on disk now hashes differently, a run resolves the same pin but VerifyBinary
		// refuses (the curated bytes drifted). Report it pre-flight. argv[0] must be an
		// absolute path on a real pin (the pin step resolves it), so we can read it.
		if len(pin.Argv) > 0 && pin.BinarySHA256 != "" {
			bin := pin.Argv[0]
			if strings.Contains(bin, string(os.PathSeparator)) {
				if actual, herr := shaOfFile(bin); herr == nil {
					if !strings.EqualFold(actual, pin.BinarySHA256) {
						row.Status = "pin_bytes_drifted"
						row.Detail = "pinned " + short12(pin.BinarySHA256) + " != on-disk " + short12(actual) + " for " + bin
						bytesDrift++
						warnings = append(warnings, "verifier_pin_drifted."+id+": the pinned binary "+bin+
							" bytes changed since it was pinned (pinned "+short12(pin.BinarySHA256)+", on-disk "+short12(actual)+"); "+
							"a run would REFUSE this check. Re-pin with `striatum verifier pin --host-here --force` after reviewing.")
						checks = append(checks, row)
						continue
					}
				}
				// A binary that is now unreadable is reported as unpinned-shaped drift below
				// only when it is also missing from the pin; an unreadable already-pinned
				// binary surfaces at run time. Keep the doctor read non-noisy here.
			}
		}

		row.Status = "pinned"
		checks = append(checks, row)
	}

	block["checks"] = checks
	block["named_but_unpinned_count"] = unpinned
	block["argv_drifted_count"] = argvDrift
	block["pin_bytes_drifted_count"] = bytesDrift
	block["would_degrade_count"] = unpinned
	block["would_refuse_count"] = argvDrift + bytesDrift
	return block, warnings
}

// resolveIntentPath returns the first committed verifier intent file that exists
// under repoRoot, or "" when none is present.
func resolveIntentPath(repoRoot string) string {
	for _, rel := range []string{defaultVerifierIntentRelPath, legacyVerifierIntentRelPath} {
		p := filepath.Join(repoRoot, rel)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// equalStringSlice reports whether two string slices are element-wise equal.
func equalStringSlice(a, b []string) bool {
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

// short12 trims a sha for human-facing diffs.
func short12(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}
