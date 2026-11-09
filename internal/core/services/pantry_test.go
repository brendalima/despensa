package services_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/core/services"
	"github.com/brendalima/despensa/internal/repository"
)

const (
	cup     = domain.Amount(180 * domain.Thousand)
	pack5kg = domain.Amount(5000 * domain.Thousand)
	slice   = domain.Thousand
)

func riceID() string  { return "11111111-1111-1111-1111-111111111111" }
func breadID() string { return "22222222-2222-2222-2222-222222222222" }

func rice() domain.Item {
	return domain.Item{
		ID:        riceID(),
		Name:      "arroz",
		Unit:      domain.UnitGram,
		OnHand:    5000 * domain.Thousand,
		Threshold: 1000 * domain.Thousand,
		Portions: []domain.Portion{
			{Name: "taça", One: cup, Source: domain.PortionSeeded},
			{Name: "pacote", One: pack5kg, Source: domain.PortionSeeded},
		},
	}
}

func bread() domain.Item {
	return domain.Item{
		ID:        breadID(),
		Name:      "pão de forma",
		Unit:      domain.UnitPiece,
		OnHand:    18 * domain.Thousand,
		Threshold: 4 * domain.Thousand,
		Portions: []domain.Portion{
			{Name: "fatia", One: slice, Source: domain.PortionSeeded},
			{Name: "pacote", One: 22 * domain.Thousand, Source: domain.PortionSeeded},
		},
	}
}

// fakeRepo records what the service did, so the tests can assert on the
// writes and not only on the returned receipts.
type fakeRepo struct {
	items  map[string]domain.Item
	events map[int64]domain.Event
	notes  []domain.Note

	nextEventID int64

	created  []domain.Item
	portions []domain.Portion
	onHand   map[string]domain.Amount
	undone   []int64
	aliases  map[string][]string
	shopping map[string]domain.ShoppingMode
	cleared  []string

	commits   int
	rollbacks int

	failOn string // the method name that should return an error
}

func newFakeRepo(items ...domain.Item) *fakeRepo {
	r := &fakeRepo{
		items:    map[string]domain.Item{},
		events:   map[int64]domain.Event{},
		onHand:   map[string]domain.Amount{},
		aliases:  map[string][]string{},
		shopping: map[string]domain.ShoppingMode{},
	}
	for _, item := range items {
		r.items[item.ID] = item
	}
	return r
}

func (r *fakeRepo) InTx(ctx context.Context, fn func(repository.PantryTx) error) error {
	snapshot := make(map[string]domain.Item, len(r.items))
	for k, v := range r.items {
		snapshot[k] = v
	}
	notes := append([]domain.Note(nil), r.notes...)

	if err := fn(&fakeTx{repo: r}); err != nil {
		// A real transaction leaves nothing behind; the fake has to be
		// just as unforgiving, or a test would pass on a half write.
		r.items = snapshot
		r.notes = notes
		r.created = nil
		r.portions = nil
		r.onHand = map[string]domain.Amount{}
		r.shopping = map[string]domain.ShoppingMode{}
		r.cleared = nil
		r.rollbacks++
		return err
	}
	r.commits++
	return nil
}

func (r *fakeRepo) List(ctx context.Context) ([]domain.Item, error) {
	if r.failOn == "List" {
		return nil, errors.New("database down")
	}
	items := []domain.Item{}
	for _, item := range r.items {
		if !item.Archived() {
			items = append(items, item)
		}
	}
	return items, nil
}

func (r *fakeRepo) ListArchived(ctx context.Context) ([]domain.Item, error) { return nil, nil }

func (r *fakeRepo) GetByID(ctx context.Context, id string) (domain.Item, error) {
	item, ok := r.items[id]
	if !ok {
		return domain.Item{}, domain.NewNotFoundError("item not found")
	}
	return item, nil
}

func (r *fakeRepo) Events(ctx context.Context, itemID string, limit int) ([]domain.Event, error) {
	return nil, nil
}

func (r *fakeRepo) Notes(ctx context.Context) ([]domain.Note, error) {
	if r.failOn == "Notes" {
		return nil, errors.New("database down")
	}
	return r.notes, nil
}

func (r *fakeRepo) AddNote(ctx context.Context, text string) (domain.Note, error) {
	n := domain.Note{ID: int64(len(r.notes) + 1), Text: text}
	r.notes = append(r.notes, n)
	return n, nil
}

func (r *fakeRepo) RemoveNote(ctx context.Context, id int64) (domain.Note, error) {
	for i, n := range r.notes {
		if n.ID == id {
			r.notes = append(r.notes[:i:i], r.notes[i+1:]...)
			return n, nil
		}
	}
	return domain.Note{}, domain.NewNotFoundError("shopping note not found")
}

type fakeTx struct {
	repo *fakeRepo
}

func (t *fakeTx) LockByID(ctx context.Context, id string) (domain.Item, error) {
	return t.repo.GetByID(ctx, id)
}

