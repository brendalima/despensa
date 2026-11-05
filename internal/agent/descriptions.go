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

const (
	toolListInventory   = "list_inventory"
	toolRecordEvents    = "record_events"
	toolUndoEvent       = "undo_event"
	toolUpdateItem      = "update_item"
	toolShoppingList    = "shopping_list"
	toolAddShoppingNote = "add_shopping_note"
)

const descListInventory = `Mostra a despensa inteira: id, nome, apelidos, unidade, quanto tem, o mínimo e as porções que já conheço, com o tamanho de cada uma.

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

const descUpdateItem = `Corrige um item: o nome, a categoria, o mínimo da lista de compras, o tamanho das porções, os apelidos, ou arquiva ele.

Use quando a pessoa disser que uma porção está errada ("taça de arroz é mais perto de 200 g") — o valor que ela der vence qualquer estimativa minha, agora e depois.

Mande só os campos que mudam.`

const descShoppingList = `O que está faltando: os itens no mínimo ou abaixo, mais os recados anotados.`

const descAddShoppingNote = `Anota na lista de compras uma coisa que não é estoque — o que se compra de vez em quando e nunca se conta, como fermento ou papel toalha. Para o que tem quantidade, use record_events.`

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
)
