package verifier

// RFC 0141 / D238 Pillar 2 — the built-in, Striatum-pinned check library.
//
// A builtin escapes the host-specific-hash problem by relocating the pin from the
// TOOL (`go`, whose hash differs per host) to the STRIATUM BINARY (whose hash the
// lane self-computes). That makes `generate --shape verification_gate` runnable
// with ZERO operator JSON for the go-test/vet/build common case.
//
// THE SHARPEST HONESTY CONSTRAINT (the cardinal invariant of this file): the
// striatum self-pin proves "striatum @ this sha invoked `go test`" — it does NOT
// prove WHICH `go` ran. A host with a trojaned `go` on PATH yields a green builtin
// receipt indistinguishable from a clean one. So a `builtin:*` receipt MUST cap at
// ASSERTED on the self-pin alone — never VERIFIED — both at lane-side
// classification AND, authoritatively, at the daemon-side gate read
// (EffectiveStatusFromReceipt). VERIFIED stays the exclusive reward of the
// external, operator-pinned-and-attested allowlist road.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// BuiltinIDPrefix marks a check id resolved from the in-binary registry rather
// than the operator allowlist.
const BuiltinIDPrefix = "builtin:"

// BuiltinStriatumVersion is stamped into every builtin receipt's transcript (and
// sealed) so a version-skew downgrade is legible. main sets it from the VERSION
// file at startup; it defaults to "unknown" so the package is self-contained.
var BuiltinStriatumVersion = "unknown"

// IsBuiltinID reports whether a check id names a builtin (registry-resolved,
// self-pinned, capped at ASSERTED).
func IsBuiltinID(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), BuiltinIDPrefix)
}

// BuiltinCheck is one Striatum-release-pinned check: a fully-resolved argv plus a
// negative_control the verifier runs first to prove the tool actually discriminates.
type BuiltinCheck struct {
	ID              string
	Argv            []string
	NegativeControl *NegativeControl
	Description     string
}

// builtinRegistry maps each builtin id to its resolved argv + negative control.
// The negative controls run the SAME tool against a known-bad invocation so a
// vacuous/trojaned tool that exits 0 on everything voids the receipt. They are
// offline and deterministic (no module fetch) so a builtin run never needs network.
var builtinRegistry = map[string]BuiltinCheck{
	"builtin:go-test": {
		ID: "builtin:go-test",
		// -buildvcs=false: per-job worktrees have a .git pointer file, not a real
		// repository. Go's VCS-status probe fails under the strict bubblewrap sandbox
		// (exit 128 "error obtaining VCS status"). The verifier self-pins the striatum
		// binary SHA and does not rely on Go's VCS stamp, so disabling it is safe.
		Argv:        []string{"go", "test", "-buildvcs=false", "./..."},
		Description: "Run the project's Go test suite (go test ./...).",
		NegativeControl: &NegativeControl{
			// `go test` of a nonexistent package fails to compile/load → non-zero.
			// A `go` that exits 0 here is not running tests at all → void.
			Argv:        []string{"go", "test", "-buildvcs=false", "./striatum-negative-control-nonexistent-pkg-do-not-create/..."},
			Description: "go test against a nonexistent package must fail to load (non-zero).",
		},
	},
	"builtin:go-vet": {
		ID:          "builtin:go-vet",
		Argv:        []string{"go", "vet", "./..."},
		Description: "Run go vet over the module (go vet ./...).",
		NegativeControl: &NegativeControl{
			Argv:        []string{"go", "vet", "./striatum-negative-control-nonexistent-pkg-do-not-create/..."},
			Description: "go vet against a nonexistent package must fail (non-zero).",
		},
	},
	"builtin:go-build": {
		ID: "builtin:go-build",
		// -buildvcs=false: per-job worktrees have a .git pointer file, not a real
		// repository. Go's VCS-status probe fails under the strict bubblewrap sandbox
		// (exit 128 "error obtaining VCS status"). The verifier self-pins the striatum
		// binary SHA and does not rely on Go's VCS stamp, so disabling it is safe.
		Argv:        []string{"go", "build", "-buildvcs=false", "./..."},
		Description: "Build every package in the module (go build ./...).",
		NegativeControl: &NegativeControl{
			Argv:        []string{"go", "build", "-buildvcs=false", "./striatum-negative-control-nonexistent-pkg-do-not-create/..."},
			Description: "go build against a nonexistent package must fail (non-zero).",
		},
	},
	"builtin:artifact-anchor-integrity": {
		ID:          "builtin:artifact-anchor-integrity",
		Argv:        []string{"git", "fsck", "--no-dangling", "--no-progress"},
		Description: "Verify the worktree's git object graph is intact (git fsck).",
		NegativeControl: &NegativeControl{
			// git fsck against a non-repository directory must error (non-zero).
			Argv:        []string{"git", "--git-dir=/nonexistent-striatum-negative-control", "fsck"},
			Description: "git fsck against a nonexistent git dir must fail (non-zero).",
		},
	},
}

