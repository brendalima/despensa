package agent

// Every word the model reads lives here. The descriptions are prompt,
// not documentation: they are in Portuguese because the conversation is,
// and they are the only place in this package with an accent — the
// architecture test enforces that, because a rule written down once and
// checked nowhere lasts until the first hurry.
//
// The golden test over these strings is not pedantry. The two sentences
// that invert the obvious behaviour — record without confirming, never
// ask what a portion weighs — are the product. Deleting them by accident
// would leave a bot that works and is useless.

// The tool names are exported: a driver names a tool when it calls one
// without the model in the way, which is what the Desfazer button does.
const (
	ToolListInventory      = "list_inventory"
	ToolRecordEvents       = "record_events"
	ToolUndoEvent          = "undo_event"
	ToolUpdateItem         = "update_item"
	ToolShoppingList       = "shopping_list"
	ToolAddShoppingNote    = "add_shopping_note"
	ToolRemoveShoppingNote = "remove_shopping_note"
)

const descListInventory = `Mostra a despensa inteira: id, nome, apelidos, unidade, quanto tem, o mínimo, as porções que já conheço com o tamanho de cada uma, e o shopping_mode de cada item.

Chame sempre esta ferramenta antes de escrever qualquer coisa — é de onde vem o item_id e a lista de palavras que eu já sei.

Se duas coisas combinarem com o que a pessoa disse, pergunte qual — não escolha sozinho.`

const descRecordEvents = `Registra o que a pessoa usou, comprou, jogou fora ou conferiu na despensa.

Grave direto, sem pedir confirmação: a resposta já traz um botão Desfazer.

Nunca pergunte quanto pesa ou quanto cabe numa porção — estime você mesmo e mande a estimativa em portion_base_hint na primeira vez que a palavra aparecer; depois ela fica salva e vale sempre o mesmo. Se a palavra já estiver na lista de porções do item, não mande portion_base_hint: o valor salvo vence.

Se a frase citar vários itens ("usei duas fatias de pão e uma taça de arroz"), mande todos numa chamada só: é uma transação e um recibo.

Se o item não existir ainda, descreva ele em new_item e crie na hora, sem perguntar nada — escolha a unidade e as porções você mesmo.

Se a resposta vier com drift: true, diga que pela sua conta o item já havia acabado e pergunte quanto tem, mais ou menos. É a única hora em que se pergunta um número, e é um chute, não uma medida.

Se a frase for vaga ("usei um pouco de arroz"), pergunte — não invente uma quantidade.`

const descUndoEvent = `Desfaz um registro pelo id do evento. Pode ser chamada duas vezes para o mesmo evento sem tirar a quantidade duas vezes.`

const descUpdateItem = `Corrige um item: o nome, a categoria, o mínimo da lista de compras, o tamanho das porções, os apelidos, se ele entra ou não na lista de compras (shopping_mode), ou arquiva ele.

Use quando a pessoa disser que uma porção está errada ("taça de arroz é mais perto de 200 g") — o valor que ela der vence qualquer estimativa minha, agora e depois.

Use também quando ela decidir algo sobre a lista de compras: não repor uma coisa agora, voltar a repor, ou lembrar de comprar algo que não está no mínimo. Isso é shopping_mode, não é recado.

Mande só os campos que mudam.`

const descShoppingList = `O que está faltando: os itens no mínimo ou abaixo que a pessoa quer repor, mais os que ela pediu para lembrar mesmo sem estar faltando, mais os recados anotados.

Os recados vêm com id, que é o que remove_shopping_note precisa.`

const descAddShoppingNote = `Anota na lista de compras uma coisa de uma vez só, que não é estoque da despensa — algo que não se conta e não se acompanha, como uma lâmpada, uma esponja ou um presente. Para o que tem quantidade e se repõe, use record_events: isso é item, não recado.

O recado existe até ser comprado; quando a pessoa disser que comprou, use remove_shopping_note.`

