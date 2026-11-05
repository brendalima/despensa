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

## The tools, and trying them with a real model

Phase B is the six tools (`internal/agent`) and an MCP server over stdio
(`cmd/mcp`). It comes before WhatsApp because it is free to try: the host is
your own Claude Desktop or Claude Code, so the model calls are on your
subscription and this binary holds no API key.

```bash
claude mcp add despensa -- env DATABASE_URL="$DATABASE_URL" go run ./cmd/mcp
```

Then talk to it. What is worth checking is the behaviour that is easy to get
backwards:

| Say | What should happen |
|---|---|
| *"usei duas fatias de pão e uma taça de arroz"* | one `record_events` call with two entries, one receipt |
| *"comprei um pacote de lentilha"* (no lentilha yet) | the item, its unit and its portions are created, **with nothing asked** |
| *"usei uma concha de arroz"* (a new word) | the size is estimated, written down, and the second `concha` is worth the same |
| *"usei um pouco de arroz"* | it asks, instead of inventing a number |
| two items matching one word | it asks which, instead of guessing |

The tools know nothing about MCP, and that is the point: `cmd/mcp` and the
WhatsApp bot hand the same six tools, with the same wording, to two different
SDKs. The day they disagree about what a `taça` is worth is the day the
inventory is wrong, so the architecture test fails the build if `internal/agent`
imports an SDK, and a golden test fails if the two sentences that invert what a
model would otherwise do — record without confirming, never ask what a portion
weighs — go missing from the prompt.

Logs go to **stderr**: stdout carries the JSON-RPC frames, and one stray line on
it corrupts the session rather than showing up as a log.

## State

| Phase | Delivers | State |
|---|---|---|
| A | Domain, ledger, repository, services, CLI | done, verified against Postgres on Neon |
| B | Tools + MCP server (stdio) | done, tested; not yet tried against a real model |
| C | WhatsApp bot | to do |
| D | Weekly digest | to do |

Picking this up: read [`docs/HANDOFF.md`](docs/HANDOFF.md) first, then
[`docs/plans/despensa.md`](docs/plans/despensa.md) for the phase-by-phase
plan and the reasoning behind each cut.
