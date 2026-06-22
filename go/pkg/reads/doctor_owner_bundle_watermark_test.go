package reads

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// watermarkDoctorRunner is a canned Runner that drives ownerBundleWatermarkDoctorBlock
// (and HandleDoctor through it) by answering the two queries OwnerBundleVersion
// issues: the to_regclass presence probe and the MAX(version) read. substrate_version
// answers keep the rest of HandleDoctor happy.
type watermarkDoctorRunner struct {
	metaPresent bool
	applied     string
}

func (r *watermarkDoctorRunner) Exec(_ context.Context, _ string, _ ...any) error { return nil }

func (r *watermarkDoctorRunner) QueryRow(_ context.Context, _ string, _ ...any) db.Row {
	return doctorFakeRow{}
}

func (r *watermarkDoctorRunner) QueryScalar(_ context.Context, sql string, _ ...any) (string, error) {
	switch {
	case strings.Contains(sql, "substrate_version"):
		return "42", nil
	case strings.Contains(sql, "to_regclass") && strings.Contains(sql, "owner_bundle_meta"):
		if r.metaPresent {
			return "true", nil
		}
		return "false", nil
	case strings.Contains(sql, "MAX(version)"):
		return r.applied, nil
	default:
		return "", nil
	}
}

func (r *watermarkDoctorRunner) BeginTx(_ context.Context) (db.TxRunner, error) {
	return nil, errors.New("not implemented")
}

// TestOwnerBundleWatermarkDoctorBlockShortfall is the RFC 0142 Layer 2 doctor
// precondition: when the applied owner-bundle watermark lags the required
// frontier, the block reports a "shortfall" with the apply remediation and emits
// a hard problem so doctor goes RED before the operator restarts onto a binary
// that would halt with awaiting_owner_ddl.
func TestOwnerBundleWatermarkDoctorBlockShortfall(t *testing.T) {
	runner := &watermarkDoctorRunner{metaPresent: true, applied: "1"}
	block, problems := ownerBundleWatermarkDoctorBlock(context.Background(), runner)
	if block["status"] != "shortfall" {
		t.Fatalf("status = %v; want shortfall", block["status"])
	}
	if block["remediation"] != "striatum daemon owner-ddl apply" {
		t.Fatalf("remediation = %v; want the owner-ddl apply command", block["remediation"])
	}
	if len(problems) != 1 {
		t.Fatalf("want exactly one problem, got %v", problems)
	}
	for _, needle := range []string{"owner_bundle_watermark_shortfall", "striatum daemon owner-ddl apply", "awaiting_owner_ddl"} {
		if !strings.Contains(problems[0], needle) {
			t.Fatalf("problem missing %q; got: %s", needle, problems[0])
		}
	}
}

// TestOwnerBundleWatermarkDoctorBlockHealthy covers the three healthy cases: in
// sync, ahead (tolerate-forward), and fresh/single-role (applied == 0). None emit
// a problem.
func TestOwnerBundleWatermarkDoctorBlockHealthy(t *testing.T) {
	required := db.RequiredOwnerBundleVersion
	cases := []struct {
		name        string
		metaPresent bool
		applied     string
		wantStatus  string
	}{
		{"in_sync", true, strconv.Itoa(required), "in_sync"},
		{"ahead_tolerate_forward", true, strconv.Itoa(required + 1), "ahead"},
		{"fresh_single_role", false, "0", "bootstrap"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &watermarkDoctorRunner{metaPresent: tc.metaPresent, applied: tc.applied}
			block, problems := ownerBundleWatermarkDoctorBlock(context.Background(), runner)
			if len(problems) != 0 {
				t.Fatalf("healthy case emitted problems: %v", problems)
			}
			if block["status"] != tc.wantStatus {
				t.Fatalf("status = %v; want %s", block["status"], tc.wantStatus)
			}
		})
	}
}

// TestHandleDoctorSurfacesWatermarkShortfall confirms the precondition flows
// through the full HandleDoctor surface: a shortfall flips ok=false, adds the
// problem to the problems list, and includes the owner_bundle_watermark block.
func TestHandleDoctorSurfacesWatermarkShortfall(t *testing.T) {
	runner := &watermarkDoctorRunner{metaPresent: true, applied: "1"}
	result, err := HandleDoctor(context.Background(), runner, rpc.Envelope{Params: map[string]any{}})
	if err != nil {
		t.Fatalf("HandleDoctor: %v", err)
	}
	if result["ok"] != false {
		t.Fatalf("ok = %v; want false (watermark shortfall is a hard problem)", result["ok"])
	}
	block, ok := result["owner_bundle_watermark"].(map[string]any)
	if !ok {
		t.Fatalf("doctor result missing owner_bundle_watermark block: %#v", result["owner_bundle_watermark"])
	}
	if block["status"] != "shortfall" {
		t.Fatalf("owner_bundle_watermark.status = %v; want shortfall", block["status"])
	}
	problems, _ := result["problems"].([]string)
	found := false
	for _, p := range problems {
		if strings.HasPrefix(p, "owner_bundle_watermark_shortfall") {
			found = true
		}
	}
	if !found {
		t.Fatalf("doctor problems missing the watermark shortfall: %v", problems)
	}
}