// BuiltinIDs returns the sorted set of builtin check ids (for diagnostics and the
// generator's refuse-VERIFIED-only-builtin guard).
func BuiltinIDs() []string {
	ids := make([]string, 0, len(builtinRegistry))
	for id := range builtinRegistry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ResolveBuiltin returns the builtin check for an id (ok=false if not a known
// builtin). Resolution is content-fixed: the registry, not the request, owns argv.
func ResolveBuiltin(id string) (BuiltinCheck, bool) {
	bc, ok := builtinRegistry[strings.TrimSpace(id)]
	return bc, ok
}

// selfSHA hashes the running striatum executable — the builtin self-pin. It is
// honest about WHO invoked the tool (this exact striatum build), never about which
// `go`/`git` ran, which is precisely why a builtin caps at ASSERTED.
func selfSHA() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve striatum executable for builtin self-pin: %w", err)
	}
	data, err := os.ReadFile(exe) //nolint:gosec // hashing our own executable for the self-pin
	if err != nil {
		return "", fmt.Errorf("read striatum executable %q for builtin self-pin: %w", exe, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// builtinResolvedExec builds the ResolvedExec for a builtin: the registry argv,
// the striatum self-pin as the recorded BinarySHA256, the BuiltinID + version that
// get sealed, and the negative control. It NEVER calls VerifyBinary (which would
// hash argv[0]=go/git, not striatum) — the self-pin is the honest binding.
func builtinResolvedExec(id string) (ResolvedExec, error) {
	bc, ok := ResolveBuiltin(id)
	if !ok {
		return ResolvedExec{}, fmt.Errorf("builtin check %q is not in the in-binary registry (known builtins: %s)", id, strings.Join(BuiltinIDs(), ", "))
	}
	sha, err := selfSHA()
	if err != nil {
		return ResolvedExec{}, err
	}
	// Resolve the tool to an ABSOLUTE host path so the builtin runs
	// PATH-independently inside the sandbox (the fixed sandbox PATH is
	// /usr/local/bin:/usr/bin:/bin, but `go`/`git` may live elsewhere e.g.
	// ~/.local/bin; the whole host FS is ro-bound in, so an absolute path resolves).
	argv, err := resolveToolArgv(bc.Argv)
	if err != nil {
		return ResolvedExec{}, err
	}
	var nc *NegativeControl
	if bc.NegativeControl != nil {
		ncArgv, ncErr := resolveToolArgv(bc.NegativeControl.Argv)
		if ncErr != nil {
			return ResolvedExec{}, ncErr
		}
		nc = &NegativeControl{Argv: ncArgv, MutationOf: bc.NegativeControl.MutationOf, Description: bc.NegativeControl.Description}
	}
	return ResolvedExec{
		CheckID:         bc.ID,
		Argv:            argv,
		BinarySHA256:    sha,
		BuiltinID:       bc.ID,
		StriatumVersion: BuiltinStriatumVersion,
		NegativeControl: nc,
		PassWhen:        passWhenExitZero,
	}, nil
}

// resolveToolArgv resolves argv[0] to an absolute host path (via PATH) so a builtin
// is found inside the sandbox regardless of the fixed sandbox PATH. The resolved
// tool's identity is NOT what a builtin pins — the receipt self-pins the striatum
// binary and caps at ASSERTED — this only ensures the tool can be located.
func resolveToolArgv(argv []string) ([]string, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return nil, fmt.Errorf("builtin check has an empty argv")
	}
	out := append([]string(nil), argv...)
	if !strings.Contains(out[0], "/") {
		abs, err := exec.LookPath(out[0])
		if err != nil {
			return nil, fmt.Errorf("builtin check tool %q is not installed on this host: %w", out[0], err)
		}
		out[0] = abs
	}
	return out, nil
}
