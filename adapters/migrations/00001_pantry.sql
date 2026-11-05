-- +goose Up

-- Every quantity in this schema is in thousandths of the item's canonical
-- unit, in a BIGINT: milligrams, microlitres, or thousandths of a piece.
-- Integers because these numbers are added to and subtracted from a few
-- times a day for years, and a float's drift would be invisible.

CREATE TABLE items (
  id              UUID PRIMARY KEY,
  name            TEXT NOT NULL CHECK (length(trim(name)) > 0),
  canonical_unit  TEXT NOT NULL CHECK (canonical_unit IN ('g', 'ml', 'un')),
  -- Deliberately allowed to go negative: a negative on-hand means the
  -- running total undercounted, and that is the signal to ask for a
  -- rough count. The application shows it as zero.
  on_hand_milli   BIGINT NOT NULL DEFAULT 0,
  threshold_milli BIGINT NOT NULL DEFAULT 0 CHECK (threshold_milli >= 0),
  category        TEXT NOT NULL DEFAULT '',
  archived_at     TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Only live items have to be uniquely named: archiving "arroz" must not
-- stop a new "arroze" from being created later.
CREATE UNIQUE INDEX items_name_key ON items (lower(name)) WHERE archived_at IS NULL;

-- The item's vocabulary: the words the person uses for an amount, and
-- what one of them is worth. 'seeded' is the model's estimate, written
-- down on first use; 'corrected' is a size the person gave, which an
-- estimate must never overwrite.
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
-- deltas of the rows here that have not been undone. Everything else —
-- undo, "how much rice do I use per month", the forecast later on — comes
-- free from this one table being the truth.
--
-- Undo sets undone_at and subtracts the delta; it does not append an
-- inverse row, so the ledger stays a record of what was said and the
-- invariant stays one filtered SUM.
CREATE TABLE events (
  id               BIGSERIAL PRIMARY KEY,
  item_id          UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  kind             TEXT NOT NULL CHECK (kind IN ('consume', 'restock', 'correction', 'discard')),
  delta_milli      BIGINT NOT NULL,
  -- What the person said, kept next to the arithmetic: a receipt can be
  -- read back in their own words, and a portion later corrected can be
  -- explained instead of just silently changing the totals.
  portion_name     TEXT NOT NULL DEFAULT '',
  portion_qty_milli BIGINT NOT NULL DEFAULT 0,
  raw_message      TEXT NOT NULL DEFAULT '',
  source           TEXT NOT NULL CHECK (source IN ('whatsapp', 'mcp', 'cli')),
  undone_at        TIMESTAMPTZ,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX events_item_idx ON events (item_id, created_at DESC);

-- Idempotency. Meta redelivers anything it does not get a quick 200 for,
-- and a redelivered "usei duas fatias" must not take four slices. The row
-- goes in before the model is called, so a repeat costs nothing at all.
CREATE TABLE processed_messages (
  provider    TEXT NOT NULL,
  message_id  TEXT NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (provider, message_id)
);

-- The shopping list is items at or below their threshold (derived, never
-- stored) plus these: the things that are not inventory at all, like
-- "fermento" or "papel toalha".
CREATE TABLE shopping_notes (
  id         BIGSERIAL PRIMARY KEY,
  text       TEXT NOT NULL CHECK (length(trim(text)) > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A short conversation history, so "qual deles?" can be answered by the
-- next message. Rows older than the window are never read; a periodic
-- delete is enough and there is no need for a TTL mechanism.
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
