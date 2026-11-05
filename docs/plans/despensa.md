# Despensa: inventário de casa por WhatsApp

## Context

O problema não é falta de app de estoque doméstico — é que todos eles pedem
que eu pare, abra o app, ache o item e digite um número **no momento em que
estou cozinhando**. Eu não faço isso, então o estoque vira ficção em três dias
e o app vira lixo.

A conclusão do plano de vendas da Venus (`i-d-like-to-play-parsed-rabbit.md`)
se aplica inteira aqui: as pessoas já descrevem estoque em linguagem natural
(*"P 1x Bege, M 2x Bege"*), e um agente que entenda a frase é a interface que
combina com o modelo mental que já existe. A diferença é que lá o estoque é
contado em peças inteiras, e aqui **ninguém conta nada**: eu uso "uma taça de
arroz", "duas fatias de pão", "um copo de leite". Eu não vou pesar, e não
quero ser perguntado quanto pesa.

**Resultado pretendido:** mando *"usei duas fatias de pão e uma taça de arroz"*
no WhatsApp e recebo de volta `pão 18 → 16 fatias · arroz ~4,8 → ~4,6 kg
(≈26 taças)` com um botão **Desfazer**. Quando chego do mercado, mando
*"comprei 2 pacotes de arroz e um pão de forma"* e o estoque sobe. Uma vez por
semana recebo a lista de compras. Nunca abro um app, nunca meço nada.

**Estimativa aproximada é o objetivo, não uma concessão.** Um estoque que diz
"~4,6 kg de arroz" com 15% de erro é infinitamente mais útil que um campo
vazio — e ainda assim melhor do que o meu palpite olhando o armário.

---

## O que vem do plano da Venus, e o que eu corto de propósito

**Carrego:**

- **Ferramentas definidas uma vez, como structs Go** (`internal/agent`), sem
  saber o que é MCP. Daí saem duas ligações: `cmd/mcp` (stdio) e o tool-use da
  API do Claude no bot. Uma implementação, dois hosts.
- **Fase MCP antes da fase WhatsApp.** A conversa inteira dá para testar no
  Claude Desktop, na assinatura Pro, **de graça** — o host chama o modelo, o
  `cmd/mcp` não tem chave. Só a fase do bot precisa de saldo no Console.
- **Catálogo compacto entregue ao modelo, sem busca difusa em Go.** Uma
  despensa doméstica cabe em poucos KB; o modelo casa "arroz branco" com
  "arroz" melhor que qualquer `strings.Contains` com acento.
- **Webhook: assinatura `X-Hub-Signature-256` antes de olhar o payload, lista
  fechada de números, 200 imediato + processamento em background.** Sem as três,
  isto é uma API de escrita pública.
- **Dois guardas, e nenhum é orçamento:** teto de 6 iterações de tool-use por
  mensagem (um loop desgovernado é **bug**, não despesa — sem o teto, um modelo
  confuso relê a despensa vinte vezes e eu descubro pela fatura) e `max_tokens`
  pequeno, que é só a forma da resposta. **Sem teto diário de mensagens e sem
  cerimônia de workspace no Console:** somos duas pessoas numa despensa, e
  US$ 4/mês no pior caso não é um risco a administrar. Fica uma higiene que não
  é de gasto: a chave vive em `fly secrets` e **nunca** no perfil do shell
  local, senão o Claude Code para de usar a assinatura e passa a cobrar nela.
- **`/healthz` não toca o banco, `/readyz` toca.** O health check da Fly usa o
  primeiro; qualquer prova de prontidão usa o segundo.
- **Tier não verificado do Meta serve:** portfólio de negócios sem CNPJ, 250
  destinatários únicos / 24h, 2 números. Eu sou um destinatário. O que falta é
  um **número que não esteja num WhatsApp comum** — esse é o pré-requisito real.

**Corto, e o motivo:**

