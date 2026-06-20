package verifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Receipt is the in-memory form of a receipt.v1 artifact (the tamper-evident
// transcript a verifier run mints). Its fields mirror the
// artifactcontracts "receipt" schema exactly, so MarshalFrontMatter produces a
// document the publisher accepts. SealDigest is the receipt's own content
// digest, computed over the canonical transcript so any tamper changes the seal.
type Receipt struct {
	CheckID      string
	Argv         []string
	BinarySHA256 string
	ExitCode     int
	StdoutSHA256 string
	CwdTreeSHA   string
	SealDigest   string
	CreatedAt    string
	// Posture and AgreementSignal are recorded in the receipt BODY (not the
	// schema-required front matter) so the gate reader can apply the two-signal
	// rule. They are part of what the seal digest covers.
	Posture         Posture
	AgreementSignal bool
	// BuiltinID and StriatumVersion are set ONLY for a RFC 0141 builtin check
	// (resolved from the in-binary registry, self-pinned to the striatum binary).
	// They are sealed into the transcript and surfaced in the body so the gate read
	// can CAP a builtin receipt at ASSERTED — a self-pin proves which harness
	// invoked the tool, never which tool ran. Empty for an operator-allowlist check.
	BuiltinID       string
	StriatumVersion string
	// NegativeControlVoid records the Pillar 3 vacuity guard: the known-bad control
	// is run FIRST, and a control that unexpectedly PASSES (exit 0) voids the
	// receipt — a check that cannot fail on a known-bad cannot be trusted to pass
	// for the right reason. NegativeControlExit is the control's exit code. Sealed.
	NegativeControlExit int
	NegativeControlVoid bool
}

// CheckResult is the per-check outcome the verifier lane derives mechanically.
// The verdict is DERIVED from the exit code, never authored. Classification maps
// to the claim-status lattice the gate reads (see EffectiveStatusFromReceipt).
type CheckResult struct {
	Receipt Receipt
	// Passed is the mechanical pass condition outcome (exit_zero).
	Passed bool
	// Classification is one of "verified_eligible" (strict sandbox, two-signal
	// agreement, exit-0), "asserted" (a lone exit-0, a non-strict sandbox, or a
	// builtin self-pin), "indeterminate" (timeout / envelope violation / network
	// touch), or "void" (the negative control failed to fail). It maps to the
	// lattice rung a receipt can EARN — never above ASSERTED unless both signals
	// hold under a strict envelope AND the check is NOT a builtin.
	Classification string
	// VoidReason names why the receipt is void (Pillar 3), empty otherwise.
	VoidReason string
}

const (
	classVerifiedEligible = "verified_eligible"
	classAsserted         = "asserted"
	classIndeterminate    = "indeterminate"
	classVoid             = "void"
)

// RunRequest is a single verifier check execution request (issued by the lane).
type RunRequest struct {
	// CheckID names an allowlist entry; the executed bytes come from the
	// allowlist, never from the request.
	CheckID string
	// Cwd is the worktree the check runs against (read-only in the sandbox).
	Cwd string
	// ScratchDir is the single writable directory inside the sandbox.
	ScratchDir string
	// Limits override the default sandbox caps (0 fields use defaults).
	Limits SandboxLimits
}

// ResolvedExec is a fully-resolved check ready to execute: the exact argv, the
// recorded binary sha (an operator pin for an external check, the striatum self-pin
// for a builtin), the optional negative control, and the builtin markers. It is the
// single input to executeResolved, so the external and builtin roads share one
// mint path and one seal.
type ResolvedExec struct {
	CheckID         string
	Argv            []string
	BinarySHA256    string
	BuiltinID       string
	StriatumVersion string
	NegativeControl *NegativeControl
	PassWhen        string
}

// ExecuteCheck runs a single OPERATOR-ALLOWLISTED check (RFC 0134 path) under the
// strict sandbox and mints a receipt. The builtin (RFC 0141) ids are handled here
// too: a `builtin:*` id resolves from the in-binary registry and self-pins,
// bypassing the operator allowlist. The two executions are the D227 "two signals".
//
// THIS RUNS IN THE VERIFIER LANE, OFF THE DAEMON GATE PATH. The daemon never
// calls ExecuteCheck; it only reads the sealed receipt this produces.
func ExecuteCheck(ctx context.Context, allowlist *Allowlist, req RunRequest) (CheckResult, error) {
	var rex ResolvedExec
	if IsBuiltinID(req.CheckID) {
		var err error
		if rex, err = builtinResolvedExec(req.CheckID); err != nil {
			return CheckResult{}, err
		}
	} else {
		entry, err := allowlist.Resolve(req.CheckID)
		if err != nil {
			return CheckResult{}, err
		}
		binarySHA, err := VerifyBinary(entry)
		if err != nil {
			return CheckResult{}, err
		}
		rex = ResolvedExec{
			CheckID:      entry.ID,
			Argv:         append([]string(nil), entry.Argv...),
			BinarySHA256: binarySHA,
			PassWhen:     entry.PassWhen,
		}
	}
	return executeResolved(ctx, rex, req)
}

// ExecuteIntentCheck is the RFC 0141 generatable-shape execution path: it resolves
// a named check against the two-layer allowlist (intent ⋈ pins ⋈ attestations) and
// runs it, carrying the intent's negative_control into the mint. It REFUSES a
// NAMED-but-unpinned check on the typed sentinel (rather than executing unverified
// bytes), so an unfilled template can never silently produce a green receipt. A
// `builtin:*` id short-circuits to the self-pinned builtin road.
func ExecuteIntentCheck(ctx context.Context, intent *AllowlistIntent, pins *Allowlist, attest *Attestations, req RunRequest) (CheckResult, error) {
	if IsBuiltinID(req.CheckID) {
		rex, err := builtinResolvedExec(req.CheckID)
		if err != nil {
			return CheckResult{}, err
		}
		return executeResolved(ctx, rex, req)
	}
	entry, ok := intent.Entry(req.CheckID)
	if !ok {
		return CheckResult{}, fmt.Errorf("check %q is not sanctioned in the verifier allowlist intent (sanctioned: %s); a workflow may NAME a check but never AUTHOR the executed bytes", req.CheckID, strings.Join(intent.IDs(), ", "))
	}
	resolved := JoinIntentPins(intent, pins, attest, HostFingerprint())
	var rc *ResolvedCheck
	for i := range resolved {
		if resolved[i].ID == req.CheckID {
			rc = &resolved[i]
			break
		}
	}
	if rc == nil || rc.Status == StatusNamedUnpinned {
		fix := "striatum verifier pin --host-here"
		return CheckResult{}, fmt.Errorf("check %q is NAMED in the intent but UNPINNED on this host (%s): refuse to run unverified bytes — pin them first: %s", req.CheckID, HostFingerprint(), fix)
	}
	binarySHA, err := VerifyBinary(*rc.Pin)
	if err != nil {
		return CheckResult{}, err
	}
	return executeResolved(ctx, ResolvedExec{
		CheckID:         entry.ID,
		Argv:            append([]string(nil), rc.Pin.Argv...),
		BinarySHA256:    binarySHA,
		NegativeControl: entry.NegativeControl,
		PassWhen:        entry.PassWhen,
	}, req)
}

// executeResolved is the single mint path: it runs the negative control FIRST
// (Pillar 3 — void the receipt if a known-bad unexpectedly passes), then runs the
// check TWICE under the strict envelope for the two-signal agreement, seals the
// transcript (including the builtin markers and the control outcome), and
// classifies — capping a builtin at ASSERTED regardless of posture.
func executeResolved(ctx context.Context, rex ResolvedExec, req RunRequest) (CheckResult, error) {
	cwd := req.Cwd
	if strings.TrimSpace(cwd) == "" {
		cwd, _ = os.Getwd()
	}
	// Resolve to an ABSOLUTE path. The sandbox uses cwd as a bwrap --ro-bind SOURCE,
	// and bwrap resolves a relative source against its own (post-chdir) working dir,
	// so a relative --cwd doubles and vanishes ("bwrap: Can't find source path ...").
	if abs, aerr := filepath.Abs(cwd); aerr == nil {
		cwd = abs
	}
	// The sandbox binds cwd READ-ONLY (the check observes, it does not mutate the
	// tree it witnesses); the scratch dir is the single writable space. A toolchain
	// that needs a writable cache (go-build, GOPATH) must target scratch, so a go
	// builtin requires one — synthesize an ephemeral scratch when the caller gave
	// none, and clean it up after.
	scratch := req.ScratchDir
	if strings.TrimSpace(scratch) == "" && isGoBuiltin(rex) {
		if tmp, terr := os.MkdirTemp("", "verifier-scratch-*"); terr == nil {
			scratch = tmp
			defer func() { _ = os.RemoveAll(tmp) }()
		}
	}
	treeSHA, err := CwdTreeSHA(ctx, cwd)
	if err != nil {
		return CheckResult{}, fmt.Errorf("compute cwd tree-sha: %w", err)
	}
	wrapper, posture := ResolveSandbox(SandboxSpec{
		CwdReadOnly:     cwd,
		ScratchWritable: scratch,
		Limits:          req.Limits,
	})
	runEnv := checkRunEnv(rex, cwd, scratch)
	rex.Argv = goBuildOutputRedirect(rex, scratch)

	// Pillar 3: run the negative control FIRST. A control that PASSES (exit 0) — or
	// could not even run its envelope — means the check does not discriminate the
	// defect it claims to; void the receipt rather than trust a green it cannot earn.
	var control *runOutcome
	if rex.NegativeControl != nil && len(rex.NegativeControl.Argv) > 0 {
		out, runErr := runOnce(ctx, wrapper, rex.NegativeControl.Argv, cwd, req.Limits, runEnv)
		if runErr != nil {
			return CheckResult{}, runErr
		}
		control = &out
		controlFailed := out.exitCode != 0 && !out.timedOut // the control DID fail as required
		if !controlFailed {
			receipt := Receipt{
				CheckID:             rex.CheckID,
				Argv:                append([]string(nil), rex.Argv...),
				BinarySHA256:        rex.BinarySHA256,
				ExitCode:            -1,
				CwdTreeSHA:          treeSHA,
				CreatedAt:           time.Now().UTC().Format(time.RFC3339),
				Posture:             posture,
				BuiltinID:           rex.BuiltinID,
				StriatumVersion:     rex.StriatumVersion,
				NegativeControlExit: out.exitCode,
				NegativeControlVoid: true,
			}
			receipt.SealDigest = receipt.computeSeal()
			return CheckResult{Receipt: receipt, Passed: false, Classification: classVoid, VoidReason: "negative_control_did_not_fail"}, nil
		}
	}

	first, err := runOnce(ctx, wrapper, rex.Argv, cwd, req.Limits, runEnv)
	if err != nil {
		return CheckResult{}, err
	}
	second, err := runOnce(ctx, wrapper, rex.Argv, cwd, req.Limits, runEnv)
	if err != nil {
		return CheckResult{}, err
	}

	// The recorded receipt binds to the FIRST execution's transcript; the second
	// is the independent agreement signal. Re-confirm the tree did not drift
	// between executions (a check that mutated its read-only-bound cwd is a
	// violation → INDETERMINATE).
	postTreeSHA, _ := CwdTreeSHA(ctx, cwd)
	treeStable := postTreeSHA == treeSHA

	agreement := first.exitCode == second.exitCode &&
		first.stdoutSHA == second.stdoutSHA &&
		!first.timedOut && !second.timedOut
	passWhen := rex.PassWhen
	if passWhen == "" {
		passWhen = passWhenExitZero
	}
	passed := passWhen == passWhenExitZero && first.exitCode == 0

	receipt := Receipt{
		CheckID:         rex.CheckID,
		Argv:            append([]string(nil), rex.Argv...),
		BinarySHA256:    rex.BinarySHA256,
		ExitCode:        first.exitCode,
		StdoutSHA256:    first.stdoutSHA,
		CwdTreeSHA:      treeSHA,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
		Posture:         posture,
		AgreementSignal: agreement,
		BuiltinID:       rex.BuiltinID,
		StriatumVersion: rex.StriatumVersion,
	}
	if control != nil {
		receipt.NegativeControlExit = control.exitCode
	}
	receipt.SealDigest = receipt.computeSeal()

	classification := classifyResult(posture, first, agreement, treeStable, passed, rex.BuiltinID != "")
	return CheckResult{Receipt: receipt, Passed: passed, Classification: classification}, nil
}

// classifyResult maps an execution to the lattice rung its receipt can EARN.
// This is the heart of the two-signal rule: VERIFIED requires (a) a STRICT
// sandbox, (b) the independent re-execution AGREEMENT, (c) a stable read-only
// tree, (d) a passing exit-0, AND (e) that the check is NOT a builtin. Anything
// that touches the network, violates the envelope, or times out is INDETERMINATE.
// A lone exit-0 (no strict envelope or no agreement) or ANY builtin earns only
// ASSERTED — a builtin self-pin proves which harness invoked the tool, never which
// tool ran, so it can never independently earn VERIFIED.
func classifyResult(posture Posture, first runOutcome, agreement, treeStable, passed, builtin bool) string {
	if first.timedOut || first.envelopeViolation || !treeStable {
		return classIndeterminate
	}
	if posture.Strict && agreement && passed && !builtin {
		return classVerifiedEligible
	}
	if passed {
		return classAsserted
	}
	// A failing check is a real, recorded result, not an error; the gate reads
	// the exit code. It earns no upgrade.
	return classAsserted
}

type runOutcome struct {
	exitCode          int
	stdoutSHA         string
	timedOut          bool
	envelopeViolation bool
}

// checkRunEnv builds the process env for a sandboxed check run. The sandbox binds
// cwd READ-ONLY, so HOME points at the writable scratch when one is available. A go
// builtin additionally needs GOCACHE/GOPATH in scratch and the host's module cache
// (read-only, visible through the sandbox's ro-bind of /) with the network OFF, so
// `go test/vet/build` runs against an offline, already-cached module. `go` is
// invoked as a bare argv resolved on PATH inside the sandbox; the fixed system PATH
// finds it only when the toolchain sits under a system bin dir, so for a go builtin
// the host go's OWN directory is prepended (resolved lane-side) — otherwise hosts
// that install go outside /usr/*/bin (e.g. ~/.local/go/bin) fail EVERY go builtin
// with a spurious "go: not found" exit 1.
func checkRunEnv(rex ResolvedExec, cwd, scratch string) []string {
	home := cwd
	if strings.TrimSpace(scratch) != "" {
		home = scratch
	}
	path := "/usr/local/bin:/usr/bin:/bin"
	var goEnv []string
	if isGoBuiltin(rex) && strings.TrimSpace(scratch) != "" {
		// Prepend just the host go's directory (not the operator's whole PATH) so
		// `go` resolves while the hermetic envelope — no network, ro rootfs,
		// GOPROXY=off — stays intact.
		if gobin := hostGoBinDir(); gobin != "" {
			path = gobin + ":" + path
		}
		goEnv = []string{
			"GOCACHE=" + filepath.Join(scratch, "go-build"),
			"GOPATH=" + filepath.Join(scratch, "go"),
			"GOPROXY=off", // deps + any toolchain must already be cached; never reach the network
			// GOTOOLCHAIN=auto so a project whose go.mod requires a newer go than the
			// host's base `go` uses the ALREADY-CACHED toolchain (in the read-only host
			// GOMODCACHE) offline; an uncached toolchain fails honestly (the strict
			// sandbox has no network), rather than silently downgrading.
			"GOTOOLCHAIN=auto",
		}
		if mod := hostGoModCache(); mod != "" {
			goEnv = append(goEnv, "GOMODCACHE="+mod)
		}
	}
	env := []string{"PATH=" + path, "HOME=" + home}
	return append(env, goEnv...)
}

// isGoBuiltin reports whether the resolved check is one of the builtin:go-* checks.
func isGoBuiltin(rex ResolvedExec) bool {
	return strings.HasPrefix(rex.BuiltinID, "builtin:go-")
}

// goBuildOutputRedirect points `go build`'s output at the writable scratch dir.
// `go build ./...` writes the executable for a SINGLE main package into the cwd,
// which is read-only in the sandbox; `-o <dir>` makes it write into scratch instead
// (and is correct for multi-package too: each main lands in the dir, non-main
// packages are discarded). No-op for any non-go-build check or when -o is already
// set. The recorded argv reflects what actually ran (the seal stays honest).
func goBuildOutputRedirect(rex ResolvedExec, scratch string) []string {
	if rex.BuiltinID != "builtin:go-build" || strings.TrimSpace(scratch) == "" {
		return rex.Argv
	}
	for _, a := range rex.Argv {
		if a == "-o" {
			return rex.Argv
		}
	}
	outDir := filepath.Join(scratch, "gobuild-out")
	_ = os.MkdirAll(outDir, 0o755)
	out := make([]string, 0, len(rex.Argv)+2)
	for i, a := range rex.Argv {
		out = append(out, a)
		if i == 1 { // insert after the "build" subcommand, before the package args
			out = append(out, "-o", outDir)
		}
	}
	return out
}

var (
	hostGoModCacheValue string
	hostGoModCacheOnce  sync.Once
)

// hostGoModCache returns the host's GOMODCACHE, read LANE-SIDE (outside the sandbox)
// so already-downloaded module deps resolve offline inside it. Best-effort: empty
// on failure (a no-dependency module needs no module cache at all).
func hostGoModCache() string {
	hostGoModCacheOnce.Do(func() {
		out, err := exec.Command("go", "env", "GOMODCACHE").Output() //nolint:gosec // fixed argv, lane-side host config read
		if err == nil {
			hostGoModCacheValue = strings.TrimSpace(string(out))
		}
	})
	return hostGoModCacheValue
}

var (
	hostGoBinDirValue string
	hostGoBinDirOnce  sync.Once
)

// hostGoBinDir returns the directory of the host `go` binary, resolved LANE-SIDE on
// the operator's PATH. The strict sandbox runs with a fixed minimal PATH for
// hermeticity, but a go builtin must still be able to launch `go`; prepending only
// this one directory (never the operator's whole PATH) keeps the envelope tight while
// fixing hosts where the toolchain is not under a system bin dir. Best-effort: empty
// when `go` is not found (the go builtins then fail honestly, as before this fix).
func hostGoBinDir() string {
	hostGoBinDirOnce.Do(func() {
		if p, err := exec.LookPath("go"); err == nil {
			hostGoBinDirValue = filepath.Dir(p)
		}
	})
	return hostGoBinDirValue
}

// runOnce executes the wrapped check once with a wall-clock deadline, hashing
// stdout. A non-zero exit is a normal result (recorded), NOT a Go error; a Go
// error is reserved for failures to launch the sandbox itself.
func runOnce(ctx context.Context, wrapper, argv []string, cwd string, limits SandboxLimits, runEnv []string) (runOutcome, error) {
	deadline := limits.withDefaults().WallClockSeconds
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(deadline)*time.Second)
	defer cancel()

	full := append(append([]string(nil), wrapper...), argv...)
	cmd := exec.CommandContext(execCtx, full[0], full[1:]...) //nolint:gosec // argv is the allowlisted, hash-pinned command wrapped by the resolved sandbox
	cmd.Dir = cwd
	// Minimal, caller-built env: no inherited secrets. checkRunEnv supplies PATH +
	// HOME (and the go-builtin cache vars when needed); the namespace blocks network.
	if len(runEnv) == 0 {
		runEnv = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + cwd}
	}
	cmd.Env = runEnv

	var stdout hashingWriter
	cmd.Stdout = &stdout
	cmd.Stderr = &hashingWriter{} // stderr digest is not part of the receipt; discard

	runErr := cmd.Run()
	out := runOutcome{stdoutSHA: stdout.sum()}
	if execCtx.Err() == context.DeadlineExceeded {
		out.timedOut = true
		out.exitCode = -1
		return out, nil
	}
	if runErr == nil {
		out.exitCode = 0
		return out, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		out.exitCode = exitErr.ExitCode()
		return out, nil
	}
	// A non-ExitError (e.g. the sandbox binary itself failed to start, or a
	// permission/namespace error) is an envelope violation — the check could not
	// run inside its envelope, so its result is INDETERMINATE, not a pass/fail.
	out.envelopeViolation = true
	out.exitCode = -1
	return out, nil
}

