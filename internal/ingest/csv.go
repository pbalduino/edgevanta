package ingest

import (
	"context"
	"encoding/csv"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pbalduino/ev_assignment/internal/domain"
)

func ParseBidCSV(ctx context.Context, path string, documentID int64) ([]domain.BidRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, nil
	}

	headers := make(map[string]int)
	for i, header := range records[0] {
		headers[strings.TrimSpace(header)] = i
	}

	rows := make([]domain.BidRow, 0, len(records)-1)
	for _, record := range records[1:] {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		row := domain.BidRow{
			DocumentID:   documentID,
			ProjectID:    field(record, headers, "PROJ_ID"),
			County:       field(record, headers, "CNTY"),
			ItemNumber:   field(record, headers, "ITEM_NO"),
			ItemDesc:     field(record, headers, "ITEM_DESC"),
			Unit:         field(record, headers, "UNIT"),
			Quantity:     parseFloat(field(record, headers, "QTY")),
			EngineerUnit: parseFloat(field(record, headers, "ENG_EST_UNIT_PR")),
			Bidder:       field(record, headers, "BIDDER"),
			BidRank:      parseInt(field(record, headers, "BID_RANK")),
			UnitPrice:    parseFloat(field(record, headers, "UNIT_PR")),
			ExtAmount:    parseFloat(field(record, headers, "EXT_AMT")),
			BidTotal:     parseFloat(field(record, headers, "BID_TOTAL")),
		}
		if letDate := field(record, headers, "LET_DT"); letDate != "" {
			row.LetDate, _ = time.Parse("2006-01-02", letDate)
		}
		rows = append(rows, row)
	}

	return rows, nil
}

func field(record []string, headers map[string]int, name string) string {
	index, ok := headers[name]
	if !ok || index >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[index])
}

func parseFloat(value string) float64 {
	parsed, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return parsed
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}