| O plano da Venus tinha | Aqui não, porque |
|---|---|
| API HTTP de escrita + tokens por admin | Um serviço só, um usuário. As ferramentas chamam os services **em processo**. Nada de `admin_tokens`, nada de DTO de borda. |
| Cliente HTTP com aquecimento `/readyz` e retry por método | Não existe segundo serviço para acordar. O POST do webhook acorda a máquina da Fly, e o Meta reentrega se o cold start demorar. |
| `expected_quantity` + 409 `quantity_changed` | Aquilo existia porque o painel competia pela escrita. Aqui o escritor sou eu, sozinho, e a frase é **relativa** por natureza ("usei uma taça"). Delta, não valor absoluto. |
| Tabela de auditoria separada do estoque | O **ledger é o estoque**. `on_hand` é soma materializada; o histórico é a fonte. |
| Confirmar **toda** escrita antes de gravar | **Esta é a inversão central.** Lá, gravar errado despublicava uma peça da loja. Aqui o raio de explosão é uma linha da minha despensa, e o modo de falha que mata o produto é **atrito**. Grava na hora, responde com recibo + botão **Desfazer**. Confirmação só para ambiguidade real ("qual arroz?") e para apagar item. |
| `pending_actions` no banco | Sem confirmação prévia, não há ação pendente. O payload do botão Desfazer **é** o id do evento. |

---

## Modelo de domínio — o núcleo

Três ideias, e tudo o mais decorre delas.

### 1. Unidade canônica + porções nomeadas, por item

```
item:   arroz        canonical_unit=g    on_hand=4_600_000
porções: taça=180g · colher=12g · pacote=5000g
item:   pão de forma canonical_unit=un   on_hand=16_000
porções: fatia=1un · pacote=22un
```

- `canonical_unit ∈ {g, ml, un}`. Toda quantidade guardada em **milésimos da
  unidade canônica, num `BIGINT`** (`base_milli`). Sem float, sem `NUMERIC` para
  escanear, e "meia fatia" continua exato.
- **Comprar e usar são o mesmo mecanismo:** "pacote" é só mais uma porção.
  *"comprei um pacote de arroz"* = `+1 × pacote`. É isto que responde ao
  "ele soma o que eu comprei".
- **Eu nunca defino uma porção.** No primeiro uso de uma porção desconhecida, o
  modelo manda a estimativa dele (`portion_base_hint`) junto da chamada, e o
  service grava a linha. A partir daí a mesma palavra vale sempre o mesmo —
  **consistência importa mais que exatidão**, porque é ela que faz a soma de
  trinta usos não virar ruído.
- Item novo é criado na hora, pelo mesmo caminho: *"comprei um pacote de
  lentilha"* sem lentilha cadastrada → o modelo cria o item com unidade e
  porções inferidas. Zero formulário.

### 2. Ledger append-only; `on_hand` é cache

```sql
events(id BIGSERIAL, item_id, kind, delta_milli, portion_name, portion_qty,
       raw_message, source, chat_message_id, created_at, undone_at)
```

`kind ∈ {consume, restock, correction, discard}`. `on_hand` em `items` é
atualizado na mesma transação do insert, e `SUM(delta_milli)` do ledger é a
verdade que o reconstrói. Isso dá, de graça: **Desfazer** (`undone_at` + delta
inverso), histórico ("quanto arroz eu gasto por mês?"), e a base do forecast
mais tarde.

`correction` grava o delta que leva ao valor absoluto declarado — *"tenho umas
3 taças de arroz"* não é um uso, é eu dizendo a verdade.

### 3. Deriva negativa é sinal, não erro

Se o `on_hand` fica negativo, minha contabilidade errou para baixo e o produto
tem que perceber. Guarda o valor negativo, mostra `0`, e devolve `drift: true`
na resposta da ferramenta — o modelo então pergunta *"pela minha conta o arroz
já tinha acabado; quanto tem aí?"* e grava uma `correction`. É o único momento
em que o bot me pede um número, e ele é um palpite, não uma medição.

### Alertas (threshold + digest semanal)

- `items.threshold_milli`, default = uma porção "pacote" (ou 20% dela quando o
  item tem pacote grande), ajustável por frase: *"me avisa quando o arroz
  chegar em 2 taças"*.
- **O alerta de mínimo pega carona na resposta.** `on_hand` só muda quando eu
  mando mensagem, então o cruzamento do limite sempre acontece dentro do turno
  — vai como uma linha a mais no recibo. **Nenhum template do Meta, custo zero.**
- O **digest semanal** é a única mensagem iniciada pelo negócio. Fora da janela
  de 24h o Meta só aceita **template aprovado** (utility) — texto livre é
  rejeitado. Então: um template (`lista_semanal`) com um parâmetro de corpo.
  Fallback se a aprovação travar: guardar o digest e entregá-lo na próxima vez
  que eu falar com o bot. **Confirmar o preço de template utility no BR na
  página oficial do Meta** — isto mudou em 2026 e as fontes de terceiros
  divergem.

