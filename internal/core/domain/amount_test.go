package domain_test

import (
	"testing"

	"github.com/brendalima/despensa/internal/core/domain"
)

// cup is 180 g, the seeded size for a cup of rice, as thousandths.
const cup = domain.Amount(180 * domain.Thousand)

func TestScale(t *testing.T) {
	tests := []struct {
		name string
		one  domain.Amount
		qty  domain.Qty
		want domain.Amount
	}{
		{"one cup", cup, domain.QtyOf(1), 180 * domain.Thousand},
		{"two cups", cup, domain.QtyOf(2), 360 * domain.Thousand},
		{"half a cup", cup, domain.QtyFromFloat(0.5), 90 * domain.Thousand},
		{"a third of a cup rounds to nearest", cup, domain.QtyFromFloat(0.333), 59_940},
		{"a package of five kilos", 5000 * domain.Thousand, domain.QtyOf(1), 5_000_000},
		{"two slices of bread", domain.Thousand, domain.QtyOf(2), 2 * domain.Thousand},
		{"half a slice stays exact", domain.Thousand, domain.QtyFromFloat(0.5), 500},
		{"zero", cup, 0, 0},
		{"negative qty", cup, domain.QtyOf(-2), -360 * domain.Thousand},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.one.Scale(tt.qty); got != tt.want {
				t.Errorf("Scale(%d) = %d, want %d", tt.qty, got, tt.want)
			}
		})
	}
}

func TestIn(t *testing.T) {
	tests := []struct {
		name string
		have domain.Amount
		one  domain.Amount
		want domain.Qty
	}{
		{"4.6 kg in cups", 4600 * domain.Thousand, cup, 25_556},
		{"exactly two cups", 360 * domain.Thousand, cup, domain.QtyOf(2)},
		{"16 slices", 16 * domain.Thousand, domain.Thousand, domain.QtyOf(16)},
		{"nothing left", 0, cup, 0},
		{"negative on hand", -360 * domain.Thousand, cup, domain.QtyOf(-2)},
		{"a portion of nothing is not counted", 4600 * domain.Thousand, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.have.In(tt.one); got != tt.want {
				t.Errorf("In() = %d, want %d", got, tt.want)
			}
		})
	}
}

// Scale and In are meant to round-trip for whole portions, because a cup
// logged is a cup shown. Fractions do not have to, and the test says so
// rather than pretending.
func TestScaleInRoundTrip(t *testing.T) {
	for _, one := range []domain.Amount{domain.Thousand, cup, 12 * domain.Thousand, 5000 * domain.Thousand} {
		for n := int64(1); n <= 30; n++ {
			qty := domain.QtyOf(n)
			if got := one.Scale(qty).In(one); got != qty {
				t.Errorf("one=%d: Scale(%d).In() = %d, want %d", one, qty, got, qty)
			}
		}
	}
}

