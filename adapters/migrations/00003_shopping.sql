-- +goose Up

-- What an item wants from the shopping list, which is the one thing
-- about an item that is a decision rather than a fact:
--
--   auto   follow the minimum, which is what every row here was already
--          doing, so it is the default;
--   never  keep it off the list even when it is low ("I know the rice is
--          low, I am not buying rice now");
--   always keep it on the list even when it is full ("buy coffee anyway
--          while you are there"). This one ends by itself: a restock
--          that clears the minimum puts the item back to auto.
--
-- One column and not two booleans, because two booleans have a fourth
-- state that means nothing.
ALTER TABLE items
  ADD COLUMN shopping_mode TEXT NOT NULL DEFAULT 'auto'
    CHECK (shopping_mode IN ('auto', 'never', 'always'));

-- +goose Down
ALTER TABLE items DROP COLUMN shopping_mode;
