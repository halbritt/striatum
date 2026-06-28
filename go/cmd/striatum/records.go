package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/halbritt/striatum/go/pkg/cli/routes"
	"github.com/halbritt/striatum/go/pkg/cli/rpcclient"
	recordsinventory "github.com/halbritt/striatum/go/pkg/records/inventory"
	recordsmigration "github.com/halbritt/striatum/go/pkg/records/migration"
)

func runRecords(args []string, stdout io.Writer, stderr io.Writer, globals leadingGlobals) int {
	if len(args) == 0 || routes.IsHelpArg(args[0]) {
		out := stdout
		if len(args) == 0 {
			out = stderr
		}
		printRecordsHelp(out)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	if args[0] != "migration" {
		_, _ = fmt.Fprintf(stderr, "unknown records command: %s\n", args[0])
		return 2
	}
	if len(args) == 1 || routes.IsHelpArg(args[1]) {
		out := stdout
		if len(args) == 1 {
			out = stderr
		}
		printRecordsMigrationHelp(out)
		if len(args) == 1 {
			return 2
		}
		return 0
	}
	if args[1] != "inventory" {
		return runRecordsMigrationRPC(args[1:], stdout, stderr, globals)
	}
	return runRecordsMigrationInventory(args[2:], stdout, stderr, globals.RepoPath)
}

func runRecordsMigrationInventory(args []string, stdout io.Writer, stderr io.Writer, repoRootOverride string) int {
	for _, arg := range args {
		if routes.IsHelpArg(arg) {
			printRecordsInventoryHelp(stdout)
			return 0
		}
	}
	roots, err := parseRecordsInventoryArgs(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if repoRootOverride == "" {
		repoRootOverride, err = os.Getwd()
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err.Error())
			return 1
		}
	}
	manifest, err := recordsinventory.Run(context.Background(), recordsinventory.Options{
		RepoRoot: repoRootOverride,
		Roots:    roots,
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	encoder := json.NewEncoder(stdout)
	if err := encoder.Encode(manifest); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return 0
}

func parseRecordsInventoryArgs(args []string) ([]string, error) {
	roots := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, value, hasValue := strings.Cut(arg, "=")
		switch key {
		case "--root":
			if !hasValue {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("--root requires a value")
				}
				value = args[i+1]
				i++
			}
			roots = append(roots, value)
		case "--json":
			parsed, err := optionalBool(value, hasValue)
			if err != nil {
				return nil, fmt.Errorf("--json must be a boolean")
			}
			if !parsed {
				return nil, fmt.Errorf("records migration inventory always emits JSON; --json=false is not supported")
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return nil, fmt.Errorf("unknown records migration inventory flag: %s", arg)
			}
			roots = append(roots, arg)
		}
	}
	return roots, nil
}

func printRecordsHelp(out io.Writer) {
	_, _ = fmt.Fprintln(out, "usage: striatum records <command> ...")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Commands:")
	_, _ = fmt.Fprintln(out, "  docket <run-id>       daemon-backed run record docket")
	_, _ = fmt.Fprintln(out, "  migration inventory   read-only historical records inventory")
}

func printRecordsMigrationHelp(out io.Writer) {
	_, _ = fmt.Fprintln(out, "usage: striatum records migration <command> ...")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Commands:")
	_, _ = fmt.Fprintln(out, "  inventory   emit a deterministic JSON manifest for historical record roots")
	_, _ = fmt.Fprintln(out, "  import      import safe manifest entries to daemon-indexed blob storage")
	_, _ = fmt.Fprintln(out, "  verify      verify imported blob/index bytes against an inventory manifest")
	_, _ = fmt.Fprintln(out, "  materialize reconstruct imported records into .striatum/scratch")
}

func printRecordsInventoryHelp(out io.Writer) {
	_, _ = fmt.Fprintln(out, "usage: striatum records migration inventory [--root path]... [path ...]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Read-only. Walks historical record roots and emits deterministic JSON entries.")
	_, _ = fmt.Fprintln(out, "Default roots: docs/operator, docs/records/audits, docs/records/_frozen, docs/dogfood, dogfoods")
	_, _ = fmt.Fprintln(out, "Repeat --root to override the defaults. Positional paths are also treated as roots.")
}