func (t *fakeTx) LockByName(ctx context.Context, name string) (domain.Item, error) {
	var found []domain.Item
	for _, item := range t.repo.items {
		if item.Archived() {
			continue
		}
		if equalFold(item.Name, name) {
			found = append(found, item)
			continue
		}
		for _, alias := range item.Aliases {
			if equalFold(alias, name) {
				found = append(found, item)
				break
			}
		}
	}
	switch len(found) {
	case 0:
		return domain.Item{}, domain.NewNotFoundError("item not found: " + name)
	case 1:
		return found[0], nil
	default:
		names := make([]string, len(found))
		for i, item := range found {
			names[i] = item.Name
		}
		return domain.Item{}, domain.NewAmbiguousItemError(name, names)
	}
}

func (t *fakeTx) CreateItem(ctx context.Context, item domain.Item) error {
	if t.repo.failOn == "CreateItem" {
		return errors.New("database down")
	}
	t.repo.created = append(t.repo.created, item)
	t.repo.items[item.ID] = item
	return nil
}

func (t *fakeTx) UpdateItem(ctx context.Context, item domain.Item) error {
	if _, ok := t.repo.items[item.ID]; !ok {
		return domain.NewNotFoundError("item not found")
	}
	t.repo.items[item.ID] = item
	return nil
}

func (t *fakeTx) SetOnHand(ctx context.Context, itemID string, onHand domain.Amount) error {
	if t.repo.failOn == "SetOnHand" {
		return errors.New("database down")
	}
	item, ok := t.repo.items[itemID]
	if !ok {
		return domain.NewNotFoundError("item not found")
	}
	item.OnHand = onHand
	t.repo.items[itemID] = item
	t.repo.onHand[itemID] = onHand
	return nil
}

func (t *fakeTx) SetShopping(ctx context.Context, itemID string, mode domain.ShoppingMode) error {
	if t.repo.failOn == "SetShopping" {
		return errors.New("database down")
	}
	item, ok := t.repo.items[itemID]
	if !ok {
		return domain.NewNotFoundError("item not found")
	}
	item.Shopping = mode
	t.repo.items[itemID] = item
	t.repo.shopping[itemID] = mode
	return nil
}

func (t *fakeTx) DeleteNotesNamed(ctx context.Context, name string) (int64, error) {
	if t.repo.failOn == "DeleteNotesNamed" {
		return 0, errors.New("database down")
	}
	t.repo.cleared = append(t.repo.cleared, name)

	kept := make([]domain.Note, 0, len(t.repo.notes))
	var removed int64
	for _, n := range t.repo.notes {
		if equalFold(strings.TrimSpace(n.Text), strings.TrimSpace(name)) {
			removed++
			continue
		}
		kept = append(kept, n)
	}
	t.repo.notes = kept
	return removed, nil
}

func (t *fakeTx) UpsertPortion(ctx context.Context, itemID string, p domain.Portion) error {
	t.repo.portions = append(t.repo.portions, p)
	item, ok := t.repo.items[itemID]
	if !ok {
		return domain.NewNotFoundError("item not found")
	}
	for i, existing := range item.Portions {
		if equalFold(existing.Name, p.Name) {
			// Mirrors the SQL: a corrected size is not replaced by an
			// estimate.
			if existing.Source == domain.PortionCorrected && p.Source != domain.PortionCorrected {
				return nil
			}
			item.Portions[i] = p
			t.repo.items[itemID] = item
			return nil
		}
	}
	item.Portions = append(item.Portions, p)
	t.repo.items[itemID] = item
	return nil
}

func (t *fakeTx) ReplaceAliases(ctx context.Context, itemID string, aliases []string) error {
	t.repo.aliases[itemID] = aliases
	item, ok := t.repo.items[itemID]
	if ok {
		item.Aliases = aliases
		t.repo.items[itemID] = item
	}
	return nil
}

func (t *fakeTx) AddEvent(ctx context.Context, e domain.Event) (int64, error) {
	if t.repo.failOn == "AddEvent" {
		return 0, errors.New("database down")
	}
	t.repo.nextEventID++
	e.ID = t.repo.nextEventID
	t.repo.events[e.ID] = e
	return e.ID, nil
}

func (t *fakeTx) LockEvent(ctx context.Context, id int64) (domain.Event, error) {
	e, ok := t.repo.events[id]
	if !ok {
		return domain.Event{}, domain.NewNotFoundError("event not found")
	}
	return e, nil
}

func (t *fakeTx) MarkEventUndone(ctx context.Context, id int64) error {
	e, ok := t.repo.events[id]
	if !ok {
		return domain.NewNotFoundError("event not found")
	}
	e.UndoneAt = nowish()
	t.repo.events[id] = e
	t.repo.undone = append(t.repo.undone, id)
	return nil
}

func consume(itemID, portion string, qty float64) domain.EventInput {
	return domain.EventInput{
		ItemID:      itemID,
		Kind:        domain.EventConsume,
		PortionName: portion,
		PortionQty:  domain.QtyFromFloat(qty),
		Source:      domain.SourceCLI,
	}
}

