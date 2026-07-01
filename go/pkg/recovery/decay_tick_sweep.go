package recovery

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/jackc/pgx/v5"
)

const (
	DefaultCullFoldTimeout = 10 * time.Second

	cullStatementTimeout = "10000"
	cullDecaySentinel    = 0.0
	statusHeadLineLimit  = 8
	statusHeadByteLimit  = 16 * 1024
)

type cullableKey struct {
	kind string
	ref  string
}

type cullableChange struct {
	key       cullableKey
	state     string
	reachable bool
}

type cullableEvaluation struct {
	key           cullableKey
	path          string
	status        string
	successors    []cullableKey
	reachable     bool
	nominated     bool
	withheldBy    string
	countedHits   []inboundHit
	protected     bool
	liveSuccessor bool
}

type inboundHit struct {
	path string
	line string
}

type decayScanFunc func(context.Context, db.Runner) ([]cullableChange, error)
type decayCommitFunc func(context.Context, db.Runner, []cullableChange) error

// DecayTickSweep is the RFC 0170 P0 observe-only cull fold. SweepOnce only
// checks the single in-flight slot and launches the detached scan; it never waits
// for the scan to finish, so it stays off the recovery scheduler wait path.
type DecayTickSweep struct {
	Runner  db.Runner
	Timeout time.Duration
	Logf    func(string, ...any)

	scan   decayScanFunc
	commit decayCommitFunc
	onDone func()

	slotMu         sync.Mutex
	nextGeneration uint64
	slotGeneration uint64
	slotExpiresAt  time.Time
	inFlight       atomic.Bool
}

func NewDecayTickSweep(runner db.Runner) *DecayTickSweep {
	return &DecayTickSweep{Runner: runner}
}

func (s *DecayTickSweep) SweepOnce(ctx context.Context) (map[string]any, error) {
	if s.Runner == nil {
		return nil, fmt.Errorf("decay tick sweep requires daemon PostgreSQL")
	}
	timeout := s.effectiveTimeout()
	generation, claimed := s.claimCullSlot(timeout)
	if !claimed {
		s.logf("decay tick sweep skipped; previous scan still inside live slot")
		return map[string]any{"status": "skipped", "reason": "in_flight"}, nil
	}
	go s.runDecayTickSweep(ctx, generation, timeout)
	return map[string]any{"status": "launched", "generation": generation}, nil
}

func (s *DecayTickSweep) runDecayTickSweep(sweepCtx context.Context, generation uint64, timeout time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			s.logf("decay tick sweep panic recovered; dropping candidacy delta and continuing: panic=%v\n%s", r, debug.Stack())
		}
		s.releaseCullSlot(generation)
		if s.onDone != nil {
			s.onDone()
		}
	}()

	cullCtx, cancel := context.WithTimeout(sweepCtx, timeout)
	defer cancel()

	scan := s.scan
	if scan == nil {
		scan = computeCullableChanges
	}
	changes, err := scan(cullCtx, s.Runner)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || cullCtx.Err() != nil {
			s.logf("decay tick sweep abandoned before write phase: %v", err)
			return
		}
		s.logf("decay tick sweep scan failed; writing nothing: %v", err)
		return
	}
	if cullCtx.Err() != nil {
		s.logf("decay tick sweep scan returned after deadline; writing nothing: %v", cullCtx.Err())
		return
	}
	if !s.generationOwnsLiveSlot(generation) {
		s.logf("decay tick sweep scan returned after its slot expired or was replaced; writing nothing: generation=%d", generation)
		return
	}

	commit := s.commit
	if commit == nil {
		commit = commitCullableChanges
	}
	if err := commit(cullCtx, s.Runner, changes); err != nil {
		s.logf("decay tick sweep commit failed: %v", err)
	}
}

func (s *DecayTickSweep) effectiveTimeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return DefaultCullFoldTimeout
}

func (s *DecayTickSweep) claimCullSlot(timeout time.Duration) (uint64, bool) {
	now := time.Now()
	deadline := now.Add(timeout)

	s.slotMu.Lock()
	defer s.slotMu.Unlock()

	if s.slotGeneration != 0 && now.Before(s.slotExpiresAt) {
		return 0, false
	}

	s.nextGeneration++
	s.slotGeneration = s.nextGeneration
	s.slotExpiresAt = deadline
	s.inFlight.Store(true)
	return s.slotGeneration, true
}

