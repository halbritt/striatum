package artifactcontracts

import (
	"fmt"
	"sort"
	"strings"
)

func validateCollaborationLedger(parsed map[string]any) error {
	participants, _ := stringList(parsed["participants"])
	participantSet := map[string]bool{}
	for _, participant := range participants {
		participantSet[participant] = true
	}
	seen := map[string]bool{}
	// RFC 0094 §5 Check-B: tally the per-challenge correspondence judgments so a
	// clearing verdict can require >=1 landed_and_rebutted and NO landed_unrebutted.
	correspondenceTally := map[string]int{}
	checkBEngaged := false
	for idx, entry := range collaborationLedgerEntryList(parsed["entries"]) {
		kind := fmt.Sprint(entry["kind"])
		if !participantSet[fmt.Sprint(entry["by"])] {
			return fmt.Errorf("collaboration_ledger artifact front matter entries[%d].by must name a participant", idx)
		}
		refs, _ := stringList(entry["refs"])
		if len(refs) == 0 {
			return fmt.Errorf("collaboration_ledger artifact front matter entries[%d].refs must be non-empty", idx)
		}
		for refIdx, ref := range refs {
			if !isDialogueRef(ref) {
				return fmt.Errorf("collaboration_ledger artifact front matter entries[%d].refs[%d] must match dialogue:<seq>", idx, refIdx)
			}
		}
		// RFC 0094 §5 per-entry semantic fields (#402). `correspondence` is the
		// Check-B challenge<->rebuttal judgment; it is only meaningful on a challenge
		// entry (the challenge is what lands or fails to land). `coverage` is the
		// fog_of_war_review judge's ground-truth score for a reconstructed constraint.
		if raw, exists := entry["correspondence"]; exists {
			value, ok := nonEmptyString(raw)
			if !ok || !oneOfValue("landed_and_rebutted", "landed_unrebutted", "not_material")(value) {
				return fmt.Errorf("collaboration_ledger artifact front matter entries[%d].correspondence must be one of landed_and_rebutted, landed_unrebutted, not_material", idx)
			}
			if kind != "challenge" {
				return fmt.Errorf("collaboration_ledger artifact front matter entries[%d].correspondence is only valid on a challenge entry (RFC 0094 §5 Check-B), not a %s entry", idx, kind)
			}
			correspondenceTally[value]++
			checkBEngaged = true
		}
		if raw, exists := entry["coverage"]; exists {
			value, ok := nonEmptyString(raw)
			if !ok || !oneOfValue("reconstructed", "hallucinated", "missed")(value) {
				return fmt.Errorf("collaboration_ledger artifact front matter entries[%d].coverage must be one of reconstructed, hallucinated, missed", idx)
			}
		}
		seen[kind] = true
	}
	verdict := fmt.Sprint(parsed["verdict"])
	clearing := verdict == "accept" || verdict == "accept_with_findings"
	if clearing {
		for _, requiredKind := range []string{"claim", "challenge", "rebuttal"} {
			if !seen[requiredKind] {
				return fmt.Errorf("collaboration_ledger clearing verdict requires at least one %s entry with refs", requiredKind)
			}
		}
	}
	// RFC 0094 §5 Check-B clearing rule: once the adjudicator records ANY
	// correspondence judgment (opts into the semantic layer above V1's structural
	// presence check), a clearing verdict requires at least one challenge that
	// `landed_and_rebutted` and NO challenge left `landed_unrebutted`. A confident
	// non-rebuttal that cites the right turn ids is recorded landed_unrebutted and
	// can no longer talk the gate past with structural presence alone.
	if clearing && checkBEngaged {
		if correspondenceTally["landed_unrebutted"] > 0 {
			return fmt.Errorf("collaboration_ledger clearing verdict requires every Check-B challenge be rebutted; %d challenge(s) are recorded landed_unrebutted (RFC 0094 §5)", correspondenceTally["landed_unrebutted"])
		}
		if correspondenceTally["landed_and_rebutted"] == 0 {
			return fmt.Errorf("collaboration_ledger clearing verdict with Check-B engaged requires at least one challenge recorded landed_and_rebutted (RFC 0094 §5)")
		}
	}
	// RFC 0094 §5 second-adjudicator-on-disagreement gate. When the ledger opts
	// into `adjudication_mode: second_on_disagreement`, a CLEARING verdict must be
	// backed by at least two DISTINCT adjudicators (the first + the second who
	// independently scored the same trajectory under RFC 0064 diversity). A
	// contested clear is conservatively recorded as needs_revision, so only a
	// clearing verdict triggers the requirement.
	if clearing {
		if err := validateSecondAdjudicatorGate(parsed); err != nil {
			return err
		}
	}
	shape := fmt.Sprint(parsed["shape"])
	schemaVersion := fmt.Sprint(parsed["schema_version"])
	if shape == "adjudicated_constraint_extraction" && schemaVersion != "striatum.collaboration_ledger.v1.1" {
		return fmt.Errorf("collaboration_ledger shape adjudicated_constraint_extraction requires schema_version striatum.collaboration_ledger.v1.1 (got %s)", schemaVersion)
	}
	findings, err := validateCollaborationLedgerFindings(parsed["findings"])
	if err != nil {
		return err
	}
	constraints, err := validateCollaborationLedgerConstraints(parsed["constraints"], findings)
	if err != nil {
		return err
	}
	if err := validateCollaborationLedgerBranches(parsed["branches"]); err != nil {
		return err
	}
	if shape == "adjudicated_constraint_extraction" && verdict == "needs_revision" && productiveConstraintRows(constraints) == 0 {
		return fmt.Errorf("adjudicated_constraint_extraction needs_revision requires a non-empty constraints[] (at least one binding constraint or unresolved_question row); see docs/reference/spec.md#artifact-front-matter-schemas")
	}
	return nil
}

