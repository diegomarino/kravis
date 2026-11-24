package main

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/diegomarino/kravis/internal/domain"
)

// groupUsage is the RunE of a command that only groups others: called
// alone it is a usage error, not help text, so agents get JSON.
func groupUsage(cmd *cobra.Command, _ []string) error {
	var names []string
	for _, c := range cmd.Commands() {
		if !c.Hidden {
			names = append(names, c.Name())
		}
	}
	return domain.UsageError(cmd.CommandPath()+" needs a subcommand: "+strings.Join(names, ", "), "run "+cmd.CommandPath()+" --help")
}

// viewFlags adds --fields and --full to a command that shows records.
func viewFlags(f *pflag.FlagSet, opt *domain.ViewOptions) {
	f.StringSliceVar(&opt.Fields, "fields", nil, "add these fields to the compact view, comma-separated")
	f.BoolVar(&opt.Full, "full", false, "show the whole record")
}

// listFlags adds the paging flags of a list.
func listFlags(f *pflag.FlagSet, opt *domain.ListOptions) {
	f.IntVar(&opt.Limit, "limit", domain.DefaultLimit, "the most records to return; 0 returns them all")
	f.StringVar(&opt.Cursor, "cursor", "", "continue from the next_cursor of the previous page")
	f.BoolVar(&opt.All, "all", false, "include records that are not live")
	viewFlags(f, &opt.View)
}

// changed returns a pointer to v when the flag was given, else nil.
func changed[T any](cmd *cobra.Command, name string, v T) *T {
	if cmd.Flags().Changed(name) {
		return &v
	}
	return nil
}

// write opens the workspace, resolves the actor and runs a write.
func (a *app) write(cmd *cobra.Command, do func(ws *domain.Workspace, actor string) (*domain.Result, error)) error {
	ws, err := a.open()
	if err != nil {
		return err
	}
	act, err := a.actor(ws, true)
	if err != nil {
		return err
	}
	res, err := do(ws, act.User.ID)
	if err != nil {
		return err
	}
	return a.emit(cmd, reply{Data: res.Data, Outcome: res.Outcome, Warnings: res.Warnings})
}

// read opens the workspace and runs a read.
func (a *app) read(cmd *cobra.Command, do func(ws *domain.Workspace) (any, error)) error {
	return a.readLimited(cmd, true, func(ws *domain.Workspace, _ string) (any, error) { return do(ws) })
}

// readLimited opens the workspace and runs a read of work records. With a
// declared actor, and unless allHolders, the read is limited to the actor's
// holder, which do receives ("" is no limit). A read without an actor is
// never limited.
func (a *app) readLimited(cmd *cobra.Command, allHolders bool, do func(ws *domain.Workspace, holder string) (any, error)) error {
	ws, err := a.open()
	if err != nil {
		return err
	}
	holder := ""
	if !allHolders {
		act, err := a.actor(ws, false)
		if err != nil {
			return err
		}
		if act != nil {
			holder = act.User.Holder
		}
	}
	data, err := do(ws, holder)
	if err != nil {
		return err
	}
	switch d := data.(type) {
	case *domain.ListResult:
		return a.emit(cmd, reply{Data: d.Data, Filters: nilIfEmpty(d.Filters), List: true, Total: d.Total, NextCursor: d.NextCursor})
	case *domain.GetResult:
		return a.emit(cmd, reply{Data: d.Data, Filters: nilIfEmpty(d.Filters)})
	}
	return a.emit(cmd, reply{Data: data})
}

// nilIfEmpty keeps an empty map out of the envelope: filters appears only
// when Kravis applied one.
func nilIfEmpty(m map[string]any) any {
	if len(m) == 0 {
		return nil
	}
	return m
}

// verbs builds the list, get and delete commands every type shares.
func (a *app) verbs(name, plural string,
	list func(*domain.Workspace, domain.ListOptions) (*domain.ListResult, error),
	get func(*domain.Workspace, string, domain.ViewOptions) (domain.View, error),
	del func(*domain.Workspace, string, domain.ViewOptions) (*domain.Result, error),
) []*cobra.Command {
	var lo domain.ListOptions
	listCmd := &cobra.Command{
		Use: "list", Short: "List " + plural + ", live ones only unless --all", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.read(cmd, func(ws *domain.Workspace) (any, error) { return list(ws, lo) })
		},
	}
	listFlags(listCmd.Flags(), &lo)

	var gv domain.ViewOptions
	getCmd := &cobra.Command{
		Use: "get <" + name + ">", Short: "Show one " + name, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.read(cmd, func(ws *domain.Workspace) (any, error) { return get(ws, args[0], gv) })
		},
	}
	viewFlags(getCmd.Flags(), &gv)

	var dv domain.ViewOptions
	delCmd := &cobra.Command{
		Use: "delete <" + name + ">", Short: "Delete a " + name + " that no record refers to", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, _ string) (*domain.Result, error) { return del(ws, args[0], dv) })
		},
	}
	viewFlags(delCmd.Flags(), &dv)
	return []*cobra.Command{listCmd, getCmd, delCmd}
}

