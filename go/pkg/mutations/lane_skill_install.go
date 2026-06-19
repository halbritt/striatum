package mutations

import (
	"fmt"
	"log"
	"strings"

	"github.com/halbritt/striatum/go/pkg/installers"
)

// installLaneUserSkillBundle installs the RFC 0015 claude_code skill bundle
// into the lane OS user's home directory (~/.claude/skills/) at supervise start
// (issue #445 / Option 1).
//
// Rationale: the skill bundle is normally installed by `striatum skills install`
// into a project .claude/skills/ directory that is gitignored — absent from the
// per-job worktree checkout — and is never written to the lane user's home. A
// CLI-fallback-path agent (one that prefers `striatum <verb>` CLI calls rather
// than MCP tools) therefore has no access to the RFC 0015 protocol skill
// documents and must rely solely on its bootstrap prompt. Installing to
// user-scope at launch time makes the bundle unconditionally available to any
// claude lane running as that user, regardless of which target repo it works in
// or whether a project-scope install was ever run.
//
// Safety properties:
//   - Idempotent: the installers.InstallSkills pipeline detects unchanged files
//     (SHA-256 comparison against the manifest) and skips re-writing them, so
//     repeated supervise start calls are cheap.
//   - Non-fatal: a failure to write (e.g. the lane user's home is unwritable or
//     does not exist) emits a warning and allows the lane to proceed. The skill
//     bundle is a best-effort protocol aid; its absence must never block a spawn.
//   - No-op when laneUser is empty: the daemon-user case (laneUser=="") means
//     the operator's own home already has the bundle from a normal install run.
func installLaneUserSkillBundle(laneUser string) {
	laneUser = strings.TrimSpace(laneUser)
	if laneUser == "" {
		return
	}
	home := laneOSUserHome(laneUser)
	if strings.TrimSpace(home) == "" {
		log.Printf("striatum: supervise start: cannot resolve home dir for lane user %q; skipping skill bundle install", laneUser)
		return
	}
	_, err := installers.InstallSkills(installers.SkillsParams{
		Home:    home,
		Profile: "claude_code",
		Scope:   "user",
	})
	if err != nil {
		log.Printf("striatum: supervise start: non-fatal: could not install skill bundle into lane user %q home %q: %v", laneUser, home, err)
	}
}

// laneSkillInstallFn is the package-level hook for installLaneUserSkillBundle,
// exposed as a variable so tests can override it without requiring a real OS
// user or a writable home directory.
var laneSkillInstallFn = installLaneUserSkillBundle

// installLaneUserSkillBundleNonFatal is the call site used by HandleSuperviseStart.
// It delegates to laneSkillInstallFn (overridable in tests) and wraps the call
// in a named function so the intent is clear at the supervise start call site.
func installLaneUserSkillBundleNonFatal(laneUser string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("striatum: supervise start: non-fatal panic during skill bundle install for lane user %q: %v", laneUser, fmt.Sprint(r))
		}
	}()
	laneSkillInstallFn(laneUser)
}
