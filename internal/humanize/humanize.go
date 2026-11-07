// Package humanize turns the domain's numbers into the Portuguese a
// person reads. Every accented string in this project lives here or in
// the tool descriptions, which are prompt.
package humanize

import (
	"fmt"
	"strings"

	"github.com/brendalima/despensa/internal/core/domain"
)

// Quantity renders an amount the way the person says it: "16 fatias",
// "4,6 kg", "180 g".
func Quantity(item domain.Item, a domain.Amount) string {
	a = a.AtLeastZero()

	// "16 fatias", not "16 un".
	if p, ok := item.UnitPortion(); ok {
		return Count(a.In(p.One), p.Name)
	}

	switch item.Unit {
	case domain.UnitGram:
		return scaled(a, "g", "kg")
	case domain.UnitMillilitre:
		return scaled(a, "ml", "l")
	default:
		return Count(a.In(domain.Thousand), "un")
	}
}

// Secondary is what makes a weight mean something in a kitchen: 4,6 kg
// of rice is "≈26 taças". Empty when no portion gives a useful number.
func Secondary(item domain.Item, a domain.Amount) string {
	a = a.AtLeastZero()
	unitPortion, hasUnit := item.UnitPortion()

	var best domain.Portion
	for _, p := range item.Portions {
		// The primary rendering already counts in the unit portion.
		if hasUnit && p.Name == unitPortion.Name {
			continue
		}
		// "20 g (≈20 g)" says nothing twice.
		if item.NamesUnit(p) {
			continue
		}
		count := a.In(p.One).RoundHuman()
		// "0,7 pacotes" and "383 colheres" say less than the weight.
		if count < domain.QtyOf(2) || count > domain.QtyOf(40) {
			continue
		}
		if p.One > best.One {
			best = p
		}
	}
	if best.One == 0 {
		return ""
	}
	return "≈" + Count(a.In(best.One).RoundHuman(), best.Name)
}

// Level is the full rendering: "4,6 kg (≈26 taças)".
func Level(item domain.Item, a domain.Amount) string {
	primary := Quantity(item, a)
	if secondary := Secondary(item, a); secondary != "" {
		return primary + " (" + secondary + ")"
	}
	return primary
}

// Count renders a portion count with its word, pluralised: 1 fatia, 16
// fatias, 2,5 taças.
func Count(q domain.Qty, word string) string {
	rounded := q.RoundHuman()
	if rounded == domain.QtyOf(1) || rounded == domain.QtyOf(-1) {
		return number(rounded.Float(), 1) + " " + word
	}
	return number(rounded.Float(), 1) + " " + Plural(word)
}

// irregular holds what the rules below get wrong. A bad plural in a
// receipt is the small wrongness that makes a bot feel broken.
//
// The abbreviations map to themselves because an abbreviation does not
// inflect: "20 un", never "20 uns". That one is not hypothetical — the
// model created an egg with a portion called "un" and the first real
// receipt read "ovo: 0 uns → 20 uns". Any item counted in pieces whose
// unit portion is named "un" renders through here, so it is every such
// item, not an edge case.
var irregular = map[string]string{
	"pão": "pães",
	"mão": "mãos",

	"un": "un",
	"g":  "g",
	"kg": "kg",
	"mg": "mg",
	"ml": "ml",
	"l":  "l",
}

// Plural inflects a Portuguese noun. The person invents these words, so
// the rules cover kitchen nouns, not the whole language.
func Plural(word string) string {
	if word == "" {
		return word
	}
	lower := strings.ToLower(word)
	if p, ok := irregular[lower]; ok {
		return p
	}

	runes := []rune(lower)
	last := runes[len(runes)-1]

	switch {
	case strings.HasSuffix(lower, "ão"):
		return strings.TrimSuffix(word, "ão") + "ões"
	case last == 's':
		// "pires", "lápis": invariant.
		return word
	case last == 'r' || last == 'z':
		return word + "es"
	case last == 'm':
		return word[:len(word)-1] + "ns"
	case last == 'l':
		if strings.HasSuffix(lower, "il") {
			return word[:len(word)-2] + "is"
		}
		return word[:len(word)-1] + "is"
	default:
		return word + "s"
	}
}

// scaled reads the way anyone would: 180 g, then 4,6 kg.
func scaled(a domain.Amount, small, large string) string {
	whole, _ := a.Units()
	if whole < 1000 {
		return number(float64(a)/float64(domain.Thousand), 0) + " " + small
	}
	return number(float64(a)/float64(domain.Thousand)/1000, 1) + " " + large
}

// number uses a comma and drops the trailing zero: 4,6, not 4,60 or 4.6.
func number(v float64, decimals int) string {
	s := fmt.Sprintf("%.*f", decimals, v)
	if decimals > 0 {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	return strings.Replace(s, ".", ",", 1)
}
