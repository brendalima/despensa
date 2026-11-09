package repository

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/brendalima/despensa/adapters"
	"github.com/brendalima/despensa/internal/core/domain"
)

// Integration tests: they talk to a real Postgres and skip without one.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	// Missing files are fine: the test skips below without a URL.
	_ = godotenv.Load("../../.env.local")
	_ = godotenv.Load("../../.env")

	// Only the dedicated test branch: these tests write and delete rows,
	// so they must never fall back to DATABASE_URL, which is dev's or
	// production's pantry.
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("no TEST_DATABASE_URL — skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := adapters.NewDatabase(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := adapters.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	return db
}

// newTestItem inserts an item with a name unique to this run, and removes
// it when the test ends.
func newTestItem(t *testing.T, repo *Pantry) domain.Item {
	t.Helper()
	item := domain.Item{
		ID:        uuid.New().String(),
		Name:      "teste-" + uuid.New().String(),
		Unit:      domain.UnitGram,
		OnHand:    5000 * domain.Thousand,
		Threshold: 1000 * domain.Thousand,
		Category:  "testes",
		Portions: []domain.Portion{
			{Name: "taça", One: 180 * domain.Thousand, Source: domain.PortionSeeded},
			{Name: "pacote", One: 5000 * domain.Thousand, Source: domain.PortionCorrected},
		},
		Aliases: []string{"apelido-" + uuid.New().String()},
	}

	t.Cleanup(func() {
		// Cascades take the portions, aliases and events with it.
		_, _ = repo.db.ExecContext(context.Background(), `DELETE FROM items WHERE id = $1`, item.ID)
	})

	err := repo.InTx(context.Background(), func(tx PantryTx) error {
		return tx.CreateItem(context.Background(), item)
	})
	if err != nil {
		t.Fatalf("creating the test item: %v", err)
	}
	return item
}

func TestCreateAndReadItem(t *testing.T) {
	repo := MustNewPantry(testDB(t))
	created := newTestItem(t, repo)

	got, err := repo.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != created.Name || got.Unit != created.Unit {
		t.Errorf("item = %+v, want %+v", got, created)
	}
	if got.OnHand != created.OnHand || got.Threshold != created.Threshold {
		t.Errorf("amounts = %d, %d, want %d, %d", got.OnHand, got.Threshold, created.OnHand, created.Threshold)
	}
	if len(got.Portions) != 2 {
		t.Fatalf("portions = %v, want two", got.Portions)
	}
	// Portions come back smallest first, which is the order a caller
	// picking a readable one wants.
	if got.Portions[0].Name != "taça" {
		t.Errorf("portions = %v, want the cup first", got.Portions)
	}
	if len(got.Aliases) != 1 {
		t.Errorf("aliases = %v, want one", got.Aliases)
	}
}

func TestGetByIDNotFound(t *testing.T) {
	repo := MustNewPantry(testDB(t))

	var notFound *domain.NotFoundError
	if _, err := repo.GetByID(context.Background(), uuid.New().String()); !errors.As(err, &notFound) {
		t.Errorf("error = %v, want *NotFoundError", err)
	}
	// A string that is not a UUID must not reach the database as one.
	if _, err := repo.GetByID(context.Background(), "arroz"); !errors.As(err, &notFound) {
		t.Errorf("error = %v, want *NotFoundError", err)
	}
}