// validateSecondAdjudicatorGate enforces RFC 0094 §5's second-adjudicator gate on
// a CLEARING verdict. With `adjudication_mode: second_on_disagreement`, the gate is
// only legitimately cleared when two DISTINCT adjudicators independently scored the
// same trajectory and agreed; the contract layer enforces the recorded provenance of
// that agreement — at least two distinct entries in `adjudicators`. In `single` mode
// (or with no mode declared) the gate is unchanged. The caller only invokes this for
// clearing verdicts (a contested clear is recorded as needs_revision, which never
// trips the requirement).
func validateSecondAdjudicatorGate(parsed map[string]any) error {
	mode := fmt.Sprint(parsed["adjudication_mode"])
	if mode != "second_on_disagreement" {
		return nil
	}
	adjudicators, _ := stringList(parsed["adjudicators"])
	distinct := map[string]bool{}
	for _, adjudicator := range adjudicators {
		if strings.TrimSpace(adjudicator) != "" {
			distinct[adjudicator] = true
		}
	}
	if len(distinct) < 2 {
		if len(adjudicators) >= 2 {
			return fmt.Errorf("collaboration_ledger adjudication_mode second_on_disagreement clearing verdict requires at least two DISTINCT adjudicators (RFC 0064 diversity); adjudicators[] repeats the same session id")
		}
		return fmt.Errorf("collaboration_ledger adjudication_mode second_on_disagreement clearing verdict requires at least two distinct adjudicators in adjudicators[] (a contested clear must be recorded as needs_revision) (RFC 0094 §5)")
	}
	return nil
}