---

## Faseamento

| Fase | Entrega | Testável com |
|---|---|---|
| A | Domínio, migrações, repositório, services, CLI | `go test` + `cmd/despensa` |
| B | Ferramentas + servidor MCP (stdio) | Claude Desktop, **de graça** |
| C | Bot de WhatsApp | celular |
| D | Digest semanal | `fly machine run` + template aprovado |

### Passo 0 — a máquina

**Não há Go instalado nesta máquina** (`/usr/local/go` não existe; o
`go1.22.0.linux-amd64.tar.gz` no `~` é velho demais até para o `go.mod` do
venus-admin, que pede 1.27.3). Instalar o Go estável atual antes de qualquer
coisa. Banco: Neon (free, scale-to-zero), na conta que já existe. O MCP do Neon
está instalado mas **não autorizado** — `/mcp` para autorizar, ou criar o
projeto pelo console.

**Por que relacional e não Mongo** (decidido, para não ser relitigado): o
núcleo é um ledger com saldo materializado, e *"usei duas fatias de pão e uma
taça de arroz"* são dois inserts em `events` mais dois updates em `items` que
**têm que acontecer juntos ou nenhum** — se o insert vai e o update não, o
estoque mente em silêncio, que é a doença que este produto existe para curar.
No Postgres isso é uma transação e é o caminho feliz; no Mongo, transação
multi-documento exige replica set, e a alternativa idiomática seria embutir os
eventos no documento do item — array que cresce para sempre contra o limite de
16 MB, o anti-padrão clássico. Somando: os dados são pequenos, regulares e
profundamente relacionais (item ↔ porções ↔ eventos ↔ apelidos), o digest e o
"quanto arroz eu gasto por mês" são `SUM() GROUP BY` de uma linha e dá para
conferir à mão no `psql`. O argumento pelo documento existe e é pequeno — o
vocabulário de porções varia por item — e é atendido por `JSONB`, que é onde
ficam o `content` dos turnos de conversa e o payload cru do Meta. Documento
onde ele paga, transação onde ela é obrigatória, e uma dependência a menos.

Projeto novo em `~/despensa`, módulo `github.com/brendalima/despensa`.

**O `AGENTS.md` do venus-admin vem igual** — copiado de
`~/venus-admin/AGENTS.md`, não reescrito nem "melhorado". As únicas mudanças
permitidas são as que o projeto obriga: as entidades dos exemplos (§3) e a
tabela de camadas (§2) ganhando `internal/agent` e `internal/bot` como drivers,
irmãos de `rest` e `subscriber`. Toda regra que ele já dá continua valendo sem
exceção — hexagonal, `contracts.go` com só interfaces, `NewX`/`MustNewX`,
`os.Getenv` **somente** em `adapters/`, SQL explícito com placeholders,
`sql.ErrNoRows` traduzido para erro de domínio, `errors.As`/`errors.Is`,
`log/slog`. O `Makefile` (`check` = fmt + vet + staticcheck + test -race +
govulncheck) e o `internal/architecture_test.go` vêm do mesmo lugar, com os
pacotes novos acrescentados à tabela de proibições — é a casa que eu já conheço,
e o arch test é o que impede a conveniência de virar exceção.

### Fronteira de idioma (§12 do AGENTS.md, e é aqui que se escorrega)

**Código em inglês. Conversa e dados em pt-BR.** A linha passa exatamente aqui:

| Em inglês, sempre | Em pt-BR, sempre |
|---|---|
| identificadores, pacotes, funções, campos (`Item`, `on_hand_milli`, `Record`) | o que o bot escreve: recibos, alertas, perguntas, a lista de compras |
| comentários e mensagens de **erro de domínio** (`Error()`: minúsculas, sem ponto) | as **descrições das ferramentas** de `internal/agent` — são prompt, e a conversa é em português |
| valores de enum do modelo de dados: `kind` (`consume`, `restock`, `correction`, `discard`), `source`, `canonical_unit` (`g`/`ml`/`un`) | **dados** digitados ou inferidos: `items.name` ("arroz", "pão de forma"), `item_aliases.alias`, e `portions.name` ("taça", "fatia", "pacote", "concha") |
| nomes de tabela, coluna e migração; mensagens de commit; logs | o `.md` de plano e as conversas comigo |