// The invariant the whole design rests on: on_hand is the sum of the
// deltas of the events that have not been undone. If this drifts, the
// pantry lies in silence.
func TestOnHandMatchesTheLedger(t *testing.T) {
	repo := MustNewPantry(testDB(t))
	item := newTestItem(t, repo)
	ctx := context.Background()

	// Start from zero so the sum covers everything, then apply a random
	// sequence of writes and undos through the same calls the service
	// makes.
	mustInTx(t, repo, func(tx PantryTx) error { return tx.SetOnHand(ctx, item.ID, 0) })

	random := rand.New(rand.NewPCG(1, 2))
	var expected domain.Amount
	var ids []int64

	for i := 0; i < 40; i++ {
		delta := domain.Amount(random.IntN(2000)-1000) * domain.Thousand
		kind := domain.EventConsume
		if delta > 0 {
			kind = domain.EventRestock
		}

		mustInTx(t, repo, func(tx PantryTx) error {
			id, err := tx.AddEvent(ctx, domain.Event{
				ItemID: item.ID, Kind: kind, Delta: delta,
				PortionName: "taça", PortionQty: domain.QtyOf(1), Source: domain.SourceCLI,
			})
			if err != nil {
				return err
			}
			ids = append(ids, id)
			return tx.SetOnHand(ctx, item.ID, expected+delta)
		})
		expected += delta

		// Undo roughly a third of them, including the occasional repeat.
		if random.IntN(3) == 0 && len(ids) > 0 {
			victim := ids[random.IntN(len(ids))]
			mustInTx(t, repo, func(tx PantryTx) error {
				event, err := tx.LockEvent(ctx, victim)
				if err != nil {
					return err
				}
				if event.Undone() {
					return nil
				}
				if err := tx.MarkEventUndone(ctx, victim); err != nil {
					return err
				}
				expected -= event.Delta
				return tx.SetOnHand(ctx, item.ID, expected)
			})
		}
	}

	got, err := repo.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.OnHand != expected {
		t.Errorf("on hand = %d, want %d", got.OnHand, expected)
	}

	var ledger sql.NullInt64
	err = repo.db.QueryRowContext(ctx,
		`SELECT SUM(delta_milli) FROM events WHERE item_id = $1 AND undone_at IS NULL`, item.ID,
	).Scan(&ledger)
	if err != nil {
		t.Fatalf("summing the ledger: %v", err)
	}
	if domain.Amount(ledger.Int64) != got.OnHand {
		t.Errorf("ledger sum = %d, on hand = %d: the inventory and its history disagree",
			ledger.Int64, got.OnHand)
	}
}

// Undo is marked once. A second attempt has to find nothing to do rather
// than take the amount off twice.
func TestMarkEventUndoneTwice(t *testing.T) {
	repo := MustNewPantry(testDB(t))
	item := newTestItem(t, repo)
	ctx := context.Background()

	var eventID int64
	mustInTx(t, repo, func(tx PantryTx) error {
		id, err := tx.AddEvent(ctx, domain.Event{
			ItemID: item.ID, Kind: domain.EventConsume, Delta: -180 * domain.Thousand,
			PortionName: "taça", PortionQty: domain.QtyOf(1), Source: domain.SourceCLI,
		})
		eventID = id
		return err
	})

	mustInTx(t, repo, func(tx PantryTx) error { return tx.MarkEventUndone(ctx, eventID) })

	err := repo.InTx(ctx, func(tx PantryTx) error { return tx.MarkEventUndone(ctx, eventID) })
	var notFound *domain.NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("second undo = %v, want *NotFoundError so the caller knows it was already undone", err)
	}
}

// A portion the person corrected is not replaced by a later estimate.
// This is the rule that makes "na verdade a taça é 170 g" stick, and it
// is enforced in the SQL, so it needs a test against real Postgres.
func TestUpsertPortionKeepsCorrectedOverSeeded(t *testing.T) {
	repo := MustNewPantry(testDB(t))
	item := newTestItem(t, repo)
	ctx := context.Background()

	mustInTx(t, repo, func(tx PantryTx) error {
		return tx.UpsertPortion(ctx, item.ID, domain.Portion{
			Name: "pacote", One: 999 * domain.Thousand, Source: domain.PortionSeeded,
		})
	})

	got, err := repo.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	p, ok := got.Portion("pacote")
	if !ok {
		t.Fatal("the package portion is gone")
	}
	if p.One != 5000*domain.Thousand || p.Source != domain.PortionCorrected {
		t.Errorf("portion = %+v, want the corrected 5000 g to have won", p)
	}

	// A correction, though, does replace it.
	mustInTx(t, repo, func(tx PantryTx) error {
		return tx.UpsertPortion(ctx, item.ID, domain.Portion{
			Name: "pacote", One: 1000 * domain.Thousand, Source: domain.PortionCorrected,
		})
	})
	got, err = repo.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if p, _ := got.Portion("pacote"); p.One != 1000*domain.Thousand {
		t.Errorf("portion = %+v, want the new corrected value", p)
	}
}

