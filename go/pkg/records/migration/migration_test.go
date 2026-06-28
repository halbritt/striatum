package migration

import "testing"

func TestRecordIDAndBlobKeyAreDeterministic(t *testing.T) {
	id1 := RecordID("abcdef1234567890", "docs/records/audits/report.md", "ABCDEF")
	id2 := RecordID("abcdef1234567890", "./docs/records/audits/report.md", "abcdef")
	if id1 != id2 {
		t.Fatalf("RecordID not normalized: %q != %q", id1, id2)
	}
	if id1 == RecordID("abcdef1234567890", "docs/records/audits/other.md", "abcdef") {
		t.Fatal("RecordID did not vary by source path")
	}
	key := BlobKey("abcdef1234567890abcdef", "docs/records/audits/report.md", "0123456789abcdef0123")
	want := "records/historical/abcdef1234567890/0123456789abcdef/docs/records/audits/report.md"
	if key != want {
		t.Fatalf("BlobKey = %q, want %q", key, want)
	}
}

func TestCompareManifestsReportsExactByteProofProblems(t *testing.T) {
	problems := CompareManifests(
		[]ManifestEntry{
			{Path: "a.md", Size: 3, SHA256: "aaa"},
			{Path: "b.md", Size: 4, SHA256: "bbb"},
		},
		[]ManifestEntry{
			{Path: "a.md", Size: 3, SHA256: "AAA"},
			{Path: "b.md", Size: 5, SHA256: "bbb"},
			{Path: "c.md", Size: 1, SHA256: "ccc"},
		},
	)
	if len(problems) != 2 {
		t.Fatalf("problems = %#v, want size mismatch + unexpected", problems)
	}
	if problems[0].Code != "reconstructed_size_mismatch" || problems[0].Path != "b.md" {
		t.Fatalf("first problem = %#v", problems[0])
	}
	if problems[1].Code != "unexpected_reconstructed_record" || problems[1].Path != "c.md" {
		t.Fatalf("second problem = %#v", problems[1])
	}
}
