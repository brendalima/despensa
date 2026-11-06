# Handoff — despensa

For whoever picks up phase D's loose ends. Everything below was verified against the repo and the database, not recalled. Where something was not verified, it says so.

Last verified: 2026-11-06 · branch `weekly-digest` · phases A, B, C and D built; C deployed to Fly and blocked on a phone number, D blocked on an approved template.

> **Read first**
> - **Phase A is done and verified end to end**, against real Postgres 18 on Neon. The CLI (`cmd/despensa`) is the whole of it, and it works: create, buy, use, undo, drift, correct, shopping list.
> - **Phase B is done and verified against the protocol and the database, but not yet against a real model.** The six tools are in `internal/agent`, the stdio server in `cmd/mcp`. The one thing left is §7's Claude Desktop pass, which is the only way to find out whether the descriptions actually steer a model.
> - **Phase C is built, tested, deployed, and has never reached a phone.** `internal/bot` is the conversation, `internal/bot/whatsapp` and `internal/bot/claude` the two adapters, `cmd/api` the webhook. It runs at `https://despensa-verdant-valley-8190.fly.dev`, the webhook is configured in Meta, and a signed delivery goes all the way through Claude and Neon and back out. The last hop fails: Meta's free test number is US-based and **cannot message Brazilian recipients** (error 130497). A real number is bought and awaiting approval. See §9.
> - **Phase D is built and tested, and has sent nothing.** `cmd/digest` is the weekly list, `internal/bot/digest.go` the sending and the fallback, `pending_digests` (migration `00002`) the debt book. It needs an approved Meta template to send as a template, and the same phone number phase C is waiting on to send at all. Without the template it is not broken — it records the digest as owed and the next inbound message delivers it as plain text. See §10.
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

### Built in phase C, and verified in production up to the last hop

- **The conversation.** `internal/bot` holds the loop and the Portuguese system prompt (`prompt.go`, with a golden test over the sentences that invert the obvious). Three ports: `Messenger` and `Webhook` for the provider, `Model` for the model. The six tools come from `internal/agent` unchanged, so Claude Desktop and WhatsApp cannot disagree about what a word is worth.
- **The adapters.** `internal/bot/whatsapp` is the Cloud API: HMAC-SHA256 over the raw body with `hmac.Equal`, the GET subscription handshake, Meta's payload (text and both button shapes), and sending text, buttons and templates. `internal/bot/claude` is the only importer of the Anthropic SDK: `claude-opus-5-5` by default, effort `low`, `max_tokens` 1024, thinking left unset.
- **The HTTP border.** `internal/rest` has the webhook (403 on a bad signature **before** the payload is parsed, 200 before the work, `Wait()` for in-flight messages on shutdown) and `/healthz` (no database) / `/readyz` (database). `cmd/api` wires it, `adapters/api_config.go` reads and validates the new variables.
- **The short history.** `conversation_turns` is now used: `repository.Conversation` plus `services.Chat`, a 30-minute window and a 20-turn cap, with three integration tests against Neon. An assistant turn is stored as the model wrote it and replayed byte for byte.
- **The tests, 35 of them across five packages:** a number outside the list costs zero model calls and zero claims; a redelivery is handled once; the iteration cap holds at 3 in the test and the answer says so; a `undo:` button never reaches the model; the receipt carries one button per event with the right payload; a schema refusal comes back as an `is_error` result and the model gets a second turn; the history reaches the model and is stored; eight shapes of wrong signature all fail closed; Meta's payload tree is read and everything unreadable is skipped; the schemas reach the model closed, with the descriptions unchanged; a model refusal is an error, not an answer.
- **Deployment.** Multi-stage `Dockerfile` (static binary, `go mod download` before the source, non-root) and `fly.toml` with the migration as `release_command`, scale to zero, and the health check on `/healthz`. **Both are now built and running** on Fly app `despensa-verdant-valley-8190` (region `gru`): `/healthz` and `/readyz` answer, so the release command ran the migration and Neon is reachable from Fly.
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

### Not done

