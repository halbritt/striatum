package workflowgenerate

import "fmt"

// enableSourceChangePublish marks every repo-write job's write_scope with
// publish_source_changes=true (#287) so the daemon commits the lane's in-scope
// source edits to the run branch at work.complete, alongside its declared
// artifacts. Review-only jobs (repo_write=false) are left untouched.
func enableSourceChangePublish(jobs []map[string]any) {
	for _, j := range jobs {
		scope, ok := j["write_scope"].(map[string]any)
		if !ok {
			continue
		}
		if scope["repo_write"] == true {
			scope["publish_source_changes"] = true
		}
	}
}

func compileShape(spec Spec) ([]map[string]any, []map[string]any, []map[string]any, []map[string]any, error) {
	base := spec.ArtifactRoot
	authorLane := authorLane(spec)
	reviewerLaneID := reviewerLane(spec, 1)
	switch spec.Shape {
	case "minimal":
		return []map[string]any{job("draft", "draft", "Draft starter artifact", "author", authorLane, base, "DRAFT.md", "handoff", "draft", "draft", "Produce the starter artifact for this workflow.")}, nil, nil, nil, nil
	case "review", "code_change":
		jobs := []map[string]any{
			job("draft", "draft", "Draft starter artifact", "author", authorLane, base, "DRAFT.md", "handoff", "draft", "draft", "Produce the starter artifact for this workflow."),
			reviewJob("review", reviewerLaneID, base+"/review/REVIEW.md", firstPosture(spec)),
			job("apply", "synthesis", "Apply the accepted review", "author", authorLane, base, "SUMMARY.md", "synthesis", "summary", "apply", "Apply the accepted review findings."),
		}
		edges := []map[string]any{{"from": "draft", "to": "review", "on": "completed"}, {"from": "review", "to": "apply", "on": "completed"}}
		cycles := []map[string]any{}
		if spec.Shape == "code_change" {
			max, err := maxCycles(spec)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			cycles = append(cycles, map[string]any{"from": "review", "to": "draft", "on_verdict": "needs_revision", "max_iterations": max})
			// #287: a code_change dogfood's point is reviewable code on the run
			// branch. Opt its repo-write jobs into source-change publishing so the
			// lane's actual edits land on the run branch (not only the declared
			// markdown). The operator widens allowed_paths from the artifact root to
			// the source paths the change touches; the publish then follows.
			enableSourceChangePublish(jobs)
		}
		return jobs, edges, cycles, nil, nil
	case "human_checkpoint":
		jobs := []map[string]any{
			job("analysis", "draft", "Analyze the requested decision", "author", authorLane, base, "ANALYSIS.md", "handoff", "analysis", "draft", ""),
			job("checkpoint", "human_checkpoint", "Open a human checkpoint", "reviewer", reviewerLaneID, base, "CHECKPOINT.md", "handoff", "checkpoint", "review", ""),
			job("apply", "synthesis", "Apply the owner decision", "author", authorLane, base, "SUMMARY.md", "synthesis", "summary", "apply", ""),
		}
		return jobs, []map[string]any{{"from": "analysis", "to": "checkpoint", "on": "completed"}, {"from": "checkpoint", "to": "apply", "on": "completed"}}, nil, nil, nil
	case "evidence_backed":
		jobs := []map[string]any{
			job("draft", "draft", "Draft evidence-backed artifact", "author", authorLane, base, "DRAFT.md", "handoff", "draft", "draft", ""),
			job("support_ledger", "build", "Map claims to evidence", "author", authorLane, base+"/support", "SUPPORT_LEDGER.md", "support_ledger", "support_ledger", "support_ledger", ""),
			reviewJob("evidence_audit", reviewerLaneID, base+"/audit/EVIDENCE_AUDIT.md", "devils_advocate"),
			reviewJob("final_review", reviewerLaneID, base+"/final/FINAL_REVIEW.md", firstPosture(spec)),
		}
		return jobs, []map[string]any{{"from": "draft", "to": "support_ledger", "on": "completed"}, {"from": "support_ledger", "to": "evidence_audit", "on": "completed"}, {"from": "evidence_audit", "to": "final_review", "on": "completed"}}, nil, nil, nil
	case "multi_review_synthesis":
		count := reviewerCount(spec)
		postures, err := postures(spec, count)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		jobs := []map[string]any{}
		edges := []map[string]any{}
		for idx := 1; idx <= count; idx++ {
			id := fmt.Sprintf("review_%d", idx)
			jobs = append(jobs, reviewJob(id, reviewerLane(spec, idx), fmt.Sprintf("%s/review_%d/REVIEW.md", base, idx), postures[idx-1]))
			edges = append(edges, map[string]any{"from": id, "to": "synthesis", "on": "completed"})
		}
		jobs = append(jobs,
			job("synthesis", "synthesis", "Synthesize independent reviews", "author", authorLane, base, "SYNTHESIS.md", "synthesis", "synthesis", "apply", ""),
			reviewJob("final_review", reviewerLane(spec, 1), base+"/final/FINAL_REVIEW.md", "neutral"),
		)
		edges = append(edges, map[string]any{"from": "synthesis", "to": "final_review", "on": "completed"})
		return jobs, edges, nil, nil, nil
	case "conversation":
		turns := 3
		if raw, ok := spec.Options["turns"]; ok {
			if v, ok := raw.(float64); ok {
				turns = int(v)
			} else if v, ok := raw.(int); ok {
				turns = v
			}
		}
		topic := "unspecified topic"
		if raw, ok := spec.Options["topic"]; ok {
			if v, ok := raw.(string); ok {
				topic = v
			}
		}
		jobs := []map[string]any{}
		edges := []map[string]any{}
		for i := 1; i <= turns; i++ {
			id := fmt.Sprintf("turn_%d", i)
			lane := "author"
			laneID := authorLane
			if i%2 == 0 {
				lane = "reviewer"
				laneID = reviewerLane(spec, 1)
			}
			label := fmt.Sprintf("Turn %d (%s)", i, topic)
			jobs = append(jobs, job(id, "conversation", label, lane, laneID, base, fmt.Sprintf("turn_%d.md", i), "handoff", id, "draft", ""))
			if i > 1 {
				edges = append(edges, map[string]any{"from": fmt.Sprintf("turn_%d", i-1), "to": id, "on": "completed"})
			}
		}
		return jobs, edges, nil, nil, nil
	case "falsification_gate", "cross_examination", "adjudicated_constraint_extraction",
		"fog_of_war_review", "synaptic_prune":
		return compileCollaborationShape(spec)
	case "implementation_panel":
		jobs, edges, cycles, err := compileImplementationPanel(spec)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		return jobs, edges, cycles, nil, nil
	case "divergent_ideation":
		jobs, edges, cycles, err := compileDivergentIdeation(spec)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		return jobs, edges, cycles, nil, nil
	case "verification_gate":
		return compileVerificationGate(spec)
	case "multi_phase":
		return compileMultiPhase(spec)
	default:
		return nil, nil, nil, nil, genErr("unknown workflow shape", "spec.shape")
	}
}
