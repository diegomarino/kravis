package main

import (
	"github.com/spf13/cobra"

	"github.com/diegomarino/kravis/internal/domain"
)

func (a *app) taskCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Tasks: what to do, answered for by a human and maybe carried out by their agent",
		Long: "A task has one assignee, a live human of its holder, and may have an assigned agent that the assignee handles. " +
			"It belongs to the holder of the actor who creates it, and an actor reads and writes only its own holder's tasks " +
			"(--all-holders widens reads). A task created with --interaction records where it comes from and has a purpose, " +
			"follow-up by default; creating it again returns the existing one. The status changes only with task status.",
		Args: cobra.NoArgs,
		RunE: groupUsage,
	}

	var in domain.TaskInput
	var descriptionFile string
	var cv domain.ViewOptions
	create := &cobra.Command{
		Use:   "create --title <text> --assignee <human> [--agent <agent>] [--due <date>] [--interaction <interaction>]",
		Short: "Create a task in todo", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, name := range []string{"title", "assignee"} {
				if !cmd.Flags().Changed(name) {
					return domain.InvalidFlag(name, nil, "missing required flag: --%s", name)
				}
			}
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				text, err := fileText(cmd, "description-file", descriptionFile)
				if err != nil {
					return nil, err
				}
				in.Description = text
				return ws.CreateTask(actor, in, cv)
			})
		},
	}
	f := create.Flags()
	f.StringVar(&in.Title, "title", "", "what is to be done")
	f.StringVar(&in.Assignee, "assignee", "", "the human who answers for it: a live human user of your holder")
	f.StringVar(&in.Agent, "agent", "", "the agent that carries it out: an agent whose handler is the assignee")
	f.StringVar(&in.Due, "due", "", "the due date, such as 2026-11-28, in the workspace timezone")
	f.StringVar(&in.Purpose, "purpose", "", "why it exists: a purpose of the catalog (follow-up when it comes from an interaction)")
	f.StringVar(&in.Interaction, "interaction", "", "the interaction it comes from; one task per interaction and purpose")
	f.StringVar(&in.Project, "project", "", "its parent project, of your holder: it takes the project's deal; not with --deal")
	f.StringVar(&in.Deal, "deal", "", "its parent deal, of your holder; not with --project")
	f.StringArrayVar(&in.Related, "related", nil, "a related person, organization, deal or project (repeatable)")
	f.StringVar(&descriptionFile, "description-file", "", "a file with the description, or - for stdin")
	viewFlags(f, &cv)

	var ch domain.TaskChange
	var title, assignee, agent, due, status, updDescription, parent string
	var allHolders bool
	var uv domain.ViewOptions
	update := &cobra.Command{
		Use: "update <task>", Short: "Change a task of your holder", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, name := range []string{"project", "deal"} {
				if cmd.Flags().Changed(name) {
					return &domain.Error{Code: domain.CodeUnknownField, Message: "unknown flag: --" + name,
						Hint:    "the parent is fixed at creation: delete the task and create it again with the right parent",
						Details: map[string]any{"field": name}}
				}
			}
			ch.Title, ch.Assignee, ch.Agent = changed(cmd, "title", title), changed(cmd, "assignee", assignee), changed(cmd, "agent", agent)
			ch.Due, ch.Status = changed(cmd, "due", due), changed(cmd, "status", status)
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				if cmd.Flags().Changed("description-file") {
					text, err := fileText(cmd, "description-file", updDescription)
					if err != nil {
						return nil, err
					}
					ch.Description = &text
				}
				return ws.UpdateTask(actor, args[0], ch, uv)
			})
		},
	}
	f = update.Flags()
	f.StringVar(&title, "title", "", "a new title")
	f.StringVar(&assignee, "assignee", "", "a new assignee: a live human of your holder, who must handle the agent if there is one")
	f.StringVar(&agent, "agent", "", `a new assigned agent, or "" to clear it`)
	f.StringVar(&due, "due", "", `a new due date, or "" to clear it`)
	f.StringVar(&updDescription, "description-file", "", `a file with the new description, - for stdin, or "" to clear it`)
	f.StringVar(&status, "status", "", "refused: the status changes only with kravis task status")
	f.StringArrayVar(&ch.Related, "related", nil, "add a related person, organization, deal or project (repeatable)")
	f.StringArrayVar(&ch.RemoveRelated, "remove-related", nil, "remove a related record (repeatable)")
	f.StringVar(&parent, "project", "", "refused: the parent is fixed at creation")
	f.StringVar(&parent, "deal", "", "refused: the parent is fixed at creation")
	_ = f.MarkHidden("project")
	_ = f.MarkHidden("deal")
	f.BoolVar(&allHolders, "all-holders", false, "accepted, and ignored: only your own holder's tasks can be changed")
	viewFlags(f, &uv)

	var sv domain.ViewOptions
	var statusAll bool
	setStatus := &cobra.Command{
		Use: "status <task> <status>", Short: "Move a task to a status of the catalog", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.SetTaskStatus(actor, args[0], args[1], sv)
			})
		},
	}
	setStatus.Flags().BoolVar(&statusAll, "all-holders", false, "accepted, and ignored: only your own holder's tasks can change status")
	viewFlags(setStatus.Flags(), &sv)

	var lf domain.TaskFilter
	var lo domain.ListOptions
	var listAll bool
	list := &cobra.Command{
		Use: "list", Short: "List tasks of your holder by due date, live ones unless --all or --category", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.readLimited(cmd, listAll, func(ws *domain.Workspace, holder string) (any, error) {
				lf.Holder = holder
				return ws.ListTasks(lf, lo)
			})
		},
	}
	f = list.Flags()
	f.StringVar(&lf.Assignee, "assignee", "", "only tasks of this human")
	f.StringVar(&lf.Agent, "agent", "", "only tasks assigned to this agent")
	f.StringVar(&lf.Category, "category", "", "only tasks in this status category: open, waiting, done or canceled")
	f.BoolVar(&lf.Overdue, "overdue", false, "only live tasks due before today in the workspace timezone")
	f.StringVar(&lf.DueBefore, "due-before", "", "only tasks due before this date")
	f.StringVar(&lf.Project, "project", "", "only tasks in this project")
	f.StringVar(&lf.Deal, "deal", "", "only tasks of this deal and of its projects")
	f.StringVar(&lf.RelatedTo, "related-to", "", "only tasks related to this person, organization, deal or project, or whose parent it is, or whose source interaction has it as a participant")
	f.BoolVar(&listAll, "all-holders", false, "do not limit the list to the actor's holder")
	listFlags(f, &lo)

	var gv domain.ViewOptions
	var getAll bool
	get := &cobra.Command{
		Use: "get <task>", Short: "Show one task of your holder", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.readLimited(cmd, getAll, func(ws *domain.Workspace, holder string) (any, error) {
				return ws.GetTask(holder, args[0], gv)
			})
		},
	}
	get.Flags().BoolVar(&getAll, "all-holders", false, "do not limit the read to the actor's holder")
	viewFlags(get.Flags(), &gv)

	var dv domain.ViewOptions
	var delAll bool
	del := &cobra.Command{
		Use: "delete <task>", Short: "Delete a task of your holder", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.DeleteTask(actor, args[0], dv)
			})
		},
	}
	del.Flags().BoolVar(&delAll, "all-holders", false, "accepted, and ignored: only your own holder's tasks can be deleted")
	viewFlags(del.Flags(), &dv)

	cmd.AddCommand(create, update, setStatus, list, get, del)
	return cmd
}
