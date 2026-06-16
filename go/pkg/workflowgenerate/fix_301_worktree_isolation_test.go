package workflowgenerate

import (
	"testing"

	"github.com/halbritt/striatum/go/pkg/workflowauthoring"
)

// #301: workflow generate must emit worktree_isolation: per_job on EVERY
// autonomous repo-write lane in a multi-lane set, not just the first one. Before
// the fix the lane-name heuristic ("any lane named *reviewer* is review-only")
// left the reviewer lane without isolation even though divergent_ideation
// round-robins repo-write diverge/deepen jobs onto it, so the generator's own
// output failed RefuseAutonomousSharedCheckoutRepoWrite at validate/prepare.
func TestMultiLaneSetsEmitWorktreeIsolationOnAllLanes(t *testing.T) {
	cases := []struct {
		name    string
		laneSet string
		lanes   map[string]any
		options map[string]any
		want    []string // lanes expected to carry per-job isolation
	}{
		{
			name:    "author_reviewer",
			laneSet: "author_reviewer",
			lanes: map[string]any{
				"author":   map[string]any{"command": []any{"claude", "--dangerously-skip-permissions"}},
				"reviewer": map[string]any{"command": []any{"claude", "--dangerously-skip-permissions"}},
			},
			want: []string{"author", "reviewer"},
		},
		{
			name:    "multi_review",
			laneSet: "multi_review",
			lanes: map[string]any{
				"author":     map[string]any{"command": []any{"claude", "--dangerously-skip-permissions"}},
				"reviewer_1": map[string]any{"command": []any{"claude", "--dangerously-skip-permissions"}},
				"reviewer_2": map[string]any{"command": []any{"claude", "--dangerously-skip-permissions"}},
			},
			options: map[string]any{"reviewer_count": 2},
			want:    []string{"author", "reviewer_1", "reviewer_2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := SpecFromMap(divergentRaw(tc.laneSet, tc.lanes, nil, tc.options))
			if err != nil {
				t.Fatalf("spec: %v", err)
			}
			// Crucially: NO worktree_isolated modifier — generate must produce
			// validate-clean output for the bare multi-lane set on its own.
			gen, err := Generate(spec)
			if err != nil {
				t.Fatalf("generate (must validate): %v", err)
			}

			lanes := mapFrom(gen.Workflow["lanes"])
			// Confirm the reviewer lane(s) actually carry a repo-write job in this
			// fan-out shape — otherwise the assertion below would be vacuous.
			jobs := jobsOf(t, gen)
			reposByLane := map[string]bool{}
			for _, j := range jobs {
				if jobDeclaresRepoWrite(j) {
					if laneID, ok := j["lane_id"].(string); ok {
						reposByLane[laneID] = true
					}
				}
			}
			for _, laneID := range tc.want {
				if !reposByLane[laneID] {
					t.Fatalf("precondition: lane %q carries no repo-write job in %s (test would be vacuous)", laneID, tc.name)
				}
				lane := mapFrom(lanes[laneID])
				if lane["worktree_isolation"] != "per_job" {
					t.Errorf("lane %q worktree_isolation = %#v, want per_job", laneID, lane["worktree_isolation"])
				}
			}

			// The generator's own output must clear the #242 launch gate that
			// validate / run prepare enforce — no hand-edit required.
			if err := workflowauthoring.RefuseAutonomousSharedCheckoutRepoWrite(gen.Workflow); err != nil {
				t.Fatalf("RefuseAutonomousSharedCheckoutRepoWrite rejected the generator's own output: %v", err)
			}
		})
	}
}
