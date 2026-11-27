package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/diegomarino/kravis/internal/domain"
)

func (a *app) searchCommand() *cobra.Command {
	var types []string
	var allHolders bool
	var lo domain.ListOptions
	cmd := &cobra.Command{
		Use:   "search <text>",
		Short: "Find records by their own text, without knowing their type or ID",
		Long: "Find the records in which every word of the text appears, as a substring and ignoring case and accents, in the record's own text: " +
			"names, emails, domains, subjects, summaries, titles, descriptions and note text. Every word must appear in the same record: " +
			"\"vogel norte\" finds nothing when the words are in a person and an organization. IDs and links are not searched: " +
			"to get what is linked to a hit, use the filters of each list (--related-to, --participant, --party, --deal, --project). " +
			"Newest update first; each hit says which fields matched, with an excerpt. Only live records unless --all " +
			"(a warning says how many were left out); --fields and --full widen each hit's compact view; " +
			"an actor limits the work records to its holder unless --all-holders.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.readLimited(cmd, allHolders, func(ws *domain.Workspace, holder string) (any, error) {
				return ws.Search(domain.SearchOptions{Text: strings.Join(args, " "), Types: types, Holder: holder, Page: lo})
			})
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&types, "type", nil, "search only this type: person, organization, interaction, task, deal, project or note (repeatable)")
	f.BoolVar(&allHolders, "all-holders", false, "do not limit the work records to the actor's holder")
	f.IntVar(&lo.Limit, "limit", domain.DefaultLimit, "the most hits to return; 0 returns them all")
	f.StringVar(&lo.Cursor, "cursor", "", "continue from the next_cursor of the previous page")
	f.BoolVar(&lo.All, "all", false, "include records that are not live")
	viewFlags(f, &lo.View)
	return cmd
}
