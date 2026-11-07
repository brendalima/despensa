# Handoff — despensa

For whoever picks up phase D's last loose ends. Everything below was verified against the repo and the database, not recalled. Where something was not verified, it says so.

Last verified: 2026-11-07, evening · `main` at PR #10 · **phase C has been in daily use by the household**, a whole day of it, by two people. `lista_semanal` is **approved, and has sent its first real template message**. What is left of phase D is two hand-runs against production and one workflow to merge; see §10. **`cmd/digest` has still never run on production** — the two attempts on 2026-11-07 ran `/app/api` instead, for the reason in §5 about `--command`.

> **`dev` is dev again, as of 2026-11-07.** The household's real pantry was moved to a Neon branch of its own — the one `fly secrets`' `DATABASE_URL` points at — and `.env.local`'s `DATABASE_URL` (`ep-flat-moon-b6rhx62n`, role `dev`) is now a throwaway. It came back empty from the move; `./bin/migrate` was run against it from here and it is at version 2. So a local run no longer writes to somebody's groceries, and the seeded rows in it (`arroz`, `leite`, `ovo`, a `papel toalha` note) are test data put there on purpose.
>
> **Nothing in this repository records which branch production is on.** `fly secrets list` shows a digest, not a value (§5), so read it from the Neon console rather than assuming the endpoint in `.env.local` is it. The previous version of this paragraph said to treat `dev` as production; that is now the opposite of true, and the cost of getting it backwards has swapped direction — the risk is no longer writing to the real pantry by accident, it is *testing against an empty one and believing it was production*.

> **Read first**
> - **Phase A is done and verified end to end**, against real Postgres 18 on Neon. The CLI (`cmd/despensa`) is the whole of it, and it works: create, buy, use, undo, drift, correct, shopping list.
> - **Phase B is done, and a model has now driven the read half.** The six tools are in `internal/agent`, the stdio server in `cmd/mcp`. The descriptions steer a model correctly on the cases that need no write; the three that matter most are still untested. See §2.
> - **Phase C is finished.** `internal/bot` is the conversation, `internal/bot/whatsapp` and `internal/bot/claude` the two adapters, `cmd/api` the webhook. It runs at `https://despensa-verdant-valley-8190.fly.dev` on a registered Brazilian number, and on 2026-11-06 a real sentence from a phone became a receipt with working Desfazer buttons. Ten items of real inventory went in by WhatsApp the same day. Error 130497 is gone: it was the US test number's **sender** country, and a Brazilian number fixed it.
> - **Five bugs that no test had caught surfaced in the first hour of real use**, and each one is now a landmine in §5 with a test behind it. That hour was worth more than the thirty-five tests that preceded it — read §5 before trusting any part of this that has not met a phone.
> - **Phase D has now sent a real template, and both of its paths have run outside the test suite.** `cmd/digest` is the weekly list, `internal/bot/digest.go` the sending and the fallback, `pending_digests` (migration `00002`) the debt book. `lista_semanal` was approved by Meta on 2026-11-07 and accepted on the first send the same evening; the owed path ran twice against a real database before that. What is left is **the scheduled Fly machine**, which still does not exist, so nothing is weekly yet. See §10.
> - The plan is `docs/plans/despensa.md` (Portuguese), phase by phase, with the reasoning behind each cut. Read its "O que vem do plano da Venus, e o que eu corto de propósito" before adding anything the plan deliberately removed.
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

### Built in phase C, and now finished

- **The conversation.** `internal/bot` holds the loop and the Portuguese system prompt (`prompt.go`, with a golden test over the sentences that invert the obvious). Three ports: `Messenger` and `Webhook` for the provider, `Model` for the model. The six tools come from `internal/agent` unchanged, so Claude Desktop and WhatsApp cannot disagree about what a word is worth.
- **The adapters.** `internal/bot/whatsapp` is the Cloud API: HMAC-SHA256 over the raw body with `hmac.Equal`, the GET subscription handshake, Meta's payload (text and both button shapes), and sending text, buttons and templates. `internal/bot/claude` is the only importer of the Anthropic SDK: `claude-opus-5-5` by default, effort `low`, `max_tokens` **8192**, and adaptive thinking configured for one reason — `block_binding`. Both of those numbers were different and both were wrong; see §5.
- **The HTTP border.** `internal/rest` has the webhook (403 on a bad signature **before** the payload is parsed, 200 before the work, `Wait()` for in-flight messages on shutdown) and `/healthz` (no database) / `/readyz` (database). `cmd/api` wires it, `adapters/api_config.go` reads and validates the new variables.
- **The short history.** `conversation_turns` is now used: `repository.Conversation` plus `services.Chat`, a 30-minute window and a 20-turn cap, with three integration tests against Neon. An assistant turn is stored as the model wrote it and replayed byte for byte.
- **The tests, 35 of them across five packages:** a number outside the list costs zero model calls and zero claims; a redelivery is handled once; the iteration cap holds at 3 in the test and the answer says so; a `undo:` button never reaches the model; the receipt carries one button per event with the right payload; a schema refusal comes back as an `is_error` result and the model gets a second turn; the history reaches the model and is stored; eight shapes of wrong signature all fail closed; Meta's payload tree is read and everything unreadable is skipped; the schemas reach the model closed, with the descriptions unchanged; a model refusal is an error, not an answer.
- **Deployment.** Multi-stage `Dockerfile` (static binary, `go mod download` before the source, non-root) and `fly.toml` with the migration as `release_command`, scale to zero, and the health check on `/healthz`. **Both are now built and running** on Fly app `despensa-verdant-valley-8190` (region `gru`): `/healthz` and `/readyz` answer, so the release command ran the migration and Neon is reachable from Fly.
- **The last hop, which is what everything above was waiting for.** On 2026-11-06 a registered Brazilian number (+55 21 97317-5280, display name "Brenda Lima Dev", `status: CONNECTED`, `account_mode: LIVE`) sent, delivered and had read a message from Fly. *"Comprei um pacote de pão e cartela de ovos com 20 unidades"* came back as a receipt with **Desfazer pão** and **Desfazer ovo**, and the egg was created on the way with `dúzia` = 12 and `cartela` = 20 — vocabulary nobody typed. Then ten items of real inventory in one sentence, one transaction, with three of them as corrections to an absolute level.
- **The guards, proved against a real phone rather than a signed curl.** A delivery status arriving on the webhook is itself proof the app is still subscribed to `messages`; the allowed list, the claim and the signature had already been proved in production on the test number.
- **Observability**, added on `observability` after a night lost to a silent failure. `Bot.Handle` logs one line per message on the happy path — outcome (`answered`, `button`, `refused`, `redelivery`, `empty`, `lost`), iterations, tool names, buttons, elapsed. `whatsapp.Messenger` logs the `wamid` Meta returns on a 2xx. `Webhook.Parse` returns a `bot.Delivery` carrying both messages and **delivery statuses**, and `internal/rest` logs a failed one with Meta's code and detail. Nothing acts on a status; it is the only place a dropped message is visible at all.

### Built in phase D, and verified against the database and the tests