func TestLockByNameFindsAliasesAndRefusesAmbiguity(t *testing.T) {
	repo := MustNewPantry(testDB(t))
	item := newTestItem(t, repo)
	ctx := context.Background()

	// By name, ignoring case.
	mustInTx(t, repo, func(tx PantryTx) error {
		found, err := tx.LockByName(ctx, strings.ToUpper(item.Name))
		if err != nil {
			return err
		}
		if found.ID != item.ID {
			t.Errorf("found %q, want %q", found.Name, item.Name)
		}
		return nil
	})

	// By alias.
	mustInTx(t, repo, func(tx PantryTx) error {
		found, err := tx.LockByName(ctx, item.Aliases[0])
		if err != nil {
			return err
		}
		if found.ID != item.ID {
			t.Errorf("found %q by alias, want %q", found.Name, item.Name)
		}
		return nil
	})

	// Two items answering to the same word is a question, not a guess.
	twin := newTestItem(t, repo)
	mustInTx(t, repo, func(tx PantryTx) error {
		return tx.ReplaceAliases(ctx, twin.ID, []string{item.Name})
	})

	err := repo.InTx(ctx, func(tx PantryTx) error {
		_, err := tx.LockByName(ctx, item.Name)
		return err
	})
	var ambiguous *domain.AmbiguousItemError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("error = %v, want *AmbiguousItemError", err)
	}
	if len(ambiguous.Candidates) != 2 {
		t.Errorf("candidates = %v, want both items", ambiguous.Candidates)
	}
}

// Two items cannot share a name while both are live, and the clash comes
// back as a domain error rather than a raw Postgres one.
func TestDuplicateNameIsAConflict(t *testing.T) {
	repo := MustNewPantry(testDB(t))
	item := newTestItem(t, repo)

	err := repo.InTx(context.Background(), func(tx PantryTx) error {
		return tx.CreateItem(context.Background(), domain.Item{
			ID: uuid.New().String(), Name: strings.ToUpper(item.Name), Unit: domain.UnitGram,
		})
	})
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) {
		t.Errorf("error = %v, want *ConflictError", err)
	}
}

// Archiving frees the name, because an archived item is gone from the
// pantry rather than hidden in it.
func TestArchivingFreesTheName(t *testing.T) {
	repo := MustNewPantry(testDB(t))
	item := newTestItem(t, repo)
	ctx := context.Background()

	item.ArchivedAt = time.Now().UTC()
	mustInTx(t, repo, func(tx PantryTx) error { return tx.UpdateItem(ctx, item) })

	reused := domain.Item{ID: uuid.New().String(), Name: item.Name, Unit: domain.UnitGram}
	t.Cleanup(func() {
		_, _ = repo.db.ExecContext(context.Background(), `DELETE FROM items WHERE id = $1`, reused.ID)
	})
	mustInTx(t, repo, func(tx PantryTx) error { return tx.CreateItem(ctx, reused) })

	live, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, got := range live {
		if got.ID == item.ID {
			t.Error("List returned the archived item")
		}
	}
}

// A failing callback leaves nothing behind: this is what makes a batch
// of three things in one sentence all-or-nothing.
func TestInTxRollsBack(t *testing.T) {
	repo := MustNewPantry(testDB(t))
	ctx := context.Background()

	id := uuid.New().String()
	t.Cleanup(func() {
		_, _ = repo.db.ExecContext(context.Background(), `DELETE FROM items WHERE id = $1`, id)
	})

	wanted := errors.New("no")
	err := repo.InTx(ctx, func(tx PantryTx) error {
		if err := tx.CreateItem(ctx, domain.Item{
			ID: id, Name: "teste-rollback-" + id, Unit: domain.UnitGram,
		}); err != nil {
			return err
		}
		return wanted
	})
	if !errors.Is(err, wanted) {
		t.Fatalf("error = %v, want the callback's own error", err)
	}

	var notFound *domain.NotFoundError
	if _, err := repo.GetByID(ctx, id); !errors.As(err, &notFound) {
		t.Errorf("the item survived a rolled back transaction: %v", err)
	}
}

func TestClaimMessageOnlyOnce(t *testing.T) {
	repo := MustNewMessage(testDB(t))
	ctx := context.Background()

	id := "wamid." + uuid.New().String()
	t.Cleanup(func() {
		_, _ = repo.db.ExecContext(context.Background(), `DELETE FROM processed_messages WHERE message_id = $1`, id)
	})

	first, err := repo.Claim(ctx, domain.SourceWhatsApp, id)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if !first {
		t.Error("first Claim = false, want true")
	}

	// Meta redelivers anything it does not get a quick 200 for, and a
	// redelivered "usei duas fatias" must not take four slices.
	second, err := repo.Claim(ctx, domain.SourceWhatsApp, id)
	if err != nil {
		t.Fatalf("second Claim: %v", err)
	}
	if second {
		t.Error("second Claim = true, want false")
	}
}