// CwdTreeSHA computes a content tree-sha over the worktree using git's plumbing
// against a TEMPORARY index, so it binds to the full working-tree contents (not
// just HEAD). It shells out to git from the LANE, never the daemon. Falls back
// to a deterministic recursive content hash when the dir is not a git tree.
func CwdTreeSHA(ctx context.Context, cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		return "", fmt.Errorf("cwd is empty")
	}
	tmpIndex, err := os.CreateTemp("", "verifier-index-*")
	if err == nil {
		_ = tmpIndex.Close()
		defer func() { _ = os.Remove(tmpIndex.Name()) }()
		env := append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex.Name())
		add := exec.CommandContext(ctx, "git", "-C", cwd, "add", "-A")
		add.Env = env
		if add.Run() == nil {
			wt := exec.CommandContext(ctx, "git", "-C", cwd, "write-tree")
			wt.Env = env
			if out, werr := wt.Output(); werr == nil {
				sha := strings.TrimSpace(string(out))
				if sha != "" {
					return sha, nil
				}
			}
		}
	}
	return recursiveContentSHA(cwd)
}

// recursiveContentSHA is the non-git fallback: a deterministic sha256 over the
// sorted relative paths + content of every regular file under root (skipping
// .git). Deterministic so two runs over an unchanged tree agree.
func recursiveContentSHA(root string) (string, error) {
	type fileHash struct {
		rel string
		sum string
	}
	var files []fileHash
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, readErr := os.ReadFile(path) //nolint:gosec // hashing the worktree the lane already has read access to
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, path)
		sum := sha256.Sum256(data)
		files = append(files, fileHash{rel: filepath.ToSlash(rel), sum: hex.EncodeToString(sum[:])})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.rel))
		h.Write([]byte{0})
		h.Write([]byte(f.sum))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// computeSeal hashes the canonical transcript (every field that makes the