- **The weekly digest.** `cmd/digest` is a binary of its own on the same image, run by a scheduled Fly machine. It builds the list, sends it as the approved template, prunes `conversation_turns`, and exits.
- **The fallback, which is currently the only path.** Without `WHATSAPP_DIGEST_TEMPLATE` — or when Meta refuses the send — the digest is recorded as *owed* in `pending_digests` rather than dropped, and the next message from the household gets it as plain text, after that message's own answer. An inbound message is also what reopens the 24-hour window, so it is the one moment free-form text is allowed.
- **Only the fact is stored, never the sentence.** The list is derived on every read, so a digest owed on Sunday and paid on Tuesday says what the pantry holds on Tuesday. A partial unique index (`pending_digests_chat_key`) keeps one debt per chat: three weeks without a template owe one list, not three.
- **An empty list is not sent.** It is the only message that costs money per send and that nobody asked for.
- **`conversation_turns` is finally pruned**, by the same weekly run — `services.Chat.Forget`, the thing that existed and nobody called.
- **The tests, 21 new ones across four packages:** an empty list sends nothing and owes nothing; the template goes out by name; the parameter is one line whatever the person typed, and clipped to Meta's documented 1024; no template means owed; a refused template means owed and *not* an error; owing three times leaves one debt; the owed digest rides out **after** the turn's own answer and settles the debt; a stranger's message never reads the debt book; a redelivery does not deliver it; a failing digest does not break the turn it rides on. Six of those run against Postgres on Neon, including the partial index refusing a second debt.
- **Two mutations were planted and each failed the right test:** the digest sent before the answer instead of after, and the `refused` guard dropped from `payDigest` so a stranger's message reached the debt book.
- **Migration `00002` applies on a fresh schema**, proved with §7's throwaway-schema trick — and the partial index really refuses the second insert there, not just in the branch where it was created.
- **CI exists.** `.github/workflows/ci.yml` runs exactly what `make check` runs, migrates the test branch first, and **fails loudly** if `TEST_DATABASE_URL` is missing rather than letting the integration tests skip in silence. `fly-deploy.yml` now waits on it (`workflow_run`) instead of racing it, because the release command runs the migration and a migration is the one thing here a redeploy cannot roll back.
- **The Graph API version was re-checked** and moved from `v23.0` to `v27.0`. See §5.

### Found by real use, in the first hour, and fixed

None of these were caught by the 56 tests that existed. Each has a test now, each test was checked by reverting the fix, and each is a landmine in §5. The pattern is worth more than the list: **every one of them lived in the gap between a fake and a real counterparty** — Meta's validation, the model's own replay rules, the model's choice of words.

- **Duplicate button titles** (`#131009`). A sentence naming two items made two buttons both reading "Desfazer", and Meta refuses a message whose button titles repeat. The ids were unique; the titles are what is checked. Buttons now name their item and `whatsapp.Messenger` guarantees uniqueness after clipping. PR #7.
- **A refused receipt claimed nothing was written.** The same bug's second half, and the worse one: the writes had already happened, `b.send` returned the error, and `Handle` turned it into `humanize.Trouble()` — *"não anotei nada"* about two rows in the ledger. The honest response to that message is to re-send the sentence and log everything twice. A refused receipt now goes out as plain text. PR #7.
- **`un` pluralised to `uns`.** The model gave an egg a portion called `un`, `UnitPortion` picked it, and `Plural` had no rule for a word ending in n. Abbreviations now map to themselves. PR #8.
- **Preserved thinking.** The 30-minute window is a history edit, so replayed thinking blocks failed their conversation check and every later message in a chat 400'd. `drop_block` under the binding beta. PR #8.
- **The token ceiling.** `max_tokens` was 1024, reasoned about as "a receipt is one or two sentences" — but it bounds thinking, the tool-call JSON and the text together, and a ten-item batch truncated mid-array five times. 8192 now, and `stop_reason: max_tokens` is no longer invisible. PR #9.

### Not done