func (s *DecayTickSweep) generationOwnsLiveSlot(generation uint64) bool {
	now := time.Now()

	s.slotMu.Lock()
	defer s.slotMu.Unlock()

	return s.slotGeneration == generation && now.Before(s.slotExpiresAt)
}

func (s *DecayTickSweep) releaseCullSlot(generation uint64) {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()

	if s.slotGeneration != generation {
		return
	}
	s.slotGeneration = 0
	s.slotExpiresAt = time.Time{}
	s.inFlight.Store(false)
}

func (s *DecayTickSweep) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

type cullRepository struct {
	repositoryID string
	repoRoot     string
}

func computeCullableChanges(ctx context.Context, runner db.Runner) ([]cullableChange, error) {
	existing, err := readExistingCullableEntities(ctx, runner)
	if err != nil {
		return nil, err
	}
	repositories, err := readActiveCullRepositories(ctx, runner)
	if err != nil {
		return nil, err
	}

	evaluations := map[cullableKey]cullableEvaluation{}
	for _, repo := range repositories {
		repoEvaluations, err := scanRepositoryCullableEvaluations(ctx, repo.repoRoot)
		if err != nil {
			return nil, fmt.Errorf("scan cullable entities for repository %s: %w", repo.repositoryID, err)
		}
		for key, evaluation := range repoEvaluations {
			evaluations[key] = evaluation
		}
	}
	return buildCullableChanges(existing, evaluations), nil
}

func readActiveCullRepositories(ctx context.Context, runner db.Runner) ([]cullRepository, error) {
	queryer, ok := runner.(queryRunner)
	if !ok {
		return nil, fmt.Errorf("decay tick sweep runner does not support queries")
	}
	rows, err := queryer.Query(ctx, `
		SELECT repository_id, repo_root
		  FROM striatumd.repositories
		 WHERE state = 'active'
		 ORDER BY repository_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var repositories []cullRepository
	for rows.Next() {
		var repo cullRepository
		if err := rows.Scan(&repo.repositoryID, &repo.repoRoot); err != nil {
			return nil, err
		}
		repositories = append(repositories, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return repositories, nil
}

type txQueryRunner interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func readExistingCullableEntities(ctx context.Context, runner db.Runner) (map[cullableKey]string, error) {
	tx, err := runner.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	if err := tx.Exec(ctx, `SET LOCAL statement_timeout = '`+cullStatementTimeout+`'`); err != nil {
		return nil, err
	}
	queryer, ok := tx.(txQueryRunner)
	if !ok {
		return nil, fmt.Errorf("decay tick sweep transaction does not support queries")
	}
	rows, err := queryer.Query(ctx, `
		SELECT kind, ref, candidacy_state
		  FROM striatumd.cullable_entity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := map[cullableKey]string{}
	for rows.Next() {
		var kind, ref, state string
		if err := rows.Scan(&kind, &ref, &state); err != nil {
			return nil, err
		}
		existing[cullableKey{kind: kind, ref: ref}] = state
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return existing, nil
}

func buildCullableChanges(existing map[cullableKey]string, evaluations map[cullableKey]cullableEvaluation) []cullableChange {
	var changes []cullableChange
	seen := map[cullableKey]bool{}
	for key, evaluation := range evaluations {
		seen[key] = true
		if evaluation.nominated {
			changes = append(changes, cullableChange{key: key, state: "nominated", reachable: evaluation.reachable})
		}
	}
	for key, state := range existing {
		if state != "nominated" || seen[key] && evaluations[key].nominated {
			continue
		}
		evaluation := evaluations[key]
		changes = append(changes, cullableChange{key: key, state: "withdrawn", reachable: evaluation.reachable})
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].key.kind != changes[j].key.kind {
			return changes[i].key.kind < changes[j].key.kind
		}
		return changes[i].key.ref < changes[j].key.ref
	})
	return changes
}

