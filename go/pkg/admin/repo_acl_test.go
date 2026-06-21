package admin

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

type repoACLCall struct {
	path  string
	specs []string
}

// stubRepoACL replaces the setfacl shell-out and the OS-user lookups with
// in-memory recorders so the committee-ACL provisioning decision logic can be
// unit-tested without root, the setfacl binary, or a real lane user. It returns
// the recorded calls slice and a restore func.
func stubRepoACL(t *testing.T, owner string, knownUsers ...string) (*[]repoACLCall, func()) {
	t.Helper()
	prevSet := setRepoACL
	prevLookup := repoACLLookupUser
	prevOwner := currentRepoOwner
	calls := []repoACLCall{}
	setRepoACL = func(path string, specs ...string) error {
		calls = append(calls, repoACLCall{path: path, specs: append([]string(nil), specs...)})
		return nil
	}
	known := map[string]bool{}
	for _, u := range knownUsers {
		known[u] = true
	}
	repoACLLookupUser = func(name string) error {
		if known[name] {
			return nil
		}
		return errors.New("no such user")
	}
	currentRepoOwner = func() string { return owner }
	return &calls, func() {
		setRepoACL = prevSet
		repoACLLookupUser = prevLookup
		currentRepoOwner = prevOwner
	}
}

func TestProvisionCommitteeACLsSkipsWhenLaneEnvUnset(t *testing.T) {
	t.Setenv(LaneOSUserEnv, "")
	calls, restore := stubRepoACL(t, "halbritt", "striatum-lane")
	defer restore()

	repo := t.TempDir()
	if err := provisionCommitteeACLs(repo); err != nil {
		t.Fatalf("provisionCommitteeACLs() error = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("setfacl should not run without %s, got %#v", LaneOSUserEnv, *calls)
	}
}

func TestProvisionCommitteeACLsSkipsWhenLaneUserIsOwner(t *testing.T) {
	t.Setenv(LaneOSUserEnv, "halbritt")
	calls, restore := stubRepoACL(t, "halbritt", "halbritt")
	defer restore()

	repo := t.TempDir()
	if err := provisionCommitteeACLs(repo); err != nil {
		t.Fatalf("provisionCommitteeACLs() error = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("setfacl should not run when lane user == owner, got %#v", *calls)
	}
}

func TestProvisionCommitteeACLsSkipsWhenLaneUserMissing(t *testing.T) {
	t.Setenv(LaneOSUserEnv, "ghost-lane")
	// ghost-lane is NOT in the known-users set, so the lookup fails.
	calls, restore := stubRepoACL(t, "halbritt", "striatum-lane")
	defer restore()

	repo := t.TempDir()
	if err := provisionCommitteeACLs(repo); err != nil {
		t.Fatalf("provisionCommitteeACLs() should skip (not error) on missing lane user, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("setfacl should not run for a non-existent lane user, got %#v", *calls)
	}
}

func TestProvisionCommitteeACLsAppliesLaneAndOwnerDefaults(t *testing.T) {
	t.Setenv(LaneOSUserEnv, "striatum-lane")
	calls, restore := stubRepoACL(t, "halbritt", "striatum-lane")
	defer restore()

	repo := t.TempDir()
	if err := provisionCommitteeACLs(repo); err != nil {
		t.Fatalf("provisionCommitteeACLs() error = %v", err)
	}

	worktrees := filepath.Join(repo, ".striatum", "worktrees")
	if info, err := os.Stat(worktrees); err != nil || !info.IsDir() {
		t.Fatalf("expected %s to be created as a directory, err=%v", worktrees, err)
	}

	wantSpecs := []string{
		"u:striatum-lane:rwx",   // access ACL: lane can write existing entries (#537)
		"d:u:striatum-lane:rwx", // default ACL: created entries stay lane-writable (#537)
		"d:u:halbritt:rwx",      // default ACL: lane-created entries stay owner-manageable (#539)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 setfacl targets (repo tree + worktrees root), got %d: %#v", len(*calls), *calls)
	}
	gotPaths := []string{(*calls)[0].path, (*calls)[1].path}
	sort.Strings(gotPaths)
	wantPaths := []string{repo, worktrees}
	sort.Strings(wantPaths)
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("setfacl target paths = %#v, want %#v", gotPaths, wantPaths)
	}
	for _, call := range *calls {
		if !reflect.DeepEqual(call.specs, wantSpecs) {
			t.Fatalf("setfacl specs on %s = %#v, want %#v", call.path, call.specs, wantSpecs)
		}
	}
}

func TestProvisionCommitteeACLsOmitsOwnerDefaultWhenOwnerUnknown(t *testing.T) {
	t.Setenv(LaneOSUserEnv, "striatum-lane")
	calls, restore := stubRepoACL(t, "", "striatum-lane") // empty owner
	defer restore()

	repo := t.TempDir()
	if err := provisionCommitteeACLs(repo); err != nil {
		t.Fatalf("provisionCommitteeACLs() error = %v", err)
	}
	wantSpecs := []string{"u:striatum-lane:rwx", "d:u:striatum-lane:rwx"}
	for _, call := range *calls {
		if !reflect.DeepEqual(call.specs, wantSpecs) {
			t.Fatalf("setfacl specs on %s = %#v, want %#v (no owner default)", call.path, call.specs, wantSpecs)
		}
	}
}

func TestProvisionCommitteeACLsResultReportsNoOp(t *testing.T) {
	t.Setenv(LaneOSUserEnv, "")
	_, restore := stubRepoACL(t, "halbritt", "striatum-lane")
	defer restore()

	provisioned, err := ProvisionCommitteeACLsResult(t.TempDir())
	if err != nil {
		t.Fatalf("ProvisionCommitteeACLsResult() error = %v", err)
	}
	if provisioned {
		t.Fatalf("expected committee_acl_provisioned=false for owner-run lanes")
	}
}

func TestProvisionCommitteeACLsResultReportsApplied(t *testing.T) {
	t.Setenv(LaneOSUserEnv, "striatum-lane")
	_, restore := stubRepoACL(t, "halbritt", "striatum-lane")
	defer restore()

	provisioned, err := ProvisionCommitteeACLsResult(t.TempDir())
	if err != nil {
		t.Fatalf("ProvisionCommitteeACLsResult() error = %v", err)
	}
	if !provisioned {
		t.Fatalf("expected committee_acl_provisioned=true when lane user is set and exists")
	}
}

func TestWithCommitteeACLResultAnnotates(t *testing.T) {
	base := map[string]any{"repository_id": "repo_x"}
	out := WithCommitteeACLResult(base, true, nil)
	if out["committee_acl_provisioned"] != true {
		t.Fatalf("committee_acl_provisioned not annotated: %#v", out)
	}
	if _, ok := out["committee_acl_error"]; ok {
		t.Fatalf("committee_acl_error should be absent on success: %#v", out)
	}
	out2 := WithCommitteeACLResult(map[string]any{}, false, errors.New("setfacl boom"))
	if out2["committee_acl_error"] != "setfacl boom" {
		t.Fatalf("committee_acl_error not annotated: %#v", out2)
	}
}
