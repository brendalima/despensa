-- +goose Up

-- Every quantity is in thousandths of the item's canonical unit, in a
-- BIGINT: milligrams, microlitres, thousandths of a piece. Integers,
-- because a float's drift over years of small writes is invisible.

CREATE TABLE items (
  id              UUID PRIMARY KEY,
  name            TEXT NOT NULL CHECK (length(trim(name)) > 0),
  canonical_unit  TEXT NOT NULL CHECK (canonical_unit IN ('g', 'ml', 'un')),
  -- Allowed to go negative: that is the signal that the running total
  -- undercounted, and the app shows it as zero.
  on_hand_milli   BIGINT NOT NULL DEFAULT 0,
  threshold_milli BIGINT NOT NULL DEFAULT 0 CHECK (threshold_milli >= 0),
  category        TEXT NOT NULL DEFAULT '',
  archived_at     TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Only live items have to be uniquely named: archiving "arroz" must not
-- stop a new "arroze" from being created later.
CREATE UNIQUE INDEX items_name_key ON items (lower(name)) WHERE archived_at IS NULL;

-- The item's vocabulary. 'seeded' is the model's estimate, written down
-- on first use; 'corrected' is the person's own, which an estimate must
-- never overwrite.
CREATE TABLE portions (
  item_id   UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  name      TEXT NOT NULL CHECK (length(trim(name)) > 0),
  one_milli BIGINT NOT NULL CHECK (one_milli > 0),
  source    TEXT NOT NULL DEFAULT 'seeded' CHECK (source IN ('seeded', 'corrected')),
  PRIMARY KEY (item_id, name)
);

-- The other names the person calls an item by, so "arroz branco" finds
-- "arroz" without any fuzzy matching in code.
CREATE TABLE item_aliases (
  item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  alias   TEXT NOT NULL CHECK (length(trim(alias)) > 0),
  PRIMARY KEY (item_id, alias)
);

-- The ledger is the inventory: items.on_hand_milli is the sum of the
-- deltas here that have not been undone. Undo, history and the forecast
-- all come free from this table being the truth.
--
-- Undo sets undone_at and subtracts the delta rather than appending an
-- inverse row, so the invariant stays one filtered SUM.
CREATE TABLE events (
  id               BIGSERIAL PRIMARY KEY,
  item_id          UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  kind             TEXT NOT NULL CHECK (kind IN ('consume', 'restock', 'correction', 'discard')),
  delta_milli      BIGINT NOT NULL,
  -- What the person said, so a receipt reads back in their own words.
  portion_name     TEXT NOT NULL DEFAULT '',
  portion_qty_milli BIGINT NOT NULL DEFAULT 0,
  raw_message      TEXT NOT NULL DEFAULT '',
  source           TEXT NOT NULL CHECK (source IN ('whatsapp', 'mcp', 'cli')),
  undone_at        TIMESTAMPTZ,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX events_item_idx ON events (item_id, created_at DESC);

-- Idempotency: a redelivered "usei duas fatias" must not take four
-- slices. The row goes in before the model is called, so a repeat is
-- free.
CREATE TABLE processed_messages (
  provider    TEXT NOT NULL,
  message_id  TEXT NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (provider, message_id)
);

-- The shopping list is the low items (derived, never stored) plus these:
-- the things that are not inventory at all.
CREATE TABLE shopping_notes (
  id         BIGSERIAL PRIMARY KEY,
  text       TEXT NOT NULL CHECK (length(trim(text)) > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A short conversation history, so "qual deles?" can be answered by the
-- next message. Rows past the window are never read, so a periodic
-- delete is enough.
CREATE TABLE conversation_turns (
  id         BIGSERIAL PRIMARY KEY,
  chat_id    TEXT NOT NULL,
  role       TEXT NOT NULL,
  content    JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX conversation_turns_chat_idx ON conversation_turns (chat_id, created_at DESC);

-- +goose Down
DROP TABLE conversation_turns;
DROP TABLE shopping_notes;
DROP TABLE processed_messages;
DROP TABLE events;
DROP TABLE item_aliases;
DROP TABLE portions;
DROP TABLE items;
