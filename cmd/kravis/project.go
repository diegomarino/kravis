package main

import (
	"github.com/spf13/cobra"

	"github.com/diegomarino/kravis/internal/domain"
)

func (a *app) projectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Projects: work of your holder, tied to at most one deal, with tasks in it",
		Long: "A project belongs to the holder of the actor who creates it, and is tied to at most one deal of that holder; " +
			"a task in a project takes its deal from the project. An actor reads and writes only its own holder's projects " +
			"(--all-holders widens reads). A project is created live and is closed with project close, which fails while it " +
			"has live tasks unless --cancel-tasks cancels them; project reopen makes it live again.",
		Args: cobra.NoArgs,
		RunE: groupUsage,
	}

	var in domain.ProjectInput
	var descriptionFile string
	var cv domain.ViewOptions
	create := &cobra.Command{
		Use:   "create --name <name> [--deal <deal>] [--related <id>]...",
		Short: "Create a live project", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("name") {
				return domain.InvalidFlag("name", nil, "missing required flag: --name")
			}
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				text, err := fileText(cmd, "description-file", descriptionFile)
				if err != nil {
					return nil, err
				}
				in.Description = text
				return ws.CreateProject(actor, in, cv)
			})
		},
	}
	f := create.Flags()
	f.StringVar(&in.Name, "name", "", "the project's name; its ID is pr_<slug>")
	f.StringVar(&in.Deal, "deal", "", "the deal it is tied to: a deal of your holder")
	f.StringArrayVar(&in.Related, "related", nil, "a related person, organization, deal or project (repeatable)")
	f.StringVar(&descriptionFile, "description-file", "", "a file with the description, or - for stdin")
	viewFlags(f, &cv)

	var ch domain.ProjectChange
	var name, deal, updDescription string
	var allHolders bool
	var uv domain.ViewOptions
	update := &cobra.Command{
		Use: "update <project>", Short: "Change a project of your holder", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ch.Name, ch.Deal = changed(cmd, "name", name), changed(cmd, "deal", deal)
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				if cmd.Flags().Changed("description-file") {
					text, err := fileText(cmd, "description-file", updDescription)
					if err != nil {
						return nil, err
					}
					ch.Description = &text
				}
				return ws.UpdateProject(actor, args[0], ch, uv)
			})
		},
	}
	f = update.Flags()
	f.StringVar(&name, "name", "", "a new name; the ID stays")
	f.StringVar(&deal, "deal", "", `a new deal of your holder, or "" to clear it`)
	f.StringArrayVar(&ch.Related, "related", nil, "add a related person, organization, deal or project (repeatable)")
	f.StringArrayVar(&ch.RemoveRelated, "remove-related", nil, "remove a related record (repeatable)")
	f.StringVar(&updDescription, "description-file", "", `a file with the new description, - for stdin, or "" to clear it`)
	f.BoolVar(&allHolders, "all-holders", false, "accepted, and ignored: only your own holder's projects can be changed")
	viewFlags(f, &uv)

	var cancel bool
	var clv domain.ViewOptions
	closeCmd := &cobra.Command{
		Use: "close <project> [--cancel-tasks]", Short: "Close a project with no live tasks", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.CloseProject(actor, args[0], cancel, clv)
			})
		},
	}
	closeCmd.Flags().BoolVar(&cancel, "cancel-tasks", false, "cancel the project's live tasks first; safe to repeat after a partial failure")
	viewFlags(closeCmd.Flags(), &clv)

	var rv domain.ViewOptions
	reopen := &cobra.Command{
		Use: "reopen <project>", Short: "Make a closed project live again", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.ReopenProject(actor, args[0], rv)
			})
		},
	}
	viewFlags(reopen.Flags(), &rv)

	var lf domain.ProjectFilter
	var lo domain.ListOptions
	var listAll bool
	list := &cobra.Command{
		Use: "list", Short: "List projects of your holder by name, live ones unless --all", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.readLimited(cmd, listAll, func(ws *domain.Workspace, holder string) (any, error) {
				lf.Holder = holder
				return ws.ListProjects(lf, lo)
			})
		},
	}
	f = list.Flags()
	f.StringVar(&lf.Deal, "deal", "", "only projects tied to this deal")
	f.BoolVar(&listAll, "all-holders", false, "do not limit the list to the actor's holder")
	listFlags(f, &lo)

	var gv domain.ViewOptions
	var getAll bool
	get := &cobra.Command{
		Use: "get <project>", Short: "Show one project of your holder", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.readLimited(cmd, getAll, func(ws *domain.Workspace, holder string) (any, error) {
				return ws.GetProject(holder, args[0], gv)
			})
		},
	}
	get.Flags().BoolVar(&getAll, "all-holders", false, "do not limit the read to the actor's holder")
	viewFlags(get.Flags(), &gv)

	var dv domain.ViewOptions
	var delAll bool
	del := &cobra.Command{
		Use: "delete <project>", Short: "Delete a project of your holder that nothing refers to", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.DeleteProject(actor, args[0], dv)
			})
		},
	}
	del.Flags().BoolVar(&delAll, "all-holders", false, "accepted, and ignored: only your own holder's projects can be deleted")
	viewFlags(del.Flags(), &dv)

	cmd.AddCommand(create, update, closeCmd, reopen, list, get, del)
	return cmd
}
