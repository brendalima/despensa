# Handoff — despensa

For whoever picks up phase C. Everything below was verified against the repo and the database, not recalled. Where something was not verified, it says so.

Last verified: 2026-11-05 · branch `whatsapp-bot` · phases A, B and C built.

> **Read first**
> - **Phase A is done and verified end to end**, against real Postgres 18 on Neon. The CLI (`cmd/despensa`) is the whole of it, and it works: create, buy, use, undo, drift, correct, shopping list.
> - **Phase B is done and verified against the protocol and the database, but not yet against a real model.** The six tools are in `internal/agent`, the stdio server in `cmd/mcp`. The one thing left is §7's Claude Desktop pass, which is the only way to find out whether the descriptions actually steer a model.
> - **Phase C is built and tested, and has never seen a real delivery.** `internal/bot` is the conversation, `internal/bot/whatsapp` and `internal/bot/claude` the two adapters, `cmd/api` the webhook. Every rule has a test; what is missing is a WhatsApp number, which is the one external blocker, and a Fly app.
> - **Phase D is not started.** The plan is `docs/plans/despensa.md` (Portuguese), phase by phase, with the reasoning behind each cut. Read its "O que vem do plano da Venus, e o que eu corto de propósito" before adding anything the plan deliberately removed.
> - **Go is not on PATH.** It is at `~/.local/go` (1.27.3, installed by hand because the machine had none). Every command below assumes `export PATH=$HOME/.local/go/bin:$PATH`.
> - **You cannot push from this machine.** See §6.

---

## 1. What this is, in one paragraph

A home pantry inventory whose interface is a sentence in WhatsApp: *"usei duas fatias de pão e uma taça de arroz"*, answered with a receipt and an undo button. Every pantry app fails for the same reason — it asks you to stop and type a number while you are cooking — so the product's whole bet is that **friction, not a wrong row, is the failure mode that matters**. That is why nothing is confirmed before being written, and why the person is never asked to measure anything.

## 2. Where things stand

### Done and verified

- **Domain, ledger, repository, service, CLI, and the Portuguese border.** 4 commits on `main`, `make check` green (gofmt, vet, staticcheck, `go test -race`, govulncheck).
- **The arithmetic**, with table tests over `domain.Amount` / `domain.Qty`: portion↔unit conversion, half portions, human rounding, negatives, and no overflow at `MaxAmount × MaxQty`.
- **The service rules**, with mocked ports: the batch as one transaction, rollback when one entry of three fails, an item and a portion created on the way, a known portion winning over the model's estimate, a corrected portion surviving a later estimate, correction as an absolute level, threshold crossing as a moment, drift, and undo being idempotent.
- **The SQL**, 11 integration tests against Neon: the ledger invariant after 40 random writes and undos, the `ON CONFLICT` rule that protects a corrected portion, `FOR UPDATE` with ambiguity by alias, the partial unique index that frees a name on archiving, rollback, and `Claim` idempotency.
- **The migration applies on a fresh schema** — proved by applying `00001`'s Up section in a throwaway schema inside a rolled-back transaction, after its comments were edited.
- **The CLI end to end** against Neon `dev`, with output matching the plan's target: `arroz: 5 kg → 4,6 kg (≈26 taças)`, `pão de forma: 22 → 20 fatias`.
- **The six tools and the MCP server.** 2 commits on `agent-tools`, `make check` green. `internal/agent` holds the tools as plain Go values and knows nothing about MCP or about a model SDK; `cmd/mcp` moves JSON across and nothing else.
- **The tools, with mocked ports:** exactly six of them, every schema closed (`additionalProperties: false`), the `kind` and `unit` enums taken from the domain, a golden test over the descriptions, twelve shapes of rubbish refused before the handler and before any transaction, half portions, an item created on the way, a learned portion reported back, and `drift`/`crossed_threshold` passed through by those names.
- **The protocol, over `mcp.NewInMemoryTransports()`** against the real `newServer`: six tools listed with their descriptions and `additionalProperties: false` intact, a round trip through `record_events`, and a refusal arriving as a tool result with `is_error` rather than as a protocol error.
- **The built binary against Neon `dev`,** driven through `mcp.CommandTransport` the way a host drives it: `list_inventory` and `shopping_list` returned the real pantry, `record_events` took rice from 540 g to 320 g while learning `concha` = 220 g, the first `undo_event` put it back, and the second said *"Esse já estava desfeito"*.
- **The test suites bite.** Four deliberate mutations were planted in phase A and each failed the right test: threshold crossing as a state, an estimate overwriting a known portion, a Portuguese literal in the core, and an `os.Getenv` inside `internal/`. Six more were planted in phase B: a Portuguese literal outside `descriptions.go`, `internal/agent` importing the MCP SDK, a schema that stopped being closed, the no-confirmation sentence deleted from the prompt, the event source hardcoded, and a tool failure raised as a protocol error.