func commitCullableChanges(ctx context.Context, runner db.Runner, changes []cullableChange) error {
	if len(changes) == 0 {
		return nil
	}
	tx, err := runner.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	for _, change := range changes {
		switch change.state {
		case "nominated":
			if err := tx.Exec(ctx, `
				INSERT INTO striatumd.cullable_entity(
				  kind, ref, last_reinforced_at, decay_score,
				  reachable_from_root, candidacy_state
				)
				VALUES ($1, $2, now(), $3, $4, 'nominated')
				ON CONFLICT (kind, ref)
				DO UPDATE SET last_reinforced_at = EXCLUDED.last_reinforced_at,
				              decay_score = EXCLUDED.decay_score,
				              reachable_from_root = EXCLUDED.reachable_from_root,
				              candidacy_state = EXCLUDED.candidacy_state`,
				change.key.kind, change.key.ref, cullDecaySentinel, change.reachable); err != nil {
				return err
			}
		case "withdrawn":
			if err := tx.Exec(ctx, `
				UPDATE striatumd.cullable_entity
				   SET decay_score = $3,
				       reachable_from_root = $4,
				       candidacy_state = 'withdrawn'
				 WHERE kind = $1
				   AND ref = $2
				   AND candidacy_state = 'nominated'`,
				change.key.kind, change.key.ref, cullDecaySentinel, change.reachable); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported cullable_entity state %q", change.state)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

type tier1Corpus struct {
	repoRoot  string
	rfcs      map[cullableKey]corpusEntity
	decisions map[cullableKey]decisionEntity
	docs      map[cullableKey]corpusEntity
	lines     []inboundLine
}

type corpusEntity struct {
	key    cullableKey
	path   string
	status string
}

type decisionEntity struct {
	corpusEntity
	cells []string
}

type inboundLine struct {
	path      string
	line      string
	sourceKey *cullableKey
	live      bool
}

func scanRepositoryCullableEvaluations(ctx context.Context, repoRoot string) (map[cullableKey]cullableEvaluation, error) {
	corpus := &tier1Corpus{
		repoRoot:  repoRoot,
		rfcs:      map[cullableKey]corpusEntity{},
		decisions: map[cullableKey]decisionEntity{},
		docs:      map[cullableKey]corpusEntity{},
	}
	if err := corpus.load(ctx); err != nil {
		return nil, err
	}

	evaluations := map[cullableKey]cullableEvaluation{}
	for key, entity := range corpus.rfcs {
		evaluations[key] = corpus.evaluateEntity(entity)
	}
	for key, entity := range corpus.decisions {
		evaluations[key] = corpus.evaluateDecision(entity)
	}
	for key, entity := range corpus.docs {
		evaluations[key] = corpus.evaluateEntity(entity)
	}
	return evaluations, nil
}

func (c *tier1Corpus) load(ctx context.Context) error {
	return filepath.WalkDir(c.repoRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(c.repoRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if shouldSkipCullWalkDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldReadCullFile(rel) {
			return nil
		}

		head, err := readHeadLines(ctx, path, statusHeadLineLimit)
		if err != nil {
			return err
		}
		status := structuralStatusFromHead(head)
		if key, ok := rfcKeyFromPath(rel); ok {
			entity := corpusEntity{key: key, path: rel, status: status}
			c.rfcs[key] = entity
		} else if rel == "docs/decisions/decision-log.md" {
			decisions, err := parseDecisionLog(ctx, path)
			if err != nil {
				return err
			}
			for key, decision := range decisions {
				c.decisions[key] = decision
			}
		} else if strings.HasPrefix(rel, "docs/") && strings.HasSuffix(rel, ".md") {
			if status != "" {
				key := cullableKey{kind: "doc", ref: rel}
				c.docs[key] = corpusEntity{key: key, path: rel, status: status}
			}
		}
		if isInboundScanFile(rel) {
			lines, err := readInboundLines(ctx, path, rel, c.sourceKeyForPath(rel), sourceLive(rel, status))
			if err != nil {
				return err
			}
			c.lines = append(c.lines, lines...)
		}
		return nil
	})
}

func shouldSkipCullWalkDir(rel string) bool {
	if rel == "." {
		return false
	}
	base := pathBase(rel)
	switch base {
	case ".git", ".striatum", "node_modules", "vendor":
		return true
	}
	return false
}

func shouldReadCullFile(rel string) bool {
	if rel == "AGENTS.md" || rel == "CLAUDE.md" || rel == "README.md" || rel == "ARCHITECTURE.md" {
		return true
	}
	if strings.HasPrefix(rel, "docs/") && strings.HasSuffix(rel, ".md") {
		return true
	}
	return false
}

func isInboundScanFile(rel string) bool {
	switch rel {
	case "AGENTS.md", "CLAUDE.md", "docs/index.md", "docs/operator/rfc-roadmap.md", "docs/reference/spec.md", "docs/reference/prd.md":
		return true
	}
	if rel == "docs/decisions/decision-log.md" {
		return true
	}
	if strings.HasPrefix(rel, "docs/rfcs/") && strings.HasSuffix(rel, ".md") {
		return true
	}
	if !strings.HasPrefix(rel, "docs/") || !strings.HasSuffix(rel, ".md") {
		return false
	}
	for _, prefix := range []string{
		"docs/records/_frozen/",
		"docs/records/_frozen/research/",
		"docs/dogfood/",
		"docs/handoffs/",
		"docs/operator/artifacts/",
		"docs/operator/plans/",
		"docs/operator/workflows/",
	} {
		if strings.HasPrefix(rel, prefix) {
			return false
		}
	}
	return true
}

func sourceLive(rel string, status string) bool {
	for _, prefix := range []string{
		"docs/records/_frozen/",
		"docs/records/_frozen/research/",
		"docs/dogfood/",
		"docs/handoffs/",
		"docs/operator/artifacts/",
		"docs/operator/plans/",
		"docs/operator/workflows/",
	} {
		if strings.HasPrefix(rel, prefix) {
			return false
		}
	}
	return !statusBeginsAny(status, "frozen", "superseded", "tombstoned", "withdrawn", "deprecated")
}

func (c *tier1Corpus) sourceKeyForPath(rel string) *cullableKey {
	if key, ok := rfcKeyFromPath(rel); ok {
		return &key
	}
	return nil
}

func (c *tier1Corpus) evaluateDecision(entity decisionEntity) cullableEvaluation {
	evaluation := c.evaluateEntity(entity.corpusEntity)
	if !statusBeginsAny(entity.status, "superseded", "tombstoned") {
		return evaluation
	}
	evaluation.successors = decisionSuccessors(entity.cells)
	evaluation.liveSuccessor = c.hasLiveSuccessor(evaluation.successors)
	return c.finalizeEvaluation(evaluation)
}

func (c *tier1Corpus) evaluateEntity(entity corpusEntity) cullableEvaluation {
	evaluation := cullableEvaluation{
		key:       entity.key,
		path:      entity.path,
		status:    entity.status,
		protected: protectedCullPath(entity.path),
	}
	if !statusBeginsAny(entity.status, "superseded", "tombstoned") {
		evaluation.withheldBy = "clause_1"
		return evaluation
	}
	if entity.key.kind == "rfc" || entity.key.kind == "doc" {
		evaluation.successors = statusLineSuccessors(entity.status, entity.key.kind)
		evaluation.liveSuccessor = c.hasLiveSuccessor(evaluation.successors)
	}
	return c.finalizeEvaluation(evaluation)
}

func (c *tier1Corpus) finalizeEvaluation(evaluation cullableEvaluation) cullableEvaluation {
	if !evaluation.liveSuccessor {
		evaluation.withheldBy = "clause_2"
		return evaluation
	}
	if evaluation.protected {
		evaluation.reachable = true
		evaluation.withheldBy = "clause_3"
		return evaluation
	}
	evaluation.countedHits = c.countedInboundHits(evaluation)
	if len(evaluation.countedHits) > 0 {
		evaluation.reachable = true
		evaluation.withheldBy = "clause_4"
		return evaluation
	}
	evaluation.nominated = true
	return evaluation
}

func (c *tier1Corpus) hasLiveSuccessor(successors []cullableKey) bool {
	for _, successor := range successors {
		switch successor.kind {
		case "rfc":
			entity, ok := c.rfcs[successor]
			if ok && !statusBeginsAny(entity.status, "superseded", "tombstoned", "withdrawn", "deprecated") {
				return true
			}
		case "decision":
			entity, ok := c.decisions[successor]
			if ok && decisionStatusLive(entity.status) {
				return true
			}
		}
	}
	return false
}

func decisionStatusLive(status string) bool {
	normalized := strings.ToLower(strings.TrimSpace(status))
	return normalized == "accepted" || normalized == "implemented" || normalized == "resolved"
}

func (c *tier1Corpus) countedInboundHits(evaluation cullableEvaluation) []inboundHit {
	refPattern := canonicalRefPattern(evaluation.key)
	if refPattern == nil {
		return nil
	}
	successorSet := map[cullableKey]bool{}
	for _, successor := range evaluation.successors {
		successorSet[successor] = true
	}
	var hits []inboundHit
	for _, line := range c.lines {
		if !line.live || !refPattern.MatchString(line.line) {
			continue
		}
		if line.sourceKey != nil && *line.sourceKey == evaluation.key {
			continue
		}
		if line.path == evaluation.path && evaluation.key.kind != "decision" {
			continue
		}
		if line.sourceKey != nil && successorSet[*line.sourceKey] {
			continue
		}
		if disposableCitationLine(line.path, line.line, evaluation.key) {
			continue
		}
		hits = append(hits, inboundHit{path: line.path, line: line.line})
	}
	return hits
}

func protectedCullPath(rel string) bool {
	exact := map[string]bool{
		"docs/reference/spec.md":       true,
		"docs/reference/prd.md":        true,
		"README.md":                    true,
		"ARCHITECTURE.md":              true,
		"AGENTS.md":                    true,
		"CLAUDE.md":                    true,
		"docs/index.md":                true,
		"docs/operator/rfc-roadmap.md": true,
	}
	if exact[rel] {
		return true
	}
	for _, prefix := range []string{
		"docs/records/_frozen/",
		"docs/records/_frozen/research/",
		"docs/dogfood/",
		"docs/handoffs/",
		"docs/operator/plans/",
		"docs/operator/workflows/",
		"examples/",
		"prompts/",
	} {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

var statusLinePattern = regexp.MustCompile(`(?i)^\s*(?:\*\*)?status\s*:\s*(?:\*\*)?\s*(.*)$`)

func structuralStatusFromHead(lines []string) string {
	for _, line := range lines {
		if match := statusLinePattern.FindStringSubmatch(line); match != nil {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

func readHeadLines(ctx context.Context, path string, limit int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	reader := io.LimitReader(file, statusHeadByteLimit)
	scanner := bufio.NewScanner(reader)
	lines := []string{}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lines = append(lines, scanner.Text())
		if len(lines) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func readInboundLines(ctx context.Context, path string, rel string, sourceKey *cullableKey, live bool) ([]inboundLine, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []inboundLine
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lineSource := sourceKey
		text := scanner.Text()
		if rel == "docs/decisions/decision-log.md" {
			if key, ok := decisionKeyFromRow(text); ok {
				lineSource = &key
			}
		}
		lines = append(lines, inboundLine{path: rel, line: text, sourceKey: lineSource, live: live})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func parseDecisionLog(ctx context.Context, path string) (map[cullableKey]decisionEntity, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	decisions := map[cullableKey]decisionEntity{}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := scanner.Text()
		key, cells, ok := decisionCellsFromRow(line)
		if !ok {
			continue
		}
		decisions[key] = decisionEntity{
			corpusEntity: corpusEntity{
				key:    key,
				path:   "docs/decisions/decision-log.md",
				status: cells[1],
			},
			cells: cells,
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return decisions, nil
}

func decisionCellsFromRow(line string) (cullableKey, []string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "| D") {
		return cullableKey{}, nil, false
	}
	parts := strings.Split(trimmed, "|")
	if len(parts) < 8 {
		return cullableKey{}, nil, false
	}
	var cells []string
	for _, part := range parts[1 : len(parts)-1] {
		cells = append(cells, strings.TrimSpace(part))
	}
	if len(cells) < 6 {
		return cullableKey{}, nil, false
	}
	id := normalizeDecisionID(cells[0])
	if id == "" {
		return cullableKey{}, nil, false
	}
	return cullableKey{kind: "decision", ref: "decision:" + id}, cells, true
}

func decisionKeyFromRow(line string) (cullableKey, bool) {
	key, _, ok := decisionCellsFromRow(line)
	return key, ok
}

func rfcKeyFromPath(rel string) (cullableKey, bool) {
	if !strings.HasPrefix(rel, "docs/rfcs/") || !strings.HasSuffix(rel, ".md") {
		return cullableKey{}, false
	}
	base := pathBase(rel)
	if len(base) < 5 || base[4] != '-' {
		return cullableKey{}, false
	}
	n, err := strconv.Atoi(base[:4])
	if err != nil {
		return cullableKey{}, false
	}
	return cullableKey{kind: "rfc", ref: fmt.Sprintf("rfc:%04d", n)}, true
}

func statusLineSuccessors(status string, defaultKind string) []cullableKey {
	return successorRefsAfterBy(status, defaultKind)
}

func decisionSuccessors(cells []string) []cullableKey {
	if len(cells) < 5 {
		return nil
	}
	if successors := successorRefsAfterBy(cells[2], "decision"); len(successors) > 0 {
		return successors
	}
	return successorRefsAfterBy(cells[4], "decision")
}

var supersededByPattern = regexp.MustCompile(`(?i)\bsupersed(?:ed|es)\s+by\s+(.+)`)
var refTokenPattern = regexp.MustCompile(`(?i)^(D0*[0-9]+|RFC[ -]?0*[0-9]+|0*[0-9]{1,4})\b`)

func successorRefsAfterBy(text string, defaultKind string) []cullableKey {
	match := supersededByPattern.FindStringSubmatch(text)
	if match == nil {
		return nil
	}
	tail := strings.TrimSpace(match[1])
	var refs []cullableKey
	lastKind := defaultKind
	for tail != "" {
		tail = strings.TrimLeft(tail, " \t")
		lower := strings.ToLower(tail)
		if strings.HasPrefix(lower, "and ") {
			tail = strings.TrimSpace(tail[4:])
		}
		tail = strings.TrimLeft(tail, " \t/,")
		token := refTokenPattern.FindString(tail)
		if token == "" {
			break
		}
		key, kind, ok := normalizeRefToken(token, defaultKind, lastKind)
		if !ok {
			break
		}
		refs = append(refs, key)
		lastKind = kind
		tail = tail[len(token):]
	}
	return dedupeKeys(refs)
}

func normalizeRefToken(token string, defaultKind string, lastKind string) (cullableKey, string, bool) {
	upper := strings.ToUpper(strings.TrimSpace(token))
	if strings.HasPrefix(upper, "D") {
		id := normalizeDecisionID(upper)
		if id == "" {
			return cullableKey{}, "", false
		}
		return cullableKey{kind: "decision", ref: "decision:" + id}, "decision", true
	}
	if strings.HasPrefix(upper, "RFC") {
		n, ok := parseTrailingNumber(upper)
		if !ok {
			return cullableKey{}, "", false
		}
		return cullableKey{kind: "rfc", ref: fmt.Sprintf("rfc:%04d", n)}, "rfc", true
	}
	if defaultKind != "rfc" && lastKind != "rfc" {
		return cullableKey{}, "", false
	}
	n, ok := parseTrailingNumber(upper)
	if !ok {
		return cullableKey{}, "", false
	}
	return cullableKey{kind: "rfc", ref: fmt.Sprintf("rfc:%04d", n)}, "rfc", true
}

func normalizeDecisionID(value string) string {
	upper := strings.ToUpper(strings.TrimSpace(value))
	if !strings.HasPrefix(upper, "D") {
		return ""
	}
	n, ok := parseTrailingNumber(upper)
	if !ok {
		return ""
	}
	return fmt.Sprintf("D%03d", n)
}

func parseTrailingNumber(value string) (int, bool) {
	var digits strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	if digits.Len() == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(digits.String())
	return n, err == nil
}

func dedupeKeys(keys []cullableKey) []cullableKey {
	seen := map[cullableKey]bool{}
	var out []cullableKey
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

func statusBeginsAny(status string, prefixes ...string) bool {
	normalized := strings.ToLower(strings.TrimSpace(status))
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func canonicalRefPattern(key cullableKey) *regexp.Regexp {
	switch key.kind {
	case "rfc":
		n, ok := parseTrailingNumber(key.ref)
		if !ok {
			return nil
		}
		return regexp.MustCompile(fmt.Sprintf(`(?i)(\bRFC[ -]0*%d\b|\brfc:0*%d\b|\b0*%04d-[a-z0-9-]+\.md\b)`, n, n, n))
	case "decision":
		n, ok := parseTrailingNumber(key.ref)
		if !ok {
			return nil
		}
		return regexp.MustCompile(fmt.Sprintf(`(?i)\bD0*%d\b`, n))
	default:
		return nil
	}
}

var referenceLinkPattern = regexp.MustCompile(`^\s*\[[^\]]+\]:\s`)
var rfcReadmeRowPattern = regexp.MustCompile(`^\|\s*\[([0-9]{4})\]`)

func disposableCitationLine(path string, line string, key cullableKey) bool {
	if referenceLinkPattern.MatchString(line) {
		return true
	}
	if path == "docs/operator/rfc-roadmap.md" && strings.HasPrefix(strings.TrimSpace(line), "|") {
		return true
	}
	if path == "docs/rfcs/README.md" && key.kind == "rfc" {
		if match := rfcReadmeRowPattern.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			own := fmt.Sprintf("%04d", mustRefNumber(key.ref))
			if match[1] != own {
				return true
			}
		}
	}
	lower := strings.ToLower(line)
	for _, token := range []string{
		"superseded",
		"supersede",
		"deprecat",
		"obsolete",
		"retired",
		"tombston",
		"graveyard",
		"historical",
		"formerly",
		"closed-out",
		"closed out",
		"do not pick up",
		"see also",
		"no longer",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func mustRefNumber(ref string) int {
	n, _ := parseTrailingNumber(ref)
	return n
}

func pathBase(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}
