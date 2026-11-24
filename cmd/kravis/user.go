package main

import (
	"github.com/spf13/cobra"

	"github.com/diegomarino/kravis/internal/domain"
)

func (a *app) userCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Users: the humans and agents that operate Kravis for one holder",
		Long: "A user is a human or an agent and works for one holder, an own organization; neither ever changes. " +
			"An agent has a handler: a live human of the same holder, which never changes. " +
			"A user is deactivated with update --live=false, not deleted.",
		Args: cobra.NoArgs,
		RunE: groupUsage,
	}

	var in domain.UserInput
	var human, agent bool
	var cv domain.ViewOptions
	create := &cobra.Command{
		Use:   "create <key> --human|--agent --holder <organization> [--handler <user>] [--person <person>]",
		Short: "Create the user us_<key>", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in.Key = args[0]
			switch {
			case human && agent:
				return domain.InvalidFlag("agent", []string{domain.KindHuman, domain.KindAgent}, "--human and --agent exclude each other")
			case human:
				in.Kind = domain.KindHuman
			case agent:
				in.Kind = domain.KindAgent
			default:
				return domain.InvalidFlag("kind", []string{domain.KindHuman, domain.KindAgent}, "a user is a human or an agent: give --human or --agent")
			}
			if !cmd.Flags().Changed("holder") {
				return domain.InvalidFlag("holder", nil, "missing required flag: --holder")
			}
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.CreateUser(actor, in, cv)
			})
		},
	}
	f := create.Flags()
	f.BoolVar(&human, "human", false, "the user is a human")
	f.BoolVar(&agent, "agent", false, "the user is an agent, which needs --handler")
	f.StringVar(&in.Holder, "holder", "", "the own organization the user works for; never changes")
	f.StringVar(&in.Handler, "handler", "", "an agent's handler: a live human of the same holder; never changes")
	f.StringVar(&in.Person, "person", "", "a human's own person, by ID or email")
	f.StringVar(&in.Name, "name", "", "the user's name")
	viewFlags(f, &cv)

	var ch domain.UserChange
	var name, person, holder, handler string
	var live bool
	var uv domain.ViewOptions
	update := &cobra.Command{
		Use: "update <user>", Short: "Change a user's name, person link or live flag", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ch.Name, ch.Live, ch.Person = changed(cmd, "name", name), changed(cmd, "live", live), changed(cmd, "person", person)
			ch.Holder, ch.Handler = changed(cmd, "holder", holder), changed(cmd, "handler", handler)
			if cmd.Flags().Changed("human") || cmd.Flags().Changed("agent") {
				kind := domain.KindAgent
				if human {
					kind = domain.KindHuman
				}
				ch.Kind = &kind
			}
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.UpdateUser(actor, args[0], ch, uv)
			})
		},
	}
	f = update.Flags()
	f.StringVar(&name, "name", "", `a new name, or "" to clear it`)
	f.StringVar(&person, "person", "", `link a human to a person, by ID or email, or "" to unlink`)
	f.BoolVar(&live, "live", true, "whether the user can still act (--live=false)")
	// Refused with a reason rather than as unknown flags: these never change.
	f.StringVar(&holder, "holder", "", "refused: a user's holder never changes")
	f.StringVar(&handler, "handler", "", "refused: an agent's handler never changes")
	f.BoolVar(&human, "human", false, "refused unless the user is a human: the kind never changes")
	f.BoolVar(&agent, "agent", false, "refused unless the user is an agent: the kind never changes")
	viewFlags(f, &uv)

	cmd.AddCommand(create, update)
	cmd.AddCommand(a.verbs("user", "users",
		(*domain.Workspace).ListUsers, (*domain.Workspace).GetUser, (*domain.Workspace).DeleteUser)...)
	return cmd
}
