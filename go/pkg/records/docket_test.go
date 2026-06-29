package records

import (
	"strings"
	"testing"
)

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	shaC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func TestDocketNormalizesSortsAndRendersJSON(t *testing.T) {
	docket := sampleDocketScrambled()
	body, err := docket.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	got := string(body) + "\n"
	want := `{
  "schema_version": "striatum.records.docket.v1",
  "run_id": "run_rfc0171",
  "generated_at": "2026-06-28T00:00:00Z",
  "merkle_root": "6a6aeb3121f470d0934fedb6684ce1a0f675dce1d526a932ef30e3b7b7bb3614",
  "entries": [
    {
      "run_id": "run_rfc0171",
      "artifact_id": "art_alpha",
      "job_id": "job_a",
      "logical_name": "alpha.md",
      "kind": "finding",
      "placement": "blob_exhaust",
      "retention_class": "generated_exhaust",
      "content_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "blob_key": "runs/run_rfc0171/jobs/job_a/artifacts/alpha.md",
      "content_type": "text/markdown",
      "size_bytes": 120,
      "uri": "striatum://artifact/art_alpha"
    },
    {
      "run_id": "run_rfc0171",
      "artifact_id": "art_beta",
      "job_id": "job_b",
      "logical_name": "beta.md",
      "kind": "synthesis",
      "placement": "git_publication",
      "retention_class": "durable_provenance",
      "content_sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "repo_path": "docs/operator/artifacts/rfc-0171/beta.md",
      "content_type": "text/markdown",
      "size_bytes": 80,
      "uri": "striatum://artifact/art_beta"
    },
    {
      "run_id": "run_rfc0171",
      "record_id": "rec_history",
      "source_path": "docs/audits/history.md",
      "class": "historical_audit",
      "placement": "git_pointer_manifest",
      "retention_class": "historical_index",
      "content_sha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
      "blob_key": "records/history.md",
      "repo_path": "docs/records/dockets/history.pointer.json",
      "content_type": "text/markdown",
      "size_bytes": 42,
      "uri": "striatum://record/rec_history"
    }
  ]
}
`
	if got != want {
		t.Fatalf("JSON render mismatch:\n%s", got)
	}
}

func TestDocketMerkleRootStableAcrossInputOrderAndWhitespace(t *testing.T) {
	first, err := sampleDocketScrambled().MerkleRoot()
	if err != nil {
		t.Fatalf("MerkleRoot first: %v", err)
	}
	second, err := sampleDocketSorted().MerkleRoot()
	if err != nil {
		t.Fatalf("MerkleRoot second: %v", err)
	}
	if first != second {
		t.Fatalf("MerkleRoot not stable: first=%s second=%s", first, second)
	}
	if first != "6a6aeb3121f470d0934fedb6684ce1a0f675dce1d526a932ef30e3b7b7bb3614" {
		t.Fatalf("MerkleRoot = %s", first)
	}
}

func TestDocketRendersCompactMarkdown(t *testing.T) {
	body, err := sampleDocketScrambled().RenderMarkdown()
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	want := `# Striatum Record Docket

- Run: ` + "`run_rfc0171`" + `
- Generated: ` + "`2026-06-28T00:00:00Z`" + `
- Merkle root: ` + "`6a6aeb3121f470d0934fedb6684ce1a0f675dce1d526a932ef30e3b7b7bb3614`" + `

| Identity | Job | Name/path | Kind/class | Placement | Retention | SHA-256 | Pointer | Size | URI |
|---|---|---|---|---|---|---|---|---:|---|
| ` + "`artifact:art_alpha`" + ` | ` + "`job_a`" + ` | ` + "`alpha.md`" + ` | ` + "`finding`" + ` | ` + "`blob_exhaust`" + ` | ` + "`generated_exhaust`" + ` | ` + "`aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`" + ` | ` + "`blob:runs/run_rfc0171/jobs/job_a/artifacts/alpha.md`" + ` | 120 | ` + "`striatum://artifact/art_alpha`" + ` |
| ` + "`artifact:art_beta`" + ` | ` + "`job_b`" + ` | ` + "`beta.md`" + ` | ` + "`synthesis`" + ` | ` + "`git_publication`" + ` | ` + "`durable_provenance`" + ` | ` + "`bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb`" + ` | ` + "`repo:docs/operator/artifacts/rfc-0171/beta.md`" + ` | 80 | ` + "`striatum://artifact/art_beta`" + ` |
| ` + "`record:rec_history`" + ` | - | ` + "`docs/audits/history.md`" + ` | ` + "`historical_audit`" + ` | ` + "`git_pointer_manifest`" + ` | ` + "`historical_index`" + ` | ` + "`cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc`" + ` | ` + "`blob:records/history.md / repo:docs/records/dockets/history.pointer.json`" + ` | 42 | ` + "`striatum://record/rec_history`" + ` |
`
	if body != want {
		t.Fatalf("Markdown render mismatch:\n%s", body)
	}
}