func TestRecordConsume(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	receipts, err := svc.Record(context.Background(), []domain.EventInput{consume(riceID(), "taça", 1)})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("got %d receipts, want 1", len(receipts))
	}

	got := receipts[0]
	if got.Before != 5000*domain.Thousand {
		t.Errorf("Before = %d, want 5000000", got.Before)
	}
	if want := domain.Amount(4820 * domain.Thousand); got.After != want {
		t.Errorf("After = %d, want %d", got.After, want)
	}
	if got.EventID == 0 {
		t.Error("EventID = 0, want the id to undo with")
	}
	if got.PortionLearned != nil {
		t.Errorf("PortionLearned = %v, want nil: the word was already known", got.PortionLearned)
	}
	if repo.commits != 1 {
		t.Errorf("commits = %d, want 1", repo.commits)
	}
}

// One sentence naming three things is one call, one transaction and three
// receipts: the whole point of the batch.
func TestRecordBatchIsOneTransaction(t *testing.T) {
	repo := newFakeRepo(rice(), bread())
	svc := services.MustNewPantry(repo)

	receipts, err := svc.Record(context.Background(), []domain.EventInput{
		consume(breadID(), "fatia", 2),
		consume(riceID(), "taça", 1),
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(receipts) != 2 {
		t.Fatalf("got %d receipts, want 2", len(receipts))
	}
	if repo.commits != 1 {
		t.Errorf("commits = %d, want 1: the batch has to be one transaction", repo.commits)
	}
	if want := domain.Amount(16 * domain.Thousand); receipts[0].After != want {
		t.Errorf("bread After = %d, want %d", receipts[0].After, want)
	}
	if want := domain.Amount(4820 * domain.Thousand); receipts[1].After != want {
		t.Errorf("rice After = %d, want %d", receipts[1].After, want)
	}
}

// The second entry failing must leave the first one unwritten: a pantry
// that applied half a sentence is worse than one that applied none.
func TestRecordBatchRollsBackOnOneBadEntry(t *testing.T) {
	repo := newFakeRepo(rice(), bread())
	svc := services.MustNewPantry(repo)

	_, err := svc.Record(context.Background(), []domain.EventInput{
		consume(breadID(), "fatia", 2),
		consume(riceID(), "concha", 1), // a word rice does not know, with no estimate
	})
	if err == nil {
		t.Fatal("Record succeeded, want a refusal on the second entry")
	}

	var unknown *domain.UnknownPortionError
	if !errors.As(err, &unknown) {
		t.Fatalf("error = %v, want *UnknownPortionError", err)
	}
	if repo.commits != 0 {
		t.Errorf("commits = %d, want 0", repo.commits)
	}
	if repo.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", repo.rollbacks)
	}
	if got := repo.items[breadID()].OnHand; got != 18*domain.Thousand {
		t.Errorf("bread on hand = %d, want it untouched at 18000", got)
	}
}

// The product promise: an unknown word is learned from the model's
// estimate, and the person is never asked to measure anything.
func TestRecordLearnsUnknownPortionFromHint(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	in := consume(riceID(), "concha", 1)
	in.PortionOneHint = 90 * domain.Thousand
	receipts, err := svc.Record(context.Background(), []domain.EventInput{in})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	learned := receipts[0].PortionLearned
	if learned == nil {
		t.Fatal("PortionLearned = nil, want the word that was just written down")
	}
	if learned.Name != "concha" || learned.One != 90*domain.Thousand {
		t.Errorf("learned = %+v, want concha at 90000", *learned)
	}
	if learned.Source != domain.PortionSeeded {
		t.Errorf("learned source = %q, want %q: it is a guess and says so", learned.Source, domain.PortionSeeded)
	}
	if want := domain.Amount(4910 * domain.Thousand); receipts[0].After != want {
		t.Errorf("After = %d, want %d", receipts[0].After, want)
	}

	// And the second time the same word is used, it is worth the same.
	again, err := svc.Record(context.Background(), []domain.EventInput{consume(riceID(), "concha", 1)})
	if err != nil {
		t.Fatalf("second Record: %v", err)
	}
	if again[0].PortionLearned != nil {
		t.Error("the word was learned twice, so the same sentence could mean two amounts")
	}
	if want := domain.Amount(4820 * domain.Thousand); again[0].After != want {
		t.Errorf("After = %d, want %d", again[0].After, want)
	}
}

// A word the item knows is never re-estimated, even when the model sends
// a hint: consistency is what makes thirty logged cups add up.
func TestRecordKeepsKnownPortionOverHint(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	in := consume(riceID(), "taça", 1)
	in.PortionOneHint = 400 * domain.Thousand
	receipts, err := svc.Record(context.Background(), []domain.EventInput{in})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if want := domain.Amount(4820 * domain.Thousand); receipts[0].After != want {
		t.Errorf("After = %d, want %d: the known 180 g had to win", receipts[0].After, want)
	}
	if len(repo.portions) != 0 {
		t.Errorf("portions written = %v, want none", repo.portions)
	}
}

func TestRecordRefusesUnknownPortionWithoutHint(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	_, err := svc.Record(context.Background(), []domain.EventInput{consume(riceID(), "concha", 1)})
	var unknown *domain.UnknownPortionError
	if !errors.As(err, &unknown) {
		t.Fatalf("error = %v, want *UnknownPortionError", err)
	}
	if unknown.Portion != "concha" || unknown.Item != "arroz" {
		t.Errorf("error = %+v, want it to name the word and the item", unknown)
	}
	if len(unknown.Known) != 2 {
		t.Errorf("Known = %v, want the two words rice does know", unknown.Known)
	}
}

// Buying is the same mechanism as using: a package is just another word.
func TestRecordRestockAddsUp(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	in := consume(riceID(), "pacote", 2)
	in.Kind = domain.EventRestock
	receipts, err := svc.Record(context.Background(), []domain.EventInput{in})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if want := domain.Amount(15000 * domain.Thousand); receipts[0].After != want {
		t.Errorf("After = %d, want %d", receipts[0].After, want)
	}
}

// "comprei um pacote de lentilha" with no lentilha in the pantry: the
// item is created on the way, with no form to fill in.
func TestRecordCreatesItemOnTheWay(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	receipts, err := svc.Record(context.Background(), []domain.EventInput{{
		NewItem: &domain.NewItem{
			Name: "lentilha",
			Unit: domain.UnitGram,
			Portions: []domain.Portion{
				{Name: "pacote", One: 500 * domain.Thousand},
				{Name: "taça", One: 200 * domain.Thousand},
			},
		},
		Kind:        domain.EventRestock,
		PortionName: "pacote",
		PortionQty:  domain.QtyOf(1),
		Source:      domain.SourceCLI,
	}})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	got := receipts[0]
	if !got.ItemCreated {
		t.Error("ItemCreated = false, want true")
	}
	if got.Item.ID == "" {
		t.Error("the created item has no id")
	}
	if got.Before != 0 || got.After != 500*domain.Thousand {
		t.Errorf("Before, After = %d, %d, want 0, 500000", got.Before, got.After)
	}
	// A fifth of the package: late enough not to nag, early enough to buy.
	if want := domain.Amount(100 * domain.Thousand); got.Item.Threshold != want {
		t.Errorf("Threshold = %d, want %d", got.Item.Threshold, want)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created %d items, want 1", len(repo.created))
	}
}

// The model may describe an item that already exists under a name it did
// not see. Taking the existing one beats a duplicate "Arroz" next to
// "arroz".
func TestRecordReusesExistingItemInsteadOfDuplicating(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	receipts, err := svc.Record(context.Background(), []domain.EventInput{{
		NewItem:     &domain.NewItem{Name: "Arroz", Unit: domain.UnitGram, Portions: []domain.Portion{{Name: "taça", One: cup}}},
		Kind:        domain.EventRestock,
		PortionName: "pacote",
		PortionQty:  domain.QtyOf(1),
		Source:      domain.SourceCLI,
	}})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if receipts[0].ItemCreated {
		t.Error("ItemCreated = true, want false: arroz already existed")
	}
	if receipts[0].Item.ID != riceID() {
		t.Errorf("item id = %q, want the existing rice", receipts[0].Item.ID)
	}
	if len(repo.created) != 0 {
		t.Errorf("created = %v, want nothing created", repo.created)
	}
}

// A correction is the person stating the truth, so the quantity is a
// level and the delta is whatever gets there.
func TestRecordCorrectionIsAbsolute(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	in := consume(riceID(), "taça", 3)
	in.Kind = domain.EventCorrection
	receipts, err := svc.Record(context.Background(), []domain.EventInput{in})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if want := domain.Amount(540 * domain.Thousand); receipts[0].After != want {
		t.Errorf("After = %d, want %d", receipts[0].After, want)
	}
	if want := domain.Amount(-4460 * domain.Thousand); repo.events[receipts[0].EventID].Delta != want {
		t.Errorf("delta = %d, want %d", repo.events[receipts[0].EventID].Delta, want)
	}
}

// Crossing the threshold is a moment, not a state: an item already low
// must not raise the alert on every single use.
func TestRecordReportsThresholdCrossingOnce(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	// 5000 g down to 920 g, under the 1000 g threshold.
	in := consume(riceID(), "taça", 1)
	in.Kind = domain.EventCorrection
	in.PortionQty = domain.QtyFromFloat(5.111)
	first, err := svc.Record(context.Background(), []domain.EventInput{in})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !first[0].CrossedThreshold {
		t.Errorf("CrossedThreshold = false at %d, want true", first[0].After)
	}

	second, err := svc.Record(context.Background(), []domain.EventInput{consume(riceID(), "taça", 1)})
	if err != nil {
		t.Fatalf("second Record: %v", err)
	}
	if second[0].CrossedThreshold {
		t.Error("CrossedThreshold = true again, want false: it already crossed")
	}
}

// Going below zero is the signal that the running total undercounted. It
// is kept, reported, and shown as zero.
func TestRecordReportsDrift(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	in := consume(riceID(), "pacote", 2)
	receipts, err := svc.Record(context.Background(), []domain.EventInput{in})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !receipts[0].Drifted {
		t.Error("Drifted = false, want true")
	}
	if want := domain.Amount(-5000 * domain.Thousand); receipts[0].After != want {
		t.Errorf("After = %d, want %d: the drift is kept, not clamped", receipts[0].After, want)
	}
	if got := receipts[0].Item.Display(); got != 0 {
		t.Errorf("Display() = %d, want 0", got)
	}
}

func TestRecordRefusals(t *testing.T) {
	tests := []struct {
		name string
		in   domain.EventInput
	}{
		{"no item at all", domain.EventInput{Kind: domain.EventConsume, PortionName: "taça", PortionQty: domain.QtyOf(1), Source: domain.SourceCLI}},
		{"both an id and a new item", domain.EventInput{
			ItemID:      riceID(),
			NewItem:     &domain.NewItem{Name: "arroz", Unit: domain.UnitGram},
			Kind:        domain.EventConsume,
			PortionName: "taça", PortionQty: domain.QtyOf(1), Source: domain.SourceCLI,
		}},
		{"unknown kind", domain.EventInput{ItemID: riceID(), Kind: "eaten", PortionName: "taça", PortionQty: domain.QtyOf(1), Source: domain.SourceCLI}},
		{"no source", domain.EventInput{ItemID: riceID(), Kind: domain.EventConsume, PortionName: "taça", PortionQty: domain.QtyOf(1)}},
		{"zero quantity used", domain.EventInput{ItemID: riceID(), Kind: domain.EventConsume, PortionName: "taça", Source: domain.SourceCLI}},
		{"zero quantity bought", domain.EventInput{ItemID: riceID(), Kind: domain.EventRestock, PortionName: "taça", Source: domain.SourceCLI}},
		{"negative quantity", domain.EventInput{ItemID: riceID(), Kind: domain.EventConsume, PortionName: "taça", PortionQty: domain.QtyOf(-1), Source: domain.SourceCLI}},
		{"no portion name", domain.EventInput{ItemID: riceID(), Kind: domain.EventConsume, PortionQty: domain.QtyOf(1), Source: domain.SourceCLI}},
		{"item that does not exist", domain.EventInput{ItemID: "33333333-3333-3333-3333-333333333333", Kind: domain.EventConsume, PortionName: "taça", PortionQty: domain.QtyOf(1), Source: domain.SourceCLI}},
		{"new item without a unit", domain.EventInput{
			NewItem:     &domain.NewItem{Name: "lentilha"},
			Kind:        domain.EventRestock,
			PortionName: "pacote", PortionQty: domain.QtyOf(1), Source: domain.SourceCLI,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo(rice())
			svc := services.MustNewPantry(repo)
			if _, err := svc.Record(context.Background(), []domain.EventInput{tt.in}); err == nil {
				t.Fatal("Record succeeded, want a refusal")
			}
			if repo.commits != 0 {
				t.Errorf("commits = %d, want 0: a refused entry writes nothing", repo.commits)
			}
			if len(repo.created) != 0 {
				t.Errorf("created = %v, want nothing", repo.created)
			}
		})
	}
}

func TestRecordEmptyBatch(t *testing.T) {
	svc := services.MustNewPantry(newFakeRepo(rice()))
	if _, err := svc.Record(context.Background(), nil); err == nil {
		t.Fatal("Record(nil) succeeded, want a refusal")
	}
}

// Two items matching what was said is a question, not a coin toss.
func TestRecordAmbiguousName(t *testing.T) {
	arborio := rice()
	arborio.ID = "44444444-4444-4444-4444-444444444444"
	arborio.Name = "arroz arbóreo"
	arborio.Aliases = []string{"arroz"}

	repo := newFakeRepo(rice(), arborio)
	svc := services.MustNewPantry(repo)

	_, err := svc.Record(context.Background(), []domain.EventInput{{
		NewItem:     &domain.NewItem{Name: "arroz", Unit: domain.UnitGram},
		Kind:        domain.EventConsume,
		PortionName: "taça", PortionQty: domain.QtyOf(1), Source: domain.SourceCLI,
	}})

	var ambiguous *domain.AmbiguousItemError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("error = %v, want *AmbiguousItemError", err)
	}
	if len(ambiguous.Candidates) != 2 {
		t.Errorf("Candidates = %v, want both rices", ambiguous.Candidates)
	}
}