### Built in phase C, and tested — but never run for real

- **The conversation.** `internal/bot` holds the loop and the Portuguese system prompt (`prompt.go`, with a golden test over the sentences that invert the obvious). Three ports: `Messenger` and `Webhook` for the provider, `Model` for the model. The six tools come from `internal/agent` unchanged, so Claude Desktop and WhatsApp cannot disagree about what a word is worth.
- **The adapters.** `internal/bot/whatsapp` is the Cloud API: HMAC-SHA256 over the raw body with `hmac.Equal`, the GET subscription handshake, Meta's payload (text and both button shapes), and sending text, buttons and templates. `internal/bot/claude` is the only importer of the Anthropic SDK: `claude-opus-5-5` by default, effort `low`, `max_tokens` 1024, thinking left unset.
- **The HTTP border.** `internal/rest` has the webhook (403 on a bad signature **before** the payload is parsed, 200 before the work, `Wait()` for in-flight messages on shutdown) and `/healthz` (no database) / `/readyz` (database). `cmd/api` wires it, `adapters/api_config.go` reads and validates the new variables.
- **The short history.** `conversation_turns` is now used: `repository.Conversation` plus `services.Chat`, a 30-minute window and a 20-turn cap, with three integration tests against Neon. An assistant turn is stored as the model wrote it and replayed byte for byte.
- **The tests, 35 of them across five packages:** a number outside the list costs zero model calls and zero claims; a redelivery is handled once; the iteration cap holds at 3 in the test and the answer says so; a `undo:` button never reaches the model; the receipt carries one button per event with the right payload; a schema refusal comes back as an `is_error` result and the model gets a second turn; the history reaches the model and is stored; eight shapes of wrong signature all fail closed; Meta's payload tree is read and everything unreadable is skipped; the schemas reach the model closed, with the descriptions unchanged; a model refusal is an error, not an answer.
- **Deployment.** Multi-stage `Dockerfile` (static binary, `go mod download` before the source, non-root) and `fly.toml` with the migration as `release_command`, scale to zero, and the health check on `/healthz`.

### Not done

- **Phase B's model pass.** Everything is tested, but no model has ever called these tools. The adversarial sentences are in §7; they are free to run, and they are the only evidence that the descriptions work.
- **Phase C against reality.** No number, no Meta app, no Fly app, no secrets set, so no real message has ever arrived. §7 has the pass to run once the number exists, including the two negatives — a stranger's message and a repeated `message_id` — that are the point of the guards.
- **Phase D** — `cmd/digest` and the Meta template. `Messenger.SendTemplate` and `WHATSAPP_DIGEST_TEMPLATE` are already there, unused.
- **No CI.** `make check` is the gate, run by hand. It was last run green on this branch: gofmt, vet, staticcheck, `go test -race` (integration tests included, against Neon `test`) and govulncheck with no findings.
- **The image has never been built.** There is no docker on this machine, so the `Dockerfile` is unverified and `fly deploy` would be its first build; `fly.toml` does pass `flyctl config validate`. No Fly app exists yet either.
- **Nothing prunes `conversation_turns`.** `services.Chat.Forget` exists and nobody calls it: the window is applied on read, so the rows are harmless but they accumulate. The obvious home is `cmd/digest`, which already runs weekly.

## 3. Non-negotiables

These are decisions, not preferences. Changing one changes the product.

