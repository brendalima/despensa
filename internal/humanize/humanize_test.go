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
