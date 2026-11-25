package main

import (
	"github.com/spf13/cobra"

	"github.com/diegomarino/kravis/internal/domain"
)

func (a *app) noteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "note",
		Short: "Notes: what you know about people, organizations, deals or projects",
		Long: "A note has one or more subjects (people, organizations, deals, projects) and a text kept in its .md sidecar. " +
			"It belongs to the holder of the actor who creates it, and the deals and projects among its subjects must be of that " +
			"holder. An actor reads and writes only its own holder's notes (--all-holders widens reads). A note is always live: " +
			"one that is no longer useful is deleted.",
		Args: cobra.NoArgs,
		RunE: groupUsage,
	}

	var in domain.NoteInput
	var textFile string
	var cv domain.ViewOptions
	create := &cobra.Command{
		Use:   "create --subject <id>... --text-file <file>",
		Short: "Create a note about one or more subjects", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(in.Subjects) == 0 {
				return domain.InvalidFlag("subject", nil, "missing required flag: --subject")
			}
			if !cmd.Flags().Changed("text-file") {
				return domain.InvalidFlag("text-file", nil, "missing required flag: --text-file")
			}
			text, err := fileText(cmd, "text-file", textFile)
			if err != nil {
				return err
			}
			in.Text = text
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.CreateNote(actor, in, cv)
			})
		},
	}
	f := create.Flags()
	f.StringArrayVar(&in.Subjects, "subject", nil, "a person (ID or email), organization, deal or project; deals and projects of your holder (repeatable)")
	f.StringVar(&textFile, "text-file", "", "a file with the text, or - for stdin")
	viewFlags(f, &cv)

	var ch domain.NoteChange
	var updText string
	var allHolders bool
	var uv domain.ViewOptions
	update := &cobra.Command{
		Use: "update <note>", Short: "Change the text or subjects of a note of your holder", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("text-file") {
				text, err := fileText(cmd, "text-file", updText)
				if err != nil {
					return err
				}
				ch.Text = &text
			}
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.UpdateNote(actor, args[0], ch, uv)
			})
		},
	}
	f = update.Flags()
	f.StringVar(&updText, "text-file", "", "a file with the new text, or - for stdin")
	f.StringArrayVar(&ch.Subjects, "subject", nil, "add a subject (repeatable)")
	f.StringArrayVar(&ch.RemoveSubjects, "remove-subject", nil, "remove a subject, never the last one (repeatable)")
	f.BoolVar(&allHolders, "all-holders", false, "accepted, and ignored: only your own holder's notes can be changed")
	viewFlags(f, &uv)

	var lf domain.NoteFilter
	var lo domain.ListOptions
	var listAll bool
	list := &cobra.Command{
		Use: "list", Short: "List notes of your holder, newest first", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.readLimited(cmd, listAll, func(ws *domain.Workspace, holder string) (any, error) {
				lf.Holder = holder
				return ws.ListNotes(lf, lo)
			})
		},
	}
	f = list.Flags()
	f.StringVar(&lf.RelatedTo, "related-to", "", "only notes with this person, organization, deal or project as a subject")
	f.BoolVar(&listAll, "all-holders", false, "do not limit the list to the actor's holder")
	listFlags(f, &lo)

	var gv domain.ViewOptions
	var getAll bool
	get := &cobra.Command{
		Use: "get <note>", Short: "Show one note of your holder", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.readLimited(cmd, getAll, func(ws *domain.Workspace, holder string) (any, error) {
				return ws.GetNote(holder, args[0], gv)
			})
		},
	}
	get.Flags().BoolVar(&getAll, "all-holders", false, "do not limit the read to the actor's holder")
	viewFlags(get.Flags(), &gv)

	var dv domain.ViewOptions
	var delAll bool
	del := &cobra.Command{
		Use: "delete <note>", Short: "Delete a note of your holder", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.DeleteNote(actor, args[0], dv)
			})
		},
	}
	del.Flags().BoolVar(&delAll, "all-holders", false, "accepted, and ignored: only your own holder's notes can be deleted")
	viewFlags(del.Flags(), &dv)

	cmd.AddCommand(create, update, list, get, del)
	return cmd
}