func (a *app) organizationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "organization",
		Short: "Organizations: companies and other groups, shared by every holder",
		Long: "Organizations are shared by every holder: they are not tenants. An own organization (--own) is one " +
			"whose work is managed in Kravis, so it can be a holder. Domains deduplicate: a create with a known domain returns that organization.",
		Args: cobra.NoArgs,
		RunE: groupUsage,
	}

	var in domain.OrganizationInput
	var cv domain.ViewOptions
	create := &cobra.Command{
		Use: "create --name <name>", Short: "Create an organization", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("name") {
				return domain.InvalidFlag("name", nil, "missing required flag: --name")
			}
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.CreateOrganization(actor, in, cv)
			})
		},
	}
	f := create.Flags()
	f.StringVar(&in.Name, "name", "", "the organization's name")
	f.BoolVar(&in.Own, "own", false, "mark it as an own organization, which can be a holder")
	f.StringArrayVar(&in.Domains, "domain", nil, "a domain of the organization, such as example.com (repeatable)")
	viewFlags(f, &cv)

	var ch domain.OrganizationChange
	var name string
	var own, live bool
	var uv domain.ViewOptions
	update := &cobra.Command{
		Use: "update <organization>", Short: "Change an organization", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ch.Name, ch.Own, ch.Live = changed(cmd, "name", name), changed(cmd, "own", own), changed(cmd, "live", live)
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.UpdateOrganization(actor, args[0], ch, uv)
			})
		},
	}
	f = update.Flags()
	f.StringVar(&name, "name", "", "a new name; the ID stays")
	f.BoolVar(&own, "own", false, "mark or unmark it as an own organization (--own=false)")
	f.BoolVar(&live, "live", true, "whether the organization is still in use (--live=false)")
	f.StringArrayVar(&ch.Domains, "domain", nil, "add a domain (repeatable)")
	f.StringArrayVar(&ch.RemoveDomains, "remove-domain", nil, "remove a domain (repeatable)")
	viewFlags(f, &uv)

	cmd.AddCommand(create, update)
	cmd.AddCommand(a.verbs("organization", "organizations",
		(*domain.Workspace).ListOrganizations, (*domain.Workspace).GetOrganization, (*domain.Workspace).DeleteOrganization)...)
	return cmd
}

func (a *app) personCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "person",
		Short: "People: individuals with whom a relationship is kept, shared by every holder",
		Long: "People are shared by every holder. A person has a name, an email or both; every email deduplicates " +
			"and works as an alias of the person wherever a person ID is accepted.",
		Args: cobra.NoArgs,
		RunE: groupUsage,
	}

	var in domain.PersonInput
	var cv domain.ViewOptions
	create := &cobra.Command{
		Use: "create [--name <name>] [--email <email>]…", Short: "Create a person, from a name, an email or both", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.CreatePerson(actor, in, cv)
			})
		},
	}
	f := create.Flags()
	f.StringVar(&in.Name, "name", "", "the person's name")
	f.StringArrayVar(&in.Emails, "email", nil, "an email of the person (repeatable)")
	viewFlags(f, &cv)

	var ch domain.PersonChange
	var name string
	var live bool
	var uv domain.ViewOptions
	update := &cobra.Command{
		Use: "update <person or email>", Short: "Change a person", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ch.Name, ch.Live = changed(cmd, "name", name), changed(cmd, "live", live)
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.UpdatePerson(actor, args[0], ch, uv)
			})
		},
	}
	f = update.Flags()
	f.StringVar(&name, "name", "", `a new name, or "" to clear it; the ID stays`)
	f.BoolVar(&live, "live", true, "whether the person is still in use (--live=false)")
	f.StringArrayVar(&ch.Emails, "email", nil, "add an email (repeatable)")
	f.StringArrayVar(&ch.RemoveEmails, "remove-email", nil, "remove an email (repeatable)")
	viewFlags(f, &uv)

	cmd.AddCommand(create, update, a.affiliationCommand())
	cmd.AddCommand(a.verbs("person", "people",
		(*domain.Workspace).ListPeople, (*domain.Workspace).GetPerson, (*domain.Workspace).DeletePerson)...)
	return cmd
}

func (a *app) affiliationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "affiliation",
		Short: "A person's affiliations: their title at an organization",
		Long: "A person has any number of affiliations, at most one per organization, each with a free-text title. " +
			"An affiliation that ends is removed.",
		Args: cobra.NoArgs,
		RunE: groupUsage,
	}

	var title string
	var av domain.ViewOptions
	add := &cobra.Command{
		Use: "add <person> <organization> [--title <text>]", Short: "Link a person to an organization, or update the title of the link",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.AddAffiliation(actor, args[0], args[1], changed(cmd, "title", title), av)
			})
		},
	}
	add.Flags().StringVar(&title, "title", "", `the person's title there, such as CFO; "" clears it`)
	viewFlags(add.Flags(), &av)

	var rv domain.ViewOptions
	remove := &cobra.Command{
		Use: "remove <person> <organization>", Short: "End a person's affiliation to an organization", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.write(cmd, func(ws *domain.Workspace, actor string) (*domain.Result, error) {
				return ws.RemoveAffiliation(actor, args[0], args[1], rv)
			})
		},
	}
	viewFlags(remove.Flags(), &rv)

	cmd.AddCommand(add, remove)
	return cmd
}
