package main

import (
	"testing"

	"github.com/wtsi-hgi/wa/saga"
)

func TestLooksLikeStudySearch(t *testing.T) {
	tests := []struct {
		query string
		want  bool
	}{
		{query: "6568", want: true},
		{query: "EGAS00001005445", want: true},
		{query: "my study title", want: true},
		{query: "AM762808", want: false},
		{query: "WTSI_wEMB10524782", want: false},
	}

	for _, test := range tests {
		if got := looksLikeStudySearch(test.query); got != test.want {
			t.Fatalf("looksLikeStudySearch(%q) = %v, want %v", test.query, got, test.want)
		}
	}
}

func TestIRODSSampleCandidateIDs(t *testing.T) {
	sample := saga.IRODSSample{
		SourceID: "1913216340",
		Data: map[string]any{
			"avu:sample": []any{"AM762808", "AM762808"},
		},
		Curated: map[string]any{
			"sanger_id": []any{"AM762808"},
		},
	}

	ids := irodsSampleCandidateIDs(sample, "AM762808")
	if len(ids) != 1 || ids[0] != "AM762808" {
		t.Fatalf("irodsSampleCandidateIDs() = %#v, want [\"AM762808\"]", ids)
	}
}

func TestIRODSSampleCandidateIDsIncludesMatchingSourceID(t *testing.T) {
	sample := saga.IRODSSample{SourceID: "folder-456"}

	ids := irodsSampleCandidateIDs(sample, "folder-456")
	if len(ids) != 1 || ids[0] != "folder-456" {
		t.Fatalf("irodsSampleCandidateIDs() = %#v, want [\"folder-456\"]", ids)
	}
}