O caso que confunde: `portions.name` guarda **a minha palavra**, não um termo
técnico — é vocabulário de usuário, e em português. Já `kind = 'consume'` é
modelo de dados, e em inglês. Cada erro de domínio nasce em inglês e é traduzido
para português **na borda** (`internal/bot`, `internal/agent`), nunca no
`domain` — do contrário o `drift`, o item ambíguo e o item inexistente viram
três strings portuguesas espalhadas por três camadas.

### Fase A — domínio e persistência

`adapters/migrations/00001_pantry.sql` (goose, como no venus-admin):

```sql
CREATE TABLE items (
  id              UUID PRIMARY KEY,
  name            TEXT NOT NULL CHECK (length(trim(name)) > 0),
  canonical_unit  TEXT NOT NULL CHECK (canonical_unit IN ('g','ml','un')),
  on_hand_milli   BIGINT NOT NULL DEFAULT 0,          -- pode ser negativo: é sinal
  threshold_milli BIGINT NOT NULL DEFAULT 0,
  category        TEXT,
  archived_at     TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX items_name_key ON items (lower(name)) WHERE archived_at IS NULL;

-- Vocabulário por item. 'seeded' = o modelo estimou; 'corrected' = eu corrigi.
CREATE TABLE portions (
  item_id    UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  base_milli BIGINT NOT NULL CHECK (base_milli > 0),
  source     TEXT NOT NULL DEFAULT 'seeded',
  PRIMARY KEY (item_id, name)
);

CREATE TABLE item_aliases (item_id UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
                           alias TEXT NOT NULL, PRIMARY KEY (item_id, alias));

-- O ledger é o estoque. Sem FK para items? Não: aqui o item apagado leva o
-- histórico com ele, e isso é aceitável numa despensa pessoal.
CREATE TABLE events (
  id              BIGSERIAL PRIMARY KEY,
  item_id         UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  kind            TEXT NOT NULL CHECK (kind IN ('consume','restock','correction','discard')),
  delta_milli     BIGINT NOT NULL,
  portion_name    TEXT,
  portion_qty     NUMERIC(10,3),
  raw_message     TEXT,
  source          TEXT NOT NULL,          -- 'whatsapp' | 'mcp' | 'cli'
  chat_message_id TEXT,
  undone_at       TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX events_item_idx ON events (item_id, created_at DESC);

-- Idempotência: o Meta reentrega. Insere antes de chamar o modelo; conflito
-- = mensagem já processada, aborta sem gastar token nenhum.
CREATE TABLE processed_messages (
  provider TEXT NOT NULL, message_id TEXT NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now(), PRIMARY KEY (provider, message_id)
);

-- Lista de compras: itens abaixo do mínimo (derivado) ∪ recados soltos.
CREATE TABLE shopping_notes (id BIGSERIAL PRIMARY KEY, text TEXT NOT NULL,
                             created_at TIMESTAMPTZ NOT NULL DEFAULT now());

-- Histórico curto da conversa, TTL ~30 min.
CREATE TABLE conversation_turns (id BIGSERIAL PRIMARY KEY, chat_id TEXT NOT NULL,
                                 role TEXT NOT NULL, content JSONB NOT NULL,
                                 created_at TIMESTAMPTZ NOT NULL DEFAULT now());
```

Camadas (AGENTS.md §1–2):

- `internal/core/domain/`: `item.go` (`Item`, `Portion`, `Unit`), `event.go`,
  `amount.go` — o tipo `Amount` em `base_milli` com `Add`/`Mul`/`In(portion)` e
  a **formatação humana** (`"~4,6 kg (≈26 taças)"`, meio em passos de 0,5 abaixo
  de 3 e inteiros acima), `errors.go` (`NotFoundError`, `ValidationError`,
  `AmbiguousItemError`). Aritmética só aqui, com testes de tabela: é o coração.
- `internal/repository/`: `item.go`, `event.go`, `message.go`, `contracts.go`.
  SQL explícito, `*Context`, upsert com `ON CONFLICT`, `sql.ErrNoRows` →
  erro de domínio.
