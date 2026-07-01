package workflowgenerate

import "fmt"

func compileMultiPhase(spec Spec) ([]map[string]any, []map[string]any, []map[string]any, []map[string]any, error) {
	rawPhases, err := objectList(defaultAny(spec.Options["phases"], []any{}), "spec.options.phases")
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if len(rawPhases) == 0 {
		return nil, nil, nil, nil, genErr("multi_phase requires at least one phase", "spec.options.phases")
	}
	jobs := []map[string]any{}
	edges := []map[string]any{}
	cycles := []map[string]any{}
	phases := []map[string]any{}
	var previousSynthesisID string
	seenPhaseIDs := map[string]struct{}{}
	for phaseIndex, rawPhase := range rawPhases {
		phasePath := fmt.Sprintf("spec.options.phases[%d]", phaseIndex)
		phaseID, err := requiredString(rawPhase, "id", phasePath)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if _, ok := seenPhaseIDs[phaseID]; ok {
			return nil, nil, nil, nil, genErr("duplicate phase id", phasePath+".id")
		}
		seenPhaseIDs[phaseID] = struct{}{}
		phaseName, err := requiredString(rawPhase, "name", phasePath)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		phase := map[string]any{"id": phaseID, "name": phaseName}
		if value, ok := rawPhase["description"]; ok {
			text, ok := value.(string)
			if !ok {
				return nil, nil, nil, nil, genErr("phase description must be a string", phasePath+".description")
			}
			phase["description"] = text
		}
		if value, ok := rawPhase["color"]; ok {
			text, ok := value.(string)
			if !ok {
				return nil, nil, nil, nil, genErr("phase color must be a string", phasePath+".color")
			}
			phase["color"] = text
		}
		tracks, err := objectList(defaultAny(rawPhase["tracks"], []any{}), phasePath+".tracks")
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if len(tracks) == 0 {
			return nil, nil, nil, nil, genErr("phase requires at least one track", phasePath+".tracks")
		}
		phaseJobs := []map[string]any{}
		phaseEdges := []map[string]any{}
		phaseCycles := []map[string]any{}
		entryIDs := []string{}
		terminalIDs := []string{}
		seenTrackIDs := map[string]struct{}{}
		trackShapes := set("minimal", "review", "code_change", "human_checkpoint", "evidence_backed", "multi_review_synthesis")
		for trackIndex, track := range tracks {
			trackPath := fmt.Sprintf("%s.tracks[%d]", phasePath, trackIndex)
			trackID, err := requiredString(track, "id", trackPath)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			if _, ok := seenTrackIDs[trackID]; ok {
				return nil, nil, nil, nil, genErr("duplicate phase track id", trackPath+".id")
			}
			seenTrackIDs[trackID] = struct{}{}
			trackShape, err := choice(track, "shape", trackShapes, trackPath)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			trackSpec, err := trackSpec(spec, phaseID, trackID, trackShape, track)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			trackJobs, trackEdges, trackCycles, _, err := compileShape(trackSpec)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			prefix := phaseID + "__" + trackID + "__"
			idMap := map[string]string{}
			for _, trackJob := range trackJobs {
				jobID := fmt.Sprint(trackJob["id"])
				idMap[jobID] = prefix + jobID
			}
			trackEntryIDs := entryJobIDs(trackJobs, trackEdges, idMap)
			parallelEntries := set(trackEntryIDs...)
			trackLaneID, laneOverride := track["lane_id"].(string)
			for _, trackJob := range trackJobs {
				remapped := cloneMap(trackJob)
				remappedID := idMap[fmt.Sprint(trackJob["id"])]
				remapped["id"] = remappedID
				remapped["phase_id"] = phaseID
				if _, ok := parallelEntries[remappedID]; ok {
					remapped["parallel_group"] = phaseID + ":" + trackID
				}
				if laneOverride && remapped["type"] != "review" {
					remapped["lane_id"] = trackLaneID
				}
				phaseJobs = append(phaseJobs, remapped)
			}
			phaseEdges = append(phaseEdges, remapEdges(trackEdges, idMap)...)
			phaseCycles = append(phaseCycles, remapCycles(trackCycles, idMap)...)
			entryIDs = append(entryIDs, trackEntryIDs...)
			terminalIDs = append(terminalIDs, terminalJobIDs(trackJobs, trackEdges, idMap)...)
		}
		synthesisID := phaseID + "__synthesis"
		synthesisLane, err := phaseSynthesisLane(spec, rawPhase)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		phase["synthesis_job_id"] = synthesisID
		phaseJobs = append(phaseJobs, phaseSynthesisJob(phaseID, phaseName, synthesisID, synthesisLane, spec.ArtifactRoot))
		for _, terminalID := range uniqueSortedStrings(terminalIDs) {
			phaseEdges = append(phaseEdges, map[string]any{"from": terminalID, "to": synthesisID, "on": "completed"})
		}
		if previousSynthesisID != "" {
			for _, entryID := range uniqueSortedStrings(entryIDs) {
				edges = append(edges, map[string]any{"from": previousSynthesisID, "to": entryID, "on": "completed"})
			}
		}
		phases = append(phases, phase)
		jobs = append(jobs, phaseJobs...)
		edges = append(edges, phaseEdges...)
		cycles = append(cycles, phaseCycles...)
		previousSynthesisID = synthesisID
	}
	return jobs, edges, cycles, phases, nil
}

