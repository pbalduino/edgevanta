package service

import (
	"testing"

	"github.com/pbalduino/ev_assignment/internal/domain"
)

func TestClassifyQuestionBidOutliers(t *testing.T) {
	route := classifyQuestion("Are there any items with unit prices that deviate significantly from the average?")
	if len(route.Tools) == 0 || route.Tools[0] != "find_price_outliers" {
		t.Fatalf("expected outlier tool, got %+v", route.Tools)
	}
	if route.NeedSearch {
		t.Fatalf("did not expect retrieval-only routing for outlier question: %+v", route)
	}
}

func TestClassifyQuestionPlansAndSummary(t *testing.T) {
	route := classifyQuestion("Summarize the project phasing and runway details from the plan set")
	if len(route.Tools) == 0 || route.Tools[0] != "get_project_summary" {
		t.Fatalf("expected project summary tool, got %+v", route.Tools)
	}
	if !route.NeedSearch {
		t.Fatalf("expected document search for plan question: %+v", route)
	}
	if route.SourceHint != domain.SourcePlans {
		t.Fatalf("expected plan source hint, got %s", route.SourceHint)
	}
}

func TestClassifyQuestionSpecsRetrieval(t *testing.T) {
	route := classifyQuestion("What do the specifications say about drainage requirements?")
	if len(route.Tools) != 0 {
		t.Fatalf("expected pure retrieval route, got %+v", route.Tools)
	}
	if !route.NeedSearch {
		t.Fatalf("expected retrieval for specs question: %+v", route)
	}
	if route.SourceHint != domain.SourceSpecifications {
		t.Fatalf("expected specifications source hint, got %s", route.SourceHint)
	}
}