func runRecordsMigrationRPC(args []string, stdout io.Writer, stderr io.Writer, globals leadingGlobals) int {
	if len(args) == 0 || routes.IsHelpArg(args[0]) {
		out := stdout
		if len(args) == 0 {
			out = stderr
		}
		printRecordsMigrationHelp(out)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "import":
		return runRecordsMigrationImport(args[1:], stdout, stderr, globals)
	case "verify":
		return runRecordsMigrationVerify(args[1:], stdout, stderr, globals)
	case "materialize", "hydrate":
		return runRecordsMigrationMaterialize(args[1:], stdout, stderr, globals)
	default:
		_, _ = fmt.Fprintf(stderr, "unknown records migration command: %s\n", args[0])
		return 2
	}
}

func runRecordsMigrationImport(args []string, stdout io.Writer, stderr io.Writer, globals leadingGlobals) int {
	for _, arg := range args {
		if routes.IsHelpArg(arg) {
			printRecordsImportHelp(stdout)
			return 0
		}
	}
	options, err := parseRecordsMigrationManifestArgs(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 2
	}
	repoRoot, client, repositoryID, exit := recordsMigrationClient(globals, stderr)
	if exit != 0 {
		return exit
	}
	manifest, manifestBytes, err := readRecordsInventoryManifest(options.ManifestPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if options.ImportBatchID == "" {
		sum := sha256.Sum256(manifestBytes)
		options.ImportBatchID = "inventory-" + fmt.Sprintf("%x", sum[:8])
	}
	results := []map[string]any{}
	for _, entry := range manifest.Entries {
		if entry.Classification != recordsinventory.ClassificationSafeToBlobIndex {
			continue
		}
		body, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(entry.Path)))
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "read %s: %v\n", entry.Path, err)
			return 1
		}
		result, err := client.Invoke(context.Background(), "records.migration.import", map[string]any{
			"repository_id":    repositoryID,
			"source_path":      entry.Path,
			"source_commit":    entry.SourceCommit,
			"record_class":     entry.RecordClass,
			"content_sha256":   entry.SHA256,
			"content_type":     contentTypeForRecordsPath(entry.Path),
			"body_base64":      base64.StdEncoding.EncodeToString(body),
			"import_batch_id":  options.ImportBatchID,
			"retention_class":  recordsmigration.DefaultRetentionClass,
			"dry_run":          options.DryRun,
			"deletion_allowed": false,
		})
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err.Error())
			return rpcclient.ExitCode(err)
		}
		results = append(results, result)
	}
	return writeRecordsJSON(stdout, stderr, map[string]any{
		"schema_version":    recordsmigration.ImportSchemaVersion,
		"repository_id":     repositoryID,
		"import_batch_id":   options.ImportBatchID,
		"imported_count":    len(results),
		"results":           results,
		"deletion_allowed":  false,
		"source_manifest":   options.ManifestPath,
		"classification":    recordsinventory.ClassificationSafeToBlobIndex,
		"source_files_kept": true,
	})
}

func runRecordsMigrationVerify(args []string, stdout io.Writer, stderr io.Writer, globals leadingGlobals) int {
	for _, arg := range args {
		if routes.IsHelpArg(arg) {
			printRecordsVerifyHelp(stdout)
			return 0
		}
	}
	options, err := parseRecordsMigrationManifestArgs(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 2
	}
	_, client, repositoryID, exit := recordsMigrationClient(globals, stderr)
	if exit != 0 {
		return exit
	}
	manifest, _, err := readRecordsInventoryManifest(options.ManifestPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	entries := safeManifestEntries(manifest)
	result, err := client.Invoke(context.Background(), "records.migration.verify", map[string]any{
		"repository_id": repositoryID,
		"entries":       entries,
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return rpcclient.ExitCode(err)
	}
	result["source_manifest"] = options.ManifestPath
	return writeRecordsJSON(stdout, stderr, result)
}

func runRecordsMigrationMaterialize(args []string, stdout io.Writer, stderr io.Writer, globals leadingGlobals) int {
	for _, arg := range args {
		if routes.IsHelpArg(arg) {
			printRecordsMaterializeHelp(stdout)
			return 0
		}
	}
	options, err := parseRecordsMaterializeArgs(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 2
	}
	repoRoot, client, repositoryID, exit := recordsMigrationClient(globals, stderr)
	if exit != 0 {
		return exit
	}
	params := map[string]any{"repository_id": repositoryID}
	if options.RecordID != "" {
		params["record_id"] = options.RecordID
	}
	if options.ImportBatchID != "" {
		params["import_batch_id"] = options.ImportBatchID
	}
	if options.SourcePath != "" {
		params["source_path"] = options.SourcePath
	}
	result, err := client.Invoke(context.Background(), "records.migration.materialize", params)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return rpcclient.ExitCode(err)
	}
	outRoot := options.OutRoot
	if outRoot == "" {
		selector := firstNonEmptyString(options.ImportBatchID, options.RecordID, "selected")
		outRoot = filepath.Join(repoRoot, ".striatum", "scratch", "records-migration", selector)
	}
	outRoot, err = validateRecordsScratchRoot(repoRoot, outRoot)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 2
	}
	written, err := writeMaterializedRecords(outRoot, result["records"])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	result["out_root"] = outRoot
	result["written"] = written
	result["deletion_allowed"] = false
	return writeRecordsJSON(stdout, stderr, result)
}

