# Construction Estimating Agent

Go service for the take-home assignment. It ingests the provided bid tabulation CSV and project PDFs, stores structured and unstructured data in SQLite, generates OpenAI embeddings for document chunks, exposes explicit programmatic tools, and answers grounded questions about the project.

The implementation is intentionally optimized for what the assignment says it evaluates:

- architecture decisions
- handling messy data
- retrieval quality
- outlier detection
- code quality
- tool-style capabilities

It is intentionally not optimized for fancy frontend work, production deployment, distributed infrastructure, or scale.

## What It Does

- Parses `docs/sample_bid_tabulation.csv` into structured SQLite tables.
- Extracts text from `specifications-vol-1.pdf` and `specifications-vol-2.pdf` with native PDF extraction.
- Falls back to OCR with `Tesseract` for pages with no extractable text, which is required for the scanned `plans.pdf`.
- Chunks document text with page metadata and stores embeddings in SQLite.
- Exposes explicit tools for:
  - `search_documents`
  - `analyze_bid_data`
  - `find_price_outliers`
  - `get_project_summary`
- Routes natural-language questions through those tools and synthesizes grounded answers with citations.

## Architecture

This is a lightweight layered modular monolith:

- `cmd/estimator`: process startup
- `internal/config`: environment configuration
- `internal/app`: HTTP routing, handlers, minimal HTML UI
- `internal/service`: orchestration, routing, question handling
- `internal/storage`: SQLite schema and queries
- `internal/ingest`: CSV parsing, PDF extraction, OCR fallback, chunking
- `internal/provider`: OpenAI embeddings and answer synthesis

The most important architectural choice is separating structured analysis from semantic retrieval:

- Bid tabulation questions use code and SQL.
- Specification and plan questions use retrieval over chunked documents.
- The answer layer combines tool output and retrieved context instead of sending everything through a generic chatbot path.

This is the main reason the system stays simple without becoming sloppy.

## Why These Choices

### Structured CSV, not just embeddings

The CSV is well-suited for deterministic analysis:

- top cost items
- bidder summaries
- outlier detection
- project totals

Using SQL and code here is more accurate, cheaper, and easier to explain than embedding the CSV and hoping retrieval does the right thing.

### Native PDF text first, OCR only when needed

The specification books contain extractable text, so native extraction is the fast path.

The plan set is effectively image-based, so OCR is required there. The implementation therefore uses:

1. native PDF text extraction
2. OCR fallback only when a page has no usable text

This is pragmatic and aligns with the assignment: the goal is not perfect OCR, but sensible handling of messy documents.

### SQLite for everything

SQLite keeps local setup fast and simple:

- one file database
- no external services
- easy inspection during debugging
- enough for this assignment’s scale

Embeddings are stored as JSON arrays in an `embeddings` table and ranked in-process with cosine similarity. This is not how I would scale a production system, but it is a good fit here.

## Tool Interface

The assignment explicitly says a stronger submission exposes capabilities as structured tools. This app does that through HTTP endpoints:

- `GET /api/tools/search?q=...&source=...`
- `GET /api/tools/analyze-bid-data`
- `GET /api/tools/price-outliers`
- `GET /api/tools/project-summary`
- `POST /api/ask`

The `POST /api/ask` path also returns:

- the chosen route
- retrieved chunks
- citations
- structured tool outputs

That makes it easy to inspect which path the system took for a given question.

## Run

Prerequisites:

- Go `1.24.1+`
- `ghostscript` available as `gs`
- `tesseract`
- `OPENAI_API_KEY`

Start the app:

```bash
export OPENAI_API_KEY=your_key_here
go run ./cmd/estimator
```

Open:

```text
http://localhost:8080
```

On first run, the app bootstraps the files in `docs/` into `data/estimator.db`.

Useful environment variables:

```bash
HTTP_ADDR=:8080
DATABASE_PATH=data/estimator.db
OPENAI_MODEL=gpt-4.1-mini
EMBEDDING_MODEL=text-embedding-3-small
CHUNK_SIZE=1600
CHUNK_OVERLAP=200
UPLOAD_DIR=data/uploads
MAX_RETRIEVED_CHUNKS=6
```

## API Examples

Ask a question:

```bash
curl -X POST http://localhost:8080/api/ask \
  -H 'Content-Type: application/json' \
  -d '{"question":"What does the plan set say about drainage requirements?"}'
```

Run structured tools directly:

```bash
curl http://localhost:8080/api/tools/analyze-bid-data
curl http://localhost:8080/api/tools/price-outliers
curl http://localhost:8080/api/tools/project-summary
curl 'http://localhost:8080/api/tools/search?q=drainage&source=plans'
```

Upload a file:

```bash
curl -X POST http://localhost:8080/api/upload \
  -F file=@/absolute/path/to/file.pdf
```

## Ingestion Notes

### CSV

- Loaded into `bid_rows`
- Used for deterministic analysis and anomaly detection

### Specifications PDFs

- Primarily handled by native extraction
- OCR fallback is rarely needed

### Plan Set PDF

- Handled by OCR page-by-page
- This is slower, but it is the correct tradeoff for a scanned plan set

The logs show progress by page so the OCR behavior is visible during local runs.

## Retrieval

Chunk metadata includes:

- source
- page start
- page end

That metadata is used in answers and citations so responses stay grounded.

## Outlier Detection

Outlier detection groups rows by:

- `item_number`
- `item_desc`
- `unit`

Then it applies a conservative rule:

- minimum peer-group size
- ratio-to-average threshold
- z-score threshold
- both ratio and z-score must agree

This intentionally trades recall for explainability and fewer false positives.

## Known Limitations

- OCR text is imperfect, especially on engineering drawings.
- Some plan sheet wording may reflect OCR artifacts from the original drawing title blocks.
- For example, a response may mention a sheet label like `Sheet 34 of 39` because that text exists in the scanned drawing content, even though the PDF file itself has 63 pages.
- Embeddings are stored in SQLite JSON rather than a real vector index.
- Tool routing is heuristic rather than model-driven JSON tool calling.
- `project-summary` is intentionally document-only. It does not assume the CSV and PDFs describe the same project.

These are acceptable tradeoffs for a local take-home implementation focused on clarity and correctness.

## Tests

Run:

```bash
go test ./...
```

Current tests are intentionally focused on:

- CSV parsing
- chunking behavior
- routing behavior

They are smoke tests, not full end-to-end coverage.

## What I’d Change With More Time

- add hybrid retrieval with keyword plus vector ranking
- replace JSON vector storage with `sqlite-vec` or `pgvector`
- improve OCR normalization for drawings and tables
- move tool selection to explicit model tool-calling
- return richer source snippets and cleaner citations
- add stronger integration tests around ingestion and retrieval
