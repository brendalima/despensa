# despensa

O inventário da despensa de casa, gerenciado por conversa.

O problema não é falta de app de estoque doméstico — é que todos eles pedem que
você pare, abra o app, ache o item e digite um número **no momento em que está
cozinhando**. Ninguém faz isso, então o estoque vira ficção em três dias.

Aqui a interface é uma frase: *"usei duas fatias de pão e uma taça de arroz"*.
A resposta é um recibo com um botão **Desfazer**:

```
pão de forma: 18 → 16 fatias
arroz: 5 kg → 4,8 kg (≈27 taças)
```

**Estimativa aproximada é o objetivo, não uma concessão.** Ninguém vai pesar uma
taça de arroz. O modelo estima na primeira vez que a palavra aparece, a
estimativa fica salva, e a partir daí a mesma palavra vale sempre o mesmo —
consistência importa mais que exatidão, porque é ela que faz a soma de trinta
usos valer algo.

## Como funciona

Três ideias, e tudo decorre delas.

**1. Unidade canônica + porções nomeadas.** Cada item é contado em `g`, `ml` ou
`un`, e tem um vocabulário próprio: arroz conhece `taça` (180 g), `colher`
(12 g) e `pacote` (5 kg); pão conhece `fatia` (1 un) e `pacote` (22 un).
Comprar e usar são o mesmo mecanismo — "pacote" é só mais uma porção.

**2. O ledger é o estoque.** `items.on_hand_milli` é a soma dos deltas dos
eventos que não foram desfeitos. Nada mais é a verdade, e é isso que dá
Desfazer, histórico e previsão de graça. Toda quantidade é um `BIGINT` em
milésimos da unidade canônica: inteiros, porque esses números são somados e
subtraídos por anos, e a deriva de um float seria invisível.

**3. Deriva negativa é sinal, não erro.** Se a conta fica negativa, ela contou
menos do que entrou. O valor negativo é guardado, exibido como zero, e o bot
pergunta quanto tem — é o único momento em que ele pede um número, e pede um
palpite, não uma medição.

## Camadas

Hexagonal, nos padrões do [`AGENTS.md`](AGENTS.md):

```
cmd ──► agent / bot ──► services ──► repository (interfaces)
                            │
                            └──► domain ◄── (todas as camadas)
                                   ▲
                          humanize ─┘  (o texto em pt-BR)
```

**Fronteira de idioma:** código em inglês, conversa e dados em pt-BR. O domínio
não tem nenhuma frase destinada a uma pessoa — o erro nasce em inglês com um
código estável, e `internal/humanize` escreve o português. O arch test cobra
isso.

## Rodando

Precisa de Go 1.27+ e um Postgres (Neon serve, e é o alvo).

```bash
cp .env-sample .env     # preencha DATABASE_URL
make migrate
make check              # fmt, vet, staticcheck, testes com -race, govulncheck
```

A CLI é a Fase A inteira, sem modelo e sem WhatsApp no caminho — se a
aritmética ou o ledger estiverem errados, estão errados aqui, onde não há nada
mais para culpar:

```bash
go run ./cmd/despensa add arroz g --portion taça=180 --portion pacote=5000
go run ./cmd/despensa buy arroz pacote      # comprei um pacote
go run ./cmd/despensa use arroz taça 2      # usei duas taças
go run ./cmd/despensa status
go run ./cmd/despensa undo 3
go run ./cmd/despensa shopping
```

Os testes do repositório falam com um Postgres de verdade e **pulam** sem
`TEST_DATABASE_URL`. Aponte para um branch descartável: eles escrevem e apagam
linhas.

## Estado

| Fase | O que entrega | Estado |
|---|---|---|
| A | Domínio, ledger, repositório, services, CLI | feito, exceto rodar as migrações num banco de verdade |
| B | Ferramentas + servidor MCP (stdio) | a fazer |
| C | Bot de WhatsApp | a fazer |
| D | Digest semanal | a fazer |

O plano completo, com as decisões e o porquê de cada corte, está em
`~/.claude/plans/sequential-giggling-goose.md`.
