package bot

import (
	"strings"
	"testing"
)

// The prompt is the product, the same way the tool descriptions are. A
// bot that confirms every cup of rice, or that asks how much a cup
// weighs, works perfectly and is useless — so the sentences that invert
// what a model would otherwise do are pinned here, and deleting one
// fails the build instead of showing up as a bad month of estimates.
func TestThePromptCarriesTheRulesThatInvertTheObvious(t *testing.T) {
	want := []string{
		"list_inventory antes de escrever",
		"sem pedir confirmação",
		"botão Desfazer",
		"Nunca pergunte quanto pesa",
		"portion_base_hint",
		"uma chamada só de record_events",
		"new_item",
		"pergunte quanto",
		"drift: true",
		"Uma ou duas frases",
	}

	for _, phrase := range want {
		if !strings.Contains(systemPrompt, phrase) {
			t.Errorf("the prompt no longer says %q", phrase)
		}
	}
}

// Numbers arrive as digits and are configured as a person writes them.
func TestNumbersAreComparedAsDigits(t *testing.T) {
	cases := map[string]string{
		"+55 11 99999-0000": "5511999990000",
		"5511999990000":     "5511999990000",
		"(11) 99999 0000":   "11999990000",
	}

	for in, want := range cases {
		if got := normalizeNumber(in); got != want {
			t.Errorf("normalizeNumber(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUndoButtonPayloadRoundTrips(t *testing.T) {
	id, ok := undoTarget(UndoButtonID(42))
	if !ok || id != 42 {
		t.Errorf("undoTarget(UndoButtonID(42)) = %d, %v", id, ok)
	}

	for _, payload := range []string{"", "undo:", "undo:zero", "undo:-1", "undo:0", "list:1", "42"} {
		if _, ok := undoTarget(payload); ok {
			t.Errorf("undoTarget(%q) accepted a payload that is not an event", payload)
		}
	}
}
