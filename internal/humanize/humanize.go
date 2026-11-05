// Package humanize turns the domain's exact numbers into the Portuguese
// a person reads. It is the border: the core holds no sentence meant for
// anyone, and every accented string in this project lives here or in the
// tool descriptions, which are prompt.
//
// It depends on the domain and the standard library, and on nothing else.
package humanize

import (
	"fmt"
	"strings"

	"github.com/brendalima/despensa/internal/core/domain"
)

// Quantity renders an amount the way the person would say it: the item's
// own word when it has one ("16 fatias"), and otherwise the unit, scaled
// to whatever reads best ("4,6 kg", "180 g").
func Quantity(item domain.Item, a domain.Amount) string {
	a = a.AtLeastZero()

	// An item counted in pieces is counted in the person's word for the
	// piece, when it has one: "16 fatias", not "16 un".
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

// Secondary is the parenthetical that makes a weight mean something in
// the kitchen: 4,6 kg of rice is "≈26 taças". It is empty when no portion
// gives a number worth saying.
func Secondary(item domain.Item, a domain.Amount) string {
	a = a.AtLeastZero()
	unitPortion, hasUnit := item.UnitPortion()

	var best domain.Portion
	for _, p := range item.Portions {
		// The primary rendering already counts in the unit portion.
		if hasUnit && p.Name == unitPortion.Name {
			continue
		}
		count := a.In(p.One).RoundHuman()
		// A count nobody would use is worse than no count: "0,7 pacotes"
		// and "383 colheres" both say less than the weight already did.
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

// irregular holds the plurals the rules below would get wrong. Kitchen
// words are regular with one notable exception, and a wrong plural in a
// receipt is the kind of small wrongness that makes a bot feel broken.
var irregular = map[string]string{
	"pão": "pães",
	"mão": "mãos",
}

// Plural inflects a Portuguese noun. The person invents these words
// ("taça", "concha", "colher"), so the rules have to cover ordinary
// kitchen nouns rather than the whole language.
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
		// "pires", "lápis": already plural in form, and invariant.
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

// scaled renders an amount in the small unit below a thousand and the
// large one above, which is how anyone would read it: 180 g, then 4,6 kg.
func scaled(a domain.Amount, small, large string) string {
	whole, _ := a.Units()
	if whole < 1000 {
		return number(float64(a)/float64(domain.Thousand), 0) + " " + small
	}
	return number(float64(a)/float64(domain.Thousand)/1000, 1) + " " + large
}

// number formats with up to decimals places, a comma for the decimal
// separator, and no trailing zero: 4,6 and not 4,60 or 4.6.
func number(v float64, decimals int) string {
	s := fmt.Sprintf("%.*f", decimals, v)
	if decimals > 0 {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	return strings.Replace(s, ".", ",", 1)
}