- **Phase B's model pass, the writing half.** A model has now read the six descriptions and called the two read tools (`list_inventory`, `shopping_list`) against Neon `dev`, and the descriptions steered it the way they are meant to on the cases that need no write: an unknown item produced a `record_events` call with `new_item` filled in and **no** confirming question, and a vague sentence produced a question rather than an invented quantity. The writing half was not run — the session that did this was not allowed to write to a shared database — so the three cases that matter most are still untested: an unknown **portion** (does it estimate, or ask how much a cup weighs?), **two items matching one word** (does it ask which?), and **drift** (does it ask for a number when `drift: true` comes back?). Those need one pass in Claude Desktop, which is free. See §7.
- **Phase C's last hop.** Everything up to the send is proven in production (see §7); no message has reached a phone, because of 130497. The write test (*"usei uma taça de arroz"* → receipt → **Desfazer**) has never been run, and neither has a real inbound message from a phone.
- **Phase D's two external halves.** The Meta template `lista_semanal` is not submitted, and the scheduled Fly machine is not created. The code handles the first being absent — the digest is owed and delivered on the next message — but nothing runs weekly until the machine exists. See §10.
- **The digest has never sent anything**, for the same reason phase C has not: there is no working number. The fallback path has been run only in tests, never against Meta.
- **`make check` still fails at `staticcheck`, and the reason in the last handoff was wrong.** v0.8.2 *is* staticcheck 2026.2.2, the newest release as of 2026-11-06, and its own notes say it supports the export data format of Go 1.27.2 — so "built against 1.26" was not it. The failure was not reproduced here either: this machine's session could not run `staticcheck` at all, because `go run` on a remote module is running code from the internet and was refused. **Everything else in the gate passes**, verified here: `gofmt -l .` clean, `go vet ./...` clean, `go test -race -count=1 ./...` green including all the Neon integration tests, and all five binaries build. CI runs staticcheck as its own step, so the next pull request is the first clean machine it can be diagnosed on.
- **`govulncheck` was not run here either**, for the same reason.
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
- **The Graph API version is pinned** (`graphVersion` in `internal/bot/whatsapp/messenger.go`, now `v27.0`). Checked against Meta's own versioning page and changelog on 2026-11-06: v27.0 is the one they call most recent, every version lasts at least two years, and the `v23.0` this used to be was good until 2027-10-08. So the bump is a refresh, not a fix — and it has **never been exercised against the live API**, because no send has ever reached a phone. The `hello_world` send in §9 step 3 is the first thing that will test it; if it fails in a new way, try `v26.0`, which is the newest version Meta's own availability table actually lists.
- **`ANTHROPIC_MODEL` defaults to `claude-opus-5-5` and effort is `low`**, set in code (`internal/bot/claude/claude.go`) because it is the shape of the work, not a knob. If estimates come out bad, raise the effort before changing the model: the plan's cost table says the model is cheaper than the WhatsApp messages either way.
- **`internal/bot/claude.Input.BaseURL` and `whatsapp.MessengerInput.BaseURL` exist for the tests.** They are the only reason the mapping can be checked without a key or a number; leave them, and do not set them in `cmd/api`.
- **The `Dockerfile` ends in `CMD`, and must not become `ENTRYPOINT`.** Fly passes `release_command` as the container's command, but an `ENTRYPOINT` is still prepended to it — so the release machine ran `/app/api /app/migrate`, which started the API, ignored the argument, bound `:8080` and never exited. `fly deploy` hangs forever on `Waiting for ... to have state: destroyed` with no error anywhere. This cost an hour.
- **A 2xx from Meta means accepted, not delivered.** The send response carries a `wamid` and `message_status: accepted` for messages that are dropped seconds later. The real outcome arrives on the webhook under `statuses`, on the **same `messages` field** — one subscription covers both directions. Before this was parsed, a dropped message left no trace in any log, in the API response, or in the Meta dashboard. Never read `accepted` as success.
- **Meta's free test number cannot message Brazilian recipients** — error `130497`, *"Business account is restricted from messaging users in this country"*. It is a US number (+1 555-145-5243) and the restriction is on the sender's country, so no amount of allow-listing, token fixing or business verification was going to help; sources disagree on whether verification ever lifts it for Brazil. A real number in a supported country is the fix. Everything else about the test number works, which is exactly what makes this expensive to diagnose.
- **A Brazilian recipient is the 13-digit form.** `5521983613531` (with the ninth digit) is accepted; `552183613531` is refused with `131030 Recipient phone number not in allowed list`. The old "WhatsApp drops the 9 in Brazil" lore does not apply here — useful, because that rejection is also a free way to confirm the allow-list is real.
- **The system user needs the *Messaging account* assigned, not a "WhatsApp account".** New Meta portfolios use the newer hierarchy, so `GET /me/assigned_whatsapp_business_accounts` stays `[]` and is not evidence of anything. Without the assignment `POST /messages` fails with a bare `(#100) Authorization Error` while reads on the same phone number succeed, which reads like a scope problem and is not one. `GET /<messaging-account-id>` with the token is the direct check.
- **Fly's secret digests are not a plain hash of the value**, so there is no way to tell from `fly secrets list` whether a deployed secret matches the local one. Do not try — compare behaviour instead.
- **A template parameter is substituted into a sentence the template owns, so it has to be one line.** `humanize.DigestLine` collapses every run of whitespace and clips at 1024 runes. The 1024 is Meta's documented limit on a template's *body text*; **the limit on a send-time parameter is not documented anywhere I could find**, and neither is the widely repeated claim that Meta refuses parameters containing newlines or runs of spaces. The code does not rely on that claim being true — it is one line because a list broken across a template's own sentence would read as garbage either way.
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