- `internal/core/services/pantry.go` + `contracts.go`:
  - `Record(ctx, []domain.EventInput) ([]domain.Receipt, error)` — uma
    transação para o lote inteiro, cria item/porção que faltar, aplica os
    deltas, devolve antes/depois por item + `crossed_threshold` + `drift`.
  - `Undo(ctx, eventID)`, `Correct`, `UpdateItem`, `List(ctx, includeArchived)`,
    `ShoppingList(ctx)`.
- `cmd/despensa` — CLI que exercita tudo **sem LLM e sem WhatsApp**:
  `despensa add`, `despensa use "arroz" taça 1`, `despensa status`,
  `despensa undo`. É o que prova a Fase A sozinha.

### Fase B — ferramentas e MCP

`internal/agent/`: `Tool{Name, Description, Schema, Handler}` como no plano da
Venus — structs Go que não conhecem MCP. Port estreito em `contracts.go`
(`Pantry`, com só os métodos usados); `services.Pantry` satisfaz implícito.

Seis ferramentas, `additionalProperties:false`, `strict: true`, enums fechados
vindos do domínio (`domain.AllUnits()`), para o SDK recusar lixo antes do
handler:

| ferramenta | papel |
|---|---|
| `list_inventory` | despensa compacta: id, nome, apelidos, unidade, `on_hand`, mínimo, porções conhecidas com os tamanhos. **Chamar sempre antes de escrever.** |
| `record_events` | **array** de entradas (`item_id` **ou** `new_item`, `kind`, `portion_name`, `portion_qty`, `portion_base_hint?`, `note`). Uma frase com três itens = uma chamada, um recibo. |
| `undo_event` | reverte por id. |
| `update_item` | mínimo, porções, apelidos, arquivar. |
| `shopping_list` | abaixo do mínimo + recados. |
| `add_shopping_note` | o que não é estoque ("fermento"). |

Descrições em português (é prompt — teste golden sobre elas), com as regras
que invertem o plano da Venus escritas explicitamente:

- `record_events`: *"Grave direto, sem pedir confirmação: a resposta já traz
  um botão Desfazer. **Nunca pergunte quanto pesa uma porção** — estime você
  mesmo e mande a estimativa em `portion_base_hint` na primeira vez que a
  palavra aparecer; depois ela fica salva. Se a frase citar vários itens, mande
  todos numa chamada só. Se vier `drift: true`, diga que pela sua conta o item
  já havia acabado e pergunte quanto tem."*
- `list_inventory`: *"Se duas coisas combinarem com o que a pessoa disse,
  pergunte qual — não escolha sozinho."*

`cmd/mcp` (stdio, `github.com/modelcontextprotocol/go-sdk`, versão exata
fixada): só wiring, **logs em stderr** (stdout corrompe o JSON-RPC — comentar
no código). Config em `adapters/mcp_config.go`, que lê só `DATABASE_URL`.
Aqui o binário fala com o banco direto: não há serviço HTTP no meio.

### Fase C — WhatsApp

Rotas novas no mux de `cmd/api` (que nesta fase é só o webhook + health):

```go
mux.HandleFunc("GET  /webhooks/whatsapp", rWhats.Verify)
mux.Handle    ("POST /webhooks/whatsapp", http.HandlerFunc(rWhats.Receive))
```

`internal/bot/` é agnóstico de provedor — os mesmos dois ports do plano da
Venus, em `contracts.go`: `Messenger` (`SendText`, `SendWithButtons`,
`SendTemplate`) e `Webhook` (`Verify(r, body)`, `Parse(body) []Message`).
`internal/bot/whatsapp/` implementa os dois (HMAC do corpo cru com
`subtle.ConstantTimeCompare`, payload do Meta, Cloud API para enviar). Telegram
depois, se o número do Meta der trabalho, é um pacote novo e nada mais.

Loop (`internal/bot/loop.go`), com o SDK `github.com/anthropics/anthropic-sdk-go`:

1. Verifica assinatura → 403 se falhar, **nada processado**.
2. `from` fora de `WHATSAPP_ALLOWED_NUMBERS` → resposta neutra, **zero chamada
   ao modelo**.
