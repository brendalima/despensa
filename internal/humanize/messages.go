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
	return "Lista de compras:\n• " + strings.Join(listEntries(l), "\n• ")
}

// Digest is the weekly list as it arrives in the chat, which is where
// it lands whenever there is no approved template to send it as. The
// header says what week it is talking about, because unlike every other
// message the bot sends, nobody asked for this one.
func Digest(l domain.ShoppingList) string {
	if l.Empty() {
		return "Lista da semana: não está faltando nada."
	}
	return "Lista da semana:\n• " + strings.Join(listEntries(l), "\n• ")
}

// digestParamLimit is Meta's documented cap on a template's body text:
// 1024 characters for the template itself. The cap on a send-time
// parameter is not documented at all, so the body's stands in for it.
const digestParamLimit = 1024

// DigestLine is the list as the one parameter of an approved template.
//
// It is a single line on purpose. A template parameter is substituted
// into a sentence the template already owns, so a newline in it would
// break that sentence rather than lay out a list. Meta is also widely
// reported to refuse parameters holding newlines or runs of whitespace,
// which their current documentation neither states nor denies — this
// does not depend on which it is.
func DigestLine(l domain.ShoppingList) string {
	if l.Empty() {
		return "nada faltando"
	}
	return clip(oneLine(strings.Join(listEntries(l), "; ")), digestParamLimit)
}

// listEntries is one line per thing to buy, shared by every rendering
// of the list so the chat and the digest cannot word it differently.
func listEntries(l domain.ShoppingList) []string {
	entries := make([]string, 0, len(l.Low)+len(l.Notes))
	for _, item := range l.Low {
		if item.Drifted() {
			entries = append(entries, item.Name+" — acabou (ou minha conta se perdeu)")
			continue
		}
		entries = append(entries, item.Name+" — restam "+Level(item, item.OnHand))
	}
	for _, note := range l.Notes {
		entries = append(entries, note.Text)
	}
	return entries
}

// oneLine collapses every run of whitespace, newlines included, into a
// single space. An item's name is the person's own text, so it can hold
// anything they typed.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// clip cuts at a rune boundary and says it was cut. A list long enough
// to need this is a shopping trip nobody is doing in one go anyway.
func clip(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit-1]) + "…"
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
	return Trouble()
}

// Trouble is what a failure nobody planned for sounds like. It says
// what was not written, because the opposite — a cheerful receipt for
// a row that does not exist — is the one thing this product cannot do.
func Trouble() string {
	return "Deu problema para salvar agora. Não anotei nada — pode repetir em um minuto?"
}

// UndoLabel is the button on every receipt.
const UndoLabel = "Desfazer"

// NotForYou answers a number that is not on the list. Neutral on
// purpose: it neither confirms what this is nor denies it, and it costs
// no model call.
func NotForYou() string {
	return "Oi! Esse número não atende por aqui."
}

// NothingToRead answers a message with nothing in it to act on — a
// photo, an audio, a tap on something that is no longer there.
func NothingToRead() string {
	return "Não consegui ler essa. Me manda em texto o que você usou ou comprou?"
}

// GotLost answers the one case where the loop gave up: the model used
// its tool budget without arriving at a sentence. What was written is
// written, so the honest answer points at the pantry instead of
// inventing a receipt.
func GotLost() string {
	return "Me perdi no meio dessa. Confere a despensa e, se faltou anotar algo, me diz de novo?"
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