Phase C in production — **steps 1 to 3 are done and passed**, 4 and 5 are blocked on the number:

1. ~~`fly secrets set` every variable in `.env-sample`~~ — done, the Anthropic key included, and never in the shell profile.
2. ~~`fly deploy`~~ — done. The release command runs `cmd/migrate`; the API never migrates. `/readyz` answering `{"status":"ready"}` is the proof both halves worked.
3. ~~Point Meta's webhook at `https://despensa-verdant-valley-8190.fly.dev/webhooks/whatsapp` with `WHATSAPP_VERIFY_TOKEN`, subscribed to `messages`~~ — done, and the GET handshake answers the challenge as plain text.
4. From the phone: *"usei duas fatias de pão e uma taça de arroz"* → a receipt in seconds → **Desfazer** → the levels come back. `fly logs` must show one `message handled` line.
5. The two negatives, which are the whole point of the guards: a message from a number **outside** `WHATSAPP_ALLOWED_NUMBERS` → zero model calls and zero tool calls in `fly logs`; the same `message_id` replayed by hand → nothing happens.

**What production has already proved**, with signed deliveries built by hand against the deployed app:

- The guards: unsigned → `403`; one character of the body changed without re-signing → `403`; wrong verify token on the handshake → `403`; correct one → the challenge echoed.
- A signed delivery from a number **outside** the list → `200`, `message from a number outside the list`, zero model calls and zero tool calls. Negative 5a is done.
- A signed delivery from a listed number went the whole way: `processed_messages` took `wamid.test-1793923058`, and `conversation_turns` holds the user turn, a `list_inventory` tool call, its result, and the model's answer — *"Agora tem 20 fatias de pão de forma e 540 g de arroz, umas 2,5 conchas. O arroz já está abaixo do mínimo."* The loop, the tools, the history and Neon all work from inside Fly.
- The send that followed was accepted by Meta with a `wamid` and then **failed with 130497**. That is the only thing left.

Reading production logs:

```bash
flyctl logs > /tmp/live.txt &    # tail first, then send — statuses arrive seconds later
sed 's/\x1b\[[0-9;]*m//g' /tmp/live.txt | grep -E 'message handled|delivery status|could not deliver'
```

`flyctl` emits ANSI colour codes, so `grep`/`awk` on raw output silently matches nothing — strip them first. The machine scales to zero, so a status arriving minutes after a send pays a cold start before it is logged.

Phase B **with** a model is the pass that has not been run, and it is free: the host is the person's Claude Desktop, so it calls the model on their subscription and `cmd/mcp` holds no API key. The adversarial sentences are in the plan's §"Verificação ponta a ponta", item 3 — especially an unknown item, an unknown portion, a vague sentence, and two items matching one word. What is being tested there is the wording in `internal/agent/descriptions.go`, not the code: if the model asks for confirmation, or asks how much a cup weighs, the sentence that forbids it needs rewriting, and the golden test needs the new phrasing.

## 8. Open questions for the human