func TestDocketValidationFailures(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Docket)
		want string
	}{
		{"missing-identity", func(d *Docket) { d.Entries[0].ArtifactID = "" }, "identity"},
		{"both-identities", func(d *Docket) { d.Entries[0].RecordID = "rec_extra" }, "identity"},
		{"missing-placement", func(d *Docket) { d.Entries[0].Placement = "" }, "placement"},
		{"missing-content-hash", func(d *Docket) { d.Entries[0].ContentSHA256 = "" }, "content_sha256"},
		{"invalid-content-hash", func(d *Docket) { d.Entries[0].ContentSHA256 = "abc" }, "content_sha256"},
		{"missing-uri", func(d *Docket) { d.Entries[0].URI = "" }, "uri"},
		{"wrong-uri", func(d *Docket) { d.Entries[0].URI = "striatum://artifact/other" }, "artifact URI must match artifact_id"},
		{"missing-any-pointer", func(d *Docket) { d.Entries[0].BlobKey = "" }, "pointer"},
		{"blob-placement-requires-blob-key", func(d *Docket) {
			d.Entries[0].BlobKey = ""
			d.Entries[0].RepoPath = "docs/pointer.md"
		}, "blob_key"},
		{"git-placement-requires-repo-path", func(d *Docket) {
			d.Entries[1].Placement = PlacementGitPublication
			d.Entries[1].RepoPath = ""
			d.Entries[1].BlobKey = "records/body.md"
		}, "repo_path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			docket := sampleDocketSorted()
			c.edit(&docket)
			err := docket.Validate()
			if err == nil {
				t.Fatal("Validate returned nil")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Validate error = %v, want substring %q", err, c.want)
			}
		})
	}
}

func sampleDocketScrambled() Docket {
	return Docket{
		RunID:       " run_rfc0171 ",
		GeneratedAt: " 2026-06-28T00:00:00Z ",
		Entries: []Entry{
			{
				RecordID:       " rec_history ",
				SourcePath:     " docs/audits/history.md ",
				Class:          " historical_audit ",
				Placement:      " git_pointer_manifest ",
				RetentionClass: " historical_index ",
				ContentSHA256:  " CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC ",
				BlobKey:        " records/history.md ",
				RepoPath:       " docs/records/dockets/history.pointer.json ",
				ContentType:    " TEXT/MARKDOWN ",
				SizeBytes:      42,
				URI:            " striatum://record/rec_history ",
			},
			{
				ArtifactID:     " art_beta ",
				JobID:          " job_b ",
				LogicalName:    " beta.md ",
				Kind:           " synthesis ",
				Placement:      " git_publication ",
				RetentionClass: " durable_provenance ",
				ContentSHA256:  " BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB ",
				RepoPath:       " docs/operator/artifacts/rfc-0171/beta.md ",
				ContentType:    " TEXT/MARKDOWN ",
				SizeBytes:      80,
				URI:            " striatum://artifact/art_beta ",
			},
			{
				ArtifactID:     " art_alpha ",
				JobID:          " job_a ",
				LogicalName:    " alpha.md ",
				Kind:           " finding ",
				Placement:      " blob_exhaust ",
				RetentionClass: " generated_exhaust ",
				ContentSHA256:  " AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA ",
				BlobKey:        " runs/run_rfc0171/jobs/job_a/artifacts/alpha.md ",
				ContentType:    " TEXT/MARKDOWN ",
				SizeBytes:      120,
				URI:            " striatum://artifact/art_alpha ",
			},
		},
	}
}

func sampleDocketSorted() Docket {
	return Docket{
		RunID:       "run_rfc0171",
		GeneratedAt: "2026-06-28T00:00:00Z",
		Entries: []Entry{
			{
				RunID:          "run_rfc0171",
				ArtifactID:     "art_alpha",
				JobID:          "job_a",
				LogicalName:    "alpha.md",
				Kind:           "finding",
				Placement:      PlacementBlobExhaust,
				RetentionClass: "generated_exhaust",
				ContentSHA256:  shaA,
				BlobKey:        "runs/run_rfc0171/jobs/job_a/artifacts/alpha.md",
				ContentType:    "text/markdown",
				SizeBytes:      120,
				URI:            "striatum://artifact/art_alpha",
			},
			{
				RunID:          "run_rfc0171",
				ArtifactID:     "art_beta",
				JobID:          "job_b",
				LogicalName:    "beta.md",
				Kind:           "synthesis",
				Placement:      PlacementGitPublication,
				RetentionClass: "durable_provenance",
				ContentSHA256:  shaB,
				RepoPath:       "docs/operator/artifacts/rfc-0171/beta.md",
				ContentType:    "text/markdown",
				SizeBytes:      80,
				URI:            "striatum://artifact/art_beta",
			},
			{
				RunID:          "run_rfc0171",
				RecordID:       "rec_history",
				SourcePath:     "docs/audits/history.md",
				Class:          "historical_audit",
				Placement:      PlacementGitPointerManifest,
				RetentionClass: "historical_index",
				ContentSHA256:  shaC,
				BlobKey:        "records/history.md",
				RepoPath:       "docs/records/dockets/history.pointer.json",
				ContentType:    "text/markdown",
				SizeBytes:      42,
				URI:            "striatum://record/rec_history",
			},
		},
	}
}
