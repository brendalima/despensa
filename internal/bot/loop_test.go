package bot_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/brendalima/despensa/internal/agent"
	"github.com/brendalima/despensa/internal/bot"
	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/humanize"
)

// Anyone who can send a message can write to the pantry, so this guard
// is the security of the service. It runs before the model, before the
// idempotency claim and before any read of the database.
func TestNumberOutsideTheListCostsNothing(t *testing.T) {
	h := newHarness(t, answer("pronto"))

	msg := text("usei uma taça de arroz")
	msg.From = stranger
	msg.ChatID = stranger

	h.bot.Handle(context.Background(), msg)

	if h.model.calls() != 0 {
		t.Errorf("the model was called %d times for a number outside the list", h.model.calls())
	}
	if h.chat.claims != 0 {
		t.Errorf("the message was claimed %d times for a number outside the list", h.chat.claims)
	}
	if len(h.pantry.recorded) != 0 {
		t.Errorf("a stranger wrote %d events", len(h.pantry.recorded))
	}
	if got := h.messenger.last(t); got.text != humanize.NotForYou() {
		t.Errorf("answered a stranger with %q", got.text)
	}
}

// WhatsApp redelivers. A second run would log the same cup of rice
// twice, so the claim comes before the model and before any write.
func TestRedeliveryIsHandledOnce(t *testing.T) {
	h := newHarness(t, answer("pronto"))

	msg := text("usei uma taça de arroz")
	h.bot.Handle(context.Background(), msg)
	h.bot.Handle(context.Background(), msg)

	if h.model.calls() != 1 {
		t.Errorf("the model was called %d times for one message", h.model.calls())
	}
	if len(h.messenger.sent) != 1 {
		t.Errorf("answered %d times for one message", len(h.messenger.sent))
	}
}

// A model that never stops asking for tools is a bug, and the bill is
// how anyone finds out. The cap is what turns it into one odd reply.
func TestToolIterationsAreCapped(t *testing.T) {
	h := newHarness(t, toolCall("list_inventory", `{}`))

	h.bot.Handle(context.Background(), text("usei uma taça de arroz"))

	if h.model.calls() != 3 {
		t.Errorf("the model was called %d times, want the cap of 3", h.model.calls())
	}
	if got := h.messenger.last(t); got.text != humanize.GotLost() {
		t.Errorf("after the cap the answer was %q", got.text)
	}
}

// The button payload is the whole state. Sending it to the model would
// pay for a round trip to learn what the id already says.
func TestUndoButtonSkipsTheModel(t *testing.T) {
	h := newHarness(t)

	msg := text("")
	msg.Choice = bot.UndoButtonID(7)
	h.bot.Handle(context.Background(), msg)

	if h.model.calls() != 0 {
		t.Errorf("the model was called %d times for a button", h.model.calls())
	}
	if len(h.pantry.undone) != 1 || h.pantry.undone[0] != 7 {
		t.Fatalf("undone events: %v, want [7]", h.pantry.undone)
	}
	if got := h.messenger.last(t); !strings.HasPrefix(got.text, "Desfeito.") {
		t.Errorf("the undo answered %q", got.text)
	}
	if got := h.messenger.last(t); len(got.buttons) != 0 {
		t.Errorf("the undo came back with %d buttons", len(got.buttons))
	}
}

// Nothing is confirmed before a write, so the receipt is what makes a
// wrong write harmless: one button per event, carrying its id.
func TestReceiptCarriesADesfazerButton(t *testing.T) {
	args := `{"entries":[{"item_id":"` + riceID + `","kind":"consume","portion_name":"taça","portion_qty":1}]}`
	h := newHarness(t, toolCall("record_events", args), answer("arroz: 5 kg → 4,8 kg"))

	h.bot.Handle(context.Background(), text("usei uma taça de arroz"))

	if len(h.pantry.recorded) != 1 {
		t.Fatalf("recorded %d events, want 1", len(h.pantry.recorded))
	}
	got := h.messenger.last(t)
	if len(got.buttons) != 1 {
		t.Fatalf("the receipt came back with %d buttons, want 1", len(got.buttons))
	}
	if want := bot.UndoButtonID(100); got.buttons[0].ID != want {
		t.Errorf("button payload %q, want %q", got.buttons[0].ID, want)
	}
	if got.buttons[0].Label != humanize.UndoLabel {
		t.Errorf("button label %q", got.buttons[0].Label)
	}
}