const descRemoveShoppingNote = `Tira um recado da lista de compras, pelo id que vem em shopping_list — chame shopping_list primeiro, é de onde sai o note_id.

Use quando a pessoa disser que comprou aquilo, ou que não quer mais. Recado é coisa de uma vez só: comprado, sai da lista.

Isto é só para os recados. Para um item da despensa que a pessoa não quer repor agora, use update_item com shopping_mode.`

// Field descriptions. Short on purpose: they are read on every call.
const (
	descEntries          = "Uma entrada por item citado na frase."
	descItemID           = "O id do item, vindo de list_inventory. Use este campo ou new_item, nunca os dois."
	descNewItem          = "Um item que ainda não está na despensa. Use este campo ou item_id, nunca os dois."
	descKind             = "consume = usei; restock = comprei ou repus; discard = joguei fora; correction = a pessoa está dizendo quanto TEM agora (um total, não uma mudança)."
	descPortionName      = "A palavra que a pessoa usou para a quantidade: taça, fatia, pacote, colher, un."
	descPortionQty       = "Quantas dessas porções. Aceita meio (0.5) e um e meio (1.5)."
	descPortionHint      = "Só na primeira vez que a palavra aparecer para este item: quanto vale UMA porção, na unidade do item (180 para uma taça de arroz em g). Se a porção já existir, deixe vazio."
	descNote             = "A frase da pessoa, como ela escreveu. Fica guardada para quando a conta parecer errada."
	descEventID          = "O id do evento, que vem no recibo de record_events."
	descItemName         = "O nome do item, em português e minúsculas, como a pessoa chamaria: \"arroz\", \"pão de forma\"."
	descUnit             = "A unidade em que o item é contado: g para peso, ml para líquido, un para o que se conta em unidades."
	descCategory         = "Uma categoria larga: grãos, limpeza, padaria. Opcional."
	descAliases          = "Outros nomes que a pessoa usa para o item. Substitui a lista inteira."
	descMinimum          = "O nível, na unidade do item, em que ele entra na lista de compras."
	descPortions         = "As porções do item."
	descPortionEntryName = "A palavra: taça, fatia, pacote."
	descPortionEntrySize = "Quanto vale uma, na unidade do item."
	descArchive          = "true tira o item da despensa sem apagar o histórico; false traz de volta."
	descNoteText         = "O que anotar, nas palavras da pessoa."
	descNoteID           = "O id do recado, vindo de shopping_list."
)

// descShoppingMode is the one field description that has to do real
// work. The three modes are two decisions in opposite directions plus
// the default, and the sentences a person actually says map onto them in
// ways that are easy to get backwards — "quero o arroz de volta na
// lista" reads like 'always' and is almost always 'auto'. So the rule is
// written out, including the case that only the item's current state can
// settle.
const descShoppingMode = `O que este item quer da lista de compras:

auto = o normal, entra na lista quando chega no mínimo. É para onde se volta: "volta a repor arroz", "arroz é recorrente de novo", "quero acompanhar o arroz outra vez" são auto, não always — a pessoa quer que o mínimo decida de novo, não uma linha fixa na lista.

never = não entra na lista mesmo estando no mínimo. É "não quero repor arroz agora", "pula o arroz". O item continua na despensa e continua sendo contado.

always = entra na lista mesmo estando cheio. É só para o que NÃO está faltando: "lembra de comprar café", "põe café na lista mesmo assim". Se o item já está no mínimo, ele já está na lista — aí o modo é auto. Esse lembrete acaba sozinho: quando a pessoa comprar e passar do mínimo, eu volto o item para auto.

"Tira o X da lista" depende de como o X está agora, e isso vem em list_inventory: se ele está com always, é auto (desfaz o lembrete); se ele está no mínimo (low), é never (segura a reposição). Se não der para saber qual dos dois, pergunte.`
