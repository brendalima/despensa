package humanize_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/humanize"
)

const (
	cup   = domain.Amount(180 * domain.Thousand)
	slice = domain.Thousand
)

func rice() domain.Item {
	return domain.Item{
		Name:      "arroz",
		Unit:      domain.UnitGram,
		OnHand:    4600 * domain.Thousand,
		Threshold: 1000 * domain.Thousand,
		Portions: []domain.Portion{
			{Name: "colher", One: 12 * domain.Thousand},
			{Name: "taça", One: cup},
			{Name: "pacote", One: 5000 * domain.Thousand},
		},
	}
}

func bread() domain.Item {
	return domain.Item{
		Name:      "pão de forma",
		Unit:      domain.UnitPiece,
		OnHand:    16 * domain.Thousand,
		Threshold: 4 * domain.Thousand,
		Portions: []domain.Portion{
			{Name: "fatia", One: slice},
			{Name: "pacote", One: 22 * domain.Thousand},
		},
	}
}

func milk() domain.Item {
	return domain.Item{
		Name:   "leite",
		Unit:   domain.UnitMillilitre,
		OnHand: 1500 * domain.Thousand,
		Portions: []domain.Portion{
			{Name: "copo", One: 200 * domain.Thousand},
			{Name: "caixa", One: 1000 * domain.Thousand},
		},
	}
}

