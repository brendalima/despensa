package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var _ MessageRepository = (*Message)(nil)

type Message struct {
	db *sql.DB
}

func NewMessage(db *sql.DB) (*Message, error) {
	if db == nil {
		return nil, errors.New("missing database dependency")
	}
	return &Message{db: db}, nil
}

func MustNewMessage(db *sql.DB) *Message {
	m, err := NewMessage(db)
	if err != nil {
		panic(fmt.Errorf("couldn't create message repository: %w", err))
	}
	return m
}

// Claim inserts the message id and reports whether this caller got it.
// ON CONFLICT DO NOTHING makes the check and the write one statement, so
// two deliveries arriving at the same moment cannot both win.
func (m *Message) Claim(ctx context.Context, provider, messageID string) (bool, error) {
	res, err := m.db.ExecContext(ctx,
		`INSERT INTO processed_messages (provider, message_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		provider, messageID)
	if err != nil {
		return false, fmt.Errorf("couldn't claim message: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("couldn't claim message: %w", err)
	}
	return n == 1, nil
}
