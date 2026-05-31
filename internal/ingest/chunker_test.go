package ingest

import "testing"

func TestChunkPagesPreservesPageRanges(t *testing.T) {
	pages := []PageText{
		{Page: 1, Text: "alpha beta gamma delta epsilon"},
		{Page: 2, Text: "zeta eta theta iota kappa"},
		{Page: 3, Text: "lambda mu nu xi omicron"},
	}

	chunks := ChunkPages(pages, 40, 0)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	if chunks[0].PageStart != 1 {
		t.Fatalf("expected first chunk to start on page 1, got %d", chunks[0].PageStart)
	}
	if chunks[len(chunks)-1].PageEnd != 3 {
		t.Fatalf("expected last chunk to end on page 3, got %d", chunks[len(chunks)-1].PageEnd)
	}
}
