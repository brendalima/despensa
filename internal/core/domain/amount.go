// Package domain holds the entities, the arithmetic and the domain
// errors. It has no dependency outside the standard library, and no
// Portuguese: every message the user reads is built at the border, from
// the stable codes these errors carry.
package domain

import "math"

// Amount is a quantity of one item, in thousandths of that item's
// canonical unit: milligrams, microlitres, or thousandths of a piece.
//
// Integers, not floats, because this number is added to and subtracted
// from a few times a day for years. A float would drift, and the drift
// would be invisible — exactly the failure this product exists to avoid.
// Thousandths, because half a slice and a third of a cup have to stay
// exact, and a thousandth of a gram is far below anything a kitchen
// notices.
type Amount int64

// Thousand is how many Amount units make one canonical unit. One gram is
// Thousand, one slice is Thousand.
const Thousand Amount = 1000

// MaxAmount is a sanity bound: a thousand kilos of one item in a home
// pantry is a typo, not a purchase. It also keeps Amount × Qty far from
// overflowing int64.
const MaxAmount Amount = 1_000_000 * Thousand

// Qty is a count of portions, in thousandths, so "two slices" is 2000 and
// "half a cup" is 500. Same reasoning as Amount.
type Qty int64

// MaxQty bounds how many portions one event can move.
const MaxQty Qty = 10_000 * Qty(Thousand)

// AmountOf returns the Amount of whole canonical units.
func AmountOf(units int64) Amount { return Amount(units) * Thousand }

// QtyOf returns the Qty of whole portions.
func QtyOf(portions int64) Qty { return Qty(portions) * Qty(Thousand) }

// QtyFromFloat converts a portion count that arrived as a JSON number
// into exact thousandths. It is the only place a float becomes a
// quantity: the model may say 1.5 cups, and from here on that is 1500 and
// never 1.4999999999999998.
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

// Float returns the portion count as a float, for a JSON response. It is
// the inverse of QtyFromFloat and the only other float boundary.
func (q Qty) Float() float64 { return float64(q) / float64(Thousand) }

// AmountFromFloat converts an amount given in canonical units — 180 for
// 180 g, 0.5 for half a piece — into exact thousandths. Same reasoning as
// QtyFromFloat: the float stops here.
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

// Float returns the amount in canonical units.
func (a Amount) Float() float64 { return float64(a) / float64(Thousand) }

// Scale returns the Amount that qty of this portion size comes to:
// 2.5 cups of 180 g each is 450 g. The division by Thousand happens last,
// so the only rounding is the final one, and it rounds to nearest.
func (a Amount) Scale(q Qty) Amount {
	product := int64(a) * int64(q)
	half := int64(Thousand) / 2
	if product < 0 {
		return Amount((product - half) / int64(Thousand))
	}
	return Amount((product + half) / int64(Thousand))
}

// In returns how many portions of size one this Amount comes to: 4600 g
// in cups of 180 g is 25.556 cups. Zero portion size returns zero rather
// than dividing, because a portion of nothing cannot be counted.
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

// AtLeastZero is the Amount as the user should see it. On-hand is allowed
// to go negative — that is the signal that the running total drifted
// below what is really in the cupboard — but "-200 g of rice" is not a
// thing to show anyone.
func (a Amount) AtLeastZero() Amount {
	if a < 0 {
		return 0
	}
	return a
}

// Units returns whole canonical units and the remaining thousandths, for
// a caller formatting the number.
func (a Amount) Units() (whole int64, milli int64) {
	return int64(a) / int64(Thousand), int64(a) % int64(Thousand)
}

// RoundHuman rounds a portion count to how a person says it: halves while
// the number is small enough for a half to mean something, whole portions
// once it is not. Nobody has 25.556 cups of rice; they have about 26.
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
