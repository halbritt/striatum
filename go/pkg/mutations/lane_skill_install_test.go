package mutations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/installers"
)

// TestInstallLaneUserSkillBundleLandsAllSixSkills verifies that
// installLaneUserSkillBundle writes all six RFC 0015 claude_code skill files
// into a temp HOME directory and that re-running is safe (idempotent).
func TestInstallLaneUserSkillBundleLandsAllSixSkills(t *testing.T) {
	home := t.TempDir()
	laneUser := "striatum-lane"

	// Override the OS user lookup to point at the temp HOME.
	orig := laneOSUserHome
	laneOSUserHome = func(name string) string {
		if name == laneUser {
			return home
		}
		return orig(name)
	}
	defer func() { laneOSUserHome = orig }()

	// First install.
	installLaneUserSkillBundle(laneUser)

	// Verify all six skill files landed.
	expectedSkills := []string{"workflow", "scaffold", "claim-loop", "supervise", "recover", "mcp"}
	for _, skill := range expectedSkills {
		skillPath := filepath.Join(home, ".claude", "skills", skill, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Errorf("skill %q missing at %s: %v", skill, skillPath, err)
		}
	}

	// Verify the manifest was written (idempotency marker).
	manifestPath := filepath.Join(home, ".claude", "skills", "workflow", ".manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("skill manifest missing at %s: %v", manifestPath, err)
	}

	// Second install must not error (idempotent).
	installLaneUserSkillBundle(laneUser)

	// Skill files must still exist and be non-empty after the second install.
	for _, skill := range expectedSkills {
		skillPath := filepath.Join(home, ".claude", "skills", skill, "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			t.Errorf("skill %q unreadable after second install: %v", err, err)
			continue
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			t.Errorf("skill %q is empty after second install", skill)
		}
	}
}

// TestInstallLaneUserSkillBundleEmptyUserIsNoOp verifies that an empty laneUser
// argument skips the install entirely (daemon-user / non-sandboxed lane case).
func TestInstallLaneUserSkillBundleEmptyUserIsNoOp(t *testing.T) {
	called := false
	orig := laneOSUserHome
	laneOSUserHome = func(name string) string {
		called = true
		return orig(name)
	}
	defer func() { laneOSUserHome = orig }()

	installLaneUserSkillBundle("")

	if called {
		t.Error("laneOSUserHome was called for an empty laneUser; expected early return")
	}
}

// TestInstallLaneUserSkillBundleUnresolvableHomeIsNonFatal verifies that when
// the lane user's home directory cannot be resolved, the call logs and returns
// without panicking or returning an error (failure tolerance).
func TestInstallLaneUserSkillBundleUnresolvableHomeIsNonFatal(t *testing.T) {
	orig := laneOSUserHome
	laneOSUserHome = func(name string) string { return "" } // simulate lookup failure
	defer func() { laneOSUserHome = orig }()

	// Must not panic.
	installLaneUserSkillBundle("nonexistent-lane-user")
}

// TestInstallLaneUserSkillBundleUnwritableHomeIsNonFatal verifies that an
// unwritable home directory causes a log warning rather than a fatal error.
func TestInstallLaneUserSkillBundleUnwritableHomeIsNonFatal(t *testing.T) {
	// Create a directory we make read-only.
	home := t.TempDir()
	if err := os.Chmod(home, 0o555); err != nil {
		t.Skip("cannot chmod temp dir:", err)
	}
	defer func() { _ = os.Chmod(home, 0o755) }()

	laneUser := "striatum-lane"
	orig := laneOSUserHome
	laneOSUserHome = func(name string) string {
		if name == laneUser {
			return home
		}
		return orig(name)
	}
	defer func() { laneOSUserHome = orig }()

	// Must not panic; the failure is logged, not propagated.
	installLaneUserSkillBundle(laneUser)
}

// TestInstallLaneUserSkillBundleNonFatalPanicSafe verifies that a panic inside
// laneSkillInstallFn does not escape to the caller.
func TestInstallLaneUserSkillBundleNonFatalPanicSafe(t *testing.T) {
	orig := laneSkillInstallFn
	laneSkillInstallFn = func(string) { panic("simulated panic") }
	defer func() { laneSkillInstallFn = orig }()

	// Must not propagate the panic.
	installLaneUserSkillBundleNonFatal("striatum-lane")
}

// TestInstallSkillsUserScopeWritesExpectedFiles is an integration-level check
// that directly exercises installers.InstallSkills with scope="user" and a temp
// HOME, asserting the six skill files land under .claude/skills/.
func TestInstallSkillsUserScopeWritesExpectedFiles(t *testing.T) {
	home := t.TempDir()
	result, err := installers.InstallSkills(installers.SkillsParams{
		Home:    home,
		Profile: "claude_code",
		Scope:   "user",
	})
	if err != nil {
		t.Fatalf("InstallSkills: %v", err)
	}
	if result.Scope != "user" {
		t.Errorf("scope = %q, want %q", result.Scope, "user")
	}
	if len(result.Files) != 6 {
		t.Errorf("files count = %d, want 6", len(result.Files))
	}
	// All files must be written (status "written") on first run.
	for _, f := range result.Files {
		if f.Status != "written" {
			t.Errorf("file %q status = %q, want %q", f.Path, f.Status, "written")
		}
	}
	// Re-run must be idempotent (all skipped_unchanged).
	result2, err := installers.InstallSkills(installers.SkillsParams{
		Home:    home,
		Profile: "claude_code",
		Scope:   "user",
	})
	if err != nil {
		t.Fatalf("InstallSkills (second run): %v", err)
	}
	for _, f := range result2.Files {
		if f.Status != "skipped_unchanged" {
			t.Errorf("file %q status on second run = %q, want %q", f.Path, f.Status, "skipped_unchanged")
		}
	}
}