1. **Nothing is confirmed before a write.** The receipt carries the event id and a Desfazer button. The plan's source project confirmed every write because a wrong write unpublished a product from a live store; here the blast radius is one row of a personal pantry.
2. **The person is never asked to measure.** An unknown portion is learned from the model's estimate (`EventInput.PortionOneHint`), written down, and from then on that word always means the same amount. Consistency over accuracy: it is what makes thirty logged cups add up.
3. **A known portion is never re-estimated**, and a `corrected` one never loses to a `seeded` one. Enforced twice, in the service and in the `ON CONFLICT ... WHERE` of `UpsertPortion`.
4. **The ledger is the inventory.** `items.on_hand_milli == SUM(delta_milli) WHERE undone_at IS NULL`, per item. Undo marks `undone_at` and subtracts the delta; it does **not** append an inverse row.
5. **Integers only.** Every quantity is a `BIGINT` in thousandths of the item's canonical unit. The only two float boundaries are `domain.QtyFromFloat` and `domain.AmountFromFloat`; keep them the only ones.
6. **The language boundary.** Code, comments, logs, `Error()` and data-model enums in English; what the bot says, the tool descriptions, and the data (`items.name`, `portions.name`, aliases) in Portuguese. No accented string literal under `internal/core/` — `internal/architecture_test.go` fails the build over it. Domain errors carry a stable `Code()`; `internal/humanize` owns every word a person reads.
7. **The allowed list of numbers is not a preference.** Anyone who can send a message can write to the pantry, so prompt injection adds no power — which is exactly why the list, and the signature in front of it, are the two things that cannot be relaxed "for a test". `internal/bot` refuses an unknown number before the model, before the claim and before any read.
8. **No HTTP between the tools and the services.** One service, one user: `cmd/mcp` and the bot call `services.Pantry` in process. The plan cut the write API, the per-admin tokens, the wake-up client and the `expected_quantity`/409 dance, and §"o que eu corto" says why.

## 4. The quantity model, which is where a new agent will go wrong

An item has one canonical unit (`g`, `ml`, `un`) and a vocabulary of portions. **Buying and using are the same mechanism** — a package is just another portion:

```
arroz          unit=g   on_hand=4_600_000   taça=180_000 · colher=12_000 · pacote=5_000_000
pão de forma   unit=un  on_hand=16_000      fatia=1_000  · pacote=22_000
```

- `Amount` and `Qty` are thousandths. `AmountOf(180)` is 180 g; `QtyOf(2)` is two portions.
- `Item.UnitPortion()` is the portion worth exactly one canonical unit. It is what makes bread read as "16 fatias" instead of "16 un".
- **`EventCorrection` carries a level, not a change** (`EventKind.Absolute()`). *"tenho umas três taças"* sets the total; the service computes the delta. It is the reset for accumulated drift.
- **Zero quantity is refused for every kind but a correction.** "usei nada de arroz" means nothing; "acabou o arroz" and an item entering the pantry empty are corrections to zero.
- **Negative on-hand is kept, shown as zero, and reported as `Receipt.Drifted`.** It is the signal that the running total undercounted, and the only moment the bot asks for a number — a guess, never a measurement.
- **`Receipt.CrossedThreshold` is the moment, not the state.** It rides along on the reply, which is why low-stock alerts need no scheduled job and no Meta template. Only the weekly digest does.

## 5. Landmines

