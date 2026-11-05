# Handoff — despensa

For whoever picks up phase B. Everything below was verified against the repo and the database, not recalled. Where something was not verified, it says so.

Last verified: 2026-11-05 · `main` at `127ff1d` · phase A done.

> **Read first**
> - **Phase A is done and verified end to end**, against real Postgres 18 on Neon. The CLI (`cmd/despensa`) is the whole of it, and it works: create, buy, use, undo, drift, correct, shopping list.
> - **Phases B, C and D are not started.** The plan is `docs/plans/despensa.md` (Portuguese), phase by phase, with the reasoning behind each cut. Read its "O que vem do plano da Venus, e o que eu corto de propósito" before adding anything the plan deliberately removed.
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
- **The test suites bite.** Four deliberate mutations were planted and each failed the right test: threshold crossing as a state, an estimate overwriting a known portion, a Portuguese literal in the core, and an `os.Getenv` inside `internal/`.

### Not done

- **Phase B** — `internal/agent` (six tools as Go structs) and `cmd/mcp` (stdio). Nothing exists yet.
- **Phase C** — `internal/bot`, `internal/bot/whatsapp`, `cmd/api`. Nothing exists; no WhatsApp number has been obtained, which is the one external blocker.
- **Phase D** — `cmd/digest` and the Meta template.
- **`conversation_turns` is created and unused** (0 references in Go). It is phase C's short history, migrated early on purpose.
- **`domain.CodeAlreadyUndone` and `domain.NewCodedValidationError` are declared and unused.** Either wire them up in phase B or delete them; do not leave them as decoration.
- **No Dockerfile, no fly.toml, no CI.** They belong with phase C, which is the first phase with something to host.

## 3. Non-negotiables

These are decisions, not preferences. Changing one changes the product.

1. **Nothing is confirmed before a write.** The receipt carries the event id and a Desfazer button. The plan's source project confirmed every write because a wrong write unpublished a product from a live store; here the blast radius is one row of a personal pantry.
2. **The person is never asked to measure.** An unknown portion is learned from the model's estimate (`EventInput.PortionOneHint`), written down, and from then on that word always means the same amount. Consistency over accuracy: it is what makes thirty logged cups add up.
3. **A known portion is never re-estimated**, and a `corrected` one never loses to a `seeded` one. Enforced twice, in the service and in the `ON CONFLICT ... WHERE` of `UpsertPortion`.
4. **The ledger is the inventory.** `items.on_hand_milli == SUM(delta_milli) WHERE undone_at IS NULL`, per item. Undo marks `undone_at` and subtracts the delta; it does **not** append an inverse row.
5. **Integers only.** Every quantity is a `BIGINT` in thousandths of the item's canonical unit. The only two float boundaries are `domain.QtyFromFloat` and `domain.AmountFromFloat`; keep them the only ones.
6. **The language boundary.** Code, comments, logs, `Error()` and data-model enums in English; what the bot says, the tool descriptions, and the data (`items.name`, `portions.name`, aliases) in Portuguese. No accented string literal under `internal/core/` — `internal/architecture_test.go` fails the build over it. Domain errors carry a stable `Code()`; `internal/humanize` owns every word a person reads.
7. **No HTTP between the tools and the services.** One service, one user: `cmd/mcp` and the bot call `services.Pantry` in process. The plan cut the write API, the per-admin tokens, the wake-up client and the `expected_quantity`/409 dance, and §"o que eu corto" says why.

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

## 6. What you cannot do from this machine

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

Phase B is testable for free, which is the reason it comes before WhatsApp: the host is the person's Claude Desktop, so it calls the model on their subscription and `cmd/mcp` holds no API key. The adversarial passes to run there are in the plan's §"Verificação ponta a ponta", item 3 — especially an unknown item, an unknown portion, a vague sentence, and two items matching one word.

## 8. Open questions for the human

1. **The WhatsApp number** is the only external blocker for phase C: it must not be registered on regular WhatsApp. The unverified Meta business portfolio tier (250 recipients/24h, no CNPJ) is enough for one household, but that came from third-party sources — confirm it in Meta's messaging-limits documentation before building on it. Telegram is the fallback behind the same port, not a replanning.
2. **The model for the bot** is a config value (`ANTHROPIC_MODEL`), unset so far. At roughly 2K in / 200 out tokens per message and ten messages a day: Opus 5.5 ≈ US$ 3.60/month, Haiku 5.5 ≈ US$ 0.09. What matters is the quality of portion estimates, so start high and measure.
3. **Neon `dev` holds test data** from the phase A run — `arroz` at 540 g after a deliberate drift experiment, `pão de forma`, and a "papel toalha" note. Nobody has decided whether to keep it as a starting pantry or wipe it.
4. **The weekly digest needs one approved Meta utility template** (`lista_semanal`). Confirm current Brazilian pricing on Meta's own pricing page; it changed in 2026 and third-party sources disagree. The fallback, if approval stalls, is to hold the digest and deliver it on the next inbound message.
