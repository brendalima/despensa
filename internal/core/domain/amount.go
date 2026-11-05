// Package domain holds the entities, the arithmetic and the domain
// errors. No dependency outside the standard library, and no Portuguese:
// the border builds every message from the codes these errors carry.
package domain

import "math"

// Amount is a quantity in thousandths of an item's canonical unit:
// milligrams, microlitres, thousandths of a piece.
//
// Integers, not floats: this number is added to and subtracted from for
// years, and a float's drift would be invisible.
type Amount int64

// Thousand is how many Amount units make one canonical unit.
const Thousand Amount = 1000

// MaxAmount keeps Amount × Qty far from overflowing int64, and a thousand
// kilos of one thing is a typo anyway.
const MaxAmount Amount = 1_000_000 * Thousand

// Qty is a count of portions in thousandths, so two slices is 2000 and
// half a cup is 500.
type Qty int64

const MaxQty Qty = 10_000 * Qty(Thousand)

func AmountOf(units int64) Amount { return Amount(units) * Thousand }

func QtyOf(portions int64) Qty { return Qty(portions) * Qty(Thousand) }

// QtyFromFloat converts a count that arrived as a JSON number. It is one
// of the two float boundaries: the model may say 1.5 cups, and from here
// on that is 1500.
func QtyFromFloat(f float64) Qty {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	rounded := math.Round(f * float64(Thousand))
	if rounded > float64(MaxQty) {
		return MaxQty
	}
	if rounded < -float64(MaxQty) {
		return -MaxQty
	}
	return Qty(rounded)
}

func (q Qty) Float() float64 { return float64(q) / float64(Thousand) }

// AmountFromFloat converts an amount given in canonical units: 180 for
// 180 g, 0.5 for half a piece. The other float boundary.
func AmountFromFloat(f float64) Amount {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	rounded := math.Round(f * float64(Thousand))
	if rounded > float64(MaxAmount) {
		return MaxAmount
	}
	if rounded < -float64(MaxAmount) {
		return -MaxAmount
	}
	return Amount(rounded)
}

func (a Amount) Float() float64 { return float64(a) / float64(Thousand) }

// Scale returns what qty of this portion size comes to. The division is
// last, so there is only one rounding.
func (a Amount) Scale(q Qty) Amount {
	product := int64(a) * int64(q)
	half := int64(Thousand) / 2
	if product < 0 {
		return Amount((product - half) / int64(Thousand))
	}
	return Amount((product + half) / int64(Thousand))
}

// In returns how many portions of size one this Amount comes to. A
// portion worth nothing cannot be counted, so it returns zero.
func (a Amount) In(one Amount) Qty {
	if one == 0 {
		return 0
	}
	product := int64(a) * int64(Thousand)
	half := int64(one) / 2
	if (product < 0) != (one < 0) {
		return Qty((product - half) / int64(one))
	}
	return Qty((product + half) / int64(one))
}

// AtLeastZero is the amount as the person should see it. On-hand may go
// negative — that is the drift signal — but nobody is shown -200 g.
func (a Amount) AtLeastZero() Amount {
	if a < 0 {
		return 0
	}
	return a
}

// Units splits into whole canonical units and the remaining thousandths.
func (a Amount) Units() (whole int64, milli int64) {
	return int64(a) / int64(Thousand), int64(a) % int64(Thousand)
}

// RoundHuman rounds to how a person says it: halves while the number is
// small, whole portions once it is not. Nobody has 25.556 cups of rice.
func (q Qty) RoundHuman() Qty {
	step := Qty(Thousand)
	if q < QtyOf(3) && q > QtyOf(-3) {
		step = Qty(Thousand) / 2
	}
	half := step / 2
	if q < 0 {
		return (q - half) / step * step
	}
	return (q + half) / step * step
}