func TestQuantity(t *testing.T) {
	tests := []struct {
		name   string
		item   domain.Item
		amount domain.Amount
		want   string
	}{
		{"kilos", rice(), 4600 * domain.Thousand, "4,6 kg"},
		{"whole kilos lose the decimal", rice(), 5000 * domain.Thousand, "5 kg"},
		{"grams below a kilo", rice(), 180 * domain.Thousand, "180 g"},
		{"an item counted in pieces uses the word for the piece", bread(), 16 * domain.Thousand, "16 fatias"},
		{"one of them is singular", bread(), domain.Thousand, "1 fatia"},
		// Portuguese pluralises anything that is not exactly one: "1,5
		// litros", "0,5 fatias".
		{"half a slice", bread(), 500, "0,5 fatias"},
		{"one and a half", bread(), 1500, "1,5 fatias"},
		{"litres", milk(), 1500 * domain.Thousand, "1,5 l"},
		{"millilitres", milk(), 200 * domain.Thousand, "200 ml"},
		{"nothing left", rice(), 0, "0 g"},
		{"drift is never shown as negative", rice(), -200 * domain.Thousand, "0 g"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := humanize.Quantity(tt.item, tt.amount); got != tt.want {
				t.Errorf("Quantity() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The parenthetical is what makes a weight mean something in a kitchen,
// and only when the number is one a person would use.
func TestSecondary(t *testing.T) {
	tests := []struct {
		name   string
		item   domain.Item
		amount domain.Amount
		want   string
	}{
		{"cups of rice", rice(), 4600 * domain.Thousand, "≈26 taças"},
		{"not spoons, which nobody counts to 383", rice(), 4600 * domain.Thousand, "≈26 taças"},
		{"not packages, at less than two of them", rice(), 4600 * domain.Thousand, "≈26 taças"},
		{"packages once there are enough", rice(), 15000 * domain.Thousand, "≈3 pacotes"},
		{"bread is already counted in slices", bread(), 16 * domain.Thousand, ""},
		{"glasses of milk", milk(), 1500 * domain.Thousand, "≈8 copos"},
		{"nothing worth saying when empty", rice(), 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := humanize.Secondary(tt.item, tt.amount); got != tt.want {
				t.Errorf("Secondary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLevel(t *testing.T) {
	if got, want := humanize.Level(rice(), 4600*domain.Thousand), "4,6 kg (≈26 taças)"; got != want {
		t.Errorf("Level() = %q, want %q", got, want)
	}
	if got, want := humanize.Level(bread(), 16*domain.Thousand), "16 fatias"; got != want {
		t.Errorf("Level() = %q, want %q", got, want)
	}
}

func TestPlural(t *testing.T) {
	tests := map[string]string{
		"fatia":    "fatias",
		"taça":     "taças",
		"copo":     "copos",
		"pacote":   "pacotes",
		"concha":   "conchas",
		"colher":   "colheres",
		"pão":      "pães",
		"mão":      "mãos",
		"limão":    "limões",
		"pires":    "pires",
		"barril":   "barris",
		"colheres": "colheres",
		"":         "",

		// An abbreviation does not inflect. "ovo: 0 uns -> 20 uns" was a
		// real receipt: every piece-counted item whose unit portion is
		// called "un" renders through here, so this is all of them.
		"un": "un",
		"g":  "g",
		"kg": "kg",
		"ml": "ml",
		"l":  "l",
	}
	for in, want := range tests {
		if got := humanize.Plural(in); got != want {
			t.Errorf("Plural(%q) = %q, want %q", in, got, want)
		}
	}
}

// The receipt is the whole product surface. These are golden on purpose:
// if the wording drifts, it drifts where someone notices.
func TestReceipt(t *testing.T) {
	item := rice()
	item.OnHand = 4820 * domain.Thousand

	got := humanize.Receipt(domain.Receipt{
		Item:   item,
		Before: 5000 * domain.Thousand,
		After:  4820 * domain.Thousand,
	})
	want := "arroz: 5 kg → 4,8 kg (≈27 taças)"
	if got != want {
		t.Errorf("Receipt() =\n%q\nwant\n%q", got, want)
	}
}

func TestReceiptWithLearnedPortion(t *testing.T) {
	item := rice()
	item.OnHand = 4510 * domain.Thousand
	learned := domain.Portion{Name: "concha", One: 90 * domain.Thousand, Source: domain.PortionSeeded}

	got := humanize.Receipt(domain.Receipt{
		Item:           item,
		Before:         4600 * domain.Thousand,
		After:          4510 * domain.Thousand,
		PortionLearned: &learned,
	})
	if !strings.Contains(got, "Anotei que 1 concha = 90 g") {
		t.Errorf("Receipt() =\n%s\nwant it to say what the word was written down as", got)
	}
	if !strings.Contains(got, "valor certo") {
		t.Errorf("Receipt() =\n%s\nwant it to offer the correction", got)
	}
}

func TestReceiptWithThresholdAlert(t *testing.T) {
	item := rice()
	item.OnHand = 900 * domain.Thousand

	got := humanize.Receipt(domain.Receipt{
		Item:             item,
		Before:           1080 * domain.Thousand,
		After:            900 * domain.Thousand,
		CrossedThreshold: true,
	})
	if !strings.Contains(got, "Chegou no mínimo") {
		t.Errorf("Receipt() =\n%s\nwant the low-stock line", got)
	}
}

// Drift is the one moment the bot asks for a number, and it asks for a
// guess, not a measurement.
func TestReceiptWithDrift(t *testing.T) {
	item := rice()
	item.OnHand = -200 * domain.Thousand

	got := humanize.Receipt(domain.Receipt{
		Item:             item,
		Before:           160 * domain.Thousand,
		After:            -200 * domain.Thousand,
		CrossedThreshold: true,
		Drifted:          true,
	})
	if !strings.Contains(got, "Quanto tem aí") {
		t.Errorf("Receipt() =\n%s\nwant it to ask for a rough count", got)
	}
	if strings.Contains(got, "mais ou menos?") == false {
		t.Errorf("Receipt() =\n%s\nwant the question to ask for an estimate", got)
	}
	// Drift already asks for a recount; the threshold line on top of it
	// would be noise.
	if strings.Contains(got, "Chegou no mínimo") {
		t.Errorf("Receipt() =\n%s\nwant no threshold line next to the drift question", got)
	}
	if strings.Contains(got, "-200") || strings.Contains(got, "-0,2") {
		t.Errorf("Receipt() =\n%s\nwant no negative amount shown", got)
	}
}

func TestUndone(t *testing.T) {
	item := rice()
	item.OnHand = 5000 * domain.Thousand

	got := humanize.Undone(domain.Receipt{Item: item, Before: 4820 * domain.Thousand, After: 5000 * domain.Thousand})
	if want := "Desfeito. arroz: 4,8 kg → 5 kg (≈28 taças)."; got != want {
		t.Errorf("Undone() = %q, want %q", got, want)
	}

	// A second tap says so instead of pretending to undo again.
	same := humanize.Undone(domain.Receipt{Item: item, Before: 5000 * domain.Thousand, After: 5000 * domain.Thousand})
	if !strings.Contains(same, "já estava desfeito") {
		t.Errorf("Undone() = %q, want it to say the event was already undone", same)
	}
}

func TestShoppingList(t *testing.T) {
	low := rice()
	low.OnHand = 500 * domain.Thousand

	got := humanize.ShoppingList(domain.ShoppingList{
		Low:   []domain.Item{low},
		Notes: []domain.Note{{Text: "fermento"}},
	})
	if !strings.Contains(got, "arroz — restam 500 g") {
		t.Errorf("ShoppingList() =\n%s\nwant the item with what is left", got)
	}
	if !strings.Contains(got, "• fermento") {
		t.Errorf("ShoppingList() =\n%s\nwant the note", got)
	}
}

func TestShoppingListEmpty(t *testing.T) {
	if got, want := humanize.ShoppingList(domain.ShoppingList{}), "Não está faltando nada."; got != want {
		t.Errorf("ShoppingList() = %q, want %q", got, want)
	}
}

func TestItem(t *testing.T) {
	plenty := rice()
	if got, want := humanize.Item(plenty), "arroz: 4,6 kg (≈26 taças)"; got != want {
		t.Errorf("Item() = %q, want %q", got, want)
	}

	low := rice()
	low.OnHand = 500 * domain.Thousand
	if !strings.Contains(humanize.Item(low), "no mínimo") {
		t.Errorf("Item() = %q, want the low marker", humanize.Item(low))
	}

	drifted := rice()
	drifted.OnHand = -100
	if !strings.Contains(humanize.Item(drifted), "me diga quanto tem") {
		t.Errorf("Item() = %q, want the recount ask", humanize.Item(drifted))
	}
}

// Every refusal the model can hit has to come out as something the person
// can act on, and none of them may leak an English error message.
func TestError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"ambiguous", domain.NewAmbiguousItemError("arroz", []string{"arroz", "arroz arbóreo"}), "arroz ou arroz arbóreo"},
		{"unknown portion", domain.NewUnknownPortionError("arroz", "concha", []string{"taça", "pacote"}), "Não sei quanto é 1 concha de arroz"},
		{"not found", domain.NewNotFoundError("item not found"), "Não encontrei"},
		{"duplicate", domain.NewConflictError("arroz"), "Já existe um item"},
		{"invalid", domain.NewValidationError("portion_qty", "is zero"), "portion_qty"},
		{"anything else", errors.New("connection reset"), "Não anotei nada"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := humanize.Error(tt.err)
			if !strings.Contains(got, tt.want) {
				t.Errorf("Error() = %q, want it to contain %q", got, tt.want)
			}
			// The core's messages are English on purpose; none of them
			// may reach the person.
			for _, leak := range []string{"not found", "is zero", "ambiguous item", "connection reset"} {
				if strings.Contains(got, leak) {
					t.Errorf("Error() = %q, leaks the internal message %q", got, leak)
				}
			}
		})
	}
}

// The digest is the only message nobody asked for, so it says what it
// is before it says anything else.
func TestDigest(t *testing.T) {
	low := rice()
	low.OnHand = 500 * domain.Thousand

	got := humanize.Digest(domain.ShoppingList{
		Low:   []domain.Item{low},
		Notes: []domain.Note{{Text: "fermento"}},
	})
	if !strings.HasPrefix(got, "Resumo semanal da despensa") {
		t.Errorf("Digest() =\n%s\nwant it to say what it is first", got)
	}
	// Meta reads "lista de compras" and a call to action as marketing,
	// and the fallback is the same message as the template.
	for _, promotional := range []string{"lista de compras", "Me diga", "compre"} {
		if strings.Contains(got, promotional) {
			t.Errorf("Digest() =\n%s\nreads as marketing: %q", got, promotional)
		}
	}
	if !strings.Contains(got, "arroz — restam 500 g") {
		t.Errorf("Digest() =\n%s\nwant the item with what is left", got)
	}
	if !strings.Contains(got, "• fermento") {
		t.Errorf("Digest() =\n%s\nwant the note", got)
	}
}

// A template parameter is substituted into a sentence the template
// already owns, so it has to be one line however the person wrote the
// name of what they want.
func TestDigestLineIsOneLine(t *testing.T) {
	low := rice()
	low.OnHand = 500 * domain.Thousand

	got := humanize.DigestLine(domain.ShoppingList{
		Low:   []domain.Item{low},
		Notes: []domain.Note{{Text: "papel\ttoalha\ne  fermento"}},
	})
	if strings.ContainsAny(got, "\n\r\t") {
		t.Errorf("DigestLine() = %q, want no newlines or tabs", got)
	}
	if strings.Contains(got, "  ") {
		t.Errorf("DigestLine() = %q, want no runs of spaces", got)
	}
	for _, want := range []string{"arroz", "500 g", "papel toalha", "fermento"} {
		if !strings.Contains(got, want) {
			t.Errorf("DigestLine() = %q, want it to contain %q", got, want)
		}
	}
}

// Meta documents 1024 characters for a template's body, and nothing at
// all about a parameter, so the body's cap is the safe assumption.
func TestDigestLineIsClippedToMetasLimit(t *testing.T) {
	notes := make([]domain.Note, 0, 200)
	for i := range 200 {
		notes = append(notes, domain.Note{ID: int64(i), Text: "uma coisa com nome comprido para comprar"})
	}

	got := humanize.DigestLine(domain.ShoppingList{Notes: notes})
	if n := len([]rune(got)); n > 1024 {
		t.Errorf("DigestLine() is %d runes, want at most 1024", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("DigestLine() = %q, want it to say it was cut", got)
	}
}

// An empty list never reaches a template — the sender skips it — but
// the parameter still has to be a non-empty string: Meta refuses a
// template whose parameter is blank.
func TestDigestWithNothingMissing(t *testing.T) {
	if got := humanize.DigestLine(domain.ShoppingList{}); got == "" {
		t.Error("DigestLine() is empty, which Meta refuses")
	}
	if got := humanize.Digest(domain.ShoppingList{}); !strings.Contains(got, "faltando nada") {
		t.Errorf("Digest() = %q, want it to say nothing is missing", got)
	}
}

// The plan's target output is "arroz: 5 kg → 4,6 kg (≈26 taças)". That
// stopped happening the moment the model started giving items a portion
// named after their own unit: the unit portion won, the weight was
// counted in grams, and 5 kg read as "5000 g".
//
// These are the items as the model actually created them.
func TestAPortionNamedAfterTheUnitDoesNotDefeatScaling(t *testing.T) {
	rice := domain.Item{Name: "arroz", Unit: domain.UnitGram, Portions: []domain.Portion{
		{Name: "g", One: domain.Thousand},
		{Name: "taça", One: 180 * domain.Thousand},
		{Name: "pacote", One: 5000 * domain.Thousand},
	}}
	milk := domain.Item{Name: "leite", Unit: domain.UnitMillilitre, Portions: []domain.Portion{
		{Name: "ml", One: domain.Thousand},
		{Name: "caixa", One: 1000 * domain.Thousand},
	}}

	tests := []struct {
		name string
		item domain.Item
		on   domain.Amount
		want string
	}{
		{"kilos, not thousands of grams", rice, 5000 * domain.Thousand, "5 kg"},
		{"grams below the kilo", rice, 433 * domain.Thousand, "433 g"},
		{"litres, not thousands of millilitres", milk, 2500 * domain.Thousand, "2,5 l"},
		{"millilitres below the litre", milk, 500 * domain.Thousand, "500 ml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := humanize.Quantity(tt.item, tt.on); got != tt.want {
				t.Errorf("Quantity() = %q, want %q", got, tt.want)
			}
		})
	}

	// And the unit must never show up as the secondary either.
	if got := humanize.Level(rice, 20*domain.Thousand); got != "20 g" {
		t.Errorf("Level() = %q, want %q — the unit said twice says nothing twice", got, "20 g")
	}
	// The real vocabulary still reads as it should.
	if got := humanize.Level(milk, 2500*domain.Thousand); got != "2,5 l (≈2,5 caixas)" {
		t.Errorf("Level() = %q", got)
	}
}

// The one line a template parameter has to be is also the line the
// household reads every Sunday, so its shape is tested rather than
// left to whatever strings.Join happened to be called with. Meta
// refuses a newline in a parameter with error 132018 — the separator
// is the only layout available.
func TestDigestLineIsCompactAndBulleted(t *testing.T) {
	low := rice()
	low.OnHand = 433 * domain.Thousand

	empty := domain.Item{Name: "ketchup", Unit: domain.UnitMillilitre}

	drifted := domain.Item{Name: "feijão", Unit: domain.UnitGram, OnHand: -20 * domain.Thousand}

	got := humanize.DigestLine(domain.ShoppingList{
		Low:   []domain.Item{low, empty, drifted},
		Notes: []domain.Note{{Text: "papel toalha"}},
	})

	want := "arroz 433 g (≈2,5 taças) • ketchup acabou • " +
		"feijão acabou (ou minha conta se perdeu) • papel toalha"
	if got != want {
		t.Errorf("DigestLine() =\n%q\nwant\n%q", got, want)
	}
	// The compact form is the whole point: "restam" eight times is
	// what made the first real digest hard to read.
	if strings.Contains(got, "restam") || strings.Contains(got, "—") {
		t.Errorf("DigestLine() = %q, want the compact entries", got)
	}
}

// Zero is "acabou" everywhere, not "restam 0 ml": the chat and the
// template say the same thing about an item that has run out.
func TestNothingLeftReadsAsAcabou(t *testing.T) {
	empty := domain.Item{Name: "ketchup", Unit: domain.UnitMillilitre}
	list := domain.ShoppingList{Low: []domain.Item{empty}}

	for name, got := range map[string]string{
		"ShoppingList": humanize.ShoppingList(list),
		"Digest":       humanize.Digest(list),
		"DigestLine":   humanize.DigestLine(list),
	} {
		if !strings.Contains(got, "acabou") {
			t.Errorf("%s() = %q, want it to say the item ran out", name, got)
		}
		if strings.Contains(got, "0 ml") {
			t.Errorf("%s() = %q, want no \"restam 0 ml\"", name, got)
		}
	}
}

// The level and the decision are two facts, so the line carries both.
// An item nobody has decided anything about reads exactly as it did
// before the mode existed, which is what the first case is for.
func TestItemCarriesTheShoppingMode(t *testing.T) {
	tests := []struct {
		name   string
		onHand domain.Amount
		mode   domain.ShoppingMode
		want   string
	}{
		{"plenty", 4600 * domain.Thousand, domain.ShoppingAuto, "arroz: 4,6 kg (≈26 taças)"},
		{"low", 500 * domain.Thousand, domain.ShoppingAuto, "arroz: 500 g (≈3 taças) (no mínimo)"},
		{"held back, plenty", 4600 * domain.Thousand, domain.ShoppingNever, "arroz: 4,6 kg (≈26 taças) (sem repor)"},
		{"held back, low", 500 * domain.Thousand, domain.ShoppingNever, "arroz: 500 g (≈3 taças) (no mínimo, sem repor)"},
		{"pinned", 4600 * domain.Thousand, domain.ShoppingAlways, "arroz: 4,6 kg (≈26 taças) (na lista)"},
		{"held back, drifted", -100, domain.ShoppingNever, "arroz: 0 g (minha conta se perdeu — me diga quanto tem, sem repor)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := rice()
			item.OnHand = tt.onHand
			item.Shopping = tt.mode
			if got := humanize.Item(item); got != tt.want {
				t.Errorf("Item() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Nothing is missing — the person asked to be reminded — so the entry
// must not read as a shortage. When it is also low the level is the
// news and says itself, which is the second case.
func TestAPinnedItemReadsAsAReminder(t *testing.T) {
	pinned := rice()
	pinned.Shopping = domain.ShoppingAlways
	list := domain.ShoppingList{Low: []domain.Item{pinned}}

	if got := humanize.ShoppingList(list); !strings.Contains(got, "arroz — lembrete") {
		t.Errorf("ShoppingList() =\n%s\nwant the reminder entry", got)
	}
	if got := humanize.DigestLine(list); !strings.Contains(got, "arroz (lembrete)") {
		t.Errorf("DigestLine() = %q, want the compact reminder entry", got)
	}

	alsoLow := rice()
	alsoLow.OnHand = 500 * domain.Thousand
	alsoLow.Shopping = domain.ShoppingAlways
	if got := humanize.ShoppingList(domain.ShoppingList{Low: []domain.Item{alsoLow}}); !strings.Contains(got, "restam 500 g") {
		t.Errorf("ShoppingList() =\n%s\nwant the level, which is the news when it is low", got)
	}
}

// Held back on purpose, so the alert must warn without claiming the
// list changed — and it offers the way back, since the mode is the only
// state and asking is how it is undone.
func TestReceiptAlertDoesNotClaimTheListForAHeldBackItem(t *testing.T) {
	item := rice()
	item.OnHand = 900 * domain.Thousand
	item.Shopping = domain.ShoppingNever

	got := humanize.Receipt(domain.Receipt{
		Item:             item,
		Before:           1080 * domain.Thousand,
		After:            900 * domain.Thousand,
		CrossedThreshold: true,
	})
	if !strings.Contains(got, "Chegou no mínimo") {
		t.Errorf("Receipt() =\n%s\nwant the low-stock line", got)
	}
	if strings.Contains(got, "entrou na lista de compras") {
		t.Errorf("Receipt() =\n%s\nclaims the list changed, and it did not", got)
	}
	if !strings.Contains(got, "sem repor") {
		t.Errorf("Receipt() =\n%s\nwant the way back", got)
	}
}

// A line vanished from the shopping list in both cases, and silence
// about it reads as the bot having lost the reminder.
func TestReceiptExplainsTheLinesItRemoved(t *testing.T) {
	item := rice()

	cleared := humanize.Receipt(domain.Receipt{Item: item, Before: item.OnHand, After: item.OnHand, NoteCleared: true})
	if !strings.Contains(cleared, "dos recados") {
		t.Errorf("Receipt() =\n%s\nwant the note becoming an item", cleared)
	}

	unpinned := humanize.Receipt(domain.Receipt{Item: item, Before: item.OnHand, After: item.OnHand, PinCleared: true})
	if !strings.Contains(unpinned, "lembrete") {
		t.Errorf("Receipt() =\n%s\nwant the reminder ending", unpinned)
	}
}

func TestNoteRemovedNamesWhatWent(t *testing.T) {
	got := humanize.NoteRemoved(domain.Note{ID: 3, Text: "papel toalha"})
	if !strings.Contains(got, "papel toalha") {
		t.Errorf("NoteRemoved() = %q, want it to name the note", got)
	}
}