1. **The WhatsApp number** is still the only external blocker for phase C, and it is now a bought number awaiting Meta's approval (as of 2026-11-06). It must not be registered on regular WhatsApp; registering it moves it off the consumer app permanently. When it is approved, `WHATSAPP_PHONE_NUMBER_ID` changes and is the one secret to update on Fly — the test number's id is in the local `.env.local` and is now useless. The unverified Meta business portfolio tier (250 recipients/24h, no CNPJ) is enough for one household, but that came from third-party sources — confirm it in Meta's messaging-limits documentation before building on it. Telegram is the fallback behind the same port, not a replanning.
2. **The model for the bot** is a config value (`ANTHROPIC_MODEL`), defaulting to `claude-opus-5-5` since phase C. At roughly 2K in / 200 out tokens per message and ten messages a day: Opus 5.5 ≈ US$ 3.60/month, Haiku 5.5 ≈ US$ 0.09. What matters is the quality of portion estimates, so start high and measure.
3. **Neon `dev` holds test data** from the phase A run — `arroz` at 540 g after a deliberate drift experiment, `pão de forma`, and a "papel toalha" note. The phase B smoke run added `concha` = 220 g to rice's vocabulary and two ledger rows, one of them undone; the levels are where they were. Nobody has decided whether to keep any of it as a starting pantry or wipe it.
4. **`TEST_DATABASE_URL`'s Neon password was leaked into a terminal** on 2026-11-05 — an unquoted `&` in the connection string made bash echo part of the line while sourcing `.env.local`. Rotate that role's password in the Neon console; it had not been done as of 2026-11-06. Read single variables with `grep -m1 '^VAR=' .env.local | cut -d= -f2-` instead of sourcing the file.
5. **The weekly digest needs one approved Meta utility template** (`lista_semanal`), with **exactly one body parameter**: the code sends the whole list as that one parameter, on one line. Something like `Lista da semana: {{1}}`. Confirm current Brazilian pricing on Meta's own pricing page; it changed in 2026 and third-party sources disagree. The fallback is built and tested — without the template the digest is owed and delivered on the next inbound message — so approval is what makes it *weekly*, not what makes it work.
6. **CI needs two repository secrets** to be useful: `TEST_DATABASE_URL` (the Neon test branch, never dev or production — those tests write and delete rows) and `FLY_API_TOKEN` for the deploy. The CI workflow fails without the first on purpose. Neither was set from here; GitHub is not reachable from this machine.
7. **Nothing decides when the digest runs.** `fly machine run --schedule weekly` is the flag (confirmed on Fly's own `machine run` docs on 2026-11-06, which also says a scheduled machine's restart policy defaults to `on-failure`), but Fly does not let you choose *which* day or hour — it is "once a week". If Sunday morning matters, a GitHub Actions cron hitting the same binary is the alternative the plan already names.

---

## 9. The first thing to do when the number is approved

In order, because each step is cheap and the next one is only worth running if the previous passed.

1. **Register it** in Meta → WhatsApp → API Setup, verify it, set a display name. Note the new `WHATSAPP_PHONE_NUMBER_ID`.
2. **`flyctl secrets set WHATSAPP_PHONE_NUMBER_ID=...`** — this restarts the machine. Nothing else changes: the token, the app secret and the verify token belong to the app and the portfolio, not to the number.
3. **Send `hello_world` and read the status**, before touching the bot at all:
   ```bash
   curl -s -X POST "https://graph.facebook.com/v23.0/$PNID/messages" \
     -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
     -d '{"messaging_product":"whatsapp","to":"55DDNNNNNNNNN","type":"template","template":{"name":"hello_world","language":{"code":"en_US"}}}'
   ```
   `accepted` is not the answer; the `delivery status ... status=delivered` line in `fly logs` is. If 130497 comes back again, the number's country is still the problem and nothing in this repo will fix it.
4. **Only then the real pass**: §7 steps 4 and 5 from the phone. An inbound message also opens the 24-hour window, which is what free-form replies need — the bot only ever replies, so in normal use the window is always open and templates are only for the phase D digest.
5. ~~**Re-check the Graph API version**~~ — done on 2026-11-06 against Meta's versioning page and changelog, and `graphVersion` is now `v27.0`. Glance at the curl example in API Setup anyway while you are in the dashboard: it shows the version Meta considers current for your app, and it is free to look.

---

## 10. The first thing to do about the digest

The digest needs two things that are not code, and they are independent of each other.