type recordsManifestOptions struct {
	ManifestPath  string
	ImportBatchID string
	DryRun        bool
}

func parseRecordsMigrationManifestArgs(args []string) (recordsManifestOptions, error) {
	var options recordsManifestOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, value, hasValue := strings.Cut(arg, "=")
		switch key {
		case "--manifest":
			if !hasValue {
				if i+1 >= len(args) {
					return options, fmt.Errorf("--manifest requires a value")
				}
				value = args[i+1]
				i++
			}
			options.ManifestPath = value
		case "--import-batch-id":
			if !hasValue {
				if i+1 >= len(args) {
					return options, fmt.Errorf("--import-batch-id requires a value")
				}
				value = args[i+1]
				i++
			}
			options.ImportBatchID = value
		case "--dry-run":
			parsed, err := optionalBool(value, hasValue)
			if err != nil {
				return options, fmt.Errorf("--dry-run must be a boolean")
			}
			options.DryRun = parsed
		case "--json":
			parsed, err := optionalBool(value, hasValue)
			if err != nil || !parsed {
				return options, fmt.Errorf("--json must be true")
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return options, fmt.Errorf("unknown records migration flag: %s", arg)
			}
			if options.ManifestPath != "" {
				return options, fmt.Errorf("multiple manifest paths supplied")
			}
			options.ManifestPath = arg
		}
	}
	if strings.TrimSpace(options.ManifestPath) == "" {
		return options, fmt.Errorf("--manifest is required")
	}
	return options, nil
}

type recordsMaterializeOptions struct {
	RecordID      string
	ImportBatchID string
	SourcePath    string
	OutRoot       string
}

func parseRecordsMaterializeArgs(args []string) (recordsMaterializeOptions, error) {
	var options recordsMaterializeOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, value, hasValue := strings.Cut(arg, "=")
		switch key {
		case "--record-id":
			if !hasValue {
				if i+1 >= len(args) {
					return options, fmt.Errorf("--record-id requires a value")
				}
				value = args[i+1]
				i++
			}
			options.RecordID = value
		case "--import-batch-id":
			if !hasValue {
				if i+1 >= len(args) {
					return options, fmt.Errorf("--import-batch-id requires a value")
				}
				value = args[i+1]
				i++
			}
			options.ImportBatchID = value
		case "--source-path":
			if !hasValue {
				if i+1 >= len(args) {
					return options, fmt.Errorf("--source-path requires a value")
				}
				value = args[i+1]
				i++
			}
			options.SourcePath = value
		case "--out":
			if !hasValue {
				if i+1 >= len(args) {
					return options, fmt.Errorf("--out requires a value")
				}
				value = args[i+1]
				i++
			}
			options.OutRoot = value
		case "--json":
			parsed, err := optionalBool(value, hasValue)
			if err != nil || !parsed {
				return options, fmt.Errorf("--json must be true")
			}
		default:
			return options, fmt.Errorf("unknown records materialize flag: %s", arg)
		}
	}
	if options.RecordID == "" && options.ImportBatchID == "" && options.SourcePath == "" {
		return options, fmt.Errorf("materialize requires --record-id, --import-batch-id, or --source-path")
	}
	return options, nil
}

