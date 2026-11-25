package main

import (
	"github.com/spf13/cobra"

	"github.com/diegomarino/kravis/internal/domain"
)

func (a *app) dealCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deal",
		Short: "Deals: transactions your holder follows through the stages of a pipeline",
		Long: "A deal belongs to the holder of the actor who creates it, records the holder's party role, and has parties: " +
			"people and organizations, each once, with any number of party roles. The holder is never a party. " +
			"An actor reads and writes only its own holder's deals (--all-holders widens reads). " +
			"The stage changes only with deal stage, and the parties only with deal party.",
		Args: cobra.NoArgs,
		RunE: groupUsage,
	}

	var in domain.DealInput
	var descriptionFile string
	var cv domain.ViewOptions
	create := &cobra.Command{
		Use:   "create --party <person or organization> [--role <party role>]... --holder-role <party role> [--name <name>] [--stage <stage>]",
		Short: "Create a deal with its first party", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("party") {
				return domain.InvalidFlag("party", nil, "missing required flag: --party")
			}
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				text, err := fileText(cmd, "description-file", descriptionFile)
				if err != nil {
					return nil, err
				}
				in.Description = text
				return ws.CreateDeal(actor, in, cv)
			})
		},
	}
	f := create.Flags()
	f.StringVar(&in.Party, "party", "", "the first party: a person (ID or email) or an organization (ID); the deal's ID comes from it")
	f.StringArrayVar(&in.Roles, "role", nil, "a party role of the first party (repeatable)")
	f.StringVar(&in.HolderRole, "holder-role", "", "the party role your holder plays in the deal")
	f.StringVar(&in.Name, "name", "", "a name for the deal")
	f.StringVar(&in.Stage, "stage", "", "the first stage (default: the pipeline's first); not a lost one")
	f.StringVar(&descriptionFile, "description-file", "", "a file with the description, or - for stdin")
	viewFlags(f, &cv)

	var ch domain.DealChange
	var name, holderRole, stage, updDescription string
	var allHolders bool
	var uv domain.ViewOptions
	update := &cobra.Command{
		Use: "update <deal>", Short: "Change the name, the holder's party role or the description of a deal of your holder",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("stage") {
				return &domain.Error{Code: domain.CodeUnknownField, Message: "unknown flag: --stage",
					Hint:    "update cannot change the stage: run kravis deal stage <deal> <stage>",
					Details: map[string]any{"field": "stage"}}
			}
			ch.Name, ch.HolderRole = changed(cmd, "name", name), changed(cmd, "holder-role", holderRole)
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				if cmd.Flags().Changed("description-file") {
					text, err := fileText(cmd, "description-file", updDescription)
					if err != nil {
						return nil, err
					}
					ch.Description = &text
				}
				return ws.UpdateDeal(actor, args[0], ch, uv)
			})
		},
	}
	f = update.Flags()
	f.StringVar(&name, "name", "", `a new name, or "" to clear it`)
	f.StringVar(&holderRole, "holder-role", "", "a new party role for your holder")
	f.StringVar(&updDescription, "description-file", "", `a file with the new description, - for stdin, or "" to clear it`)
	f.StringVar(&stage, "stage", "", "refused: the stage changes only with kravis deal stage")
	_ = f.MarkHidden("stage")
	f.BoolVar(&allHolders, "all-holders", false, "accepted, and ignored: only your own holder's deals can be changed")
	viewFlags(f, &uv)

	var reason string
	var sv domain.ViewOptions
	var stageAll bool
	setStage := &cobra.Command{
		Use: "stage <deal> <stage> [--reason <lost reason>]", Short: "Move a deal to any stage of its pipeline",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.SetDealStage(actor, args[0], args[1], reason, sv)
			})
		},
	}
	setStage.Flags().StringVar(&reason, "reason", "", "the lost reason: required for a stage of the lost category, refused for any other")
	setStage.Flags().BoolVar(&stageAll, "all-holders", false, "accepted, and ignored: only your own holder's deals can change stage")
	viewFlags(setStage.Flags(), &sv)

	var lf domain.DealFilter
	var lo domain.ListOptions
	var listAll bool
	list := &cobra.Command{
		Use: "list", Short: "List deals of your holder by name, live ones unless --all, --stage or --category", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.readLimited(cmd, listAll, func(ws *domain.Workspace, holder string) (any, error) {
				lf.Holder = holder
				return ws.ListDeals(lf, lo)
			})
		},
	}
	f = list.Flags()
	f.StringVar(&lf.Party, "party", "", "only deals with this party")
	f.StringVar(&lf.Stage, "stage", "", "only deals in this stage")
	f.StringVar(&lf.Category, "category", "", "only deals in this stage category: ongoing, won or lost")
	f.BoolVar(&listAll, "all-holders", false, "do not limit the list to the actor's holder")
	listFlags(f, &lo)

	var gv domain.ViewOptions
	var getAll bool
	get := &cobra.Command{
		Use: "get <deal>", Short: "Show one deal of your holder", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.readLimited(cmd, getAll, func(ws *domain.Workspace, holder string) (any, error) {
				return ws.GetDeal(holder, args[0], gv)
			})
		},
	}
	get.Flags().BoolVar(&getAll, "all-holders", false, "do not limit the read to the actor's holder")
	viewFlags(get.Flags(), &gv)

	var dv domain.ViewOptions
	var delAll bool
	del := &cobra.Command{
		Use: "delete <deal>", Short: "Delete a deal of your holder that nothing refers to", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.DeleteDeal(actor, args[0], dv)
			})
		},
	}
	del.Flags().BoolVar(&delAll, "all-holders", false, "accepted, and ignored: only your own holder's deals can be deleted")
	viewFlags(del.Flags(), &dv)

	cmd.AddCommand(create, update, setStage, a.partyCommand(), list, get, del)
	return cmd
}

func (a *app) partyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "party",
		Short: "A deal's parties: people and organizations, each once, with their party roles",
		Args:  cobra.NoArgs,
		RunE:  groupUsage,
	}

	var roles []string
	var av domain.ViewOptions
	add := &cobra.Command{
		Use:   "add <deal> <party> [--role <party role>]...",
		Short: "Add a party to a deal, or the roles it lacks", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.AddDealParty(actor, args[0], args[1], roles, av)
			})
		},
	}
	add.Flags().StringArrayVar(&roles, "role", nil, "a party role of the catalog (repeatable)")
	viewFlags(add.Flags(), &av)

	var role string
	var rv domain.ViewOptions
	remove := &cobra.Command{
		Use:   "remove <deal> <party> [--role <party role>]",
		Short: "Remove a party from a deal, or only one of its roles", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.RemoveDealParty(actor, args[0], args[1], role, rv)
			})
		},
	}
	remove.Flags().StringVar(&role, "role", "", "remove only this role; the party stays even with none left")
	viewFlags(remove.Flags(), &rv)

	cmd.AddCommand(add, remove)
	return cmd
}
