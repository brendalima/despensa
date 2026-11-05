# despensa

The home pantry inventory, managed by conversation.

The problem is not a shortage of pantry apps — it is that every one of them asks
you to stop, open the app, find the item and type a number **while you are
cooking**. Nobody does that, so the inventory becomes fiction within three days.

Here the interface is a sentence: *"usei duas fatias de pão e uma taça de
arroz"*. The answer is a receipt with an undo button:

```
pão de forma: 18 → 16 fatias
arroz: 5 kg → 4,8 kg (≈27 taças)
```

**A rough estimate is the goal, not a concession.** Nobody is going to weigh a
cup of rice. The model estimates the first time a word is used, the estimate is
written down, and from then on the same word is always worth the same —
consistency matters more than accuracy, because it is what makes thirty logged
uses add up to something.

## How it works

Three ideas; everything else follows from them.

**1. A canonical unit plus named portions.** Each item is counted in `g`, `ml`
or `un`, and carries its own vocabulary: rice knows `taça` (180 g), `colher`
(12 g) and `pacote` (5 kg); bread knows `fatia` (1 un) and `pacote` (22 un).
Buying and using are the same mechanism — a package is just another portion.

**2. The ledger is the inventory.** `items.on_hand_milli` is the sum of the
deltas of the events that have not been undone. Nothing else is the truth, and
that is what gives undo, history and a future forecast for free. Every quantity
is a `BIGINT` in thousandths of the item's canonical unit: integers, because
these numbers are added to and subtracted from for years, and a float's drift
would be invisible.

**3. Negative drift is a signal, not an error.** If the running total goes
below zero, it counted less than went in. The negative value is kept, shown as
zero, and the bot asks how much is there — the only moment it asks for a number,
and it asks for a guess, not a measurement.

**Nothing is confirmed before being written.** The blast radius is one row of a
personal pantry, and the failure mode that kills this product is friction, so
receipts carry the event id to undo instead of a confirmation prompt.

## Layers

Hexagonal, following [`AGENTS.md`](AGENTS.md):

```
cmd ──► agent / bot ──► services ──► repository (interfaces)
                            │
                            └──► domain ◄── (every layer)
                                   ▲
                          humanize ─┘  (the Portuguese the person reads)
```

**Language boundary:** code in English, conversation and data in Portuguese. The
domain holds no sentence meant for a person — an error is raised in English with
a stable code, and `internal/humanize` writes the Portuguese. The architecture
test enforces it: an accented string literal under `internal/core/` fails the
build.

## Running it

Needs Go 1.27+ and a Postgres (Neon is the target).

```bash
cp .env-sample .env     # fill in DATABASE_URL
make migrate
make check              # fmt, vet, staticcheck, tests with -race, govulncheck
```

The CLI is the whole of Phase A, with no model and no WhatsApp in the way — if
the arithmetic or the ledger are wrong, they are wrong here, where there is
nothing else to blame:

```bash
go run ./cmd/despensa add arroz g --portion taça=180 --portion pacote=5000
go run ./cmd/despensa buy arroz pacote      # bought a package
go run ./cmd/despensa use arroz taça 2      # used two cups
go run ./cmd/despensa status
go run ./cmd/despensa undo 3
go run ./cmd/despensa shopping
```

The repository tests talk to a real Postgres and **skip** without
`TEST_DATABASE_URL`. Point it at a throwaway branch: they write and delete rows.

## State

| Phase | Delivers | State |
|---|---|---|
| A | Domain, ledger, repository, services, CLI | done, except running the migrations against a real database |
| B | Tools + MCP server (stdio) | to do |
| C | WhatsApp bot | to do |
| D | Weekly digest | to do |

Picking this up: read [`docs/HANDOFF.md`](docs/HANDOFF.md) first, then
[`docs/plans/despensa.md`](docs/plans/despensa.md) for the phase-by-phase
plan and the reasoning behind each cut.