1. **Submit the template.** It is **not in the developer app dashboard** — nothing under developers.facebook.com → your app → WhatsApp has it, which is the whole reason it is hard to find after spending phase C in that dashboard. Templates are assets of the *Messaging account* (the WABA), not of the app and not of the phone number, and they live in WhatsApp Manager:

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
         "text": "Resumo semanal da sua despensa. Itens em falta: {{1}}",
         "example": {"body_text": [["arroz — restam 540 g (≈2,5 conchas); papel toalha"]]}
       }]
     }'
   ```

   Four things matter and were confirmed on Meta's own pages on 2026-11-06:

   - **`example.body_text` is required.** A template whose parameter has no sample value is rejected; it is an array of arrays, one inner array per component.
   - **`pt_BR`** is the exact code Meta lists for Brazilian Portuguese (underscore, capital BR). It is what `languageCode` in `messenger.go` sends, and Meta matches it against the approved template.
   - **Exactly one body parameter.** The code sends the whole list as `{{1}}`, on one line.
   - **The body sentence lives at Meta, not here.** Changing its wording needs no deploy — only the parameter comes from this repo. Keep it roughly in step with `humanize.Digest`, which is what the fallback sends in the chat.

   **The wording is load-bearing, and it was chosen against Meta's categorization guidelines** (read on 2026-11-06, <https://developers.facebook.com/documentation/business-messaging/whatsapp/templates/template-categorization>). A UTILITY template must meet **both** criteria: non-promotional, **and** "specific to or requested by the user (clearly related to their order, account, services, or transactions)". A pantry digest is the second one — Meta lists **"Account Alerts or Updates"** as a utility objective outright — so the category is right and the only job is not tripping the marketing triggers. Three of them apply here:

   - **A call to action is marketing.** Meta's marketing examples list "calls to action", and add "These are marketing even if requested by users." So no *"me diga o que você comprou"* on the end, however useful it would be. An earlier draft of this template had exactly that and it was the riskiest thing in it.
   - **An unclear body is marketing.** Meta classifies "Templates where contents are unclear (for example, where contents are only `{{1}}`)" as marketing, so a bare `Lista da semana: {{1}}` is its own problem. The body has to be a real sentence.
   - **Purchase vocabulary buys nothing.** "Lista de compras" is the person's own list, not an offer, but "compras" brushes against sales language for no gain. "Resumo semanal da sua despensa" instead names it as a recurring update about their own data, which is the utility case stated out loud.

   `humanize.Digest` was reworded to match, because the template and the in-chat fallback are the same message and the person should not be able to tell which path it took. There is a test that fails if either drifts back towards marketing wording.

   **Getting the category wrong costs money, it does not block.** "If you selected `UTILITY` as the template's category and WhatsApp determined it should be `MARKETING`, the template is approved as `MARKETING`" — it still sends, at marketing pricing, and becomes subject to a per-user marketing frequency cap (error `131049`, which Meta describes as adaptive and never puts a number on). A category decision can be appealed afterwards, but only through WhatsApp Manager's Business Support flow, not the API.

   Do **not** bother with `allow_category_change`: Meta made that the default on 2025-04-09, and the field is now redundant.

   Review is automatic on creation and takes up to 24 hours. Check it without the UI:

   ```bash
   curl -s "https://graph.facebook.com/v27.0/$WABA_ID/message_templates?fields=name,language,category,status" \
     -H "Authorization: Bearer $TOK"
   ```

   `PENDING` is what WhatsApp Manager shows as "In-Review". When it says `APPROVED`, `flyctl secrets set WHATSAPP_DIGEST_TEMPLATE=lista_semanal`.

   Two cautions: a name is unique per language, and a second attempt at the same one fails with error `100` subcode `2388024` rather than replacing it; and **deleting** an approved template blocks reuse of its name for 30 days, so do not delete `lista_semanal` to "start clean".
2. **Create the scheduled machine**, on the image the API already runs:
   ```bash
   fly machine run --schedule weekly --command /app/digest --restart on-failure <image>
   ```
   Fly picks the day and hour, not you. It needs the same secrets the app has; a machine created this way inherits the app's.
3. **Before either, run it by hand once** against production, with the template unset, and watch the fallback: `fly machine run --command /app/digest <image>` should log `digest owed`, write one row in `pending_digests`, and then the next message from the phone should arrive with the answer first and the list second. That proves the whole path without spending a template.
4. **Then set the template and run it by hand again.** `accepted` is not the answer; the `delivery status ... status=delivered` line in `fly logs` is. If Meta refuses it, the digest is owed instead and nothing is lost — which is the point, but it also means **a refused template is quiet**. Grep for `the digest template was refused`.

The digest is also the only thing that prunes `conversation_turns`. If the scheduled machine is never created, those rows accumulate forever; they are never read, so nothing breaks, but it stops being true that this product forgets what was said in the kitchen.
