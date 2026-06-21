package admin

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

// LaneOSUserEnv names the OS user supervised lanes are spawned as when the
// operator has adopted the PG-less lane sandbox (docs/how-to/lane-sandbox.md).
// It mirrors the constant used in cmd/striatumd (daemonLaneOSUserEnv) and
// pkg/reads (laneOSUserEnv); kept local so the admin package does not import the
// daemon binary. When unset (or equal to the daemon's own user) lanes run as the
// daemon's OS user and need no cross-user ACL provisioning.
const LaneOSUserEnv = "STRIATUM_LANE_OS_USER"

// setRepoACL applies (`setfacl -R -m`) ACL entries recursively to a path. It is
// a package variable so tests can observe the planned ACL operations without
// requiring root or the setfacl binary (mirrors setDaemonSocketACL in
// cmd/striatumd and setScratchACL in pkg/mutations).
var setRepoACL = func(path string, specs ...string) error {
	args := []string{"-R"}
	for _, spec := range specs {
		args = append(args, "-m", spec)
	}
	args = append(args, path)
	return exec.Command("setfacl", args...).Run()
}

// repoACLLookupUser is overridable for tests; it resolves the lane OS user so a
// misconfigured (non-existent) lane user does not cause repo.init to shell out
// to setfacl with a bogus principal.
var repoACLLookupUser = func(name string) error {
	_, err := user.Lookup(name)
	return err
}

// currentRepoOwner resolves the daemon's own OS user (the user repo.init runs
// as), used to add an inheritable default ACL so files the lane later creates
// under the repo tree stay manageable by the daemon/operator without sudo
// (#539). Overridable for tests.
var currentRepoOwner = func() string {
	if u, err := user.Current(); err == nil && strings.TrimSpace(u.Username) != "" {
		return strings.TrimSpace(u.Username)
	}
	return strings.TrimSpace(os.Getenv("USER"))
}

// configuredRepoLaneUser returns the lane OS user that needs the committee ACL,
// collapsing the owner case to "" exactly like the socket-ACL path: when lanes
// run as the daemon's own user there is no cross-user permission gap to bridge,
// so provisioning is a no-op.
func configuredRepoLaneUser() string {
	laneUser := strings.TrimSpace(os.Getenv(LaneOSUserEnv))
	if laneUser == "" {
		return ""
	}
	if owner := currentRepoOwner(); owner != "" && laneUser == owner {
		return ""
	}
	return laneUser
}

// provisionCommitteeACLs grants the lane OS user the POSIX ACLs a
// `review_only_artifact` lane needs to publish into the repo tree, and grants
// the daemon/operator an inheritable default ACL so lane-written committee
// provenance stays manageable without sudo. It mirrors the convention already
// applied to repos set up before the ACL convention existed (#537 verified fix:
// `setfacl -R -m u:<lane>:rwx -m d:u:<lane>:rwx -m d:u:<owner>:rwx <repo>`):
//
//   - `u:<lane>:rwx`   — access ACL: the lane can read/write/traverse existing
//     tree entries (so a no-repo-write review lane can create its staging path).
//   - `d:u:<lane>:rwx` — default ACL: files/dirs created later inherit the grant,
//     so a freshly-created committee dir is lane-writable (#537).
//   - `d:u:<owner>:rwx`— default ACL: files/dirs the *lane* creates inherit an
//     owner grant, so the daemon (running as <owner>) can `decision record` into
//     a lane-created committee dir and the operator can land it without
//     `sudo chown` (#539).
//
// The recursive form (`-R`) and the inheritable default ACLs (`d:`) together are
// the load-bearing combination — an access-only grant would not survive new
// directory creation, and a default-only grant would not cover existing entries.
//
// Provisioning is a strict no-op when:
//   - lanes run as the daemon owner (configuredRepoLaneUser collapses to ""),
//   - the platform has no setfacl semantics (windows),
//   - the configured lane user does not exist (don't apply a bogus principal).
//
// This only EXTENDS an already-accepted provisioning convention to a path that
// lacked it (clone/worktree-registered repos). It does not widen who can read
// any daemon capability token or private operator state; it touches the target
// repository tree and its `.striatum/worktrees` directory only.
func provisionCommitteeACLs(repoRoot string) error {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return nil
	}
	if runtime.GOOS == "windows" {
		// configuredLaneRunAsUser already rejects RunAsUser on windows at config
		// time; guard here too so the helper is safe to call unconditionally.
		return nil
	}
	laneUser := configuredRepoLaneUser()
	if laneUser == "" {
		return nil
	}
	if err := repoACLLookupUser(laneUser); err != nil {
		// A misconfigured lane user surfaces elsewhere (doctor lane_pg_reachable);
		// repo.init must not fail registration on it, and must not apply an ACL for
		// a principal that does not exist. Skip quietly.
		return nil
	}

	specs := []string{
		"u:" + laneUser + ":rwx",
		"d:u:" + laneUser + ":rwx",
	}
	if owner := currentRepoOwner(); owner != "" && owner != laneUser {
		specs = append(specs, "d:u:"+owner+":rwx")
	}

	// The repo tree itself (covers review-only lane staging paths anywhere under
	// the target repository), plus the per-job worktree root the daemon stages
	// isolated lanes into. Provision the worktrees root explicitly so the default
	// ACL is present even before the first worktree is created.
	worktreesRoot := filepath.Join(repoRoot, ".striatum", "worktrees")
	if err := os.MkdirAll(worktreesRoot, 0o755); err != nil {
		return fmt.Errorf("create %s for committee ACL provisioning: %w", worktreesRoot, err)
	}
	for _, target := range []string{repoRoot, worktreesRoot} {
		if err := setRepoACL(target, specs...); err != nil {
			return fmt.Errorf("provision committee ACL on %s: %w", target, err)
		}
	}
	return nil
}

// ProvisionCommitteeACLsResult runs provisionCommitteeACLs and reports whether
// any ACL was applied (false when it was a no-op: owner-run lanes, no lane user,
// windows) and any error. repo.add / repo.init treat the error as advisory so a
// missing `setfacl` binary or an unprivileged daemon never blocks registration —
// but it is surfaced in the result so an operator can see why review-only lanes
// might still fail to publish. Exported so the live `repo.add` handler
// (pkg/repositories) shares one provisioning convention with `repo.init`.
func ProvisionCommitteeACLsResult(repoRoot string) (bool, error) {
	if configuredRepoLaneUser() == "" {
		return false, nil
	}
	if err := provisionCommitteeACLs(repoRoot); err != nil {
		return false, err
	}
	return true, nil
}

// WithCommitteeACLResult annotates a repo registration result with the
// committee-ACL provisioning outcome so the operator/CLI can report it.
func WithCommitteeACLResult(result map[string]any, provisioned bool, err error) map[string]any {
	result["committee_acl_provisioned"] = provisioned
	if err != nil {
		result["committee_acl_error"] = err.Error()
	}
	return result
}