// A refusal has to reach the model as a result it can read, or the
// person is left with nothing where a correction would have done.
func TestToolRefusalComesBackAsAResult(t *testing.T) {
	// portion_qty is missing, which the schema refuses before the
	// handler and before any transaction.
	h := newHarness(t,
		toolCall("record_events", `{"entries":[{"item_id":"`+riceID+`","kind":"consume","portion_name":"taça"}]}`),
		answer("quantas taças?"))

	h.bot.Handle(context.Background(), text("usei arroz"))

	if len(h.pantry.recorded) != 0 {
		t.Errorf("a refused call wrote %d events", len(h.pantry.recorded))
	}
	if h.model.calls() != 2 {
		t.Fatalf("the model was called %d times, want 2", h.model.calls())
	}

	turns := h.model.requests[1].Turns
	last := turns[len(turns)-1]
	if len(last.ToolResults) != 1 {
		t.Fatalf("the model got %d tool results back", len(last.ToolResults))
	}
	if !last.ToolResults[0].IsError {
		t.Error("the refusal did not come back as an error result")
	}
	if got := h.messenger.last(t); got.text != "quantas taças?" {
		t.Errorf("answered %q", got.text)
	}
}

// The history is what lets "e mais uma" still mean rice a minute later,
// and it is stored as the turns themselves so the next request replays
// what the model actually said.
func TestTurnsAreRemembered(t *testing.T) {
	h := newHarness(t, answer("pronto"))

	h.bot.Handle(context.Background(), text("usei uma taça de arroz"))

	if len(h.chat.remembered) != 2 {
		t.Fatalf("stored %d turns, want the question and the answer", len(h.chat.remembered))
	}
	if h.chat.remembered[0].Role != domain.TurnUser {
		t.Errorf("first stored turn is %q", h.chat.remembered[0].Role)
	}

	var stored bot.Turn
	if err := json.Unmarshal(h.chat.remembered[1].Content, &stored); err != nil {
		t.Fatalf("couldn't decode a stored turn: %v", err)
	}
	if stored.Text != "pronto" {
		t.Errorf("stored answer %q", stored.Text)
	}
}

// What came before is read back and handed to the model.
func TestHistoryReachesTheModel(t *testing.T) {
	h := newHarness(t, answer("pronto"))
	h.chat.history = []domain.Turn{{
		ChatID:  mine,
		Role:    domain.TurnUser,
		Content: []byte(`{"role":"user","text":"usei uma taça de arroz"}`),
	}}

	h.bot.Handle(context.Background(), text("e mais uma"))

	turns := h.model.requests[0].Turns
	if len(turns) != 2 {
		t.Fatalf("the model got %d turns, want the stored one and the new one", len(turns))
	}
	if turns[0].Text != "usei uma taça de arroz" {
		t.Errorf("the first turn was %q", turns[0].Text)
	}
}

// A photo, a tap on something that is gone: nothing to act on, and no
// reason to pay for a model call to find that out. An audio is no
// longer in this list — it has words in it, they just have to be
// fetched first.
func TestMessageWithNothingInItNeverReachesTheModel(t *testing.T) {
	h := newHarness(t, answer("pronto"))

	h.bot.Handle(context.Background(), text("   "))

	if h.model.calls() != 0 {
		t.Errorf("the model was called %d times for an empty message", h.model.calls())
	}
	if got := h.messenger.last(t); got.text != humanize.NothingToRead() {
		t.Errorf("answered %q", got.text)
	}
}

// Six tools and the whole wording of the prompt go to the model on
// every call. The tools come from internal/agent, never from here.
func TestTheModelGetsTheToolsAndThePrompt(t *testing.T) {
	h := newHarness(t, answer("pronto"))

	h.bot.Handle(context.Background(), text("o que tem na despensa?"))

	req := h.model.requests[0]
	if len(req.Tools) != 7 {
		t.Errorf("the model got %d tools, want 7", len(req.Tools))
	}
	if !strings.Contains(req.System, "list_inventory") {
		t.Error("the prompt does not tell the model to read the pantry first")
	}
}