// A failing write must not leave a receipt behind.
func TestRecordDependencyFailure(t *testing.T) {
	for _, method := range []string{"AddEvent", "SetOnHand", "CreateItem"} {
		t.Run(method, func(t *testing.T) {
			repo := newFakeRepo(rice())
			repo.failOn = method
			svc := services.MustNewPantry(repo)

			in := consume(riceID(), "taça", 1)
			if method == "CreateItem" {
				in = domain.EventInput{
					NewItem:     &domain.NewItem{Name: "lentilha", Unit: domain.UnitGram},
					Kind:        domain.EventRestock,
					PortionName: "pacote", PortionQty: domain.QtyOf(1), PortionOneHint: 500 * domain.Thousand,
					Source: domain.SourceCLI,
				}
			}
			receipts, err := svc.Record(context.Background(), []domain.EventInput{in})
			if err == nil {
				t.Fatal("Record succeeded, want the dependency failure")
			}
			if receipts != nil {
				t.Errorf("receipts = %v, want nil", receipts)
			}
			if repo.commits != 0 {
				t.Errorf("commits = %d, want 0", repo.commits)
			}
		})
	}
}

func TestUndo(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	receipts, err := svc.Record(context.Background(), []domain.EventInput{consume(riceID(), "taça", 1)})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	undone, err := svc.Undo(context.Background(), receipts[0].EventID)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if want := domain.Amount(5000 * domain.Thousand); undone.After != want {
		t.Errorf("After = %d, want %d", undone.After, want)
	}
	if len(repo.undone) != 1 {
		t.Errorf("undone = %v, want one event marked", repo.undone)
	}
}

