package main

import (
	"bytes"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/diegomarino/kravis/internal/domain"
)

// summaryText reads --summary-file: a path, or - for stdin. An empty path
// is an empty summary.
func summaryText(cmd *cobra.Command, path string) (string, error) {
	return fileText(cmd, "summary-file", path)
}

// fileText reads the text of a --<flag>-file flag: a path, or - for stdin.
// Stdin drops one trailing newline; a file's text is kept as is. An empty
// path is an empty text.
func fileText(cmd *cobra.Command, flag, path string) (string, error) {
	var data []byte
	var err error
	switch path {
	case "":
		return "", nil
	case "-":
		data, err = io.ReadAll(cmd.InOrStdin())
		// A pipe adds a newline the text did not have; a file stays as is.
		if rest, ok := bytes.CutSuffix(data, []byte("\n")); ok {
			data = bytes.TrimSuffix(rest, []byte("\r"))
		}
	default:
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", domain.InvalidFlag(flag, nil, "cannot read the text of --%s: %v", flag, err)
	}
	return string(data), nil
}

func (a *app) interactionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "interaction",
		Short: "Interactions: what happened with people or organizations, kept by one holder",
		Long: "An interaction is something that happened through a channel: an email, a meeting, a call. It belongs to the " +
			"holder of the actor who records it, and an actor reads and writes only its own holder's interactions " +
			"(--all-holders widens reads). It has participants, people or organizations, and source references that point " +
			"back to the original, which Kravis never copies. Recording the same email again returns the existing interaction.",
		Args: cobra.NoArgs,
		RunE: groupUsage,
	}

	var in domain.InteractionInput
	var summaryFile string
	var cv domain.ViewOptions
	create := &cobra.Command{
		Use:   "create --channel <channel> --occurred-at <time> --subject <text> --participant <person or organization>…",
		Short: "Record an interaction", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, name := range []string{"occurred-at", "subject", "participant"} {
				if !cmd.Flags().Changed(name) {
					return domain.InvalidFlag(name, nil, "missing required flag: --%s", name)
				}
			}
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				text, err := summaryText(cmd, summaryFile)
				if err != nil {
					return nil, err
				}
				in.Summary = text
				return ws.CreateInteraction(actor, in, cv)
			})
		},
	}
	f := create.Flags()
	f.StringVar(&in.Channel, "channel", "", "how it took place: a channel of the catalog, such as email or meeting")
	f.StringVar(&in.OccurredAt, "occurred-at", "", "when it happened, RFC 3339, such as 2026-11-23T17:02:00Z")
	f.StringVar(&in.Subject, "subject", "", "what it was about")
	f.StringArrayVar(&in.Participants, "participant", nil, "a person (ID or email) or an organization (ID) that took part (repeatable)")
	f.StringArrayVar(&in.Refs, "ref", nil, "a source reference, <source>:<kind>:<id>[@<scope>], such as email:message:<message-id> (repeatable)")
	f.StringVar(&in.Direction, "direction", "", "outbound (ours started it, the default) or inbound")
	f.StringVar(&in.InitiatedBy, "initiated-by", "", "who started it: a user of the holder if outbound, a participant if inbound; never inferred")
	f.StringVar(&summaryFile, "summary-file", "", "a file with the summary, or - for stdin")
	f.StringArrayVar(&in.Deals, "deal", nil, "a deal of your holder it belongs to (repeatable)")
	f.StringArrayVar(&in.Projects, "project", nil, "a project of your holder it belongs to (repeatable)")
	viewFlags(f, &cv)

	var ch domain.InteractionChange
	var channel, subject, direction, initiatedBy, updSummary string
	var live, allHolders bool
	var uv domain.ViewOptions
	update := &cobra.Command{
		Use: "update <interaction>", Short: "Change an interaction of your holder", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ch.Channel, ch.Subject = changed(cmd, "channel", channel), changed(cmd, "subject", subject)
			ch.Direction, ch.InitiatedBy = changed(cmd, "direction", direction), changed(cmd, "initiated-by", initiatedBy)
			ch.Live = changed(cmd, "live", live)
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				if cmd.Flags().Changed("summary-file") {
					text, err := summaryText(cmd, updSummary)
					if err != nil {
						return nil, err
					}
					ch.Summary = &text
				}
				return ws.UpdateInteraction(actor, args[0], ch, uv)
			})
		},
	}
	f = update.Flags()
	f.StringVar(&channel, "channel", "", "a new channel of the catalog")
	f.StringVar(&subject, "subject", "", "a new subject; the ID stays")
	f.StringVar(&direction, "direction", "", "outbound or inbound")
	f.StringVar(&initiatedBy, "initiated-by", "", `who started it, or "" to clear it`)
	f.StringVar(&updSummary, "summary-file", "", `a file with the new summary, - for stdin, or "" to clear it`)
	f.BoolVar(&live, "live", true, "whether the interaction still counts (--live=false)")
	f.StringArrayVar(&ch.Participants, "participant", nil, "add a participant (repeatable)")
	f.StringArrayVar(&ch.RemoveParticipants, "remove-participant", nil, "remove a participant (repeatable)")
	f.StringArrayVar(&ch.Refs, "ref", nil, "add a source reference (repeatable)")
	f.StringArrayVar(&ch.RemoveRefs, "remove-ref", nil, "remove a source reference (repeatable)")
	f.StringArrayVar(&ch.Deals, "deal", nil, "link a deal of your holder (repeatable)")
	f.StringArrayVar(&ch.RemoveDeals, "remove-deal", nil, "unlink a deal (repeatable)")
	f.StringArrayVar(&ch.Projects, "project", nil, "link a project of your holder (repeatable)")
	f.StringArrayVar(&ch.RemoveProjects, "remove-project", nil, "unlink a project (repeatable)")
	f.BoolVar(&allHolders, "all-holders", false, "accepted, and ignored: only your own holder's interactions can be changed")
	viewFlags(f, &uv)

	var lf domain.InteractionFilter
	var lo domain.ListOptions
	var listAll bool
	list := &cobra.Command{
		Use: "list", Short: "List interactions of your holder, newest first, live ones unless --all", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.readLimited(cmd, listAll, func(ws *domain.Workspace, holder string) (any, error) {
				lf.Holder = holder
				return ws.ListInteractions(lf, lo)
			})
		},
	}
	f = list.Flags()
	f.StringVar(&lf.Participant, "participant", "", "only those with this participant, a person (ID or email) or an organization")
	f.StringVar(&lf.Direction, "direction", "", "only outbound or inbound ones")
	f.StringVar(&lf.InitiatedBy, "initiated-by", "", "only those started by this user, person or organization")
	f.BoolVar(&lf.WithAgents, "with-agents", false, "with --initiated-by <human>: also those started by the human's agents")
	f.StringVar(&lf.Channel, "channel", "", "only those through this channel")
	f.StringVar(&lf.Deal, "deal", "", "only those linked to this deal or to a project tied to it")
	f.StringVar(&lf.Project, "project", "", "only those linked to this project")
	f.StringVar(&lf.From, "from", "", "only from this date (in the workspace timezone) or instant")
	f.StringVar(&lf.To, "to", "", "only up to this date (included, in the workspace timezone) or instant")
	f.BoolVar(&listAll, "all-holders", false, "do not limit the list to the actor's holder")
	listFlags(f, &lo)

	var gv domain.ViewOptions
	var getAll bool
	get := &cobra.Command{
		Use: "get <interaction>", Short: "Show one interaction of your holder", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.readLimited(cmd, getAll, func(ws *domain.Workspace, holder string) (any, error) {
				return ws.GetInteraction(holder, args[0], gv)
			})
		},
	}
	get.Flags().BoolVar(&getAll, "all-holders", false, "do not limit the read to the actor's holder")
	viewFlags(get.Flags(), &gv)

	var dv domain.ViewOptions
	var delAll bool
	del := &cobra.Command{
		Use: "delete <interaction>", Short: "Delete an interaction of your holder that no record refers to", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.DeleteInteraction(actor, args[0], dv)
			})
		},
	}
	del.Flags().BoolVar(&delAll, "all-holders", false, "accepted, and ignored: only your own holder's interactions can be deleted")
	viewFlags(del.Flags(), &dv)

	cmd.AddCommand(create, update, list, get, del)
	return cmd
}
