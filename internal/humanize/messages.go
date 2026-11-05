package humanize

import (
	"errors"
	"strings"

	"github.com/brendalima/despensa/internal/core/domain"
)

// Receipt is the line the person gets back after something is recorded:
// what it was, what it is now, and the arithmetic in their own words.
//
//	pão de forma: 18 → 16 fatias
//	arroz: 5 kg → 4,8 kg (≈27 taças)
func Receipt(r domain.Receipt) string {
	var b strings.Builder
	b.WriteString(r.Item.Name)
	b.WriteString(": ")
	b.WriteString(Quantity(r.Item, r.Before))
	b.WriteString(" → ")
	b.WriteString(Level(r.Item, r.After))

	if r.ItemCreated {
		b.WriteString("\nItem novo, anotado agora.")
	}
	// Say what a new word was written down as, so a bad estimate gets
	// corrected in one sentence instead of skewing the totals.
	if r.PortionLearned != nil {
		b.WriteString("\nAnotei que 1 ")
		b.WriteString(r.PortionLearned.Name)
		b.WriteString(" = ")
		b.WriteString(Quantity(unitOnly(r.Item), r.PortionLearned.One))
		b.WriteString(". Se estiver muito fora, me diga o valor certo.")
	}
	// Riding along on the reply is why this needs no scheduled job and
	// no approved message template.
	if r.CrossedThreshold && !r.Drifted {
		b.WriteString("\n⚠️ Chegou no mínimo: ")
		b.WriteString(r.Item.Name)
		b.WriteString(" entrou na lista de compras.")
	}
	if r.Drifted {
		b.WriteString("\nPela minha conta ")
		b.WriteString(r.Item.Name)
		b.WriteString(" já tinha acabado, então em algum momento eu contei menos do que entrou. Quanto tem aí, mais ou menos?")
	}
	return b.String()
}

// Receipts joins a batch: one sentence gets one answer.
func Receipts(rs []domain.Receipt) string {
	lines := make([]string, 0, len(rs))
	for _, r := range rs {
		lines = append(lines, Receipt(r))
	}
	return strings.Join(lines, "\n")
}

// Undone is the answer to the Desfazer button.
func Undone(r domain.Receipt) string {
	if r.Before == r.After {
		return "Esse já estava desfeito. " + r.Item.Name + ": " + Level(r.Item, r.After) + "."
	}
	return "Desfeito. " + r.Item.Name + ": " + Quantity(r.Item, r.Before) + " → " + Level(r.Item, r.After) + "."
}

// ShoppingList renders what to buy.
func ShoppingList(l domain.ShoppingList) string {
	if l.Empty() {
		return "Não está faltando nada."
	}

	var b strings.Builder
	b.WriteString("Lista de compras:")
	for _, item := range l.Low {
		b.WriteString("\n• ")
		b.WriteString(item.Name)
		b.WriteString(" — ")
		if item.Drifted() {
			b.WriteString("acabou (ou minha conta se perdeu)")
			continue
		}
		b.WriteString("restam ")
		b.WriteString(Level(item, item.OnHand))
	}
	for _, note := range l.Notes {
		b.WriteString("\n• ")
		b.WriteString(note.Text)
	}
	return b.String()
}

// Item renders one line of the pantry.
func Item(item domain.Item) string {
	line := item.Name + ": " + Level(item, item.OnHand)
	switch {
	case item.Drifted():
		return line + " (minha conta se perdeu — me diga quanto tem)"
	case item.BelowThreshold():
		return line + " (no mínimo)"
	default:
		return line
	}
}

// Error turns a domain error into something the person can act on.
func Error(err error) string {
	var ambiguous *domain.AmbiguousItemError
	if errors.As(err, &ambiguous) {
		return "Tenho mais de uma coisa que combina com \"" + ambiguous.Said + "\": " +
			joinOr(ambiguous.Candidates) + ". Qual delas?"
	}

	var unknownPortion *domain.UnknownPortionError
	if errors.As(err, &unknownPortion) {
		msg := "Não sei quanto é 1 " + unknownPortion.Portion + " de " + unknownPortion.Item + "."
		if len(unknownPortion.Known) > 0 {
			msg += " Eu conheço: " + joinOr(unknownPortion.Known) + "."
		}
		return msg
	}

	var notFound *domain.NotFoundError
	if errors.As(err, &notFound) {
		return "Não encontrei isso na despensa."
	}

	var conflict *domain.ConflictError
	if errors.As(err, &conflict) {
		return "Já existe um item chamado \"" + conflict.Name + "\"."
	}

	var invalid *domain.ValidationError
	if errors.As(err, &invalid) {
		return "Não consegui entender essa parte (" + invalid.Field + ")."
	}

	// A bug or an outage. Saying so beats a cheerful lie.
	return "Deu problema para salvar agora. Não anotei nada — pode repetir em um minuto?"
}

// unitOnly renders in the plain unit even when the item has a word for
// the piece.
func unitOnly(item domain.Item) domain.Item {
	return domain.Item{Unit: item.Unit}
}

func joinOr(options []string) string {
	switch len(options) {
	case 0:
		return ""
	case 1:
		return options[0]
	default:
		return strings.Join(options[:len(options)-1], ", ") + " ou " + options[len(options)-1]
	}
}