func trackSpec(spec Spec, phaseID, trackID, trackShape string, track map[string]any) (Spec, error) {
	lanes := map[string]map[string]any{}
	for laneID, lane := range spec.Lanes {
		lanes[laneID] = cloneMap(lane)
	}
	if laneID, ok := track["lane_id"].(string); ok {
		if _, ok := stringSet(laneIDsFor(spec))[laneID]; !ok {
			return Spec{}, genErr("track lane_id references missing lane", "spec.options.phases[].tracks[].lane_id")
		}
	}
	options := cloneMap(spec.Options)
	delete(options, "phases")
	if rawOptions, ok := track["options"].(map[string]any); ok {
		for key, value := range rawOptions {
			options[key] = value
		}
	}
	return Spec{
		SchemaVersion:    spec.SchemaVersion,
		Shape:            trackShape,
		LaneSet:          spec.LaneSet,
		WorkflowID:       fmt.Sprintf("%s-%s-%s", spec.WorkflowID, phaseID, trackID),
		Name:             fmt.Sprintf("%s %s %s", spec.Name, phaseID, trackID),
		WorkflowVer:      spec.WorkflowVer,
		Branch:           cloneMap(spec.Branch),
		ScaffoldRoot:     spec.ScaffoldRoot,
		ArtifactRoot:     fmt.Sprintf("%s/%s/%s", spec.ArtifactRoot, phaseID, trackID),
		Lanes:            lanes,
		Options:          options,
		LaneModifiers:    append([]string(nil), spec.LaneModifiers...),
		ContextDocs:      append([]any(nil), spec.ContextDocs...),
		Parallelism:      spec.Parallelism,
		PlacementPosture: spec.PlacementPosture,
		BlobConfigured:   spec.BlobConfigured,
	}, nil
}

func remapEdges(edges []map[string]any, idMap map[string]string) []map[string]any {
	result := []map[string]any{}
	for _, edge := range edges {
		result = append(result, map[string]any{
			"from": idMap[fmt.Sprint(edge["from"])],
			"to":   idMap[fmt.Sprint(edge["to"])],
			"on":   fmt.Sprint(defaultAny(edge["on"], "completed")),
		})
	}
	return result
}

func remapCycles(cycles []map[string]any, idMap map[string]string) []map[string]any {
	result := []map[string]any{}
	for _, cycle := range cycles {
		remapped := cloneMap(cycle)
		remapped["from"] = idMap[fmt.Sprint(cycle["from"])]
		remapped["to"] = idMap[fmt.Sprint(cycle["to"])]
		result = append(result, remapped)
	}
	return result
}

func entryJobIDs(jobs, edges []map[string]any, idMap map[string]string) []string {
	incoming := map[string]int{}
	for _, job := range jobs {
		incoming[fmt.Sprint(job["id"])] = 0
	}
	for _, edge := range edges {
		incoming[fmt.Sprint(edge["to"])]++
	}
	result := []string{}
	for jobID, count := range incoming {
		if count == 0 {
			result = append(result, idMap[jobID])
		}
	}
	return result
}

func terminalJobIDs(jobs, edges []map[string]any, idMap map[string]string) []string {
	outgoing := map[string]int{}
	for _, job := range jobs {
		outgoing[fmt.Sprint(job["id"])] = 0
	}
	for _, edge := range edges {
		outgoing[fmt.Sprint(edge["from"])]++
	}
	result := []string{}
	for jobID, count := range outgoing {
		if count == 0 {
			result = append(result, idMap[jobID])
		}
	}
	return result
}

func phaseSynthesisLane(spec Spec, phase map[string]any) (string, error) {
	if laneID, ok := phase["synthesis_lane_id"].(string); ok {
		if _, ok := stringSet(laneIDsFor(spec))[laneID]; !ok {
			return "", genErr("synthesis_lane_id references missing lane", "spec.options.phases[].synthesis_lane_id")
		}
		return laneID, nil
	}
	return reviewerLane(spec, 1), nil
}

func phaseSynthesisJob(phaseID, phaseName, synthesisID, laneID, artifactRoot string) map[string]any {
	result := job(
		synthesisID,
		"phase_synthesis",
		"Synthesize "+phaseName,
		"reviewer",
		laneID,
		artifactRoot+"/"+phaseID,
		"SYNTHESIS.md",
		"synthesis",
		"phase_synthesis",
		"apply",
		"Synthesize the completed work in "+phaseName+" and record a phase verdict.",
	)
	result["phase_id"] = phaseID
	return result
}