func TestNotes(t *testing.T) {
	repo := MustNewPantry(testDB(t))
	ctx := context.Background()

	note, err := repo.AddNote(ctx, "teste-"+uuid.New().String())
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	t.Cleanup(func() {
		_, _ = repo.db.ExecContext(context.Background(), `DELETE FROM shopping_notes WHERE id = $1`, note.ID)
	})

	notes, err := repo.Notes(ctx)
	if err != nil {
		t.Fatalf("Notes: %v", err)
	}
	found := false
	for _, got := range notes {
		if got.ID == note.ID {
			found = true
		}
	}
	if !found {
		t.Error("the note is not in the list")
	}

	// The row comes back, because being bought is a note's end and the
	// answer the person gets names what went.
	removed, err := repo.RemoveNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("RemoveNote: %v", err)
	}
	if removed.ID != note.ID || removed.Text != note.Text {
		t.Errorf("RemoveNote returned %+v, want %+v", removed, note)
	}
	var notFound *domain.NotFoundError
	if _, err := repo.RemoveNote(ctx, note.ID); !errors.As(err, &notFound) {
		t.Errorf("removing it twice = %v, want *NotFoundError", err)
	}
}

func mustInTx(t *testing.T, repo *Pantry, fn func(PantryTx) error) {
	t.Helper()
	if err := repo.InTx(context.Background(), fn); err != nil {
		t.Fatalf("transaction: %v", err)
	}
}

// The migration's default, proved on a real row rather than inferred
// from the DDL: an item nobody has decided anything about follows its
// minimum, the way every row did before the column existed.
func TestAFreshItemFollowsTheMinimum(t *testing.T) {
	repo := MustNewPantry(testDB(t))
	ctx := context.Background()
	item := newTestItem(t, repo)

	stored, err := repo.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if stored.Shopping != domain.ShoppingAuto {
		t.Errorf("a fresh item is %q, want %q", stored.Shopping, domain.ShoppingAuto)
	}
}

func TestShoppingModeRoundTrip(t *testing.T) {
	repo := MustNewPantry(testDB(t))
	ctx := context.Background()
	item := newTestItem(t, repo)

	for _, mode := range []domain.ShoppingMode{domain.ShoppingNever, domain.ShoppingAlways, domain.ShoppingAuto} {
		// UpdateItem writes the whole row, SetShopping only this field:
		// both have to land, since the pin clears itself through the
		// narrow one.
		item.Shopping = mode
		mustInTx(t, repo, func(tx PantryTx) error { return tx.UpdateItem(ctx, item) })

		stored, err := repo.GetByID(ctx, item.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if stored.Shopping != mode {
			t.Errorf("UpdateItem to %q read back as %q", mode, stored.Shopping)
		}

		mustInTx(t, repo, func(tx PantryTx) error { return tx.SetShopping(ctx, item.ID, mode) })
		stored, err = repo.GetByID(ctx, item.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if stored.Shopping != mode {
			t.Errorf("SetShopping to %q read back as %q", mode, stored.Shopping)
		}
	}

	err := repo.InTx(ctx, func(tx PantryTx) error {
		return tx.SetShopping(ctx, uuid.New().String(), domain.ShoppingNever)
	})
	var notFound *domain.NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("SetShopping on a missing item = %v, want *NotFoundError", err)
	}
}

// The note and the item name the same thing, and the note is the one
// with nothing behind it. The match ignores case and surrounding space,
// because the text is whatever the person typed.
func TestDeleteNotesNamed(t *testing.T) {
	repo := MustNewPantry(testDB(t))
	ctx := context.Background()

	name := "teste-" + uuid.New().String()
	note, err := repo.AddNote(ctx, "  "+strings.ToUpper(name)+" ")
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	t.Cleanup(func() {
		_, _ = repo.db.ExecContext(context.Background(), `DELETE FROM shopping_notes WHERE id = $1`, note.ID)
	})

	var removed int64
	mustInTx(t, repo, func(tx PantryTx) error {
		var err error
		removed, err = tx.DeleteNotesNamed(ctx, name)
		return err
	})
	if removed != 1 {
		t.Errorf("DeleteNotesNamed removed %d, want 1", removed)
	}

	// Most items are created with no note waiting for them, so nothing
	// matching is the normal case and not an error.
	mustInTx(t, repo, func(tx PantryTx) error {
		n, err := tx.DeleteNotesNamed(ctx, name)
		if n != 0 {
			t.Errorf("DeleteNotesNamed removed %d the second time, want 0", n)
		}
		return err
	})
}