// receipt tamper-evident, including posture and the agreement signal). Any edit
// to the recorded transcript changes the seal, so a hand-tampered receipt cannot
// keep its seal_digest — and a claim binding evidence_digest to the old seal
// auto-decays.
func (r Receipt) computeSeal() string {
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	write("striatum.receipt.v1", r.CheckID)
	write(r.Argv...)
	write(r.BinarySHA256, strconv.Itoa(r.ExitCode), r.StdoutSHA256, r.CwdTreeSHA)
	write(string(r.Posture.Mechanism), strconv.FormatBool(r.Posture.Strict), strconv.FormatBool(r.AgreementSignal))
	// RFC 0141: the builtin markers and the negative-control outcome are part of
	// the tamper-evident transcript — stripping builtin_id to dodge the ASSERTED
	// cap, or flipping the void, changes the seal.
	write(r.BuiltinID, r.StriatumVersion)
	write(strconv.Itoa(r.NegativeControlExit), strconv.FormatBool(r.NegativeControlVoid))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

// hashingWriter is an io.Writer that streams its bytes into a sha256, so the
// receipt records a stdout digest without buffering the whole stream.
type hashingWriter struct{ h hash.Hash }

func (w *hashingWriter) Write(p []byte) (int, error) {
	if w.h == nil {
		w.h = sha256.New()
	}
	return w.h.Write(p)
}

func (w *hashingWriter) sum() string {
	if w.h == nil {
		w.h = sha256.New()
	}
	return hex.EncodeToString(w.h.Sum(nil))
}
