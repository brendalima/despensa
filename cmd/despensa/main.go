// Command despensa is the pantry on the command line, with no model and
// no WhatsApp in the way. It is how Phase A is proved: if the arithmetic
// and the ledger are wrong, they are wrong here, where there is nothing
// else to blame.
//
// Usage:
//
//	despensa status
//	despensa add arroz g --portion taça=180 --portion pacote=5000
//	despensa use arroz taça          # one cup
//	despensa use arroz taça 2        # two
//	despensa buy arroz pacote
//	despensa have arroz taça 3       # "I have about three cups"
//	despensa undo 42
//	despensa shopping
//	despensa note fermento
//	despensa history arroz
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/brendalima/despensa/adapters"
	"github.com/brendalima/despensa/internal/core/domain"
	"github.com/brendalima/despensa/internal/core/services"
	"github.com/brendalima/despensa/internal/humanize"
	"github.com/brendalima/despensa/internal/repository"
)

// errReported marks an error already shown to the person.
var errReported = errors.New("reported")

func main() {
	if err := run(os.Args[1:]); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintln(os.Stderr, "erro:", err)
		}
		os.Exit(1)
	}
}

// report prints what the person can act on, then the English detail for
// whoever is debugging.
func report(err error) error {
	fmt.Fprintln(os.Stderr, humanize.Error(err))
	fmt.Fprintf(os.Stderr, "(%v)\n", err)
	return errReported
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("missing command")
	}

	cfg, err := adapters.LoadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := adapters.NewDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	pRepository := repository.MustNewPantry(db)
	pService := services.MustNewPantry(pRepository)

	command, rest := args[0], args[1:]
	switch command {
	case "status":
		return status(ctx, pService)
	case "add":
		return add(ctx, pService, rest)
	case "use":
		return record(ctx, pService, domain.EventConsume, rest)
	case "buy":
		return record(ctx, pService, domain.EventRestock, rest)
	case "have":
		return record(ctx, pService, domain.EventCorrection, rest)
	case "toss":
		return record(ctx, pService, domain.EventDiscard, rest)
	case "undo":
		return undo(ctx, pService, rest)
	case "shopping":
		return shopping(ctx, pService)
	case "note":
		return note(ctx, pService, rest)
	case "history":
		return history(ctx, pService, rest)
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `despensa — a despensa de casa na linha de comando

  status                                   o que tem na despensa
  add NOME UNIDADE --portion nome=qtd ...  cria um item (unidade: g, ml, un)
  use NOME PORÇÃO [QTD]                    usei
  buy NOME PORÇÃO [QTD]                    comprei
  have NOME PORÇÃO QTD                     tenho mais ou menos isso
  toss NOME PORÇÃO [QTD]                   joguei fora
  undo ID                                  desfaz um evento
  shopping                                 lista de compras
  note TEXTO                               anota algo que não é estoque
  history NOME                             o que aconteceu com um item

`)
}

func status(ctx context.Context, svc services.PantryService) error {
	items, err := svc.List(ctx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("A despensa está vazia.")
		return nil
	}
	for _, item := range items {
		fmt.Println(humanize.Item(item))
	}
	return nil
}

func add(ctx context.Context, svc services.PantryService, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	var portions portionList
	fs.Var(&portions, "portion", `a word and what one of it is worth, in the item's unit: taça=180`)
	threshold := fs.Float64("threshold", 0, "restock level, in the item's unit (default: a fifth of the largest portion)")
	category := fs.String("category", "", "category, for grouping")
	aliases := fs.String("aliases", "", "other names, comma separated")

	// flag stops parsing at the first positional argument, so
	// "add arroz g --portion taça=180" would drop every flag.
	rest, flags := splitArgs(args)
	if err := fs.Parse(flags); err != nil {
		return err
	}

	if len(rest) < 2 {
		return errors.New("usage: despensa add NOME UNIDADE --portion nome=qtd")
	}
	name, unit := rest[0], domain.Unit(rest[1])
	if len(portions) == 0 {
		return errors.New("an item with no portions cannot be used: give at least one, like --portion pacote=500")
	}

	// Creating an item goes through the same path the model uses — a
	// correction to zero — so the CLI cannot drift from the bot.
	receipts, err := svc.Record(ctx, []domain.EventInput{{
		NewItem: &domain.NewItem{
			Name:      name,
			Unit:      unit,
			Category:  *category,
			Aliases:   splitList(*aliases),
			Portions:  portions,
			Threshold: domain.AmountFromFloat(*threshold),
		},
		Kind:        domain.EventCorrection,
		PortionName: portions[0].Name,
		PortionQty:  0,
		RawMessage:  strings.Join(args, " "),
		Source:      domain.SourceCLI,
	}})
	if err != nil {
		return report(err)
	}
	fmt.Println(humanize.Receipts(receipts))
	return nil
}