func validateCollaborationLedgerFindings(value any) (map[string]map[string]any, error) {
	result := map[string]map[string]any{}
	if value == nil {
		return result, nil
	}
	rows, ok := mapList(value)
	if !ok {
		return nil, fmt.Errorf("collaboration_ledger artifact front matter findings must be a list of objects")
	}
	allowedKeys := map[string]bool{
		"id":                         true,
		"severity":                   true,
		"posture":                    true,
		"status":                     true,
		"challenge":                  true,
		"closest_acceptable_answer":  true,
		"affected_invariants":        true,
		"requested_constraint_shape": true,
		"requires_convener_rebuttal": true,
		"source_refs":                true,
	}
	for idx, row := range rows {
		if err := rejectUnknownKeys(fmt.Sprintf("findings[%d]", idx), row, allowedKeys); err != nil {
			return nil, err
		}
		id, ok := nonEmptyString(row["id"])
		if !ok {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter findings[%d].id must be a non-empty string", idx)
		}
		if result[id] != nil {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter findings[%d].id %q is duplicated", idx, id)
		}
		if !oneOfValue("low", "medium", "high", "critical")(row["severity"]) {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter findings[%d].severity must be one of low, medium, high, critical", idx)
		}
		if _, ok := nonEmptyString(row["posture"]); !ok {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter findings[%d].posture must be a non-empty string", idx)
		}
		if !oneOfValue("open", "answered", "accepted", "rejected", "converted_to_constraint", "deferred_with_owner")(row["status"]) {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter findings[%d].status is invalid", idx)
		}
		if _, ok := nonEmptyString(row["challenge"]); !ok {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter findings[%d].challenge must be a non-empty string", idx)
		}
		if value, exists := row["closest_acceptable_answer"]; exists && !isStringValue(value) {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter findings[%d].closest_acceptable_answer must be a string", idx)
		}
		if value, exists := row["affected_invariants"]; exists && !isStringListValue(value) {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter findings[%d].affected_invariants must be a list of strings", idx)
		}
		if value, exists := row["requested_constraint_shape"]; exists && !isStringAnyMapValue(value) {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter findings[%d].requested_constraint_shape must be a mapping", idx)
		}
		if value, exists := row["requires_convener_rebuttal"]; exists && !isBoolValue(value) {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter findings[%d].requires_convener_rebuttal must be a bool", idx)
		}
		if value, exists := row["source_refs"]; exists {
			if err := validateDialogueRefs(fmt.Sprintf("findings[%d].source_refs", idx), value); err != nil {
				return nil, err
			}
		}
		result[id] = row
	}
	return result, nil
}

func validateCollaborationLedgerConstraints(value any, findings map[string]map[string]any) ([]map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	rows, ok := mapList(value)
	if !ok {
		return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints must be a list of objects")
	}
	allowedKeys := map[string]bool{
		"id":                    true,
		"posture":               true,
		"severity":              true,
		"kind":                  true,
		"binding":               true,
		"text":                  true,
		"source_finding":        true,
		"source_refs":           true,
		"verification":          true,
		"final_review_required": true,
	}
	for idx, row := range rows {
		if err := rejectUnknownKeys(fmt.Sprintf("constraints[%d]", idx), row, allowedKeys); err != nil {
			return nil, err
		}
		if _, ok := nonEmptyString(row["id"]); !ok {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].id must be a non-empty string", idx)
		}
		if _, ok := nonEmptyString(row["posture"]); !ok {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].posture must be a non-empty string", idx)
		}
		if !oneOfValue("low", "medium", "high", "critical")(row["severity"]) {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].severity must be one of low, medium, high, critical", idx)
		}
		if !oneOfValue("invariant", "gate", "schema", "policy", "non_goal", "accepted_risk", "unresolved_question")(row["kind"]) {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].kind is invalid", idx)
		}
		binding, ok := row["binding"].(bool)
		if !ok {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].binding must be a bool", idx)
		}
		if _, ok := nonEmptyString(row["text"]); !ok {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].text must be a non-empty string", idx)
		}
		sourceFinding := ""
		if value, exists := row["source_finding"]; exists {
			var ok bool
			sourceFinding, ok = nonEmptyString(value)
			if !ok {
				return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].source_finding must be a non-empty string", idx)
			}
		}
		if value, exists := row["source_refs"]; exists {
			if err := validateDialogueRefs(fmt.Sprintf("constraints[%d].source_refs", idx), value); err != nil {
				return nil, err
			}
		}
		if value, exists := row["final_review_required"]; exists && !isBoolValue(value) {
			return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].final_review_required must be a bool", idx)
		}
		verification, hasVerification, err := constraintVerification(idx, row["verification"])
		if err != nil {
			return nil, err
		}
		if binding {
			if sourceFinding == "" {
				return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].source_finding is required when binding is true", idx)
			}
			finding, ok := findings[sourceFinding]
			if !ok {
				return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].source_finding %q must resolve to a findings[] row", idx, sourceFinding)
			}
			if !oneOfValue("high", "critical")(finding["severity"]) {
				return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].source_finding %q must resolve to a high or critical findings[] row", idx, sourceFinding)
			}
			if !hasVerification || !verificationHasSignal(verification) {
				return nil, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].verification requires a non-empty gate or expected_stage when binding is true", idx)
			}
		}
	}
	return rows, nil
}