3. `INSERT ... ON CONFLICT DO NOTHING` em `processed_messages`; 0 linhas → fim.
4. **Responde 200 e processa em goroutine** (o Meta reentrega o que demora).
5. Tool-use loop: histórico dos últimos 30 min + `tools` de `internal/agent`,
   `max_tokens` ~1024 (as respostas são uma ou duas frases),
   `output_config.effort: "low"`, **teto de 6 iterações** de tool-use —
   é o teto de iterações, não o teto mensal, que transforma um loop
   desgovernado em nada. Erro de ferramenta volta como `tool_result`
   com `is_error`, nunca como erro de protocolo.
6. Recibo com botão `Desfazer` (payload `undo:<event_id>` — o id **é** o
   estado, sem tabela de pendências), mais a linha de alerta se cruzou o mínimo.
7. Botão apertado chega como `Message.Choice` → chama `undo_event` direto,
   **sem passar pelo modelo**.

Modelo: config (`ANTHROPIC_MODEL`). Default `claude-opus-5-5`; a troca para
`claude-haiku-5-5` é uma variável. A ~2K tokens de entrada e 200 de saída por
mensagem, 10 mensagens/dia: Opus 5.5 ≈ US$ 3,60/mês, Haiku 5.5 ≈ US$ 0,09/mês.
A diferença prática está em inferir "uma taça de arroz ≈ 180 g" — começar no
Opus, medir no uso real e descer se a qualidade aguentar. Em qualquer dos dois,
as mensagens do WhatsApp custam mais que o modelo.

### Fase D — digest semanal

`cmd/digest`: monta a lista, manda pelo `Messenger.SendTemplate`. Agendado por
máquina programada da Fly (`fly machine run --schedule weekly` na mesma imagem
— **confirmar a grafia da flag**); alternativa é um cron do GitHub Actions
batendo num endpoint autenticado. Sem template aprovado, guarda e entrega no
próximo turno.

---

## Variáveis (`.env-sample`, lidas só em `adapters/`)

`DATABASE_URL`, `PORT`, `APP_ENV`; `ANTHROPIC_API_KEY`, `ANTHROPIC_MODEL`,
`BOT_MAX_TOOL_ITERATIONS` (6);
`WHATSAPP_VERIFY_TOKEN`, `WHATSAPP_APP_SECRET`, `WHATSAPP_PHONE_NUMBER_ID`,
`WHATSAPP_ACCESS_TOKEN`, `WHATSAPP_ALLOWED_NUMBERS`, `WHATSAPP_DIGEST_TEMPLATE`.
Em produção, `fly secrets set` — e a chave da Anthropic **nunca** no perfil do
shell local.

## Testes

- `domain/amount_test.go` — table-driven em cima de `Amount`: conversão
  porção↔canônica, arredondamento humano, meia fatia, negativo, overflow.
  É aqui que o produto é correto ou não é.
- `services/pantry_test.go` (mocks dos ports) — lote de 3 itens numa
  transação; item e porção criados na hora; porção já conhecida **não** é
  sobrescrita pelo `portion_base_hint`; `correction` calcula o delta certo;
  `Undo` zera o efeito e é idempotente; cruzou o mínimo → `crossed_threshold`;
  negativo → `drift: true` e `on_hand` exibido 0.
- `repository/*_test.go` contra `TEST_DATABASE_URL` — `on_hand` materializado
  igual a `SUM(delta_milli)` depois de uma sequência aleatória; `ON CONFLICT`
  de `processed_messages`.
- `agent/tools_test.go` — round trip real com `mcp.NewInMemoryTransports()`:
  exatamente seis ferramentas; argumento fora do enum recusado pelo schema
  antes do handler; **teste golden nas descrições**, que são prompt.
- `bot/whatsapp_test.go` — assinatura inválida → 403 e **nenhuma** chamada ao
  modelo; número fora da lista → nenhuma ferramenta; mensagem repetida →
  processada uma vez; teto de iterações respeitado; botão `undo:` não chama o
  modelo.
- `internal/architecture_test.go` — pacotes novos na tabela de proibições;
  falha se `internal/agent` importar `internal/repository` ou pgx; mais um caso
  para a fronteira de idioma: **nenhum arquivo de `internal/core/` tem literal
  de string com acento** (o texto em português vive só em
  `internal/bot/messages.go` e `internal/agent/descriptions.go`). É grosseiro,
  mas pega exatamente o escorregão que importa — mensagem para o usuário nascendo
  no domínio.
- `make check` antes de cada PR.