func recordsMigrationClient(globals leadingGlobals, stderr io.Writer) (string, rpcclient.Client, string, int) {
	config, err := rpcclient.ResolveConfig(os.Environ(), globals.SocketPath, globals.Token, globals.TokenFile, globals.DeadlineMS)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return "", rpcclient.Client{}, "", 1
	}
	client := rpcclient.Client{Config: config}
	repositoryID := globals.RepositoryID
	repoRoot := globals.RepoPath
	if repositoryID == "" {
		repositoryID = envLookup(os.Environ(), "STRIATUM_REPOSITORY_ID")
	}
	resolvedRoot, err := clientRepoRoot(repoRoot)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return "", rpcclient.Client{}, "", 1
	}
	repoRoot = resolvedRoot
	if repositoryID == "" {
		resolved, err := client.Invoke(context.Background(), "repo.resolve", map[string]any{"path": repoRoot})
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err.Error())
			return "", rpcclient.Client{}, "", rpcclient.ExitCode(err)
		}
		repositoryID, _ = resolved["repository_id"].(string)
		if root, _ := resolved["repo_root"].(string); root != "" {
			repoRoot = root
		}
	}
	if repositoryID == "" {
		_, _ = fmt.Fprintln(stderr, "repo.resolve response did not include repository_id")
		return "", rpcclient.Client{}, "", 1
	}
	return repoRoot, client, repositoryID, 0
}

func readRecordsInventoryManifest(pathText string) (recordsinventory.Manifest, []byte, error) {
	body, err := os.ReadFile(pathText)
	if err != nil {
		return recordsinventory.Manifest{}, nil, err
	}
	var manifest recordsinventory.Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return recordsinventory.Manifest{}, nil, err
	}
	if manifest.SchemaVersion != recordsinventory.SchemaVersion {
		return recordsinventory.Manifest{}, nil, fmt.Errorf("manifest schema_version = %q, want %q", manifest.SchemaVersion, recordsinventory.SchemaVersion)
	}
	return manifest, body, nil
}

func safeManifestEntries(manifest recordsinventory.Manifest) []map[string]any {
	entries := []map[string]any{}
	for _, entry := range manifest.Entries {
		if entry.Classification != recordsinventory.ClassificationSafeToBlobIndex {
			continue
		}
		entries = append(entries, map[string]any{
			"path":          entry.Path,
			"size":          entry.Size,
			"sha256":        entry.SHA256,
			"source_commit": entry.SourceCommit,
			"record_class":  entry.RecordClass,
		})
	}
	return entries
}

func validateRecordsScratchRoot(repoRoot, outRoot string) (string, error) {
	if !filepath.IsAbs(outRoot) {
		outRoot = filepath.Join(repoRoot, outRoot)
	}
	clean := filepath.Clean(outRoot)
	scratch := filepath.Join(repoRoot, ".striatum", "scratch")
	rel, err := filepath.Rel(scratch, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("--out must be under %s", scratch)
	}
	return clean, nil
}

func writeMaterializedRecords(outRoot string, raw any) ([]string, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("records.migration.materialize response missing records array")
	}
	written := []string{}
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("records.migration.materialize returned malformed record")
		}
		sourcePath, _ := row["source_path"].(string)
		bodyB64, hasBody := row["body_base64"].(string)
		if recordsmigration.PathEscapesRepository(sourcePath) || !hasBody {
			return nil, fmt.Errorf("materialized record has unsafe source_path or missing body: %s", sourcePath)
		}
		body, err := base64.StdEncoding.DecodeString(bodyB64)
		if err != nil {
			return nil, err
		}
		target := filepath.Join(outRoot, filepath.FromSlash(sourcePath))
		rel, err := filepath.Rel(outRoot, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("materialized path escapes output root: %s", sourcePath)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, body, 0o644); err != nil {
			return nil, err
		}
		written = append(written, target)
	}
	return written, nil
}

func writeRecordsJSON(stdout io.Writer, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	return 0
}

func contentTypeForRecordsPath(pathText string) string {
	lower := strings.ToLower(pathText)
	switch {
	case strings.HasSuffix(lower, ".md"):
		return "text/markdown; charset=utf-8"
	case strings.HasSuffix(lower, ".json"):
		return "application/json"
	case strings.HasSuffix(lower, ".txt"):
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func printRecordsImportHelp(out io.Writer) {
	_, _ = fmt.Fprintln(out, "usage: striatum records migration import --manifest inventory.json [--import-batch-id id] [--dry-run] [--json]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Imports only safe_to_blob_index entries through daemon RPC and never deletes source files.")
}

func printRecordsVerifyHelp(out io.Writer) {
	_, _ = fmt.Fprintln(out, "usage: striatum records migration verify --manifest inventory.json [--json]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Verifies generated_records rows and blob bytes against safe inventory entries.")
}

func printRecordsMaterializeHelp(out io.Writer) {
	_, _ = fmt.Fprintln(out, "usage: striatum records migration materialize (--record-id id | --import-batch-id id | --source-path path) [--out .striatum/scratch/path] [--json]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Writes verified blob bodies only under .striatum/scratch.")
}