func validateCollaborationLedgerBranches(value any) error {
	if value == nil {
		return nil
	}
	branches, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("collaboration_ledger artifact front matter branches must be a mapping")
	}
	for posture, rawDisposition := range branches {
		if strings.TrimSpace(posture) == "" {
			return fmt.Errorf("collaboration_ledger artifact front matter branches keys must be non-empty postures")
		}
		disposition, ok := rawDisposition.(string)
		if !ok || !oneOfValue("cleared", "cleared_with_constraints", "blocked", "blocked_pending_answer", "defer_with_successor")(disposition) {
			return fmt.Errorf("collaboration_ledger artifact front matter branches[%q] disposition must be one of cleared, cleared_with_constraints, blocked, blocked_pending_answer, defer_with_successor", posture)
		}
	}
	return nil
}

// ConstraintDischargeRow is one row of a final_review finding's
// constraint_discharge[] table (RFC 0098 §4). It reports how a binding
// constraint from the cleared collaboration_ledger landed in the published
// spec. `discharged` and `accepted_risk` are the passing statuses; `missing`
// and `partial` are failing unless modeled as `accepted_risk`. The RFC §5
// invariant 4 typecheck (implemented in pkg/mutations) treats `accepted_risk`
// as a pass only when Owner and Stage are both present.
type ConstraintDischargeRow struct {
	ConstraintID string
	Status       string
	Evidence     string
	Owner        string
	Stage        string
}

// constraintDischargeStatuses is the closed enum for a discharge row's status.
// `accepted_risk` represents the RFC §1/§5 accepted-risk disposition; it is a
// status here (rather than a parallel block) because the spec describes the
// discharge outcome per constraint and accepting the risk is one such outcome.
var constraintDischargeStatuses = []string{"discharged", "missing", "partial", "accepted_risk"}

