package main

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/diegomarino/kravis/internal/schema"
)

func (a *app) schemaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "schema [type]",
		Short: "Describe the record types: fields, catalogs, defaults, order and errors",
		Long: "Print a JSON Schema for the input of a record type (user, organization, person, interaction, task, deal, project or note), or of every type " +
			"without an argument. The catalogs that apply to the actor's holder appear as enumerations, with the category of every " +
			"task status, the value used when a field is omitted, the default list order, the compact view and the error codes. " +
			"Agents query it instead of copying it.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := a.open()
			if err != nil {
				return err
			}
			holder := ""
			if act, err := a.actor(ws, false); err != nil {
				return err
			} else if act != nil {
				holder = act.User.Holder
			}
			cfg := ws.ConfigFor(holder)
			names := schema.Names()
			if len(args) == 1 {
				names = []string{args[0]}
			}
			all := map[string]any{}
			for _, n := range names {
				s, err := schema.For(n, cfg)
				if err != nil {
					return err
				}
				all[n] = s
			}
			a.output = "json" // a schema is for agents; a table would only hide it
			if len(args) == 1 {
				return a.emit(cmd, reply{Data: all[args[0]]})
			}
			return a.emit(cmd, reply{Data: all})
		},
	}
}

// commandNode is one command of the tree that the hidden tree command prints.
type commandNode struct {
	Command string   `json:"command"`
	Flags   []string `json:"flags"`
}

// treeCommand prints every runnable command with its flags, for the test
// that keeps the skill true to the binary.
func (a *app) treeCommand() *cobra.Command {
	return &cobra.Command{
		Use: "tree", Hidden: true, Args: cobra.NoArgs,
		Short: "Print the command tree with its flags",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var out []commandNode
			var walk func(c *cobra.Command)
			walk = func(c *cobra.Command) {
				if c.Hidden {
					return
				}
				if c.Runnable() {
					path := strings.TrimPrefix(c.CommandPath(), c.Root().Name())
					seen := map[string]bool{}
					flags := []string{}
					add := func(f *pflag.Flag) {
						if !f.Hidden && !seen[f.Name] {
							seen[f.Name] = true
							flags = append(flags, "--"+f.Name)
						}
					}
					c.LocalFlags().VisitAll(add)
					c.InheritedFlags().VisitAll(add)
					sort.Strings(flags)
					out = append(out, commandNode{Command: strings.TrimSpace(path), Flags: flags})
				}
				for _, sub := range c.Commands() {
					walk(sub)
				}
			}
			walk(cmd.Root())
			sort.Slice(out, func(i, j int) bool { return out[i].Command < out[j].Command })
			return a.emit(cmd, reply{Data: out})
		},
	}
}
