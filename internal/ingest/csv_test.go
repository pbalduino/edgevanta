package ingest

import (
	"context"
	"testing"
)

func TestParseBidCSV(t *testing.T) {
	rows, err := ParseBidCSV(context.Background(), "../../docs/sample_bid_tabulation.csv", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected bid rows")
	}
	if rows[0].DocumentID != 7 {
		t.Fatalf("expected document id 7, got %d", rows[0].DocumentID)
	}
	if rows[0].ProjectID == "" || rows[0].ItemDesc == "" {
		t.Fatal("expected populated structured fields")
	}
}