// ParseConstraintDischarge validates an optional constraint_discharge[] block
// and returns the typed rows. It is exported so the discharge-verifying gate in
// pkg/mutations can parse the same structure without duplicating the schema. A
// nil value yields no rows and no error (the block is optional at the contract
// layer; the gate is what requires it for the ACE final-review step).
//
// Per-row rules:
//   - constraint_id: required non-empty string, unique across rows.
//   - status: required, one of discharged|missing|partial|accepted_risk.
//   - evidence: optional string.
//   - owner / stage: optional strings, but BOTH required when status is
//     accepted_risk (RFC §5 invariant 4: an accepted risk must name an owner
//     and the stage that owns it).
func ParseConstraintDischarge(value any) ([]ConstraintDischargeRow, error) {
	if value == nil {
		return nil, nil
	}
	rows, ok := mapList(value)
	if !ok {
		return nil, fmt.Errorf("finding artifact front matter constraint_discharge must be a list of objects")
	}
	allowedKeys := map[string]bool{
		"constraint_id": true,
		"status":        true,
		"evidence":      true,
		"owner":         true,
		"stage":         true,
	}
	result := make([]ConstraintDischargeRow, 0, len(rows))
	seen := map[string]bool{}
	for idx, row := range rows {
		for key := range row {
			if !allowedKeys[key] {
				extra := []string{}
				for k := range row {
					if !allowedKeys[k] {
						extra = append(extra, k)
					}
				}
				sort.Strings(extra)
				return nil, fmt.Errorf("finding artifact front matter constraint_discharge[%d] has unknown fields: %s", idx, strings.Join(extra, ", "))
			}
		}
		constraintID, ok := nonEmptyString(row["constraint_id"])
		if !ok {
			return nil, fmt.Errorf("finding artifact front matter constraint_discharge[%d].constraint_id must be a non-empty string", idx)
		}
		if seen[constraintID] {
			return nil, fmt.Errorf("finding artifact front matter constraint_discharge[%d].constraint_id %q is duplicated", idx, constraintID)
		}
		seen[constraintID] = true
		if !oneOfValue(constraintDischargeStatuses...)(row["status"]) {
			return nil, fmt.Errorf("finding artifact front matter constraint_discharge[%d].status must be one of discharged, missing, partial, accepted_risk", idx)
		}
		status := fmt.Sprint(row["status"])
		evidence := ""
		if v, exists := row["evidence"]; exists {
			text, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("finding artifact front matter constraint_discharge[%d].evidence must be a string", idx)
			}
			evidence = text
		}
		owner := ""
		if v, exists := row["owner"]; exists {
			text, ok := nonEmptyString(v)
			if !ok {
				return nil, fmt.Errorf("finding artifact front matter constraint_discharge[%d].owner must be a non-empty string", idx)
			}
			owner = text
		}
		stage := ""
		if v, exists := row["stage"]; exists {
			text, ok := nonEmptyString(v)
			if !ok {
				return nil, fmt.Errorf("finding artifact front matter constraint_discharge[%d].stage must be a non-empty string", idx)
			}
			stage = text
		}
		if status == "accepted_risk" && (owner == "" || stage == "") {
			return nil, fmt.Errorf("finding artifact front matter constraint_discharge[%d] with status accepted_risk requires both owner and stage", idx)
		}
		result = append(result, ConstraintDischargeRow{
			ConstraintID: constraintID,
			Status:       status,
			Evidence:     evidence,
			Owner:        owner,
			Stage:        stage,
		})
	}
	return result, nil
}

// BindingConstraint is a binding constraint extracted from a cleared
// collaboration_ledger.v1.1 (RFC 0098 §4). The discharge-verifying gate reads
// these from the latest cleared ACE ledger and checks each one against the
// final_review finding's constraint_discharge[] table.
type BindingConstraint struct {
	ID                  string
	FinalReviewRequired bool
}

// BindingConstraintsFromLedger parses a collaboration_ledger.v1.1 front matter
// map and returns its binding constraints (binding: true). The caller decides
// whether to filter to final_review_required: true. This reuses the same
// constraints[] structure validateCollaborationLedger already enforces; it does
// not re-validate (the ledger was validated at publish time), it only extracts.
func BindingConstraintsFromLedger(parsed map[string]any) []BindingConstraint {
	rows, ok := mapList(parsed["constraints"])
	if !ok {
		return nil
	}
	result := []BindingConstraint{}
	for _, row := range rows {
		binding, _ := row["binding"].(bool)
		if !binding {
			continue
		}
		id, ok := nonEmptyString(row["id"])
		if !ok {
			continue
		}
		finalReviewRequired := true
		if v, exists := row["final_review_required"]; exists {
			if b, ok := v.(bool); ok {
				finalReviewRequired = b
			}
		}
		result = append(result, BindingConstraint{ID: id, FinalReviewRequired: finalReviewRequired})
	}
	return result
}

