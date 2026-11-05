// Package repository is the persistence adapter: SQL over database/sql,
// translating infrastructure errors into domain errors.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/brendalima/despensa/internal/core/domain"
)

var (
	_ PantryRepository = (*Pantry)(nil)
	_ PantryTx         = (*pantryTx)(nil)
)

const (
	pgUniqueViolation = "23505"
	itemsNameKey      = "items_name_key"

	itemColumns = `id, name, canonical_unit, on_hand_milli, threshold_milli, category, archived_at, created_at`
)

type Pantry struct {
	db *sql.DB
}

func NewPantry(db *sql.DB) (*Pantry, error) {
	if db == nil {
		return nil, errors.New("missing database dependency")
	}
	return &Pantry{db: db}, nil
}

func MustNewPantry(db *sql.DB) *Pantry {
	p, err := NewPantry(db)
	if err != nil {
		panic(fmt.Errorf("couldn't create pantry repository: %w", err))
	}
	return p
}

// InTx runs fn against a transaction-bound repository and commits only if
// it returns nil.
func (r *Pantry) InTx(ctx context.Context, fn func(PantryTx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("couldn't begin transaction: %w", err)
	}
	// A no-op after a successful Commit, so it covers every path out.
	defer func() { _ = tx.Rollback() }()

	if err := fn(&pantryTx{tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("couldn't commit transaction: %w", err)
	}
	return nil
}

func (r *Pantry) List(ctx context.Context) ([]domain.Item, error) {
	return readItems(ctx, r.db, `WHERE archived_at IS NULL ORDER BY lower(name)`)
}

func (r *Pantry) ListArchived(ctx context.Context) ([]domain.Item, error) {
	return readItems(ctx, r.db, `WHERE archived_at IS NOT NULL ORDER BY archived_at DESC`)
}

func (r *Pantry) GetByID(ctx context.Context, id string) (domain.Item, error) {
	if uuid.Validate(id) != nil {
		return domain.Item{}, domain.NewNotFoundError("item not found")
	}
	items, err := readItems(ctx, r.db, `WHERE id = $1`, id)
	if err != nil {
		return domain.Item{}, err
	}
	if len(items) == 0 {
		return domain.Item{}, domain.NewNotFoundError("item not found")
	}
	return items[0], nil
}

func (r *Pantry) Events(ctx context.Context, itemID string, limit int) ([]domain.Event, error) {
	if uuid.Validate(itemID) != nil {
		return nil, domain.NewNotFoundError("item not found")
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, item_id, kind, delta_milli, portion_name, portion_qty_milli, raw_message, source, undone_at, created_at
		 FROM events WHERE item_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, itemID, limit)
	if err != nil {
		return nil, fmt.Errorf("couldn't read events: %w", err)
	}
	defer rows.Close()

	events := []domain.Event{}
	for rows.Next() {
		var e domain.Event
		if err := rows.Scan(&e.ID, &e.ItemID, &e.Kind, &e.Delta, &e.PortionName, &e.PortionQty,
			&e.RawMessage, &e.Source, (*nullTime)(&e.UndoneAt), &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("couldn't read events: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("couldn't read events: %w", err)
	}
	return events, nil
}

func (r *Pantry) Notes(ctx context.Context) ([]domain.Note, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, text, created_at FROM shopping_notes ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("couldn't read shopping notes: %w", err)
	}
	defer rows.Close()

	notes := []domain.Note{}
	for rows.Next() {
		var n domain.Note
		if err := rows.Scan(&n.ID, &n.Text, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("couldn't read shopping notes: %w", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("couldn't read shopping notes: %w", err)
	}
	return notes, nil
}

func (r *Pantry) AddNote(ctx context.Context, text string) (domain.Note, error) {
	var n domain.Note
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO shopping_notes (text) VALUES ($1) RETURNING id, text, created_at`, text,
	).Scan(&n.ID, &n.Text, &n.CreatedAt)
	if err != nil {
		return domain.Note{}, fmt.Errorf("couldn't add shopping note: %w", err)
	}
	return n, nil
}

func (r *Pantry) RemoveNote(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM shopping_notes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("couldn't remove shopping note: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return domain.NewNotFoundError("shopping note not found")
	}
	return nil
}

// pantryTx is the repository bound to one transaction.
type pantryTx struct {
	tx *sql.Tx
}

func (t *pantryTx) LockByID(ctx context.Context, id string) (domain.Item, error) {
	if uuid.Validate(id) != nil {
		return domain.Item{}, domain.NewNotFoundError("item not found")
	}
	items, err := readItems(ctx, t.tx, `WHERE id = $1 FOR UPDATE`, id)
	if err != nil {
		return domain.Item{}, err
	}
	if len(items) == 0 {
		return domain.Item{}, domain.NewNotFoundError("item not found")
	}
	return items[0], nil
}

// LockByName matches the name and the aliases, ignoring case. Two
// matches is a refusal, not a coin toss.
func (t *pantryTx) LockByName(ctx context.Context, name string) (domain.Item, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return domain.Item{}, domain.NewValidationError("name", "is empty")
	}

	// A subquery rather than a join, so an item with two matching
	// aliases is still one row.
	items, err := readItems(ctx, t.tx,
		`WHERE archived_at IS NULL
		   AND (lower(name) = lower($1)
		        OR EXISTS (SELECT 1 FROM item_aliases a WHERE a.item_id = items.id AND lower(a.alias) = lower($1)))
		 ORDER BY lower(name)
		 FOR UPDATE`, trimmed)
	if err != nil {
		return domain.Item{}, err
	}

	switch len(items) {
	case 0:
		return domain.Item{}, domain.NewNotFoundError("item not found: " + trimmed)
	case 1:
		return items[0], nil
	default:
		names := make([]string, len(items))
		for i, item := range items {
			names[i] = item.Name
		}
		return domain.Item{}, domain.NewAmbiguousItemError(trimmed, names)
	}
}

func (t *pantryTx) CreateItem(ctx context.Context, item domain.Item) error {
	_, err := t.tx.ExecContext(ctx,
		`INSERT INTO items (id, name, canonical_unit, on_hand_milli, threshold_milli, category)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		item.ID, item.Name, string(item.Unit), int64(item.OnHand), int64(item.Threshold), item.Category)
	if err != nil {
		return translateWriteError(err, item.Name, "couldn't create item")
	}

	for _, p := range item.Portions {
		if err := t.UpsertPortion(ctx, item.ID, p); err != nil {
			return err
		}
	}
	return t.ReplaceAliases(ctx, item.ID, item.Aliases)
}

func (t *pantryTx) UpdateItem(ctx context.Context, item domain.Item) error {
	res, err := t.tx.ExecContext(ctx,
		`UPDATE items SET name = $2, canonical_unit = $3, threshold_milli = $4, category = $5,
		        archived_at = NULLIF($6, '1970-01-01 00:00:00+00'::timestamptz)
		 WHERE id = $1`,
		item.ID, item.Name, string(item.Unit), int64(item.Threshold), item.Category, archivedParam(item.ArchivedAt))
	if err != nil {
		return translateWriteError(err, item.Name, "couldn't update item")
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return domain.NewNotFoundError("item not found")
	}
	return nil
}

func (t *pantryTx) SetOnHand(ctx context.Context, itemID string, onHand domain.Amount) error {
	res, err := t.tx.ExecContext(ctx,
		`UPDATE items SET on_hand_milli = $2 WHERE id = $1`, itemID, int64(onHand))
	if err != nil {
		return fmt.Errorf("couldn't set on hand: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return domain.NewNotFoundError("item not found")
	}
	return nil
}

// UpsertPortion keeps a size the person gave over the model's estimate.
func (t *pantryTx) UpsertPortion(ctx context.Context, itemID string, p domain.Portion) error {
	source := p.Source
	if source == "" {
		source = domain.PortionSeeded
	}
	_, err := t.tx.ExecContext(ctx,
		`INSERT INTO portions (item_id, name, one_milli, source) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (item_id, name) DO UPDATE
		   SET one_milli = EXCLUDED.one_milli, source = EXCLUDED.source
		   WHERE portions.source <> 'corrected' OR EXCLUDED.source = 'corrected'`,
		itemID, p.Name, int64(p.One), source)
	if err != nil {
		return fmt.Errorf("couldn't save portion: %w", err)
	}
	return nil
}

func (t *pantryTx) ReplaceAliases(ctx context.Context, itemID string, aliases []string) error {
	if _, err := t.tx.ExecContext(ctx, `DELETE FROM item_aliases WHERE item_id = $1`, itemID); err != nil {
		return fmt.Errorf("couldn't replace aliases: %w", err)
	}
	for _, alias := range aliases {
		if strings.TrimSpace(alias) == "" {
			continue
		}
		if _, err := t.tx.ExecContext(ctx,
			`INSERT INTO item_aliases (item_id, alias) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			itemID, alias); err != nil {
			return fmt.Errorf("couldn't replace aliases: %w", err)
		}
	}
	return nil
}

func (t *pantryTx) AddEvent(ctx context.Context, e domain.Event) (int64, error) {
	var id int64
	err := t.tx.QueryRowContext(ctx,
		`INSERT INTO events (item_id, kind, delta_milli, portion_name, portion_qty_milli, raw_message, source)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		e.ItemID, string(e.Kind), int64(e.Delta), e.PortionName, int64(e.PortionQty), e.RawMessage, e.Source,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("couldn't record event: %w", err)
	}
	return id, nil
}

func (t *pantryTx) LockEvent(ctx context.Context, id int64) (domain.Event, error) {
	var e domain.Event
	err := t.tx.QueryRowContext(ctx,
		`SELECT id, item_id, kind, delta_milli, portion_name, portion_qty_milli, raw_message, source, undone_at, created_at
		 FROM events WHERE id = $1 FOR UPDATE`, id,
	).Scan(&e.ID, &e.ItemID, &e.Kind, &e.Delta, &e.PortionName, &e.PortionQty,
		&e.RawMessage, &e.Source, (*nullTime)(&e.UndoneAt), &e.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Event{}, domain.NewNotFoundError("event not found")
	}
	if err != nil {
		return domain.Event{}, fmt.Errorf("couldn't read event: %w", err)
	}
	return e, nil
}

func (t *pantryTx) MarkEventUndone(ctx context.Context, id int64) error {
	res, err := t.tx.ExecContext(ctx, `UPDATE events SET undone_at = now() WHERE id = $1 AND undone_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("couldn't undo event: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return domain.NewNotFoundError("event not found")
	}
	return nil
}

// readItems reads items, then their portions and aliases: three queries
// however many items there are.
func readItems(ctx context.Context, q querier, tail string, args ...any) ([]domain.Item, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+itemColumns+` FROM items `+tail, args...)
	if err != nil {
		return nil, fmt.Errorf("couldn't read items: %w", err)
	}
	defer rows.Close()

	items := []domain.Item{}
	for rows.Next() {
		var item domain.Item
		if err := rows.Scan(&item.ID, &item.Name, &item.Unit, &item.OnHand, &item.Threshold,
			&item.Category, (*nullTime)(&item.ArchivedAt), &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("couldn't read items: %w", err)
		}
		item.Portions = []domain.Portion{}
		item.Aliases = []string{}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("couldn't read items: %w", err)
	}
	if len(items) == 0 {
		return items, nil
	}

	ids := make([]string, len(items))
	byID := make(map[string]*domain.Item, len(items))
	for i := range items {
		ids[i] = items[i].ID
		byID[items[i].ID] = &items[i]
	}

	if err := attachPortions(ctx, q, ids, byID); err != nil {
		return nil, err
	}
	if err := attachAliases(ctx, q, ids, byID); err != nil {
		return nil, err
	}
	return items, nil
}

func attachPortions(ctx context.Context, q querier, ids []string, byID map[string]*domain.Item) error {
	rows, err := q.QueryContext(ctx,
		`SELECT item_id, name, one_milli, source FROM portions WHERE item_id = ANY($1) ORDER BY one_milli`, ids)
	if err != nil {
		return fmt.Errorf("couldn't read portions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var itemID string
		var p domain.Portion
		if err := rows.Scan(&itemID, &p.Name, &p.One, &p.Source); err != nil {
			return fmt.Errorf("couldn't read portions: %w", err)
		}
		if item, ok := byID[itemID]; ok {
			item.Portions = append(item.Portions, p)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("couldn't read portions: %w", err)
	}
	return nil
}

func attachAliases(ctx context.Context, q querier, ids []string, byID map[string]*domain.Item) error {
	rows, err := q.QueryContext(ctx,
		`SELECT item_id, alias FROM item_aliases WHERE item_id = ANY($1) ORDER BY alias`, ids)
	if err != nil {
		return fmt.Errorf("couldn't read aliases: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var itemID, alias string
		if err := rows.Scan(&itemID, &alias); err != nil {
			return fmt.Errorf("couldn't read aliases: %w", err)
		}
		if item, ok := byID[itemID]; ok {
			item.Aliases = append(item.Aliases, alias)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("couldn't read aliases: %w", err)
	}
	return nil
}

// querier is what *sql.DB and *sql.Tx have in common, for code that runs
// in a transaction or outside one.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// nullTime scans a nullable timestamp into a time.Time, NULL becoming the
// zero time, which is how the domain says "not set".
type nullTime time.Time

func (t *nullTime) Scan(src any) error {
	var n sql.NullTime
	if err := n.Scan(src); err != nil {
		return err
	}
	*t = nullTime(n.Time)
	return nil
}

// archivedParam maps the zero time to a sentinel the UPDATE turns back
// into NULL, so "not archived" survives the round trip.
func archivedParam(t time.Time) time.Time {
	if t.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return t
}

func translateWriteError(err error, name, action string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == itemsNameKey {
		return domain.NewConflictError(name)
	}
	return fmt.Errorf("%s: %w", action, err)
}