// A second tap on the same Desfazer button must not take the amount off
// twice.
func TestUndoIsIdempotent(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	receipts, err := svc.Record(context.Background(), []domain.EventInput{consume(riceID(), "taça", 1)})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := svc.Undo(context.Background(), receipts[0].EventID); err != nil {
		t.Fatalf("first Undo: %v", err)
	}

	again, err := svc.Undo(context.Background(), receipts[0].EventID)
	if err != nil {
		t.Fatalf("second Undo: %v", err)
	}
	if want := domain.Amount(5000 * domain.Thousand); again.After != want {
		t.Errorf("After = %d, want %d: the second undo changed the total", again.After, want)
	}
	if again.Before != again.After {
		t.Errorf("Before, After = %d, %d, want them equal on a no-op", again.Before, again.After)
	}
	if len(repo.undone) != 1 {
		t.Errorf("undone = %v, want the event marked once", repo.undone)
	}
}

func TestUndoUnknownEvent(t *testing.T) {
	svc := services.MustNewPantry(newFakeRepo(rice()))
	if _, err := svc.Undo(context.Background(), 404); err == nil {
		t.Fatal("Undo succeeded, want not found")
	}
}

func TestUpdate(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	threshold := domain.Amount(360 * domain.Thousand)
	category := "grãos"
	updated, err := svc.Update(context.Background(), domain.ItemChange{
		ItemID:    riceID(),
		Threshold: &threshold,
		Category:  &category,
		Portions:  []domain.Portion{{Name: "taça", One: 170 * domain.Thousand}},
		Aliases:   []string{"arroz branco"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Threshold != threshold {
		t.Errorf("Threshold = %d, want %d", updated.Threshold, threshold)
	}

	// A size the person gave is corrected, so no later estimate moves it.
	p, ok := updated.Portion("taça")
	if !ok {
		t.Fatal("taça is gone after the update")
	}
	if p.One != 170*domain.Thousand || p.Source != domain.PortionCorrected {
		t.Errorf("portion = %+v, want 170000 and corrected", p)
	}
	if len(updated.Aliases) != 1 || updated.Aliases[0] != "arroz branco" {
		t.Errorf("Aliases = %v, want the one given", updated.Aliases)
	}
	// The name was never mentioned, so it must not have moved.
	if updated.Name != "arroz" {
		t.Errorf("Name = %q, want it untouched", updated.Name)
	}
}

// A corrected size outranks the model's estimate for good, which is what
// makes "na verdade a taça é 170 g" stick.
func TestCorrectedPortionSurvivesLaterHint(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	corrected := domain.Amount(170 * domain.Thousand)
	if _, err := svc.Update(context.Background(), domain.ItemChange{
		ItemID:   riceID(),
		Portions: []domain.Portion{{Name: "taça", One: corrected}},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	in := consume(riceID(), "taça", 1)
	in.PortionOneHint = 400 * domain.Thousand
	receipts, err := svc.Record(context.Background(), []domain.EventInput{in})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if want := domain.Amount(5000*domain.Thousand) - corrected; receipts[0].After != want {
		t.Errorf("After = %d, want %d", receipts[0].After, want)
	}
}

func TestUpdateRefusals(t *testing.T) {
	bad := domain.Amount(-1)
	empty := ""
	unknownMode := domain.ShoppingMode("sometimes")
	tests := []struct {
		name   string
		change domain.ItemChange
	}{
		{"nothing asked", domain.ItemChange{ItemID: riceID()}},
		{"negative threshold", domain.ItemChange{ItemID: riceID(), Threshold: &bad}},
		{"empty name", domain.ItemChange{ItemID: riceID(), Name: &empty}},
		{"portion worth nothing", domain.ItemChange{ItemID: riceID(), Portions: []domain.Portion{{Name: "taça", One: 0}}}},
		{"unknown item", domain.ItemChange{ItemID: "33333333-3333-3333-3333-333333333333", Aliases: []string{"x"}}},
		{"unknown shopping mode", domain.ItemChange{ItemID: riceID(), Shopping: &unknownMode}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := services.MustNewPantry(newFakeRepo(rice()))
			if _, err := svc.Update(context.Background(), tt.change); err == nil {
				t.Fatal("Update succeeded, want a refusal")
			}
		})
	}
}

func TestArchiveAndBringBack(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	yes, no := true, false
	archived, err := svc.Update(context.Background(), domain.ItemChange{ItemID: riceID(), Archive: &yes})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !archived.Archived() {
		t.Error("Archived() = false after archiving")
	}

	back, err := svc.Update(context.Background(), domain.ItemChange{ItemID: riceID(), Archive: &no})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if back.Archived() {
		t.Error("Archived() = true after bringing it back")
	}
}

func TestShoppingList(t *testing.T) {
	low := rice()
	low.OnHand = 500 * domain.Thousand // under its 1000 g threshold

	repo := newFakeRepo(low, bread())
	repo.notes = []domain.Note{{ID: 1, Text: "fermento"}}
	svc := services.MustNewPantry(repo)

	list, err := svc.ShoppingList(context.Background())
	if err != nil {
		t.Fatalf("ShoppingList: %v", err)
	}
	if len(list.Low) != 1 || list.Low[0].Name != "arroz" {
		t.Errorf("Low = %v, want only arroz", list.Low)
	}
	if len(list.Notes) != 1 {
		t.Errorf("Notes = %v, want the one note", list.Notes)
	}
	if list.Empty() {
		t.Error("Empty() = true with something to buy")
	}
}

func TestShoppingListEmpty(t *testing.T) {
	svc := services.MustNewPantry(newFakeRepo(bread()))
	list, err := svc.ShoppingList(context.Background())
	if err != nil {
		t.Fatalf("ShoppingList: %v", err)
	}
	if !list.Empty() {
		t.Errorf("Empty() = false, want true: %+v", list)
	}
}

func TestShoppingListDependencyFailure(t *testing.T) {
	for _, method := range []string{"List", "Notes"} {
		t.Run(method, func(t *testing.T) {
			repo := newFakeRepo(rice())
			repo.failOn = method
			svc := services.MustNewPantry(repo)
			if _, err := svc.ShoppingList(context.Background()); err == nil {
				t.Fatal("ShoppingList succeeded, want the dependency failure")
			}
		})
	}
}

func TestAddNoteRefusesEmpty(t *testing.T) {
	svc := services.MustNewPantry(newFakeRepo())
	if _, err := svc.AddNote(context.Background(), "   "); err == nil {
		t.Fatal("AddNote succeeded, want a refusal")
	}
}

func TestNewPantryNeedsRepository(t *testing.T) {
	if _, err := services.NewPantry(nil); err == nil {
		t.Fatal("NewPantry(nil) succeeded, want an error")
	}
}

func equalFold(a, b string) bool { return strings.EqualFold(a, b) }

func nowish() time.Time { return time.Now().UTC() }

// "acabou o arroz" is a correction to zero, and an item can enter the
// pantry empty, so zero is refused for every kind but a correction.
func TestRecordAcceptsZeroOnlyAsCorrection(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	in := consume(riceID(), "taça", 1)
	in.Kind = domain.EventCorrection
	in.PortionQty = 0
	receipts, err := svc.Record(context.Background(), []domain.EventInput{in})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if receipts[0].After != 0 {
		t.Errorf("After = %d, want 0", receipts[0].After)
	}
	if !receipts[0].CrossedThreshold {
		t.Error("CrossedThreshold = false, want true: it just ran out")
	}
}

func TestRecordCreatesEmptyItem(t *testing.T) {
	repo := newFakeRepo()
	svc := services.MustNewPantry(repo)

	receipts, err := svc.Record(context.Background(), []domain.EventInput{{
		NewItem: &domain.NewItem{
			Name:     "fermento",
			Unit:     domain.UnitGram,
			Portions: []domain.Portion{{Name: "pacote", One: 100 * domain.Thousand}},
		},
		Kind:        domain.EventCorrection,
		PortionName: "pacote",
		PortionQty:  0,
		Source:      domain.SourceCLI,
	}})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !receipts[0].ItemCreated || receipts[0].After != 0 {
		t.Errorf("receipt = %+v, want a created item holding nothing", receipts[0])
	}
}

// The list is where the level and the person's decision meet, and it is
// the only place "what to buy" is defined — the weekly digest reads it
// through this same call.
func TestShoppingListObeysTheShoppingMode(t *testing.T) {
	heldBack := rice()
	heldBack.OnHand = 500 * domain.Thousand // under its 1000 g minimum
	heldBack.Shopping = domain.ShoppingNever

	pinned := bread() // well above its minimum
	pinned.Shopping = domain.ShoppingAlways

	svc := services.MustNewPantry(newFakeRepo(heldBack, pinned))
	list, err := svc.ShoppingList(context.Background())
	if err != nil {
		t.Fatalf("ShoppingList: %v", err)
	}
	if len(list.Low) != 1 || list.Low[0].Name != "pão de forma" {
		t.Fatalf("Low = %v, want only the pinned bread", names(list.Low))
	}
}

func TestUpdateSetsTheShoppingMode(t *testing.T) {
	repo := newFakeRepo(rice())
	svc := services.MustNewPantry(repo)

	for _, mode := range []domain.ShoppingMode{domain.ShoppingNever, domain.ShoppingAlways, domain.ShoppingAuto} {
		item, err := svc.Update(context.Background(), domain.ItemChange{ItemID: riceID(), Shopping: &mode})
		if err != nil {
			t.Fatalf("Update to %q: %v", mode, err)
		}
		if item.Shopping != mode {
			t.Errorf("Update to %q left the item at %q", mode, item.Shopping)
		}
	}
}

// "Buy it anyway" is a one-time intent, so the purchase is its end —
// but only a purchase that clears the minimum. Using one slice of a
// pinned loaf is not the person having bought bread.
func TestRestockClearsThePinOnlyWhenItClearsTheMinimum(t *testing.T) {
	tests := []struct {
		name       string
		kind       domain.EventKind
		onHand     domain.Amount
		qty        domain.Qty
		wantMode   domain.ShoppingMode
		wantReport bool
	}{
		{"bought a package", domain.EventRestock, 5000 * domain.Thousand, domain.QtyOf(1), domain.ShoppingAuto, true},
		{"bought a cup, still low", domain.EventRestock, 0, domain.QtyOf(1), domain.ShoppingAlways, false},
		{"used a cup", domain.EventConsume, 5000 * domain.Thousand, domain.QtyOf(1), domain.ShoppingAlways, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinned := rice()
			pinned.OnHand = tt.onHand
			pinned.Shopping = domain.ShoppingAlways

			portion := "pacote"
			if tt.kind == domain.EventConsume || tt.onHand == 0 {
				portion = "taça"
			}

			repo := newFakeRepo(pinned)
			svc := services.MustNewPantry(repo)
			receipts, err := svc.Record(context.Background(), []domain.EventInput{{
				ItemID:      riceID(),
				Kind:        tt.kind,
				PortionName: portion,
				PortionQty:  tt.qty,
				Source:      domain.SourceCLI,
			}})
			if err != nil {
				t.Fatalf("Record: %v", err)
			}

			if got := repo.items[riceID()].Shopping; got != tt.wantMode {
				t.Errorf("the stored mode is %q, want %q", got, tt.wantMode)
			}
			if receipts[0].PinCleared != tt.wantReport {
				t.Errorf("PinCleared = %v, want %v", receipts[0].PinCleared, tt.wantReport)
			}
			if receipts[0].Item.Shopping != tt.wantMode {
				t.Errorf("the receipt carries %q, want %q", receipts[0].Item.Shopping, tt.wantMode)
			}
		})
	}
}

// A note and an item naming the same thing ask for the same purchase
// twice, and the note is the one with nothing behind it. This is the
// collision the real pantry hit with "papel toalha".
func TestCreatingAnItemClearsANoteNamingIt(t *testing.T) {
	repo := newFakeRepo()
	repo.notes = []domain.Note{{ID: 1, Text: "Papel Toalha"}, {ID: 2, Text: "fermento"}}
	svc := services.MustNewPantry(repo)

	receipts, err := svc.Record(context.Background(), []domain.EventInput{{
		NewItem: &domain.NewItem{
			Name:     "papel toalha",
			Unit:     domain.UnitPiece,
			Portions: []domain.Portion{{Name: "rolo", One: domain.Thousand}},
		},
		Kind:        domain.EventRestock,
		PortionName: "rolo",
		PortionQty:  domain.QtyOf(2),
		Source:      domain.SourceCLI,
	}})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !receipts[0].NoteCleared {
		t.Error("NoteCleared = false, so the person is never told the line went")
	}
	if len(repo.notes) != 1 || repo.notes[0].Text != "fermento" {
		t.Errorf("notes = %v, want only fermento", repo.notes)
	}
}

// Same transaction, which is the whole point: a batch that fails on its
// second entry must leave the note where it was.
func TestAFailedBatchPutsTheNoteBack(t *testing.T) {
	repo := newFakeRepo()
	repo.notes = []domain.Note{{ID: 1, Text: "papel toalha"}}
	svc := services.MustNewPantry(repo)

	_, err := svc.Record(context.Background(), []domain.EventInput{
		{
			NewItem: &domain.NewItem{
				Name:     "papel toalha",
				Unit:     domain.UnitPiece,
				Portions: []domain.Portion{{Name: "rolo", One: domain.Thousand}},
			},
			Kind:        domain.EventRestock,
			PortionName: "rolo",
			PortionQty:  domain.QtyOf(2),
			Source:      domain.SourceCLI,
		},
		{ItemID: "44444444-4444-4444-4444-444444444444", Kind: domain.EventConsume, PortionName: "taça", PortionQty: domain.QtyOf(1), Source: domain.SourceCLI},
	})
	if err == nil {
		t.Fatal("Record succeeded, want the second entry to fail")
	}
	if len(repo.notes) != 1 {
		t.Errorf("notes = %v, want the note back after the rollback", repo.notes)
	}
}

// Held back on purpose, and still low: the alert is about the level, so
// it still fires. What changes is the sentence, which is humanize's job.
func TestAHeldBackItemStillReportsCrossingTheMinimum(t *testing.T) {
	heldBack := rice()
	heldBack.Shopping = domain.ShoppingNever
	svc := services.MustNewPantry(newFakeRepo(heldBack))

	receipts, err := svc.Record(context.Background(), []domain.EventInput{{
		ItemID:      riceID(),
		Kind:        domain.EventCorrection,
		PortionName: "taça",
		PortionQty:  domain.QtyOf(1),
		Source:      domain.SourceCLI,
	}})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !receipts[0].CrossedThreshold {
		t.Error("CrossedThreshold = false: the mode moved the level, which it must not")
	}
}

func names(items []domain.Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}