func constraintVerification(idx int, value any) (map[string]any, bool, error) {
	if value == nil {
		return nil, false, nil
	}
	verification, ok := value.(map[string]any)
	if !ok {
		return nil, true, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].verification must be a mapping", idx)
	}
	if err := rejectUnknownKeys(fmt.Sprintf("constraints[%d].verification", idx), verification, map[string]bool{
		"expected_stage": true,
		"gate":           true,
	}); err != nil {
		return nil, true, err
	}
	for key, raw := range verification {
		if _, ok := nonEmptyString(raw); !ok {
			return nil, true, fmt.Errorf("collaboration_ledger artifact front matter constraints[%d].verification.%s must be a non-empty string", idx, key)
		}
	}
	return verification, true, nil
}

func verificationHasSignal(verification map[string]any) bool {
	if verification == nil {
		return false
	}
	for _, key := range []string{"gate", "expected_stage"} {
		if _, ok := nonEmptyString(verification[key]); ok {
			return true
		}
	}
	return false
}

func productiveConstraintRows(rows []map[string]any) int {
	count := 0
	for _, row := range rows {
		binding, _ := row["binding"].(bool)
		if binding || fmt.Sprint(row["kind"]) == "unresolved_question" {
			count++
		}
	}
	return count
}

func validateDialogueRefs(label string, value any) error {
	refs, ok := stringList(value)
	if !ok {
		return fmt.Errorf("collaboration_ledger artifact front matter %s must be a list of dialogue:<seq> refs", label)
	}
	for idx, ref := range refs {
		if !isDialogueRef(ref) {
			return fmt.Errorf("collaboration_ledger artifact front matter %s[%d] must match dialogue:<seq>", label, idx)
		}
	}
	return nil
}

func rejectUnknownKeys(label string, row map[string]any, allowed map[string]bool) error {
	extra := []string{}
	for key := range row {
		if !allowed[key] {
			extra = append(extra, key)
		}
	}
	if len(extra) == 0 {
		return nil
	}
	sort.Strings(extra)
	return fmt.Errorf("collaboration_ledger artifact front matter %s has unknown fields: %s", label, strings.Join(extra, ", "))
}

func isCollaborationLedgerEntriesValue(value any) bool {
	entries := collaborationLedgerEntryList(value)
	if len(entries) == 0 {
		return false
	}
	requiredKeys := []string{"kind", "by", "refs", "text"}
	// RFC 0094 §5 (#402) added optional per-entry fields. `correspondence` is the
	// Check-B challenge<->rebuttal judgment; `coverage` is the fog_of_war_review
	// ground-truth score. They are ADDITIVE: an entry without them stays a valid V1
	// entry, so this structural gate tolerates them and the detailed value/enum +
	// placement checks live in validateCollaborationLedger (which runs after this
	// field-presence gate passes).
	allowedKeys := map[string]bool{"kind": true, "by": true, "refs": true, "text": true, "correspondence": true, "coverage": true}
	for _, entry := range entries {
		for key := range entry {
			if !allowedKeys[key] {
				return false
			}
		}
		for _, key := range requiredKeys {
			if _, ok := entry[key]; !ok {
				return false
			}
		}
		if !oneOfValue("claim", "challenge", "rebuttal", "constraint", "nomination")(entry["kind"]) {
			return false
		}
		if !isNonEmptyStringValue(entry["by"]) {
			return false
		}
		refs, ok := stringList(entry["refs"])
		if !ok || len(refs) == 0 {
			return false
		}
		for _, ref := range refs {
			if !isDialogueRef(ref) {
				return false
			}
		}
		if !isNonEmptyStringValue(entry["text"]) {
			return false
		}
	}
	return true
}

func collaborationLedgerEntryList(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			entry, ok := item.(map[string]any)
			if !ok {
				return nil
			}
			result = append(result, entry)
		}
		return result
	default:
		return nil
	}
}

func isDialogueRef(value string) bool {
	if !strings.HasPrefix(value, "dialogue:") {
		return false
	}
	seq := strings.TrimPrefix(value, "dialogue:")
	if seq == "" {
		return false
	}
	for _, ch := range seq {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}