- **`flag` stops at the first positional argument.** It silently dropped every `--portion` in `add arroz g --portion taça=180`, creating items with no vocabulary. `splitArgs` in `cmd/despensa/main.go` fixes it, with a test per shape. Any new subcommand with flags must go through it.
- **A new Neon role cannot create tables.** Postgres 15+ removed `CREATE` on `public` from `PUBLIC`, and `neon_superuser` does not have it either — nor can it `SET ROLE`. The human ran, as `neondb_owner`, in both databases: `GRANT USAGE, CREATE ON SCHEMA public TO dev;` (and `TO test`). A fresh branch or role needs it again, or `cmd/migrate` fails with `permission denied for schema public`.
- **Editing an applied migration is invisible to goose.** It tracks the version number, not the contents, so a typo lands on the next fresh database. Re-validate with the throwaway-schema trick in §7.
- **The next migration is `00002`.** goose refuses a migration older than the last applied; never land a lower number after a higher one.
- **`DATABASE_URL_UNPOOLED` currently points at the `-pooler` host**, same as `DATABASE_URL`. `adapters/config.go` prefers the direct connection on purpose — pgx keeps a prepared-statement cache per connection and transaction pooling breaks it. It worked in every test here, so this is a latent problem, not an active one. Fix by copying the non-pooled string from the Neon console.
- **`Scan` into `domain.Amount` / `Qty` / `Unit` works through reflection on the underlying type.** Changing one of those to a struct breaks every repository read at runtime, not at compile time.
- **The CLI never estimates a portion** — it refuses an unknown word. Only the model estimates. Do not add a fallback estimate to `cmd/despensa`; it would make the CLI and the bot disagree about what a word is worth.
- **The tool schemas are maps, not inferred from structs, and that is deliberate.** `jsonschema-go` reads only descriptions from struct tags, so an enum cannot come from `domain.AllUnits()` that way, and the generic `mcp.AddTool` would own the validation that the WhatsApp bot also needs. The map is both what the model is shown and what validates the call; `internal/agent/schema.go` is where `additionalProperties: false` lives, in one place. Changing a schema to a Go struct quietly reopens every object.
- **`internal/agent` validates its own arguments, on purpose.** Phase C has no SDK in front of it doing that, and two validators would mean two sets of rules. Call tools through `ToolSet.Call`, never `Tool.Handler` directly, or nothing is checked.
- **A word is learned even if the event is undone.** The portion is written before the event, and `undo_event` reverses the quantity, not the vocabulary. That is the right way round — consistency of the word is the point — but it means the smoke run left `concha` = 220 g on rice in Neon `dev`.
- **`humanize.Secondary` picks the largest portion that lands between 2 and 40.** Adding a word therefore changes how an existing item reads: once rice knew `concha`, `540 g (≈3 taças)` became `540 g (≈2,5 conchas)`. Not a bug, but it will look like one in a diff of expected output.
- **The order of the guards in `internal/bot.handle` is the security of the service**, and nothing in the compiler protects it: the allowed list before the claim, the claim before the model, the button before the model. Moving the claim above the allowed list would let a stranger spend a row; moving it below the model would let a redelivery spend a model call.
- **`cmd/api` answers 200 before the work and hands each message a context of its own.** Using the request's context would cancel the work the moment the reply is written, and the person would get nothing while Meta is told the message arrived. Shutdown calls `rest.WhatsApp.Wait()` for the same reason.
- **An assistant turn is replayed from `Turn.Raw`, not rebuilt.** The generic fields are what the loop reasons about; the raw copy is what the model is given back. Rebuilding one drops the reasoning blocks, which on current models is an edited history — and an edited history is refused outright for accounts created after 2026-08-31. If a new field is needed on a turn, add it alongside `Raw`, do not start assembling messages from parts.
- **The Graph API version is pinned** (`graphVersion` in `internal/bot/whatsapp/messenger.go`, currently `v23.0`). Meta retires versions on a schedule, and this one was not confirmed against their docs — confirm it when the number is registered.
- **`ANTHROPIC_MODEL` defaults to `claude-opus-5-5` and effort is `low`**, set in code (`internal/bot/claude/claude.go`) because it is the shape of the work, not a knob. If estimates come out bad, raise the effort before changing the model: the plan's cost table says the model is cheaper than the WhatsApp messages either way.
- **`internal/bot/claude.Input.BaseURL` and `whatsapp.MessengerInput.BaseURL` exist for the tests.** They are the only reason the mapping can be checked without a key or a number; leave them, and do not set them in `cmd/api`.
- **Logs in `cmd/mcp` must go to stderr.** `main` sets a stderr `slog` handler before anything else; stdout carries the JSON-RPC frames, and one stray line corrupts the session instead of showing up as a log.

## 6. What you cannot do from this machine

- **Commit to `main`.** It takes no direct commits: one branch per phase, integrated by pull request (AGENTS.md §15). Branch off `main` before the first edit.
- **Push.** `origin` is `git@github.com:brendalima/despensa.git`, and `~/.ssh/id_ed25519` has a passphrase with no agent running. Shell state does not survive between tool calls, so `ssh-agent` loaded in one command is gone by the next. Commit locally, one branch per phase, and let the human push. (The local `origin/main` ref matches `HEAD`, so a push happened from their side; this was not re-verified from here.)
- **Reach GitHub at all** — no `gh` CLI installed, and `git ls-remote` fails on the same key.
- **Change the Neon project** — the Neon MCP server is installed but unauthorized (`/mcp`). Credentials live in `.env.local`, which is gitignored and must stay that way.
- **Call the Claude API** — phase C is the first phase that needs `ANTHROPIC_API_KEY`, and it must live in `fly secrets`, never exported in the shell: an exported key makes Claude Code stop using the subscription and bill the key instead.

## 7. How to verify your work

```bash
export PATH=$HOME/.local/go/bin:$PATH
cd /home/brendalima/despensa
make check                 # gofmt, vet, staticcheck, go test -race, govulncheck
```

The integration tests need `TEST_DATABASE_URL` (set in `.env.local`, a separate Neon endpoint) and **skip silently without it** — check the output says `ok`, not `[no test files]`, and force a real run with `-count=1`.

To prove an edited migration still applies on a fresh database, apply its Up section in a throwaway schema inside a transaction and roll it back:

```sql
BEGIN;
CREATE SCHEMA migration_check;
SET LOCAL search_path = migration_check;
-- the file's Up section
ROLLBACK;
```

Phase A by hand, against Neon `dev`:

```bash
make build
./bin/despensa add lentilha g --portion pacote=500 --portion taça=200
./bin/despensa buy lentilha pacote 2
./bin/despensa use lentilha taça
./bin/despensa status
./bin/despensa history lentilha
./bin/despensa undo <id>
```

Phase B, without a model — the protocol and the database, driven the way a host drives them:

```bash
make build
claude mcp add despensa -- env DATABASE_URL="$DATABASE_URL" go run ./cmd/mcp
```

Phase C, without a number — what the tests already cover, by hand:

```bash
make check
make api &   # needs ANTHROPIC_API_KEY and the WHATSAPP_* variables in .env.local
# A fake delivery, signed the way Meta signs it. `from` has to be a number
# on WHATSAPP_ALLOWED_NUMBERS, or the only thing proved is the guard.
BODY='{"entry":[{"changes":[{"field":"messages","value":{"messages":[{"id":"wamid.test","from":"<seu numero>","type":"text","text":{"body":"usei uma taça de arroz"}}]}}]}]}'
SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$WHATSAPP_APP_SECRET" -hex | awk '{print $2}')"
curl -i -XPOST localhost:8080/webhooks/whatsapp -H "X-Hub-Signature-256: $SIG" -d "$BODY"
```

Expect `200`, a model call, and a send attempt that fails at Meta — which is as far as this goes without a number. Repeat the same body: the second one must do nothing (the claim), and `processed_messages` must have one row. Change one character of `$BODY` without re-signing: `403`, and the logs must show nothing parsed.

Phase C **with** a number, once there is one:

1. `fly secrets set` every variable in `.env-sample` — the Anthropic key included, and never in the shell profile.
2. `fly deploy`. The release command runs `cmd/migrate`; the API never migrates.
3. Point Meta's webhook at `https://<app>.fly.dev/webhooks/whatsapp` with `WHATSAPP_VERIFY_TOKEN`. The GET handshake has to answer the challenge as plain text.
4. From the phone: *"usei duas fatias de pão e uma taça de arroz"* → a receipt in seconds → **Desfazer** → the levels come back. `fly logs` must show one model call.
5. The two negatives, which are the whole point of the guards: a message from a number **outside** `WHATSAPP_ALLOWED_NUMBERS` → zero model calls and zero tool calls in `fly logs`; the same `message_id` replayed by hand → nothing happens.

Phase B **with** a model is the pass that has not been run, and it is free: the host is the person's Claude Desktop, so it calls the model on their subscription and `cmd/mcp` holds no API key. The adversarial sentences are in the plan's §"Verificação ponta a ponta", item 3 — especially an unknown item, an unknown portion, a vague sentence, and two items matching one word. What is being tested there is the wording in `internal/agent/descriptions.go`, not the code: if the model asks for confirmation, or asks how much a cup weighs, the sentence that forbids it needs rewriting, and the golden test needs the new phrasing.

## 8. Open questions for the human

1. **The WhatsApp number** is the only external blocker for phase C: it must not be registered on regular WhatsApp. The unverified Meta business portfolio tier (250 recipients/24h, no CNPJ) is enough for one household, but that came from third-party sources — confirm it in Meta's messaging-limits documentation before building on it. Telegram is the fallback behind the same port, not a replanning.
2. **The model for the bot** is a config value (`ANTHROPIC_MODEL`), defaulting to `claude-opus-5-5` since phase C. At roughly 2K in / 200 out tokens per message and ten messages a day: Opus 5.5 ≈ US$ 3.60/month, Haiku 5.5 ≈ US$ 0.09. What matters is the quality of portion estimates, so start high and measure.
3. **Neon `dev` holds test data** from the phase A run — `arroz` at 540 g after a deliberate drift experiment, `pão de forma`, and a "papel toalha" note. The phase B smoke run added `concha` = 220 g to rice's vocabulary and two ledger rows, one of them undone; the levels are where they were. Nobody has decided whether to keep any of it as a starting pantry or wipe it.
4. **The weekly digest needs one approved Meta utility template** (`lista_semanal`). Confirm current Brazilian pricing on Meta's own pricing page; it changed in 2026 and third-party sources disagree. The fallback, if approval stalls, is to hold the digest and deliver it on the next inbound message.
