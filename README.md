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

**The list is derived, and the person can still overrule it.** The shopping list
is computed on every read — the items at or below their minimum — so it cannot
disagree with the ledger. But *"I know the rice is low, I am not buying rice this
month"* and *"the coffee is fine, buy some anyway"* are decisions the level
cannot hold, so each item carries a `shopping_mode`: `auto` follows the minimum,
`never` holds it back, `always` pins it on. A pin ends by itself, on the restock
that clears the minimum, because "buy it anyway" is a one-time intent. The other
half of the list is the recados — a one-time thing that is not pantry inventory,
removed once it has been bought, and removed on its own if it ever becomes an
item.

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

The same gate runs in CI on every pull request
([`.github/workflows/ci.yml`](.github/workflows/ci.yml)); it needs
`TEST_DATABASE_URL` as a repository secret, and fails loudly rather than letting
the integration tests skip in silence. The Fly deploy waits for it.

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

Phase B is the seven tools (`internal/agent`) and an MCP server over stdio
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
| *"não quero repor arroz agora"* | `update_item` with `shopping_mode: never`, and rice leaves the list while staying in the pantry |
| *"volta a repor arroz"* | `shopping_mode: auto` — **not** `always`: going back to normal is the minimum deciding again |
| *"lembra de comprar café"*, with coffee full | `shopping_mode: always`, not a recado; buying a packet puts it back to `auto` |

The tools know nothing about MCP, and that is the point: `cmd/mcp` and the
WhatsApp bot hand the same seven tools, with the same wording, to two different
SDKs. The day they disagree about what a `taça` is worth is the day the
inventory is wrong, so the architecture test fails the build if `internal/agent`
imports an SDK, and a golden test fails if the two sentences that invert what a
model would otherwise do — record without confirming, never ask what a portion
weighs — go missing from the prompt.

Logs go to **stderr**: stdout carries the JSON-RPC frames, and one stray line on
it corrupts the session rather than showing up as a log.

## The bot

Phase C is the WhatsApp bot: `cmd/api` serves the webhook, `internal/bot` is the
conversation, and the same seven tools do the work. There is no HTTP between them
and the pantry — one service, one household, so the tools call the services in
process.

```
POST /webhooks/whatsapp ─► signature ─► allowed number ─► claim ─► model ─► tools
GET  /webhooks/whatsapp     Meta's subscription handshake
GET  /healthz               does not touch the database
GET  /readyz                touches the database
```

The order of those steps is the security of the service, and each one of them is
a test:

1. **The signature first.** HMAC-SHA256 over the raw body, compared in constant
   time. Without it this is a public write API for the household's pantry, so a
   wrong signature is `403` and the payload is never parsed.
2. **The allowed list.** A number outside `WHATSAPP_ALLOWED_NUMBERS` gets a
   neutral line and **zero** model calls. Prompt injection adds no power here —
   anyone who can send a message can already write to the pantry — which is
   exactly why the list is not optional.
3. **The idempotency claim.** `INSERT ... ON CONFLICT DO NOTHING` on
   `processed_messages`. Meta redelivers, and a second run would log the same cup
   of rice twice.
4. **`200` before the work.** The reply goes out first and the message is handled
   in the background, because Meta redelivers what takes too long. Shutdown waits
   for what it already answered for.
5. **The loop**, capped at `BOT_MAX_TOOL_ITERATIONS` (6). A runaway loop is a
   bug, not an expense — but the bill is how anyone would find out, so the cap is
   the fix. A tool that refuses comes back as a `tool_result` the model can read,
   never as a protocol error.
6. **The receipt**, with a `Desfazer` button per event written. The payload is
   `undo:<event_id>`: the id **is** the state, so there is no table of pending
   actions — nothing is pending, the write already happened.
7. **A tap skips the model.** The button carries the event id, so there is
   nothing to infer and nothing to pay for.

Portions of a conversation are kept for half an hour in `conversation_turns`, so
*"e mais uma taça"* still means rice. An assistant turn is stored as the model
wrote it and replayed byte for byte: a turn rebuilt from its parts would be a
turn edited, and an edited history invalidates the reasoning that came with it.

The provider and the model are both behind ports — `Messenger` and `Webhook` in
`internal/bot/whatsapp`, `Model` in `internal/bot/claude`. Telegram, if Meta's
number turns out to be a problem, is a new package behind the same two ports and
nothing else changes.

```bash
make api     # needs the WHATSAPP_* variables and ANTHROPIC_API_KEY
```

The Anthropic key belongs in `fly secrets`, **never** in the local shell
profile: an exported key makes Claude Code stop using the subscription and bill
the key instead. Deployment is `fly.toml` plus the `Dockerfile`; the migration is
the release command, never the startup.

## The weekly digest

Phase D is `cmd/digest`: the one message this service ever starts. Everything
else it sends is a reply to something a person typed, and that single difference
is what shapes the whole binary.

Outside WhatsApp's 24-hour window Meta accepts nothing but a template it has
approved, so the digest goes out as `WHATSAPP_DIGEST_TEMPLATE` with the list as
its one body parameter — on one line, because a parameter is substituted into a
sentence the template already owns. If there is no approved template, or Meta
refuses the send, the digest is **owed** rather than dropped: a row in
`pending_digests`, delivered as plain text on the next message from the
household. An inbound message is also what reopens the 24-hour window, so that
is the one moment free-form text is allowed at all.

Only the fact is stored, never the sentence. The shopping list is derived on
every read, so a digest owed on Sunday and paid on Tuesday says what the pantry
holds on Tuesday — a stored sentence would be the only thing here that can
disagree with the ledger. A partial unique index keeps one debt per chat, so
three weeks without an approved template owe one list and not three.

An empty list is not sent at all. It is the only message that costs money per
send and that nobody asked for, and a weekly *"não está faltando nada"* is the
exact habit that teaches a person to stop reading it.

The same run prunes `conversation_turns` past the window. Nothing else does, and
rows nobody will ever read are still a log of everything said in a kitchen.

```bash
make digest     # send it once, now

# Weekly, on the same image the API runs, as a scheduled Fly machine.
fly machine run --schedule weekly --command /app/digest <image>
```

Low-stock alerts need none of this. `Receipt.CrossedThreshold` is a *moment*,
not a state, so it rides out on the reply to the message that caused it: no
scheduled job, no template, no cost. It fires for a held-back item too — the
level is a fact and the mode is a decision — but then the line says so instead
of claiming the item went on a list it is not on.

## State

| Phase | Delivers | State |
|---|---|---|
| A | Domain, ledger, repository, services, CLI | done, verified against Postgres on Neon |
| B | Tools + MCP server (stdio) | done, tested; a model has read the descriptions and the read tools, never the writes |
| C | WhatsApp bot | built, tested and deployed; no message has reached a phone (see the handoff) |
| D | Weekly digest | built and tested; needs an approved template and the scheduled machine |

Picking this up: read [`docs/HANDOFF.md`](docs/HANDOFF.md) first, then
[`docs/plans/despensa.md`](docs/plans/despensa.md) for the phase-by-phase
plan and the reasoning behind each cut.