## Verificação ponta a ponta

1. `make check` limpo; `go run ./cmd/migrate`.
2. **Fase A, sem LLM:** `despensa add arroz g --porcao taça=180 --porcao pacote=5000`,
   `despensa use arroz pacote 1`, `despensa use arroz taça 1` → `status` mostra
   `~4,8 kg (≈26 taças)`; `undo` volta; conferir no `psql` que `on_hand_milli`
   == `SUM(delta_milli)`.
3. **Fase B, no Claude Desktop (de graça):**
   `claude mcp add despensa -- env DATABASE_URL=... go run ./cmd/mcp`, e dizer
   literalmente:
   - *"usei duas fatias de pão e uma taça de arroz"* → uma chamada de
     `record_events` com duas entradas, recibo com os dois antes/depois.
   - *"comprei 2 pacotes de arroz"* → estoque sobe 10 kg.
   - *"comprei um pacote de lentilha"* (item inexistente) → cria item, unidade e
     porções sozinho, **sem me perguntar nada**.
   - *"usei uma concha de arroz"* (porção nova) → estima, grava a porção, e a
     segunda concha vale o mesmo que a primeira (conferir em `portions`).
   - *"usei um pouco de arroz"* (vago) → pergunta, não inventa.
   - usar arroz dez vezes até passar do zero → pergunta quanto tem, grava
     `correction`.
   - duas coisas combinando ("arroz" e "arroz arbóreo") → pergunta qual.
4. **Fase C:** mensagem real do celular → recibo em segundos → **Desfazer** →
   estoque volta. Negativo obrigatório: mensagem de um número **fora** da lista
   → `fly logs` tem que mostrar zero chamada ao modelo e zero ferramenta.
   Reentrega: repetir o mesmo `message_id` no webhook à mão → nada acontece.
5. **Fase D:** disparar `cmd/digest` à mão, depois esperar o agendamento.

## Riscos e pré-requisitos

- **O número do WhatsApp é o único bloqueio externo.** Precisa de um número
  que não esteja num WhatsApp comum, um portfólio de negócios do Meta (grátis,
  pessoa física) e um app de desenvolvedor. O tier não verificado basta —
  mas é fonte de terceiros: **confirmar na doc de messaging limits** antes de
  construir em cima. Se o Meta exigir verificação, o Telegram é um pacote novo
  atrás do mesmo port, não um replanejamento.
- **Template para o digest.** É a única mensagem iniciada por mim; sem
  aprovação, só o fallback de entregar no próximo turno.
- **A inferência de porção é a qualidade do produto.** Se o modelo estimar
  "taça de arroz" em 400 g, o estoque fica errado por semanas sem eu notar.
  Mitigação: toda porção criada aparece no recibo da primeira vez
  (*"anotei taça = 180 g"*), e `update_item` corrige numa frase.
- **Deriva acumulada é inevitável e aceitável** — o `correction` é o reset, e
  o `drift: true` é o gatilho para pedir um.
- **O SDK de MCP puxa ~9 módulos transitivos** num `go.mod` que começa enxuto;
  fixar versão exata e aceitar que `govulncheck` passa a vigiar mais superfície.
- **Injeção de prompt não acrescenta poder** (quem manda mensagem já pode
  escrever no estoque), mas é exatamente por isso que a lista fechada de
  números é inegociável.
- **Fora de escopo de propósito, com a porta aberta:** foto da nota do mercado
  para reabastecer dez itens de uma vez. O `Messenger` já prevê `Media`;
  implementar depois, não agora.

## Sequência de PRs

1. `pantry-domain` — `Amount`, domínio, migração `00001`, repositório, services,
   `cmd/despensa`, `cmd/migrate`, `AGENTS.md`, `Makefile`, arch test.
2. `agent-tools` — `internal/agent` + as seis ferramentas + testes.
3. `mcp-server` — SDK, `cmd/mcp`, `adapters/mcp_config.go`, README.
4. `whatsapp-bot` — `internal/bot`, `internal/bot/whatsapp`, `cmd/api`,
   webhook, botões, guardas de gasto, Dockerfile, `fly.toml`.
5. `weekly-digest` — `cmd/digest`, template, agendamento.

**Git:** uma branch por fase, commit local; o push é seu (a chave desta máquina
tem passphrase e não há agent).