func record(ctx context.Context, svc services.PantryService, kind domain.EventKind, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: despensa %s NOME PORÇÃO [QTD]", kind)
	}
	name, portion := args[0], args[1]

	qty := domain.QtyOf(1)
	if len(args) > 2 {
		parsed, err := strconv.ParseFloat(strings.Replace(args[2], ",", ".", 1), 64)
		if err != nil {
			return fmt.Errorf("invalid quantity %q", args[2])
		}
		qty = domain.QtyFromFloat(parsed)
	}

	item, err := findByName(ctx, svc, name)
	if err != nil {
		return err
	}

	receipts, err := svc.Record(ctx, []domain.EventInput{{
		ItemID:      item.ID,
		Kind:        kind,
		PortionName: portion,
		PortionQty:  qty,
		RawMessage:  strings.Join(args, " "),
		Source:      domain.SourceCLI,
	}})
	if err != nil {
		return report(err)
	}
	fmt.Println(humanize.Receipts(receipts))
	return nil
}

func undo(ctx context.Context, svc services.PantryService, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: despensa undo ID")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid event id %q", args[0])
	}
	receipt, err := svc.Undo(ctx, id)
	if err != nil {
		return report(err)
	}
	fmt.Println(humanize.Undone(receipt))
	return nil
}

func shopping(ctx context.Context, svc services.PantryService) error {
	list, err := svc.ShoppingList(ctx)
	if err != nil {
		return err
	}
	fmt.Println(humanize.ShoppingList(list))
	return nil
}

func note(ctx context.Context, svc services.PantryService, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: despensa note TEXTO")
	}
	n, err := svc.AddNote(ctx, strings.Join(args, " "))
	if err != nil {
		return report(err)
	}
	fmt.Printf("Anotado: %s (#%d)\n", n.Text, n.ID)
	return nil
}

func history(ctx context.Context, svc services.PantryService, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: despensa history NOME")
	}
	item, err := findByName(ctx, svc, strings.Join(args, " "))
	if err != nil {
		return err
	}
	events, err := svc.History(ctx, item.ID, 20)
	if err != nil {
		return err
	}
	for _, e := range events {
		mark := ""
		if e.Undone() {
			mark = " (desfeito)"
		}
		fmt.Printf("#%d %s %s %s %s%s\n",
			e.ID, e.CreatedAt.Local().Format(time.DateTime), e.Kind,
			humanize.Count(e.PortionQty, e.PortionName),
			humanize.Quantity(item, magnitude(e.Delta)), mark)
	}
	return nil
}

// findByName matches a name or an alias, ignoring case, over the whole
// pantry. The CLI resolves names itself for the same reason the model
// does: the pantry is small, and a list is more robust than a clever
// query.
func findByName(ctx context.Context, svc services.PantryService, name string) (domain.Item, error) {
	items, err := svc.List(ctx)
	if err != nil {
		return domain.Item{}, err
	}

	var found []domain.Item
	for _, item := range items {
		if strings.EqualFold(item.Name, name) {
			found = append(found, item)
			continue
		}
		for _, alias := range item.Aliases {
			if strings.EqualFold(alias, name) {
				found = append(found, item)
				break
			}
		}
	}

	switch len(found) {
	case 0:
		return domain.Item{}, fmt.Errorf("item not found: %s", name)
	case 1:
		return found[0], nil
	default:
		names := make([]string, len(found))
		for i, item := range found {
			names[i] = item.Name
		}
		return domain.Item{}, fmt.Errorf("ambiguous item %s: %s", name, strings.Join(names, ", "))
	}
}

// portionList collects repeated --portion name=amount flags.
type portionList []domain.Portion

func (l *portionList) String() string { return "" }

func (l *portionList) Set(v string) error {
	name, amount, ok := strings.Cut(v, "=")
	if !ok {
		return fmt.Errorf("invalid portion %q: use nome=qtd, like taça=180", v)
	}
	parsed, err := strconv.ParseFloat(strings.Replace(strings.TrimSpace(amount), ",", ".", 1), 64)
	if err != nil {
		return fmt.Errorf("invalid portion amount %q", amount)
	}
	// A portion given by hand is the person's own number, so it outranks
	// any later estimate from the model.
	*l = append(*l, domain.Portion{
		Name:   strings.TrimSpace(name),
		One:    domain.AmountFromFloat(parsed),
		Source: domain.PortionCorrected,
	})
	return nil
}

func splitList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// splitArgs separates positional arguments from flags, so flags may come
// after the item's name. Every flag this CLI has takes a value, which is
// what makes the one-token lookahead enough.
func splitArgs(args []string) (positional, flags []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		if !strings.Contains(arg, "=") && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return positional, flags
}

// magnitude is the size of a delta regardless of direction: the kind
// already says which way it went.
func magnitude(a domain.Amount) domain.Amount {
	if a < 0 {
		return -a
	}
	return a
}