// The receipt that broke in production: two items, two buttons, and
// Meta refusing the message because both titles read "Desfazer".
func TestASentenceNamingTwoItemsGetsDistinguishableButtons(t *testing.T) {
	h := newHarness(t,
		toolCall(agent.ToolRecordEvents, `{"entries":[
			{"kind":"consume","portion_name":"taça","portion_qty":1,"item_id":"`+riceID+`"},
			{"kind":"consume","portion_name":"fatia","portion_qty":2,"item_id":"22222222-2222-2222-2222-222222222222"}
		]}`),
		answer("anotado"))

	h.bot.Handle(context.Background(), text("usei uma taça de arroz e duas fatias de pão"))

	got := h.messenger.last(t)
	if len(got.buttons) != 2 {
		t.Fatalf("got %d buttons, want one per event: %+v", len(got.buttons), got.buttons)
	}

	// The ids were always unique; the titles were not, and the titles
	// are what Meta rejects.
	if got.buttons[0].Label == got.buttons[1].Label {
		t.Errorf("both buttons read %q — Meta refuses that with 131009", got.buttons[0].Label)
	}
	if got.buttons[0].ID == got.buttons[1].ID {
		t.Errorf("both buttons carry %q", got.buttons[0].ID)
	}
	// A label that does not name its item is no use to a person either.
	for _, want := range []string{"arroz", "pão"} {
		found := false
		for _, b := range got.buttons {
			if strings.Contains(b.Label, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no button names %q: %+v", want, got.buttons)
		}
	}
}

// A single event keeps the plain label: there is nothing to tell apart.
func TestOneEventKeepsThePlainUndoLabel(t *testing.T) {
	h := newHarness(t,
		toolCall(agent.ToolRecordEvents, `{"entries":[{"kind":"consume","portion_name":"taça","portion_qty":1,"item_id":"`+riceID+`"}]}`),
		answer("anotado"))

	h.bot.Handle(context.Background(), text("usei uma taça de arroz"))

	got := h.messenger.last(t)
	if len(got.buttons) != 1 {
		t.Fatalf("got %d buttons, want 1", len(got.buttons))
	}
	if got.buttons[0].Label != humanize.UndoLabel {
		t.Errorf("label = %q, want the plain %q", got.buttons[0].Label, humanize.UndoLabel)
	}
}

// The bug that actually hurt. The writes had already happened when the
// send was refused, and the person was told "não anotei nada" about two
// rows sitting in the ledger — so the natural next move is to re-send
// the sentence and log everything twice.
func TestARefusedReceiptStillSaysWhatWasWritten(t *testing.T) {
	h := newHarness(t,
		toolCall(agent.ToolRecordEvents, `{"entries":[
			{"kind":"consume","portion_name":"taça","portion_qty":1,"item_id":"`+riceID+`"},
			{"kind":"consume","portion_name":"fatia","portion_qty":2,"item_id":"22222222-2222-2222-2222-222222222222"}
		]}`),
		answer("arroz: 5 kg → 4,8 kg"))
	h.messenger.buttonErr = errors.New(`whatsapp refused the message: 400: (#131009) Duplicate button title`)

	h.bot.Handle(context.Background(), text("usei uma taça de arroz e duas fatias de pão"))

	if len(h.recorded()) != 2 {
		t.Fatalf("the test needs both writes to have happened, got %d", len(h.recorded()))
	}

	got := h.messenger.last(t)
	if strings.Contains(got.text, "anotei nada") {
		t.Errorf("the person was told nothing was written, about rows that were:\n%s", got.text)
	}
	if got.text == humanize.Trouble() {
		t.Error("a refused button turned into humanize.Trouble()")
	}
	if !strings.Contains(got.text, "arroz") {
		t.Errorf("the receipt itself did not arrive: %q", got.text)
	}
	if len(got.buttons) != 0 {
		t.Errorf("the fallback should be plain text, got %d buttons", len(got.buttons))
	}
}

// A ten-item inventory truncated the tool call mid-array five times:
// the arguments arrived as {}, the schema refused them for "missing
// properties: [entries]", and the loop spent its whole budget retrying
// a call that truncates in the same place every time.
//
// A truncated turn is now neither run nor stored.
func TestATruncatedTurnIsNeitherRunNorStored(t *testing.T) {
	h := newHarness(t)
	h.model.replies = []bot.Turn{{
		Role:      domain.TurnAssistant,
		ToolCalls: []bot.ToolCall{{ID: "call-1", Name: agent.ToolRecordEvents, Args: json.RawMessage(`{}`)}},
		Truncated: true,
	}}

	h.bot.Handle(context.Background(), text("anota aí o que eu tenho na despensa hoje: ..."))

	// Running a call whose arguments stop mid-array could write the
	// wrong thing, so it must not reach the pantry at all.
	if len(h.recorded()) != 0 {
		t.Errorf("a truncated tool call reached the pantry: %+v", h.recorded())
	}
	// One model call, not six: retrying truncates in the same place.
	if h.model.calls() != 1 {
		t.Errorf("the loop made %d model calls on a truncated turn, want 1", h.model.calls())
	}
	// An assistant tool_use with no tool_result makes the next request
	// invalid for this chat until the window forgets it.
	for _, turn := range h.chat.remembered {
		var stored bot.Turn
		if err := json.Unmarshal(turn.Content, &stored); err != nil {
			t.Fatal(err)
		}
		if len(stored.ToolCalls) > 0 {
			t.Errorf("a turn with an unanswered tool call was stored: %s", turn.Content)
		}
	}
	// And the person is told to check rather than given a receipt.
	if got := h.messenger.last(t).text; got != humanize.GotLost() {
		t.Errorf("answer = %q, want GotLost()", got)
	}
}

// A household is more than one phone. Everyone on the list writes to
// the same pantry, and nobody off it does.
func TestEveryNumberOnTheListIsLetIn(t *testing.T) {
	use := func(qty string) bot.Turn {
		return toolCall(agent.ToolRecordEvents,
			`{"entries":[{"kind":"consume","portion_name":"taça","portion_qty":`+qty+`,"item_id":"`+riceID+`"}]}`)
	}
	h := newHouseholdHarness(t, use("1"), answer("anotado"), use("2"), answer("anotado"))
	ctx := context.Background()

	h.bot.Handle(ctx, from(mine, "wamid.her", "usei uma taça de arroz"))
	h.bot.Handle(ctx, from(spouse, "wamid.his", "usei duas taças de arroz"))

	if len(h.recorded()) != 2 {
		t.Fatalf("the pantry took %d writes, want one from each phone", len(h.recorded()))
	}
	if h.model.calls() != 4 {
		t.Errorf("%d model calls, want a tool round trip per message", h.model.calls())
	}
	for _, sent := range h.messenger.sent {
		if sent.text == humanize.NotForYou() {
			t.Error("someone on the list was turned away")
		}
	}
}

// Adding a second phone must not open the door. The guard is the only
// thing between a stranger and the household's pantry.
func TestAStrangerIsStillRefusedWithTwoNumbersAllowed(t *testing.T) {
	h := newHouseholdHarness(t)

	h.bot.Handle(context.Background(), from(stranger, "wamid.them", "usei uma taça de arroz"))

	if h.model.calls() != 0 {
		t.Errorf("a stranger cost %d model calls", h.model.calls())
	}
	if len(h.recorded()) != 0 {
		t.Errorf("a stranger wrote to the pantry: %+v", h.recorded())
	}
	if got := h.messenger.last(t).text; got != humanize.NotForYou() {
		t.Errorf("answer = %q, want the neutral refusal", got)
	}
}

// Two people talking at once are two conversations. "e mais uma taça"
// from one of them must not be answered with the other's rice.
func TestEachPhoneHasItsOwnHistory(t *testing.T) {
	h := newHouseholdHarness(t, answer("anotado"), answer("anotado"))
	ctx := context.Background()

	h.bot.Handle(ctx, from(mine, "wamid.her", "usei uma taça de arroz"))
	h.bot.Handle(ctx, from(spouse, "wamid.his", "e mais uma"))

	for _, turn := range h.chat.remembered {
		if turn.ChatID != mine && turn.ChatID != spouse {
			t.Errorf("a turn was stored against %q", turn.ChatID)
		}
	}
	// The second message's request must not carry the first person's
	// turns: the fake chat keys history by nothing, so what this pins
	// is that the bot asked for the history of the chat it answered.
	if len(h.chat.historyFor) != 2 {
		t.Fatalf("history was read for %v, want one read per chat", h.chat.historyFor)
	}
	if h.chat.historyFor[0] != mine || h.chat.historyFor[1] != spouse {
		t.Errorf("history read for %v, want [%s %s]", h.chat.historyFor, mine, spouse)
	}
}

// normalizeNumber is unit-tested elsewhere; this is about the list.
// Every entry has to be normalised, not just the first: the spouse's
// number is written into the configuration with spaces, brackets and a
// stray space at each end, and WhatsApp sends bare digits.
func TestEveryEntryInTheListIsNormalised(t *testing.T) {
	h := newHouseholdHarness(t, answer("anotado"))

	h.bot.Handle(context.Background(), from("5521988887777", "wamid.his", "usei uma taça de arroz"))

	if got := h.messenger.last(t).text; got == humanize.NotForYou() {
		t.Error("a number written with spaces and punctuation in the configuration was not matched")
	}
}

// An audio is a sentence nobody typed. Once it is heard, the message
// is indistinguishable from a typed one: the model reads the
// transcript, and the answer is the ordinary receipt.
func TestAnAudioReachesTheModelAsTheWordsInIt(t *testing.T) {
	h := newHarness(t, answer("anotei uma taça de arroz"))
	h.ears.text = "usei uma taça de arroz"

	h.bot.Handle(context.Background(), voice(mine, messageID))

	if h.model.calls() != 1 {
		t.Fatalf("the model was called %d times", h.model.calls())
	}
	turns := h.model.requests[0].Turns
	if got := turns[len(turns)-1].Text; got != "usei uma taça de arroz" {
		t.Errorf("the model read %q", got)
	}
	if got := h.messenger.last(t); got.text != "anotei uma taça de arroz" {
		t.Errorf("answered %q", got.text)
	}
}

// The media type travels with the bytes: the transcriber picks its
// decoder from it, and an audio offered as nothing at all is refused
// by the API.
func TestTheTranscriberIsToldWhatKindOfAudioItIs(t *testing.T) {
	h := newHarness(t, answer("pronto"))
	h.media.mediaType = "audio/mpeg"

	h.bot.Handle(context.Background(), voice(mine, messageID))

	if len(h.ears.heard) != 1 || h.ears.heard[0] != "audio/mpeg" {
		t.Errorf("the transcriber heard %v", h.ears.heard)
	}
	if len(h.media.fetched) != 1 || h.media.fetched[0] != "media-"+messageID {
		t.Errorf("fetched %v", h.media.fetched)
	}
}

// A transcription that fails is not a failure of the pantry. The
// person is asked for text, the model is never called, and nothing is
// written.
func TestAnAudioThatCannotBeHeardAsksForText(t *testing.T) {
	h := newHarness(t, answer("pronto"))
	h.ears.err = errors.New("the transcriber heard nothing")

	h.bot.Handle(context.Background(), voice(mine, messageID))

	if h.model.calls() != 0 {
		t.Errorf("the model was called %d times for an audio nobody heard", h.model.calls())
	}
	if len(h.recorded()) != 0 {
		t.Errorf("an unheard audio wrote %d events", len(h.recorded()))
	}
	if got := h.messenger.last(t); got.text != humanize.CouldntHear() {
		t.Errorf("answered %q", got.text)
	}
}

// A download that fails answers the same way: from where the person
// stands there is no difference between an audio that did not arrive
// and one that was not understood.
func TestAnAudioThatCannotBeFetchedAsksForText(t *testing.T) {
	h := newHarness(t, answer("pronto"))
	h.media.err = errors.New("the media api answered 404")

	h.bot.Handle(context.Background(), voice(mine, messageID))

	if h.ears.calls != 0 {
		t.Errorf("the transcriber was called %d times without any bytes", h.ears.calls)
	}
	if got := h.messenger.last(t); got.text != humanize.CouldntHear() {
		t.Errorf("answered %q", got.text)
	}
}

// Meta redelivers anything it is not told arrived in time. The claim
// comes before the download and before the transcription precisely so
// that the second copy of a voice note costs nothing.
func TestARedeliveredAudioIsNeverTranscribedTwice(t *testing.T) {
	h := newHarness(t, answer("pronto"), answer("pronto"))

	h.bot.Handle(context.Background(), voice(mine, messageID))
	h.bot.Handle(context.Background(), voice(mine, messageID))

	if h.ears.calls != 1 {
		t.Errorf("the transcriber was called %d times for one voice note", h.ears.calls)
	}
	if len(h.media.fetched) != 1 {
		t.Errorf("the audio was fetched %d times", len(h.media.fetched))
	}
}

// An audio from a number outside the list costs nothing at all: not a
// download, not a transcription, not a model call.
func TestAnAudioFromAStrangerCostsNothing(t *testing.T) {
	h := newHarness(t, answer("pronto"))

	h.bot.Handle(context.Background(), voice(stranger, messageID))

	if len(h.media.fetched) != 0 || h.ears.calls != 0 || h.model.calls() != 0 {
		t.Errorf("a stranger's audio cost %d fetches, %d transcriptions and %d model calls",
			len(h.media.fetched), h.ears.calls, h.model.calls())
	}
	if got := h.messenger.last(t); got.text != humanize.NotForYou() {
		t.Errorf("answered %q", got.text)
	}
}
