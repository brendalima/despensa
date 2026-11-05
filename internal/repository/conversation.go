package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/brendalima/despensa/internal/core/domain"
)

var _ ConversationRepository = (*Conversation)(nil)

type Conversation struct {
	db *sql.DB
}

func NewConversation(db *sql.DB) (*Conversation, error) {
	if db == nil {
		return nil, errors.New("missing database dependency")
	}
	return &Conversation{db: db}, nil
}

func MustNewConversation(db *sql.DB) *Conversation {
	c, err := NewConversation(db)
	if err != nil {
		panic(fmt.Errorf("couldn't create conversation repository: %w", err))
	}
	return c
}

// Recent returns the turns of one chat since a moment, oldest first,
// which is the order a model reads them in. The limit is on the newest
// turns, so a long argument with the bot does not push a request over
// the context window: the query takes the last ones and the result is
// turned back around.
func (c *Conversation) Recent(ctx context.Context, chatID string, since time.Time, limit int) ([]domain.Turn, error) {
	rows, err := c.db.QueryContext(ctx,
		`SELECT id, chat_id, role, content, created_at
		   FROM conversation_turns
		  WHERE chat_id = $1 AND created_at >= $2
		  ORDER BY created_at DESC, id DESC
		  LIMIT $3`,
		chatID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("couldn't read conversation turns: %w", err)
	}
	defer rows.Close()

	turns := []domain.Turn{}
	for rows.Next() {
		var t domain.Turn
		if err := rows.Scan(&t.ID, &t.ChatID, &t.Role, &t.Content, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("couldn't read conversation turns: %w", err)
		}
		turns = append(turns, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("couldn't read conversation turns: %w", err)
	}

	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}
	return turns, nil
}

func (c *Conversation) Append(ctx context.Context, t domain.Turn) (domain.Turn, error) {
	err := c.db.QueryRowContext(ctx,
		`INSERT INTO conversation_turns (chat_id, role, content)
		 VALUES ($1, $2, $3)
		 RETURNING id, chat_id, role, content, created_at`,
		t.ChatID, string(t.Role), t.Content,
	).Scan(&t.ID, &t.ChatID, &t.Role, &t.Content, &t.CreatedAt)
	if err != nil {
		return domain.Turn{}, fmt.Errorf("couldn't append conversation turn: %w", err)
	}
	return t, nil
}

// Forget drops the turns that fell out of the window. The history is
// there to finish a sentence; keeping months of it would be a log of
// everything said in the kitchen, for no gain.
func (c *Conversation) Forget(ctx context.Context, before time.Time) (int64, error) {
	res, err := c.db.ExecContext(ctx,
		`DELETE FROM conversation_turns WHERE created_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("couldn't forget conversation turns: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("couldn't forget conversation turns: %w", err)
	}
	return n, nil
}
