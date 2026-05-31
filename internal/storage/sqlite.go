package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/pbalduino/ev_assignment/internal/domain"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(context.Background()); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	stmts := []string{
		`create table if not exists documents (
			id integer primary key autoincrement,
			name text not null,
			path text not null,
			type text not null,
			source text not null,
			page_count integer not null default 0,
			uploaded_at text not null
		)`,
		`create table if not exists chunks (
			id integer primary key autoincrement,
			document_id integer not null,
			chunk_index integer not null,
			page_start integer not null,
			page_end integer not null,
			section text not null default '',
			source text not null,
			text text not null,
			metadata text not null default '{}',
			foreign key(document_id) references documents(id)
		)`,
		`create table if not exists embeddings (
			chunk_id integer primary key,
			vector text not null,
			foreign key(chunk_id) references chunks(id)
		)`,
		`create table if not exists bid_rows (
			id integer primary key autoincrement,
			document_id integer not null,
			project_id text not null,
			let_date text,
			county text not null,
			item_number text not null,
			item_desc text not null,
			unit text not null,
			quantity real not null,
			engineer_unit_price real not null,
			bidder text not null,
			bid_rank integer not null,
			unit_price real not null,
			ext_amount real not null,
			bid_total real not null,
			foreign key(document_id) references documents(id)
		)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) CreateDocument(ctx context.Context, doc domain.Document) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		insert into documents (name, path, type, source, page_count, uploaded_at)
		values (?, ?, ?, ?, ?, ?)`,
		doc.Name, doc.Path, string(doc.Type), string(doc.Source), doc.PageCount, doc.UploadedAt.Format(time.RFC3339),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) ListDocuments(ctx context.Context) ([]domain.Document, error) {
	rows, err := s.db.QueryContext(ctx, `select id, name, path, type, source, page_count, uploaded_at from documents order by id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []domain.Document
	for rows.Next() {
		var doc domain.Document
		var uploadedAt string
		if err := rows.Scan(&doc.ID, &doc.Name, &doc.Path, &doc.Type, &doc.Source, &doc.PageCount, &uploadedAt); err != nil {
			return nil, err
		}
		doc.UploadedAt, _ = time.Parse(time.RFC3339, uploadedAt)
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

func (s *SQLiteStore) DeleteDocument(ctx context.Context, documentID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `delete from embeddings where chunk_id in (select id from chunks where document_id = ?)`, documentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from chunks where document_id = ?`, documentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from bid_rows where document_id = ?`, documentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `delete from documents where id = ?`, documentID); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *SQLiteStore) InsertChunks(ctx context.Context, chunks []domain.DocumentChunk, vectors [][]float64) error {
	if len(chunks) != len(vectors) {
		return errors.New("chunks and vectors length mismatch")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i, chunk := range chunks {
		res, err := tx.ExecContext(ctx, `
			insert into chunks (document_id, chunk_index, page_start, page_end, section, source, text, metadata)
			values (?, ?, ?, ?, ?, ?, ?, ?)`,
			chunk.DocumentID, chunk.ChunkIndex, chunk.PageStart, chunk.PageEnd, chunk.Section, string(chunk.Source), chunk.Text, chunk.Metadata,
		)
		if err != nil {
			return err
		}
		chunkID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		rawVector, err := json.Marshal(vectors[i])
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `insert into embeddings (chunk_id, vector) values (?, ?)`, chunkID, string(rawVector)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) InsertBidRows(ctx context.Context, rows []domain.BidRow) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		insert into bid_rows (
			document_id, project_id, let_date, county, item_number, item_desc, unit, quantity,
			engineer_unit_price, bidder, bid_rank, unit_price, ext_amount, bid_total
		) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, row := range rows {
		letDate := ""
		if !row.LetDate.IsZero() {
			letDate = row.LetDate.Format("2006-01-02")
		}
		if _, err := stmt.ExecContext(ctx,
			row.DocumentID, row.ProjectID, letDate, row.County, row.ItemNumber, row.ItemDesc, row.Unit,
			row.Quantity, row.EngineerUnit, row.Bidder, row.BidRank, row.UnitPrice, row.ExtAmount, row.BidTotal,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteStore) SearchChunks(ctx context.Context, vector []float64, limit int, filter domain.KnowledgeSource) ([]domain.DocumentChunk, error) {
	rows, err := s.db.QueryContext(ctx, `
		select c.id, c.document_id, c.chunk_index, c.page_start, c.page_end, c.section, c.source, c.text, c.metadata, e.vector
		from chunks c
		join embeddings e on e.chunk_id = c.id
		where (? = '' or c.source = ?)
	`, string(filter), string(filter))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []domain.DocumentChunk
	for rows.Next() {
		var chunk domain.DocumentChunk
		var source string
		var rawVector string
		if err := rows.Scan(&chunk.ID, &chunk.DocumentID, &chunk.ChunkIndex, &chunk.PageStart, &chunk.PageEnd, &chunk.Section, &source, &chunk.Text, &chunk.Metadata, &rawVector); err != nil {
			return nil, err
		}
		chunk.Source = domain.KnowledgeSource(source)
		var stored []float64
		if err := json.Unmarshal([]byte(rawVector), &stored); err != nil {
			return nil, err
		}
		chunk.Score = cosineSimilarity(vector, stored)
		results = append(results, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *SQLiteStore) BidSummary(ctx context.Context) (map[string]any, error) {
	row := s.db.QueryRowContext(ctx, `
		select count(*), count(distinct item_number), count(distinct bidder), coalesce(sum(ext_amount), 0), coalesce(max(bid_total), 0)
		from bid_rows`)
	var rowCount, itemCount, bidderCount int
	var extSum, maxTotal float64
	if err := row.Scan(&rowCount, &itemCount, &bidderCount, &extSum, &maxTotal); err != nil {
		return nil, err
	}
	return map[string]any{
		"row_count": rowCount,
		"item_count": itemCount,
		"bidder_count": bidderCount,
		"extended_amount_sum": extSum,
		"max_bid_total": maxTotal,
	}, nil
}

func (s *SQLiteStore) TopBidItems(ctx context.Context, limit int) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
		select item_number, item_desc, unit, sum(ext_amount) as total_ext
		from bid_rows
		group by item_number, item_desc, unit
		order by total_ext desc
		limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []map[string]any
	for rows.Next() {
		var itemNumber, itemDesc, unit string
		var total float64
		if err := rows.Scan(&itemNumber, &itemDesc, &unit, &total); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"item_number": itemNumber,
			"item_desc": itemDesc,
			"unit": unit,
			"total_ext_amount": total,
		})
	}
	return items, rows.Err()
}

func (s *SQLiteStore) TopBidItemsLowestBidder(ctx context.Context, limit int) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
		select item_number, item_desc, unit, sum(ext_amount) as total_ext
		from bid_rows
		where bid_rank = 1
		group by item_number, item_desc, unit
		order by total_ext desc
		limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []map[string]any
	for rows.Next() {
		var itemNumber, itemDesc, unit string
		var total float64
		if err := rows.Scan(&itemNumber, &itemDesc, &unit, &total); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"item_number":      itemNumber,
			"item_desc":        itemDesc,
			"unit":             unit,
			"total_ext_amount": total,
		})
	}
	return items, rows.Err()
}

func (s *SQLiteStore) PriceOutliers(ctx context.Context, minRatio, minZScore float64) ([]domain.PriceOutlier, error) {
	rows, err := s.db.QueryContext(ctx, `
		select item_number, item_desc, unit, bidder, unit_price
		from bid_rows
		where unit_price > 0
		order by item_number`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type rowData struct {
		itemNumber string
		itemDesc   string
		unit       string
		bidder     string
		unitPrice  float64
	}
	grouped := map[string][]rowData{}
	for rows.Next() {
		var row rowData
		if err := rows.Scan(&row.itemNumber, &row.itemDesc, &row.unit, &row.bidder, &row.unitPrice); err != nil {
			return nil, err
		}
		key := row.itemNumber + "|" + row.itemDesc + "|" + row.unit
		grouped[key] = append(grouped[key], row)
	}

	var outliers []domain.PriceOutlier
	for _, items := range grouped {
		if len(items) < 3 {
			continue
		}
		var sum float64
		for _, item := range items {
			sum += item.unitPrice
		}
		avg := sum / float64(len(items))
		var variance float64
		for _, item := range items {
			diff := item.unitPrice - avg
			variance += diff * diff
		}
		stdDev := math.Sqrt(variance / float64(len(items)))
		for _, item := range items {
			ratio := 1.0
			if avg > 0 {
				ratio = item.unitPrice / avg
			}
			z := 0.0
			if stdDev > 0 {
				z = (item.unitPrice - avg) / stdDev
			}
			isHigh := ratio >= minRatio && z >= minZScore
			isLow := ratio <= (1/minRatio) && z <= -minZScore
			if isHigh || isLow {
				classification := "high"
				if isLow {
					classification = "low"
				}
				outliers = append(outliers, domain.PriceOutlier{
					ItemNumber:     item.itemNumber,
					ItemDesc:       item.itemDesc,
					Bidder:         item.bidder,
					Unit:           item.unit,
					UnitPrice:      item.unitPrice,
					AveragePrice:   avg,
					RatioToAverage: ratio,
					ZScore:         z,
					DeviationClass: classification,
				})
			}
		}
	}

	sort.Slice(outliers, func(i, j int) bool {
		return math.Abs(outliers[i].ZScore) > math.Abs(outliers[j].ZScore)
	})
	return outliers, nil
}

func (s *SQLiteStore) DocumentSummary(ctx context.Context) (map[string]any, error) {
	row := s.db.QueryRowContext(ctx, `
		select count(*), coalesce(sum(page_count), 0)
		from documents
		where type = 'pdf'`)
	var documentCount int
	var pageCount int
	if err := row.Scan(&documentCount, &pageCount); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		select source, count(*)
		from chunks
		group by source
		order by source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chunkCounts := map[string]int{}
	for rows.Next() {
		var source string
		var count int
		if err := rows.Scan(&source, &count); err != nil {
			return nil, err
		}
		chunkCounts[source] = count
	}

	return map[string]any{
		"pdf_document_count": documentCount,
		"pdf_page_count":     pageCount,
		"chunk_counts":       chunkCounts,
	}, nil
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, magA, magB float64
	for i := range a {
		dot += a[i] * b[i]
		magA += a[i] * a[i]
		magB += b[i] * b[i]
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}
