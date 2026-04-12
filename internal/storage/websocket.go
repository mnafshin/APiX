package storage

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// WebSocketFrameRecord is a stored WebSocket frame captured for a transaction.
type WebSocketFrameRecord struct {
	ID            string
	TransactionID string
	Direction     string
	Opcode        int
	Payload       []byte
	Timestamp     time.Time
}

// SaveWebSocketFrame inserts a WebSocket frame for a captured upgrade request.
func (d *DB) SaveWebSocketFrame(frame *WebSocketFrameRecord) error {
	if frame == nil {
		return fmt.Errorf("websocket frame is nil")
	}
	if frame.ID == "" {
		frame.ID = uuid.NewString()
	}
	_, err := d.db.Exec(
		`INSERT INTO ws_frames (id, transaction_id, direction, opcode, payload, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		frame.ID,
		frame.TransactionID,
		frame.Direction,
		frame.Opcode,
		frame.Payload,
		frame.Timestamp.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("save websocket frame: %w", err)
	}
	return nil
}

// ListWebSocketFrames returns all stored frames for a transaction in timestamp order.
func (d *DB) ListWebSocketFrames(transactionID string) ([]*WebSocketFrameRecord, error) {
	rows, err := d.db.Query(
		`SELECT id, transaction_id, direction, opcode, payload, timestamp
		 FROM ws_frames
		 WHERE transaction_id = ?
		 ORDER BY timestamp ASC, id ASC`,
		transactionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list websocket frames: %w", err)
	}
	defer func() { _ = rows.Close() }()

	frames := []*WebSocketFrameRecord{}
	for rows.Next() {
		frame := &WebSocketFrameRecord{}
		var timestampMS int64
		if err := rows.Scan(
			&frame.ID,
			&frame.TransactionID,
			&frame.Direction,
			&frame.Opcode,
			&frame.Payload,
			&timestampMS,
		); err != nil {
			return nil, fmt.Errorf("scan websocket frame: %w", err)
		}
		frame.Timestamp = time.UnixMilli(timestampMS)
		frames = append(frames, frame)
	}
	return frames, rows.Err()
}
