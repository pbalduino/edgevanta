# Bruno Collection

Open `docs/bruno` as a Bruno collection.

Included requests:

- `00 Health`
- `01 List Documents`
- `02 Ask Drainage Question`
- `03 Analyze Bid Data`
- `04 Price Outliers`
- `05 Project Summary`
- `06 Search Plans Drainage`
- `07 Ingest Autoload CSV`
- `08 Upload CSV`

Notes:

- All requests assume the API is running on `http://localhost:8080`.
- `07 Ingest Autoload CSV` expects `autoload/sample_bid_tabulation.csv` to exist locally.
- `08 Upload CSV` uploads the fixture at `docs/sample_bid_tabulation.csv`.
