package storage

import (
	"context"
	"fmt"
	"time"
)

// PruneOldTransactions enforces the history retention policy.
//
//   - When maxAgeDays > 0, all transactions with a timestamp older than
//     maxAgeDays days are deleted (cascade removes associated responses,
//     WebSocket frames, etc. via foreign-key ON DELETE CASCADE).
//
//   - When maxRows > 0, the oldest transactions are deleted until the total
//     row count is at most maxRows.
//
// Both conditions may be applied in the same call.  When both values are 0
// the function is a no-op and returns nil immediately.
func (d *DB) PruneOldTransactions(maxAgeDays int, maxRows int) error {
	if maxAgeDays <= 0 && maxRows <= 0 {
		return nil
	}

	if maxAgeDays > 0 {
		cutoffMs := time.Now().AddDate(0, 0, -maxAgeDays).UnixMilli()
		if _, err := d.db.Exec(`DELETE FROM requests WHERE timestamp < ?`, cutoffMs); err != nil {
			return fmt.Errorf("prune by age: %w", err)
		}
	}

	if maxRows > 0 {
		var count int
		if err := d.db.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&count); err != nil {
			return fmt.Errorf("count requests: %w", err)
		}
		if excess := count - maxRows; excess > 0 {
			_, err := d.db.Exec(
				`DELETE FROM requests WHERE id IN (
					SELECT id FROM requests ORDER BY timestamp ASC LIMIT ?
				)`,
				excess,
			)
			if err != nil {
				return fmt.Errorf("prune by count: %w", err)
			}
		}
	}

	return nil
}

// StartPeriodicPrune runs PruneOldTransactions in a background goroutine on
// the given interval. It also prunes once immediately on startup. The goroutine
// stops when ctx is cancelled.
func (d *DB) StartPeriodicPrune(ctx context.Context, interval time.Duration, maxAgeDays, maxRows int) {
	if maxAgeDays <= 0 && maxRows <= 0 {
		return // nothing to do
	}
	go func() {
		// Prune once immediately on startup.
		_ = d.PruneOldTransactions(maxAgeDays, maxRows)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = d.PruneOldTransactions(maxAgeDays, maxRows)
			}
		}
	}()
}
