-- +goose Up

-- The weekly digest is the only message this service starts, and outside
-- WhatsApp's 24-hour window Meta accepts nothing but an approved
-- template. This table is the fallback for when there is no approved
-- template yet, or when Meta refuses the send: it records that a digest
-- was owed, and the next inbound message pays the debt.
--
-- Only the fact is stored, never the text. The shopping list is derived
-- on every read, so a digest owed on Sunday and delivered on Tuesday
-- should say what the pantry holds on Tuesday — a stored sentence would
-- be the one thing in this product that can disagree with the ledger.
CREATE TABLE pending_digests (
  id           BIGSERIAL PRIMARY KEY,
  chat_id      TEXT NOT NULL CHECK (length(trim(chat_id)) > 0),
  due_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  delivered_at TIMESTAMPTZ
);

-- One debt per chat. Three weeks without an approved template owe one
-- digest, not three: the list is the same list, and three copies of it
-- on the next message would read as a broken bot. The partial index is
-- what makes "owe a digest" idempotent, the same way processed_messages
-- makes claiming a delivery idempotent.
CREATE UNIQUE INDEX pending_digests_chat_key
  ON pending_digests (chat_id) WHERE delivered_at IS NULL;

-- +goose Down
DROP TABLE pending_digests;