func TestQtyFromFloat(t *testing.T) {
	tests := []struct {
		in   float64
		want domain.Qty
	}{
		{1, domain.QtyOf(1)},
		{2.5, 2500},
		{0.5, 500},
		{0.333, 333},
		{0.0001, 0},
		{-2, domain.QtyOf(-2)},
		{1e9, domain.MaxQty},
		{-1e9, -domain.MaxQty},
	}
	for _, tt := range tests {
		if got := domain.QtyFromFloat(tt.in); got != tt.want {
			t.Errorf("QtyFromFloat(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
	// A float that is not a number must not become a quantity.
	if got := domain.QtyFromFloat(zeroDiv()); got != 0 {
		t.Errorf("QtyFromFloat(NaN) = %d, want 0", got)
	}
}

func zeroDiv() float64 {
	var zero float64
	return zero / zero
}

func TestRoundHuman(t *testing.T) {
	tests := []struct {
		name string
		in   domain.Qty
		want domain.Qty
	}{
		{"25.556 cups is about 26", 25_556, domain.QtyOf(26)},
		{"25.4 cups is about 25", 25_400, domain.QtyOf(25)},
		{"2.4 cups keeps the half", 2_400, 2_500},
		{"2.1 cups rounds to two", 2_100, domain.QtyOf(2)},
		{"0.4 of a package is half", 400, 500},
		{"0.1 is nothing worth saying", 100, 0},
		{"exactly three", domain.QtyOf(3), domain.QtyOf(3)},
		{"negative keeps its sign", -2_400, -2_500},
		{"zero", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.RoundHuman(); got != tt.want {
				t.Errorf("RoundHuman(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestAtLeastZero(t *testing.T) {
	// Negative on-hand is kept in the ledger as a signal, and never
	// shown: "-200 g of rice" is not a thing to tell anyone.
	if got := domain.Amount(-200 * domain.Thousand).AtLeastZero(); got != 0 {
		t.Errorf("AtLeastZero() = %d, want 0", got)
	}
	if got := domain.Amount(500).AtLeastZero(); got != 500 {
		t.Errorf("AtLeastZero() = %d, want 500", got)
	}
}

func TestUnits(t *testing.T) {
	whole, milli := domain.Amount(4_600_500).Units()
	if whole != 4600 || milli != 500 {
		t.Errorf("Units() = %d, %d, want 4600, 500", whole, milli)
	}
}

// Scale must not overflow at the bounds the service allows, because an
// overflow would wrap to a negative on-hand and read as drift.
func TestScaleAtBounds(t *testing.T) {
	got := domain.MaxAmount.Scale(domain.MaxQty)
	if got <= 0 {
		t.Fatalf("Scale at bounds overflowed: got %d", got)
	}
}

func TestUnitValid(t *testing.T) {
	for _, u := range domain.AllUnits() {
		if !u.Valid() {
			t.Errorf("AllUnits contains %q but Valid() says no", u)
		}
	}
	if domain.Unit("kg").Valid() {
		t.Error(`Unit("kg").Valid() = true, want false: kg is a rendering of g, not a unit`)
	}
}

func TestUnitPortion(t *testing.T) {
	bread := domain.Item{
		Unit: domain.UnitPiece,
		Portions: []domain.Portion{
			{Name: "fatia", One: domain.Thousand},
			{Name: "pacote", One: 22 * domain.Thousand},
		},
	}
	p, ok := bread.UnitPortion()
	if !ok || p.Name != "fatia" {
		t.Errorf("UnitPortion() = %q, %v, want fatia, true", p.Name, ok)
	}

	rice := domain.Item{Unit: domain.UnitGram, Portions: []domain.Portion{{Name: "taça", One: cup}}}
	if _, ok := rice.UnitPortion(); ok {
		t.Error("UnitPortion() found one for rice, which is counted in grams")
	}
}

func TestPortionLookupIgnoresCase(t *testing.T) {
	rice := domain.Item{Portions: []domain.Portion{{Name: "taça", One: cup}}}
	if _, ok := rice.Portion("Taça"); !ok {
		t.Error(`Portion("Taça") not found: the same word typed differently is the same word`)
	}
	if _, ok := rice.Portion("concha"); ok {
		t.Error(`Portion("concha") found, want not found`)
	}
}

func TestDriftAndThreshold(t *testing.T) {
	item := domain.Item{OnHand: -200 * domain.Thousand, Threshold: 500 * domain.Thousand}
	if !item.Drifted() {
		t.Error("Drifted() = false for a negative on-hand")
	}
	if item.Display() != 0 {
		t.Errorf("Display() = %d, want 0", item.Display())
	}
	if !item.BelowThreshold() {
		t.Error("BelowThreshold() = false below the threshold")
	}

	plenty := domain.Item{OnHand: 5000 * domain.Thousand, Threshold: 500 * domain.Thousand}
	if plenty.Drifted() || plenty.BelowThreshold() {
		t.Error("a full item reads as drifted or low")
	}

	// A threshold of zero means "tell me when it runs out", so empty is
	// below it and anything left is not.
	empty := domain.Item{OnHand: 0}
	if !empty.BelowThreshold() {
		t.Error("an empty item with no threshold set does not read as low")
	}
}

// The model writes down portions named after the unit — it gave rice a
// portion called "g" and milk one called "ml". Those are the unit under
// its own name, not a word anyone invented, and letting one count as
// the unit portion is what stops 5 kg from reading as 5 kg.
func TestUnitPortionIgnoresAPortionNamedAfterTheUnit(t *testing.T) {
	tests := []struct {
		name     string
		unit     domain.Unit
		portions []domain.Portion
		want     string // the unit portion's name, empty for none
	}{
		{
			name: "a gram item with a portion called g",
			unit: domain.UnitGram,
			portions: []domain.Portion{
				{Name: "g", One: domain.Thousand},
				{Name: "taça", One: 180 * domain.Thousand},
			},
			want: "",
		},
		{
			name:     "case and padding do not get around it",
			unit:     domain.UnitMillilitre,
			portions: []domain.Portion{{Name: " ML ", One: domain.Thousand}},
			want:     "",
		},
		{
			name: "a real word worth one unit still wins",
			unit: domain.UnitPiece,
			portions: []domain.Portion{
				{Name: "fatia", One: domain.Thousand},
				{Name: "pacote", One: 22 * domain.Thousand},
			},
			want: "fatia",
		},
		{
			name: "un on a piece item adds nothing either",
			unit: domain.UnitPiece,
			portions: []domain.Portion{
				{Name: "un", One: domain.Thousand},
				{Name: "cartela", One: 20 * domain.Thousand},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := domain.Item{Unit: tt.unit, Portions: tt.portions}
			got, ok := item.UnitPortion()
			if tt.want == "" {
				if ok {
					t.Errorf("UnitPortion() = %q, want none", got.Name)
				}
				return
			}
			if !ok || got.Name != tt.want {
				t.Errorf("UnitPortion() = %q/%v, want %q", got.Name, ok, tt.want)
			}
		})
	}
}