- **Phase B's model pass, the writing half.** A model has now read the six descriptions and called the two read tools (`list_inventory`, `shopping_list`) against Neon `dev`, and the descriptions steered it the way they are meant to on the cases that need no write: an unknown item produced a `record_events` call with `new_item` filled in and **no** confirming question, and a vague sentence produced a question rather than an invented quantity. The writing half was not run — the session that did this was not allowed to write to a shared database — so the three cases that matter most are still untested: an unknown **portion** (does it estimate, or ask how much a cup weighs?), **two items matching one word** (does it ask which?), and **drift** (does it ask for a number when `drift: true` comes back?). Those need one pass in Claude Desktop, which is free. See §7.
- **~~Phase C's last hop.~~** Done on 2026-11-06. A real message from a phone became a receipt, a Desfazer button has been tapped, and the household's inventory went in by WhatsApp.
- **One branch is still not merged: `unit-named-portions`, now rebased onto `main`** (it was based on PR #9, two merges back). It stops a portion named after the unit defeating the kg/l scaling, so without it the pantry reads `arroz: 5000 g` and `leite: 2500 ml` instead of `5 kg` and `2,5 l`. Green, mutation-checked, and the only outstanding code. On 2026-11-07 it was also **exercised against a real database** for the first time, on the dev branch: an item `leite` given a portion literally named `ml` reads `2 l (≈2 caixas)` with the fix, and `arroz` reads `5,5 kg`. **It is not deployed**, so the weekly digest will say `5000 g` until it is — which is the reason to land it before the scheduled machine exists, not after.
- **One of phase D's two external halves is done. ~~The Meta template~~ `lista_semanal` is approved** — the category wording went through two rounds with Meta and the reasoning is kept in §10, because a resubmission would need it again. **The scheduled Fly machine is still not created**, so nothing runs weekly: the image to create it on is `registry.fly.io/despensa-verdant-valley-8190:deployment-01M6S3T5HCQAHWJAQD7CZ04KEX`, built from `ad02f2e` (the `GH_SHA` label on `fly image show` says so), which is `main`'s tip and therefore contains `/app/digest`. That was inferred from the label and the `Dockerfile` building `./cmd/...`, **not** verified by listing the image — production reads were refused in that session.
- **~~The digest has never sent anything.~~ Both paths have now run for real, and neither ran against production.** From this machine, against the dev branch, on 2026-11-07:
  - **The owed path**, with no template set: `digest owed, it will ride out on the next message` with `already_owed:false`, then a second run the same minute with `already_owed:true`. That is the partial unique index refusing a second debt outside the test suite, which is the one thing about `pending_digests` worth seeing with your own eyes.
  - **The template path**, with `WHATSAPP_DIGEST_TEMPLATE=lista_semanal`: Meta answered `accepted` with a `wamid` on the first attempt, and the webhook then reported `sent`, `delivered` and `read` for that same id within eight seconds — **the template reached a phone and a person opened it.** `accepted` would have proved less than it looks, but the parameter count is matched exactly at send time, so even that much proved the name, `pt_BR` and **both** body parameters arriving in the right shape; `read` proves the rest. The list it carried was the dev branch's seeded test data, not the household's pantry, which is the one thing about that message that was a lie.
  - **Production ran neither.** Two one-shot machines were created for it on 2026-11-07 and both started `/app/api`, because `--command` is a flag of `--shell` (§5). They were left running on a stale image — web servers the proxy never routes to, since they have no services — and they are what made the Fly dashboard warn about multiple images and partially deployed secrets. `fly machine destroy --force <id>` each, and the warnings go with them.
  - **What is still unproven:** only the fallback's second half — the owed list riding out after the next real message's own answer. That has only ever happened in a test: the debt written on 2026-11-07 is in the **dev** database, and production reads a different one, so it will never be paid. §10 step 2 is how to prove it, and it costs nothing.
- **A shopping note has no end state, and nothing can remove one.** `RemoveNote` is implemented in the repository *and* the service, and deliberately exposed nowhere: no tool, and the CLI only adds. So a note is append-only through every interface in the project. It is already wrong in the real pantry — "papel toalha" is a note from before the item existed, and now the item exists at 2 rolos, so the list shows a thing the household has. The bot says so out loud: *"Eu não consigo apagar recados."*

  The design intent, for whoever decides this: a note is a thing you **never count** (`domain.Note` — "tracking those as items with portions would be work for no gain"), so the list is a union of a *computed* set that self-clears and a *written* set that nothing clears. The collision is sharper than it looks: `add_shopping_note`'s own description names **papel toalha** as the canonical example of a note, while `record_events` says anything with a quantity is an item and to create it without asking. Both fired on the same object. So there are two gaps — a note has no end, and a note and an item can name the same thing. The options weighed were: a removal tool alone; a removal tool plus the list skipping notes that name a tracked item; or a removal tool plus deleting the note row when such an item is created. **Deliberately left undecided on 2026-11-07.**
- **`make check` still fails at `staticcheck`, and the reason in the last handoff was wrong.** v0.8.2 *is* staticcheck 2026.2.2, the newest release as of 2026-11-06, and its own notes say it supports the export data format of Go 1.27.2 — so "built against 1.26" was not it. The failure was not reproduced here either: this machine's session could not run `staticcheck` at all, because `go run` on a remote module is running code from the internet and was refused. **Everything else in the gate passes**, verified here: `gofmt -l .` clean, `go vet ./...` clean, `go test -race -count=1 ./...` green including all the Neon integration tests, and all five binaries build. CI runs staticcheck as its own step, so the next pull request is the first clean machine it can be diagnosed on.
- **`govulncheck` was not run here either**, for the same reason.
- **Phase B's write pass is still the cheapest unrun test in the project**, and it is now more valuable than before: the model's real behaviour has already produced three surprises that the descriptions arguably invited — an egg portion called `un`, a rice portion called `g`, and `ml` offered as a portion name for milk. See the first item above and §7.
- **~~The image has never been built.~~** It has now, by Fly's remote builder (there is still no docker on this machine). The first build exposed the `ENTRYPOINT` bug in §5. **The image has not been rebuilt since `cmd/digest` was added** — it builds `./cmd/...`, so `/app/digest` will be in the next one, but that has not been seen.
- **~~Nothing prunes `conversation_turns`.~~** `cmd/digest` does, on every run.

## 3. Non-negotiables

These are decisions, not preferences. Changing one changes the product.

1. **Nothing is confirmed before a write.** The receipt carries the event id and a Desfazer button. The plan's source project confirmed every write because a wrong write unpublished a product from a live store; here the blast radius is one row of a personal pantry.
2. **The person is never asked to measure.** An unknown portion is learned from the model's estimate (`EventInput.PortionOneHint`), written down, and from then on that word always means the same amount. Consistency over accuracy: it is what makes thirty logged cups add up.
3. **A known portion is never re-estimated**, and a `corrected` one never loses to a `seeded` one. Enforced twice, in the service and in the `ON CONFLICT ... WHERE` of `UpsertPortion`.
4. **The ledger is the inventory.** `items.on_hand_milli == SUM(delta_milli) WHERE undone_at IS NULL`, per item. Undo marks `undone_at` and subtracts the delta; it does **not** append an inverse row.
5. **Integers only.** Every quantity is a `BIGINT` in thousandths of the item's canonical unit. The only two float boundaries are `domain.QtyFromFloat` and `domain.AmountFromFloat`; keep them the only ones.
6. **The language boundary.** Code, comments, logs, `Error()` and data-model enums in English; what the bot says, the tool descriptions, and the data (`items.name`, `portions.name`, aliases) in Portuguese. No accented string literal under `internal/core/` — `internal/architecture_test.go` fails the build over it. Domain errors carry a stable `Code()`; `internal/humanize` owns every word a person reads.
7. **The allowed list of numbers is not a preference.** Anyone who can send a message can write to the pantry, so prompt injection adds no power — which is exactly why the list, and the signature in front of it, are the two things that cannot be relaxed "for a test". `internal/bot` refuses an unknown number before the model, before the claim and before any read.
8. **A digest is never lost, only late.** The weekly list is the one message this service starts, and the one that can fail for reasons that have nothing to do with the household — a template pending review, a window that closed, Meta changing its mind. So a send that does not happen becomes a row that says it is owed, and the next message pays it. The alternative is a product that quietly stops reminding you to buy rice, which is indistinguishable from a product nobody wrote.
9. **No HTTP between the tools and the services.** One service, one user: `cmd/mcp` and the bot call `services.Pantry` in process. The plan cut the write API, the per-admin tokens, the wake-up client and the `expected_quantity`/409 dance, and §"o que eu corto" says why.

## 4. The quantity model, which is where a new agent will go wrong

An item has one canonical unit (`g`, `ml`, `un`) and a vocabulary of portions. **Buying and using are the same mechanism** — a package is just another portion:

```
arroz          unit=g   on_hand=4_600_000   taça=180_000 · colher=12_000 · pacote=5_000_000
pão de forma   unit=un  on_hand=16_000      fatia=1_000  · pacote=22_000
```

- `Amount` and `Qty` are thousandths. `AmountOf(180)` is 180 g; `QtyOf(2)` is two portions.
- `Item.UnitPortion()` is the portion worth exactly one canonical unit, **excluding one named after the unit itself**. It is what makes bread read as "16 fatias" instead of "16 un" — and the exclusion is what keeps rice reading as "5 kg" once the model has given it a portion called "g", which it does. `Item.NamesUnit` is that test, and it is applied to the data rather than to what gets written next: the rows already exist.
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
- **The Graph API version is pinned** (`graphVersion` in `internal/bot/whatsapp/messenger.go`, now `v27.0`). Checked against Meta's own versioning page and changelog on 2026-11-06: v27.0 is the one they call most recent, every version lasts at least two years, and the `v23.0` this used to be was good until 2027-10-08. So the bump is a refresh, not a fix — and it has **never been exercised against the live API**, because no send has ever reached a phone. The `hello_world` send in §9 step 3 is the first thing that will test it; if it fails in a new way, try `v26.0`, which is the newest version Meta's own availability table actually lists.
- **`ANTHROPIC_MODEL` defaults to `claude-opus-5-5` and effort is `low`**, set in code (`internal/bot/claude/claude.go`) because it is the shape of the work, not a knob. If estimates come out bad, raise the effort before changing the model: the plan's cost table says the model is cheaper than the WhatsApp messages either way.
- **`internal/bot/claude.Input.BaseURL` and `whatsapp.MessengerInput.BaseURL` exist for the tests.** They are the only reason the mapping can be checked without a key or a number; leave them, and do not set them in `cmd/api`.
- **The `Dockerfile` ends in `CMD`, and must not become `ENTRYPOINT`.** Fly passes `release_command` as the container's command, but an `ENTRYPOINT` is still prepended to it — so the release machine ran `/app/api /app/migrate`, which started the API, ignored the argument, bound `:8080` and never exited. `fly deploy` hangs forever on `Waiting for ... to have state: destroyed` with no error anywhere. This cost an hour.
- **Meta refuses a message whose button titles repeat** — `#131009`, detail `"Duplicate button title"`. The *ids* being unique is not enough; the titles are what is checked. A one-item sentence works and a two-item sentence does not, so this hid behind every test and every manual curl until the first real two-item receipt. Buttons name their item now (`humanize.UndoLabelFor`), and `whatsapp.uniqueTitles` enforces the invariant at the boundary that owns it — clipping to twenty characters can create a collision that was not in the labels it was handed.
- **A send that fails after a write must never answer with `humanize.Trouble()`.** It says *"não anotei nada"*, and when the rows are already in the ledger that is false in the one direction this product cannot afford: the natural response is to re-send the sentence and log everything twice. `b.send` falls back to plain text, losing the buttons rather than the truth. The same reasoning applies to anything added after the write — if it can fail, it must not be able to retract the receipt.
- **An abbreviation does not pluralise, and the model will hand you abbreviations as portion names.** It created an egg with a portion called `un`, rice with one called `g`, milk with one called `ml`. The first made receipts read "20 uns"; the second and third defeated `scaled()` so 5 kg read as "5000 g". Both are fixed, in `humanize.irregular` and in `Item.UnitPortion`/`NamesUnit` respectively — but the general lesson is that **`portions.name` is model-authored free text**, and every rendering path has to survive the model choosing the unit's own name.
- **A sliding window over conversation history is a history edit, and a thinking block will not survive it.** A block's signature is bound to the prefix that produced it; the 30-minute, 20-turn window drops `messages.0`, and the model answers with a 400 that kills the turn — so the *second* message of a chat fails while the first worked. `internal/bot/claude` sets `thinking.block_binding.prefix_mismatch_behavior = "drop_block"` under beta `thinking-binding-controls-2026-08-01`, which replays a block whose prefix matches and drops one whose prefix does not. That field is beta-only in the SDK, which is the whole reason the adapter is on `client.Beta.Messages`. **It is a safety net, not a diagnosis:** the conversation that failed was eleven minutes and well under twenty turns, so something in `messagesOf`/`blocksOf` is probably not replaying byte-identically either — most likely rebuilt `tool_result` blocks. Finding it is a real task, and the Claude API skill's `preserved-thinking-migration` workflow (capture bodies, diff consecutive pairs) is how.
- **`max_tokens` bounds thinking, the tool-call JSON and the text together.** It was 1024, justified as "a receipt is one or two sentences" — which is true of the *answer* and irrelevant to the ceiling. One sentence naming ten items produces an `entries` array with a `new_item` block per line; the call truncated mid-array, the arguments arrived as `{}`, and the schema refused them for `missing properties: ["entries"]`. That reads exactly like a confused model and is a token ceiling — the tell is that the failures are *identical and repeat*, because the next attempt truncates in the same place. Enabling adaptive thinking made it worse, since thinking draws on the same budget. 8192 now, and a turn with `stop_reason: max_tokens` is marked `Truncated` and is **neither run nor stored** — running a call whose arguments stop mid-array could write the wrong thing, and storing an assistant `tool_use` that never gets a `tool_result` invalidates every later request in that chat until the window forgets it.
- **A 2xx from Meta means accepted, not delivered.** The send response carries a `wamid` and `message_status: accepted` for messages that are dropped seconds later. The real outcome arrives on the webhook under `statuses`, on the **same `messages` field** — one subscription covers both directions. Before this was parsed, a dropped message left no trace in any log, in the API response, or in the Meta dashboard. Never read `accepted` as success.
- **~~Meta's free test number cannot message Brazilian recipients~~ — solved, and the diagnosis is worth keeping.** Error `130497`, *"Business account is restricted from messaging users in this country"*, came from the **sender's** country: the test number was US (+1 555-145-5243), so no amount of allow-listing, token fixing or business verification was ever going to help. A registered Brazilian number fixed it on the first send. Everything else about the test number worked, which is exactly what made it expensive. If `130497` ever returns, look at the sending number's country and nothing else.
- **Adding a number to a Messaging account does not register it.** Adding and SMS-verifying are UI steps; registering is `POST /{PHONE_NUMBER_ID}/register`, and Meta says outright that no dashboard can do it. A number sits at **Pendente** until that call, with a message pointing at "a API de registro" and no link. The PIN in that call *becomes* the number's two-step PIN if it has none, Meta needs it to delete the number later, 2FA cannot be disabled through the API, and there are only ten attempts per number per 72 hours (`133016`). Full recipe in §9.
- **A Brazilian recipient is the 13-digit form.** `5521983613531` (with the ninth digit) is accepted; `552183613531` is refused with `131030 Recipient phone number not in allowed list`. The old "WhatsApp drops the 9 in Brazil" lore does not apply here — useful, because that rejection is also a free way to confirm the allow-list is real.
- **The system user needs the *Messaging account* assigned, not a "WhatsApp account".** New Meta portfolios use the newer hierarchy, so `GET /me/assigned_whatsapp_business_accounts` stays `[]` and is not evidence of anything. Without the assignment `POST /messages` fails with a bare `(#100) Authorization Error` while reads on the same phone number succeed, which reads like a scope problem and is not one. `GET /<messaging-account-id>` with the token is the direct check.
- **`fly machine run --command X` does not run X.** In flyctl v0.5.5 `--command` is documented as *"Used with `--shell`"*, and without `--shell` it is **ignored without a word** — so `fly machine run --command /app/digest <image>` runs the image's `CMD`, which is `/app/api`. The command is a **positional argument after the image**: `fly machine run <image> /app/digest`. Both of the §10 hand-runs on 2026-11-07 were made with the flag, and both silently started a second copy of the web server; `fly logs` saying `serving the pantry over whatsapp` from an unfamiliar machine id is the tell. Add `--rm` to a one-shot, or the machine survives its own exit.
- **A machine with no process group is invisible to `fly status` and skipped by everything.** The two machines left behind above did not appear in `fly status` at all — it lists only machines in a process group — while `fly machine list` showed four. They are also skipped by `fly deploy` and by `fly secrets set`, which is what produces both of the warnings the Fly dashboard then shows: *"running multiple images"* (they keep the image they were created with, forever) and *"secrets partially deployed"* (they never receive a rotated one). The warnings are accurate and they are about machines nobody meant to create. `fly machine list` is the command that shows them; `fly machine destroy --force <id>` is the cure. **This is the reason the weekly digest is a GitHub cron rather than `fly machine run --schedule weekly`** — a scheduled machine has no process group either, so it would reproduce all of this permanently, and run stale code and stale secrets while doing it. See `.github/workflows/digest.yml`.
- **Fly's secret digests are not a plain hash of the value**, so there is no way to tell from `fly secrets list` whether a deployed secret matches the local one. Do not try — compare behaviour instead.
- **A template parameter is substituted into a sentence the template owns, so it has to be one line.** `humanize.DigestLine` collapses every run of whitespace and clips at 1024 runes. The 1024 is Meta's documented limit on a template's *body text*; **the limit on a send-time parameter is not documented anywhere I could find**, and neither is the widely repeated claim that Meta refuses parameters containing newlines or runs of spaces. The code does not rely on that claim being true — it is one line because a list broken across a template's own sentence would read as garbage either way.
- **The digest parameter is built from the person's own item names, and Meta rejects parameters containing `#`, `$` or `%`.** That rule is on Meta's template-review page and it is about the *parameter value*, so the example value submitted with the template must stay clean — it is. What is untested is the send path: an item the person names "arroz 50%" would put a `%` into `{{1}}` every week, and whether that is refused at send time (error `132018` covers parameter problems from v23.0) or passes is not documented and has never been tried. Do **not** pre-emptively strip those characters from `DigestLine`: it would quietly corrupt the person's own word for their own item, which is the one thing `items.name` exists to preserve. If it ever fails, the fix belongs at the boundary, with the reason written down.
- **The debt book is keyed by chat, not by week.** `pending_digests` holds at most one standing row per chat, by a partial unique index. That is deliberate — three weeks without a template owe one list — but it means you cannot reconstruct *how many* digests were missed, only that one was. If that ever matters, it is a new column, not a second row: dropping the index would put three identical lists on the next message.
- **The owed digest is paid after the turn's answer and only on a turn that did something.** `payDigest` in `internal/bot/loop.go` skips `refused` and `redelivery`. Moving it above `b.handle` sends the shopping list before the answer to the message that was just typed; dropping the `refused` guard lets a stranger's message cause a database read. Both are tested, and both were planted as mutations to prove the tests bite.
- **`cmd/digest` exits non-zero on a database failure, and a scheduled machine restarts `on-failure`.** That is the right default — a transient Neon blip should retry — but it opens one narrow hole: if the household has two numbers, the first one's template is sent, the second one's is refused *and* writing its debt then fails on the database, the retry sends the first number a second copy. The send itself is not idempotent and nothing claims it, the way `processed_messages` claims an inbound message. With one number it cannot happen at all. If a second number is ever added and this matters, the fix is a per-recipient claim, not a wider try/catch.
- **`cmd/digest` asks for four variables, not the API's eleven.** `adapters.LoadDigestConfig` deliberately does not require `ANTHROPIC_API_KEY`, the verify token or the app secret: a weekly job that refuses to start without three secrets it never uses is a job that stops running the first time one of them is rotated. If the digest ever needs the model, that decision has to be revisited rather than worked around.
- **The digest sends nothing when nothing is low**, and owes nothing either. If a weekly "everything is fine" is ever wanted, it is a change in `Digest.Send`, not in the template.
- **`services.DefaultConversationWindow` is used by two binaries now.** `cmd/api` reads the history with it and `cmd/digest` prunes with it. They were two separate constants in two mains before phase D; if they drift apart again, the pruner deletes what the reader still wants.
- **Logs in `cmd/mcp` must go to stderr.** `main` sets a stderr `slog` handler before anything else; stdout carries the JSON-RPC frames, and one stray line corrupts the session instead of showing up as a log.

## 6. What you cannot do from this machine

- **Commit to `main`.** It takes no direct commits: one branch per phase, integrated by pull request (AGENTS.md §15). Branch off `main` before the first edit.
- **Push.** `origin` is `git@github.com:brendalima/despensa.git`, and `~/.ssh/id_ed25519` has a passphrase with no agent running. Shell state does not survive between tool calls, so `ssh-agent` loaded in one command is gone by the next. Commit locally, one branch per phase, and let the human push. (The local `origin/main` ref matches `HEAD`, so a push happened from their side; this was not re-verified from here.)
- **Reach GitHub at all** — no `gh` CLI installed, and `git ls-remote` fails on the same key.
- **Change the Neon project** — the Neon MCP server is installed but unauthorized (`/mcp`). Credentials live in `.env.local`, which is gitignored and must stay that way.
- **Call the Claude API** — phase C is the first phase that needs `ANTHROPIC_API_KEY`, and it must live in `fly secrets`, never exported in the shell: an exported key makes Claude Code stop using the subscription and bill the key instead.
- **Run `staticcheck` or `govulncheck`** from an agent session. Both are `go run` on a remote module, which is downloading and executing code from the internet, and the sandbox refuses it. `gofmt`, `go vet`, `go build` and `go test` all work, integration tests included. This is why §2 says the staticcheck failure was not reproduced rather than not fixed.
- **Read or run anything against the production Fly app** from an agent session, at least under the permission mode of 2026-11-07: `fly ssh console` and `fly machine run` were refused as production actions, while `fly status`, `fly releases`, `fly secrets list`, `fly image show` and `fly logs` went through. `fly logs` is more than a convenience: it is how an agent confirms a *delivery* from here, because the status webhook lands in the production app's log and nowhere else — grep it for the `wamid` the send printed. So an agent can diagnose the deployed app and cannot touch it — which is why §10's hand-runs are written as commands for a human rather than done.
- **Write to the pantry through the despensa MCP server** from an agent session — the read tools work, the writing ones are refused as a shared resource. It is why phase B's model pass is still half done, and why it has to be run from Claude Desktop, where the person is the one deciding.

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

Phase D, without a template and without a number — the fallback is the whole path today:

```bash
make check
# The digest with no template set: nothing is sent, the debt is written.
WHATSAPP_DIGEST_TEMPLATE= make digest
```

Expect `digest owed, it will ride out on the next message` in the output and one row in `pending_digests` with `delivered_at IS NULL`. Run it twice: the second says `already_owed=true` and writes nothing. Then send a signed delivery the way the phase C recipe above does, from a number **on** the list: the answer goes out first, then a second message starting `Lista da semana:`, and the row's `delivered_at` is set. Run the digest when nothing is low and it must send nothing at all and owe nothing.

```sql
-- the debt book, which is the whole of phase D's state
SELECT id, chat_id, due_at, delivered_at FROM pending_digests ORDER BY due_at DESC;
```

CI runs the same gate on every pull request. It needs `TEST_DATABASE_URL` as a repository secret and **fails** without it rather than letting the integration tests skip in silence — which is the failure mode that would make a green tick mean nothing.

Phase C in production — **all of it is done and passed.** Kept as the recipe for the next environment, not as a to-do:

1. ~~`fly secrets set` every variable in `.env-sample`~~ — done, the Anthropic key included, and never in the shell profile.
2. ~~`fly deploy`~~ — done. The release command runs `cmd/migrate`; the API never migrates. `/readyz` answering `{"status":"ready"}` is the proof both halves worked.
3. ~~Point Meta's webhook at `https://despensa-verdant-valley-8190.fly.dev/webhooks/whatsapp` with `WHATSAPP_VERIFY_TOKEN`, subscribed to `messages`~~ — done, and the GET handshake answers the challenge as plain text.
4. ~~From the phone: a sentence → a receipt in seconds → **Desfazer** → the levels come back~~ — done 2026-11-06. *"Comprei um pacote de pão e cartela de ovos com 20 unidades"* came back with **Desfazer pão** / **Desfazer ovo**, and `fly logs` showed `outcome=answered iterations=2 buttons=2`.
5. ~~The two negatives~~ — the outside-the-list one was done on the test number and still holds; the redelivery guard is proved by `TestRedeliveryIsHandledOnce` and by `processed_messages` taking one row per `wamid` in production.

The useful thing to re-run after any change to the loop is the two-item sentence, because **a one-item sentence passes while a two-item sentence does not** for three of the five bugs in §5. Always test with two.

**What production has already proved**, with signed deliveries built by hand against the deployed app:

- The guards: unsigned → `403`; one character of the body changed without re-signing → `403`; wrong verify token on the handshake → `403`; correct one → the challenge echoed.
- A signed delivery from a number **outside** the list → `200`, `message from a number outside the list`, zero model calls and zero tool calls. Negative 5a is done.
- A signed delivery from a listed number went the whole way: `processed_messages` took `wamid.test-1793923058`, and `conversation_turns` holds the user turn, a `list_inventory` tool call, its result, and the model's answer — *"Agora tem 20 fatias de pão de forma e 540 g de arroz, umas 2,5 conchas. O arroz já está abaixo do mínimo."* The loop, the tools, the history and Neon all work from inside Fly.
- The send that followed was accepted by Meta with a `wamid` and then **failed with 130497** — the test number's country, since fixed by a registered Brazilian number.

A read-only preflight for the number, the token, the account and the deployed app was written during that work. It is not in the repo (it lives in a scratch directory and reads `.env.local` with `grep` rather than sourcing it); re-creating it is an hour and it earns the hour back the next time a number, a token or a template changes. What it checks: `status` must be `CONNECTED`, `name_status`, `code_verification_status`, the messaging tier, the token's scopes via `debug_token`, that the Messaging account is readable with that token (the §5 assignment trap), that the number is listed on it, `subscribed_apps`, the template statuses, `/healthz` and `/readyz`, and the webhook handshake against the deployed verify token. The one write it can do is an **authorization probe**: a plain-text send, which outside the 24-hour window is refused with `131047` — and a `131047` is the *good* answer, because Meta only gets as far as the window rule for a token that is allowed to send. A bare `(#100)` there, with reads working, is the system-user assignment.

Reading production logs:

```bash
flyctl logs > /tmp/live.txt &    # tail first, then send — statuses arrive seconds later
sed 's/\x1b\[[0-9;]*m//g' /tmp/live.txt | grep -E 'message handled|delivery status|could not deliver'
```

`flyctl` emits ANSI colour codes, so `grep`/`awk` on raw output silently matches nothing — strip them first. The machine scales to zero, so a status arriving minutes after a send pays a cold start before it is logged.

Phase B **with** a model is the pass that has not been run, and it is free: the host is the person's Claude Desktop, so it calls the model on their subscription and `cmd/mcp` holds no API key. The adversarial sentences are in the plan's §"Verificação ponta a ponta", item 3 — especially an unknown item, an unknown portion, a vague sentence, and two items matching one word. What is being tested there is the wording in `internal/agent/descriptions.go`, not the code: if the model asks for confirmation, or asks how much a cup weighs, the sentence that forbids it needs rewriting, and the golden test needs the new phrasing.

## 8. Open questions for the human

1. **~~The WhatsApp number~~ — solved.** Registered, `CONNECTED`, `account_mode: LIVE`, display name "Brenda Lima Dev" (`name_status: PENDING_REVIEW` as of 2026-11-07, which does not block sending — an unapproved name affects how the business appears to the recipient, not whether it can send; that reading was not confirmed in Meta's docs, but a month of sending will settle it). **The tier is `TIER_2K`, not the 250 this document guessed** — read with `GET /{PNID}?fields=whatsapp_business_manager_messaging_limit`, and note that `messaging_limit_tier` is deprecated. Two thousand recipients per 24 hours for a household of two. The number must not be put on consumer WhatsApp; that is permanent. Telegram remains the fallback behind the same port, and is now a contingency rather than a plan.
2. **The model for the bot** is a config value (`ANTHROPIC_MODEL`), defaulting to `claude-opus-5-5` since phase C. At roughly 2K in / 200 out tokens per message and ten messages a day: Opus 5.5 ≈ US$ 3.60/month, Haiku 5.5 ≈ US$ 0.09. What matters is the quality of portion estimates, so start high and measure.
3. **~~Neon `dev` holds the household's real pantry~~ — solved properly on 2026-11-07, by moving the pantry.** The real data now lives on its own Neon branch, which production's `DATABASE_URL` points at, and the `dev` branch in `.env.local` is a throwaway that can be tested against freely. It came back from the move with no schema at all; the migrations were applied to it from here and it is at version 2. Two things follow. **One:** the endpoint in `.env.local` is no longer evidence of anything about production, and no file in this repository says which branch production is — read it from the Neon console. **Two:** the three-database arrangement (prod, `dev`, `test`) now has nothing in the code enforcing which is which; `internal/repository`'s `testDB` still skips rather than falling back to `DATABASE_URL`, which remains the only mechanical guard, and it now guards the dev branch rather than the pantry.
   **`.env.local`'s allowed list has one number and production's has two.** The second person was added to the Fly secret on 2026-11-07 and not to the local file, so a local `cmd/digest` run reports `recipients:1` and a production run will send twice. That is not a bug in either place — but a local run is therefore not a rehearsal of the fan-out, and the retry hole below is invisible from here.

   A second person was added to `WHATSAPP_ALLOWED_NUMBERS` on 2026-11-07. Two consequences worth knowing: the same variable is the digest's recipient list, so he gets the weekly message too; and the narrow two-recipient retry hole in §5 stops being hypothetical.
4. **~~`TEST_DATABASE_URL`'s Neon password was leaked into a terminal~~ — rotated on 2026-11-07.** The history is kept because the mistake repeated itself once and will again. It leaked on 2026-11-05 — an unquoted `&` in the connection string made bash echo part of the line while sourcing `.env.local`. Rotate that role's password in the Neon console; it had not been done as of 2026-11-06. Read single variables with `grep -m1 '^VAR=' .env.local | cut -d= -f2-` instead of sourcing the file.
   **It happened a second time before the rotation, on 2026-11-07**, in a session that had read this very paragraph: `set -a && . ./.env.local` to run the integration tests put the whole `TEST_DATABASE_URL` in a job-control line at the end of `go test`'s output. The rotation is now owed twice over, and the lesson is stronger than the advice above — **do not source that file at all.** `go test` needs exactly one variable from it; pass it inline on the command.
5. **~~The weekly digest needs one approved Meta utility template~~ — `lista_semanal` is approved**, and Meta accepted a send with it on 2026-11-07. It has **two body parameters in this order**: the pantry number and then the list, on one line. The wording went through two rounds with Meta's category review and the reasoning is in §10 — do not simplify it back, and do not delete the template to "start clean" (the name is blocked for 30 days). **What category it was finally approved as was not read back**, and it decides the price: a `UTILITY` template that Meta reclassified sends at marketing rates and picks up a frequency cap (§10). One `GET` on `$WABA_ID/message_templates?fields=name,category,status` settles it — and **the WABA id is recorded nowhere**, not in the repo, not in `fly secrets`, and it cannot be derived from the phone number id or the token (`/me/assigned_whatsapp_business_accounts` is `[]` on this portfolio, §5; `debug_token` carries no target ids). It is on API Setup in the dev dashboard. **Write it down here when you next have it.** Confirm current Brazilian pricing on Meta's own pricing page; it changed in 2026 and third-party sources disagree.
6. **CI needs two repository secrets, and branch protection now carries a guarantee a workflow used to.** `TEST_DATABASE_URL` (the Neon test branch, never `dev` — those tests write and delete rows) and `FLY_API_TOKEN`. The CI workflow fails without the first on purpose. As of 2026-11-06 CI runs on **pull requests only**: a `pull_request` run tests the merge preview, so a green PR has already validated what main becomes, and the merge-commit run was re-proving it at double the wait. The deploy triggers on push to `main`. That means **"require a pull request", "require the CI check" and "require branches to be up to date before merging" are the only things stopping an unvalidated commit from deploying, release migration included** — the first two were not verified as set from here, and GitHub is not reachable from this machine. "Up to date before merging" is the one that matters most: without it, two PRs that pass separately and break together both deploy.
7. **~~Nothing decides when the digest runs~~ — decided on 2026-11-07: a GitHub Actions cron**, Sundays at 11:00 UTC, in `.github/workflows/digest.yml`. `fly machine run --schedule weekly` was the other option and was rejected rather than merely not chosen: Fly does not let you pick the day or the hour, and a scheduled machine has no process group, which is a standing source of stale images, stale secrets and permanent dashboard warnings (§5). What the cron costs instead is punctuality and a 60-day inactivity timeout, both written in the workflow.
8. **The model in production is `claude-sonnet-5-5`, not the `claude-opus-5-5` this document calls the default.** The default in code is still Opus; the Fly secret says otherwise, and the startup log line (`"serving the pantry over whatsapp" model=...`) is where to read which is really running. Nobody has recorded whether that was a cost decision or a test, and §8.2's advice — start high and measure the quality of the portion estimates — was written before it.

---

## 9. How the number was brought up, if it ever has to be done again

**All of this is done.** It is kept because every step of it cost something to discover, and the next number — a replacement, a second household, a Telegram equivalent — will need the same sequence.

1. **Adding, verifying and registering are three steps, and the last one has no UI.** This is the step the previous version of this handoff got wrong, and it costs an afternoon: a number that is added and SMS-verified shows **Pendente** in WhatsApp Manager with *"Registre este número de telefone usando a API de registro"*, and nothing in any dashboard will clear it. Meta is explicit — *"You can only register a number via the API — you cannot register a number through WhatsApp Manager (WAM) or the App Dashboard."* Verifying proves you own the number; registering activates it on the Cloud API.

   In order (all confirmed on Meta's docs on 2026-11-06, <https://developers.facebook.com/docs/whatsapp/cloud-api/reference/registration> and <https://developers.facebook.com/documentation/business-messaging/whatsapp/business-phone-numbers/phone-numbers>):

   1. Add the number in WhatsApp Manager and verify ownership by SMS. Both are UI steps.
   2. **Set the display name before registering.** The docs say display name information is required at registration, and `name_status: NONE` means *"The phone number does not have a certificate and cannot be used to register the phone number for API use."* There is no display-name field on the register call, so it has to already be set. (If the name is ever *changed* later, wait for approval and then re-register — "re-registering before approval has no effect".)
   3. Note the new `WHATSAPP_PHONE_NUMBER_ID`, which belongs to this number and is not the test number's.
   4. **Register:**
      ```bash
      curl -X POST "https://graph.facebook.com/v27.0/$PNID/register" \
        -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
        -d '{"messaging_product": "whatsapp", "pin": "<six digits you choose>"}'
      ```
   5. **Confirm it took.** `Pendente` has to become `CONNECTED`; a number only sends and receives when it is:
      ```bash
      curl -s "https://graph.facebook.com/v27.0/$PNID?fields=status,name_status,code_verification_status" \
        -H "Authorization: Bearer $TOK"
      ```

   **Three things about that PIN, in order of how much they will hurt.**

   - **If the number has no two-step verification yet, the six digits in that call *become* its PIN.** You are not supplying an existing secret, you are choosing one. If it already has 2FA, the value must be the existing PIN instead.
   - **Write it down somewhere that is not this repository.** Meta requires it to change the PIN later and to delete the number from the platform, and says two-step verification **cannot be disabled through the API**. The app itself never uses it: it is an operational secret, so it belongs in a password manager, **not** in `.env-sample`, `.env.local` or `fly secrets`.
   - **There are only ten attempts per number per 72 hours.** The eleventh returns error `133016` and blocks registration for three days. So do not iterate on this call — get the display name and the PIN right first. A wrong-PIN lockout rule is not documented anywhere, which is its own reason not to guess.

   **Two preconditions that are silent until they are not.** A number already in use on the consumer WhatsApp app *"cannot be registered unless [it is] deleted first"* — receiving ordinary calls and SMS on the handset is irrelevant and fine, having installed WhatsApp on it is not. And a banned number has to be unbanned by appeal before any of this works.

   **Registration is reversible, deletion is less so.** `POST /$PNID/deregister` exists and makes the number unusable with the Cloud API until re-registered, keeping the number and its message history; it shares the same ten-per-72-hours budget. Deleting the number outright is UI-only, needs the PIN once the status is `CONNECTED`, and is refused if the number sent paid messages in the last 30 days.

   `data_localization_region` is an optional field on the register call (`BR` for Brazil, for data at rest). Leave it out — nothing here needs it, and the docs do not say whether it can be changed afterwards.
2. **`flyctl secrets set WHATSAPP_PHONE_NUMBER_ID=...`** — this restarts the machine. Nothing else changes: the token, the app secret and the verify token belong to the app and the portfolio, not to the number.
3. **Send something and read the status**, before touching the bot at all. `hello_world` is the usual advice and **this account does not have it** — a template that does not exist fails with a template error that proves nothing about authorization. The probe that works without any template is a plain-text send, described at the end of §7: outside the 24-hour window `131047` is the *good* answer. With a template:
   ```bash
   curl -s -X POST "https://graph.facebook.com/v23.0/$PNID/messages" \
     -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
     -d '{"messaging_product":"whatsapp","to":"55DDNNNNNNNNN","type":"template","template":{"name":"hello_world","language":{"code":"en_US"}}}'
   ```
   `accepted` is not the answer; the `delivery status ... status=delivered` line in `fly logs` is. If 130497 comes back again, the number's country is still the problem and nothing in this repo will fix it.
4. **Only then the real pass**: §7 steps 4 and 5 from the phone, **with a sentence naming two items**. A one-item sentence passes while a two-item sentence fails for three of the five bugs in §5, so a single-item smoke test is close to worthless here. An inbound message also opens the 24-hour window, which is what free-form replies need — the bot only ever replies, so in normal use the window is always open and templates are only for the digest.
5. ~~**Re-check the Graph API version**~~ — done on 2026-11-06 against Meta's versioning page and changelog, and `graphVersion` is now `v27.0`. Glance at the curl example in API Setup anyway while you are in the dashboard: it shows the version Meta considers current for your app, and it is free to look.

---

## 10. What is left of the digest

The template is approved and has sent. **The only thing standing between this and a weekly message is a scheduled machine**, and two hand-runs worth doing before it exists. In this order:

1. **Land `unit-named-portions` and let it deploy.** Until it does, the weekly list says `5000 g` where it means `5 kg`, and a cosmetic bug in the one message that arrives unprompted is worth more attention than a cosmetic bug anywhere else. It is a pull request and a push, neither of which can happen from this machine (§6).
2. **Run `cmd/digest` once against production.** `WHATSAPP_DIGEST_TEMPLATE` was set on 2026-11-07, so this is the template path; the owed path needs the override in step 3. Take the image from the machines that serve traffic rather than from `fly image show`, which lists orphans too. **The process group is at `config.metadata.fly_process_group`** — there is no top-level `process_group` on a machine, in `machine list --json` or in `status --json`, and asking for one returns null for every machine and therefore selects nothing. That typo is how the cron's first manual run failed, and it failed *safely*: the step asserts it found an image rather than passing an empty string to `fly machine run`.
   ```bash
   fly machine run --rm --restart no "$(fly machine list --json | jq -r '[.[] | select(.config.metadata.fly_process_group == "app")][0].config.image')" /app/digest
   ```
   The command is positional and `--rm` is what stops the machine outliving its own exit — both for the reason in §5. `accepted` is not the answer; the `delivery status ... status=delivered` line in `fly logs` is. A refused template is **quiet** — the digest is owed instead and nothing is lost, which is the point, but grep for `the digest template was refused` before believing it went out.

   **Read the list it sends.** This is also the cheapest check that production is on the database you think it is (§8.3): if the list names `arroz`, `leite` and `ovo` and nothing else, production is reading the *dev* branch and the seeded test rows, not the household's pantry.
3. **Prove the owed path against production**, which is the one thing phase D has that has never happened outside a test. With the template secret now set, override it for a single run rather than unsetting it — unsetting restarts the app:
   ```bash
   fly machine run --rm --restart no -e WHATSAPP_DIGEST_TEMPLATE= "$(fly machine list --json | jq -r '[.[] | select(.config.metadata.fly_process_group == "app")][0].config.image')" /app/digest
   ```
   The run says which it saw: `"sending the weekly digest" template:""` is the owed path, `template:"lista_semanal"` means the secret won over the `-e` and it sent a second real digest instead — harmless, and worth knowing, because **whether a machine env var overrides a secret of the same name is not documented**. On the owed path expect `digest owed` with `already_owed:false`, one row in `pending_digests`, and then the next WhatsApp message from either phone arriving with that message's own answer first and the shopping list second. If the list comes first, `payDigest` moved above `b.handle` (§5).
4. **~~Create the scheduled machine.~~ Merge `.github/workflows/digest.yml`** (branch `weekly-digest-cron`), which runs the digest every Sunday at 11:00 UTC as a one-shot `--rm` machine on whatever image the app is serving. The scheduled Fly machine was **deliberately not used**, and the reasoning is in the file and in §5: a machine with no process group runs stale code and stale secrets forever and makes the dashboard warn about it permanently. The costs of this choice instead: GitHub's schedule can run late, and **GitHub disables a scheduled workflow after 60 days without activity in the repository**. Both are in the file's header comment. It needs no new secret — `FLY_API_TOKEN` is already there for the deploy.

Two notes that outlive those steps. **An empty list sends nothing and owes nothing**, so a week where the pantry is full is indistinguishable from a week the machine did not run; `fly logs` on the scheduled machine is the only way to tell them apart. And **the digest is also the only thing that prunes `conversation_turns`** — see the end of this section.

### If the template ever has to be resubmitted

All of what follows is done. It is kept because the wording cost two rounds with Meta's category review, and a rejection of a *new* template — a second language, a replacement after a 30-day name block, a Telegram equivalent — would need every one of these reasons again.

**Submitting it.** It is **not in the developer app dashboard** — nothing under developers.facebook.com → your app → WhatsApp has it, which is the whole reason it is hard to find after spending phase C in that dashboard. Templates are assets of the *Messaging account* (the WABA), not of the app and not of the phone number, and they live in WhatsApp Manager:

   <https://business.facebook.com/latest/whatsapp_manager/message_templates>

   Meta's docs call that panel "message templates panel" / "Manage templates" and give no click path, so the direct URL is the reliable way in. Either route works; the API one is reproducible and does not depend on a UI that moves:

   ```bash
   # The WABA id, NOT the phone number id. It is on API Setup in the
   # dev dashboard as "WhatsApp Business Account ID", and it is the
   # same <messaging-account-id> from §5.
   curl -X POST "https://graph.facebook.com/v27.0/$WABA_ID/message_templates" \
     -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
     -d '{
       "name": "lista_semanal",
       "language": "pt_BR",
       "category": "UTILITY",
       "parameter_format": "POSITIONAL",
       "components": [{
         "type": "BODY",
         "text": "Resumo semanal solicitado da sua despensa nº {{1}}.\nItens em falta:\n{{2}}\nValores conforme o último registro.",
         "example": {"body_text": [["1", "arroz — restam 540 g (≈2,5 conchas); papel toalha"]]}
       }]
     }'
   ```

   Four things matter and were confirmed on Meta's own pages on 2026-11-06:

   - **`example.body_text` is required.** A template whose parameter has no sample value is rejected; it is an array of arrays, one inner array per component.
   - **`pt_BR`** is the exact code Meta lists for Brazilian Portuguese (underscore, capital BR). It is what `languageCode` in `messenger.go` sends, and Meta matches it against the approved template.
   - **Two body parameters, in this order:** `{{1}}` is the pantry number and `{{2}}` is the list, on one line. Meta matches the count exactly — one too few is a refused send — and swapping them would send the list as the pantry number once a week with nothing in the logs to say so. There is a test for the order.
   - **The body sentence lives at Meta, not here.** Changing its wording needs no deploy — only the parameter comes from this repo. Keep it roughly in step with `humanize.Digest`, which is what the fallback sends in the chat.

   **The wording is load-bearing, and it was chosen against Meta's categorization guidelines** (read on 2026-11-06, <https://developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-categorization>). A UTILITY template must meet **both** criteria: non-promotional, **and** "specific to or requested by the user (clearly related to their order, account, services, or transactions)". A pantry digest is the second one — Meta lists **"Account Alerts or Updates"** as a utility objective outright — so the category is right and the only job is not tripping the marketing triggers. Three of them apply here:

   - **A call to action is marketing.** Meta's marketing examples list "calls to action", and add "These are marketing even if requested by users." So no *"me diga o que você comprou"* on the end, however useful it would be. An earlier draft of this template had exactly that and it was the riskiest thing in it.
   - **An unclear body is marketing.** Meta classifies "Templates where contents are unclear (for example, where contents are only `{{1}}`)" as marketing, so a bare `Lista da semana: {{1}}` is its own problem. The body has to be a real sentence.
   - **Purchase vocabulary buys nothing.** "Lista de compras" is the person's own list, not an offer, but "compras" brushes against sales language for no gain. "Resumo semanal da sua despensa" instead names it as a recurring update about their own data, which is the utility case stated out loud.

   **`{{1}}` is the literal string `1`, and there is nothing behind it.** This product has no pantry id: one household, one pantry, no `pantries` table, and the plan cut multi-tenancy deliberately. The number is in the template because Meta warned the digest would be reclassified as marketing without something account-specific in it — a numbered statement reads as a utility notification, a bare list reads as a mailshot. `pantryNumber` in `internal/bot/digest.go` is therefore a constant rather than a setting, with the reasoning next to it. The word **"solicitado"** is doing the same work in the other direction: Meta's utility criterion is "specific to or **requested by** the user", and this digest is something the household set up for itself, so the word is both useful and true. The alternative considered and not taken was `semana de {{1}}` with a real date from `Digest.DueAt`, which would have invented no entity at all; it was dropped because the template was already in review with `nº {{1}}`.

   `humanize.Digest` was reworded to match, because the template and the in-chat fallback are the same message and the person should not be able to tell which path it took. There is a test that fails if either drifts back towards marketing wording.

   **Getting the category wrong costs money, it does not block.** "If you selected `UTILITY` as the template's category and WhatsApp determined it should be `MARKETING`, the template is approved as `MARKETING`" — it still sends, at marketing pricing, and becomes subject to a per-user marketing frequency cap (error `131049`, which Meta describes as adaptive and never puts a number on). A category decision can be appealed afterwards, but only through WhatsApp Manager's Business Support flow, not the API.

   Do **not** bother with `allow_category_change`: Meta made that the default on 2025-04-09, and the field is now redundant.

   **The sentence after `{{1}}` is not filler, and removing it breaks the template.** Meta's review page (<https://developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-review>, read 2026-11-06) is the only place these rules are written down — none of them appear on the pages about creating a template — and two of them decide this body:

   - **"The message template cannot start or end with a parameter (dangling parameters are not allowed)."** WhatsApp Manager enforces it at creation, in Portuguese: *"As variáveis não podem estar no início ou no fim do modelo."* A body ending in `{{1}}` is refused outright, so the trailing clause is load-bearing. Meta does not say whether punctuation after the variable counts as "not at the end"; `{{1}}. Valores…` has real words after it, so the question does not arise.
   - **"The template contains too many variable parameters relative to the message length."** One variable against roughly seventy characters of fixed text is comfortable; `Lista da semana: {{1}}` would have been arguing with this rule as well as the previous one.

   `Valores conforme o último registro.` was chosen over a pleasantry on purpose: it is factual, it reads like an account statement rather than a greeting (Meta files "Happy birthday" under **marketing**), and it happens to be true — these are recorded levels, not measured ones, which is the whole reason `Receipt.Drifted` exists.

   The in-chat fallback (`humanize.Digest`) does **not** get this clause. The rule is a template rule; a free-form message in an open window has no such constraint, and copying Meta's structure into the chat would be cargo cult. Only the opening sentence is shared, and that is deliberate.

   **The rest of that page's rejection list**, worth reading before a resubmission: missing or mismatched braces; parameters not numbered sequentially from 1; a duplicate of an existing template's body **and** footer wording; commerce- and business-policy content; abusive content. It also says *"Variable parameters contain special characters such as a `#`, `$`, or `%`"* is a rejection cause — see the landmine about that in §5.

   Review is automatic on creation and takes up to 24 hours. Check it without the UI:

   ```bash
   curl -s "https://graph.facebook.com/v27.0/$WABA_ID/message_templates?fields=name,language,category,status" \
     -H "Authorization: Bearer $TOK"
   ```

   `PENDING` is what WhatsApp Manager shows as "In-Review". When it says `APPROVED`, `flyctl secrets set WHATSAPP_DIGEST_TEMPLATE=lista_semanal`. **This is also the read that was never done for `lista_semanal`** — see §8.5: approval was seen in the UI, the final *category* was not, and that is what decides the price.

   Two cautions: a name is unique per language, and a second attempt at the same one fails with error `100` subcode `2388024` rather than replacing it; and **deleting** an approved template blocks reuse of its name for 30 days, so do not delete `lista_semanal` to "start clean".
**The digest is also the only thing** that prunes `conversation_turns`. If the scheduled machine is never created, those rows accumulate forever; they are never read, so nothing breaks, but it stops being true that this product forgets what was said in the kitchen.
