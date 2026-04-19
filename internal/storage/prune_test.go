package storage

import (
	"testing"
	"time"
)

// insertRequest inserts a minimal request row with the given timestamp (Unix ms).
func insertRequest(t *testing.T, db *DB, id string, tsMs int64) {
	t.Helper()
	_, err := db.db.Exec(
		`INSERT INTO requests (id, method, url, timestamp) VALUES (?,?,?,?)`,
		id, "GET", "http://example.com", tsMs,
	)
	if err != nil {
		t.Fatalf("insertRequest %s: %v", id, err)
	}
}

func countRequests(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&n); err != nil {
		t.Fatalf("countRequests: %v", err)
	}
	return n
}

func openPruneTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPruneOldTransactions_NoOp(t *testing.T) {
	db := openPruneTestDB(t)
	insertRequest(t, db, "r1", time.Now().UnixMilli())

	if err := db.PruneOldTransactions(0, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countRequests(t, db); n != 1 {
		t.Errorf("expected 1 row, got %d", n)
	}
}

func TestPruneOldTransactions_ByAge(t *testing.T) {
	db := openPruneTestDB(t)

	// Two rows older than 7 days.
	old := time.Now().AddDate(0, 0, -10).UnixMilli()
	insertRequest(t, db, "old1", old)
	insertRequest(t, db, "old2", old)
	// One recent row.
	insertRequest(t, db, "new1", time.Now().UnixMilli())

	if err := db.PruneOldTransactions(7, 0); err != nil {
		t.Fatalf("PruneOldTransactions: %v", err)
	}
	if n := countRequests(t, db); n != 1 {
		t.Errorf("expected 1 row after age prune, got %d", n)
	}
}

func TestPruneOldTransactions_ByCount(t *testing.T) {
	db := openPruneTestDB(t)

	base := time.Now().UnixMilli()
	for i := range 10 {
		insertRequest(t, db, "r"+string(rune('a'+i)), base+int64(i))
	}

	if err := db.PruneOldTransactions(0, 5); err != nil {
		t.Fatalf("PruneOldTransactions: %v", err)
	}
	if n := countRequests(t, db); n != 5 {
		t.Errorf("expected 5 rows after count prune, got %d", n)
	}
}

func TestPruneOldTransactions_BothConditions(t *testing.T) {
	db := openPruneTestDB(t)

	old := time.Now().AddDate(0, 0, -10).UnixMilli()
	insertRequest(t, db, "old1", old)
	insertRequest(t, db, "old2", old)

	now := time.Now().UnixMilli()
	for i := range 8 {
		insertRequest(t, db, "new"+string(rune('a'+i)), now+int64(i))
	}

	// Age prune removes 2 old rows → 8 remain; count prune to 5 removes 3 more.
	if err := db.PruneOldTransactions(7, 5); err != nil {
		t.Fatalf("PruneOldTransactions: %v", err)
	}
	if n := countRequests(t, db); n != 5 {
		t.Errorf("expected 5 rows, got %d", n)
	}
}

func TestPruneOldTransactions_CountAlreadyWithinLimit(t *testing.T) {
	db := openPruneTestDB(t)
	insertRequest(t, db, "r1", time.Now().UnixMilli())
	insertRequest(t, db, "r2", time.Now().UnixMilli())

	if err := db.PruneOldTransactions(0, 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n := countRequests(t, db); n != 2 {
		t.Errorf("expected 2 rows, got %d", n)
	}
}
