package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/brendalima/despensa/internal/core/domain"
)

var _ DigestRepository = (*Digest)(nil)

type Digest struct {
	db *sql.DB
}

func NewDigest(db *sql.DB) (*Digest, error) {
	if db == nil {
		return nil, errors.New("missing database dependency")
	}
	return &Digest{db: db}, nil
}

func MustNewDigest(db *sql.DB) *Digest {
	d, err := NewDigest(db)
	if err != nil {
		panic(fmt.Errorf("couldn't create digest repository: %w", err))
	}
	return d
}

// Owe writes the debt and reports whether this call created it. The
// partial unique index does the deciding, so the check and the write
// are one statement: two schedulers firing at once cannot both win, the
// same way they cannot both claim a delivery.
func (d *Digest) Owe(ctx context.Context, chatID string, dueAt time.Time) (bool, error) {
	res, err := d.db.ExecContext(ctx,
		`INSERT INTO pending_digests (chat_id, due_at) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		chatID, dueAt)
	if err != nil {
		return false, fmt.Errorf("couldn't owe a digest: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("couldn't owe a digest: %w", err)
	}
	return n == 1, nil
}

// Owed reads the standing debt. Nothing owed is the normal case, so it
// is a false and not a *domain.NotFoundError: the caller is asking, not
// looking something up.
func (d *Digest) Owed(ctx context.Context, chatID string) (domain.Digest, bool, error) {
	var (
		digest    domain.Digest
		delivered sql.NullTime
	)
	err := d.db.QueryRowContext(ctx,
		`SELECT id, chat_id, due_at, delivered_at
		   FROM pending_digests
		  WHERE chat_id = $1 AND delivered_at IS NULL
		  ORDER BY due_at ASC
		  LIMIT 1`,
		chatID,
	).Scan(&digest.ID, &digest.ChatID, &digest.DueAt, &delivered)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Digest{}, false, nil
	}
	if err != nil {
		return domain.Digest{}, false, fmt.Errorf("couldn't read the owed digest: %w", err)
	}
	digest.DeliveredAt = delivered.Time
	return digest, true, nil
}

// Paid settles the debt. An id that is already paid, or never existed,
// is not an error: the caller's intent was that nothing be owed, and it
// is not.
func (d *Digest) Paid(ctx context.Context, id int64) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE pending_digests SET delivered_at = now()
		  WHERE id = $1 AND delivered_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("couldn't settle the digest: %w", err)
	}
	return nil
}
