package main

import "slices"

// A contract case is one command line, the state it runs in and what it must
// produce. The list is data on purpose: TestCLIMatchesTheContract runs it
// against docs/cli.md, and the golden snapshots of task 09 reuse it.
//
// What a case says:
//   - State: a key of contractStates (contract_world_test.go).
//   - Args, Stdin, Env, TTY: the command line, its standard input, extra
//     environment variables and whether stdout is a terminal. {name} in Args,
//     Env values and Before is replaced by a variable of the world: {task},
//     {task2}, {task3}, {note}, {note2}, {ws}, and {cursor}, the next_cursor
//     of the last Before step that gave one.
//   - Before: commands run first, in the same state.
//   - Outcome: the outcome of a successful write ("" for a read).
//   - Code: the error code of a failure ("" for a success). The exit code is
//     not stated: the test takes it from docs/cli.md.
//   - Warnings: the warning codes a success must carry, in order.
//   - Findings: for `check` with findings (exit 6), the finding codes.
type contractCase struct {
	State    string
	Args     []string
	Stdin    string
	Env      map[string]string
	TTY      bool
	Before   [][]string
	Outcome  string
	Code     string
	Warnings []string
	Findings []string
}

func (c contractCase) in(stdin string) contractCase { c.Stdin = stdin; return c }
func (c contractCase) warns(codes ...string) contractCase {
	c.Warnings = codes
	return c
}
func (c contractCase) env(k, v string) contractCase {
	c.Env = map[string]string{k: v}
	return c
}
func (c contractCase) tty() contractCase { c.TTY = true; return c }
func (c contractCase) inState(state string) contractCase {
	c.State = state
	return c
}
func (c contractCase) after(steps ...[]string) contractCase { c.Before = steps; return c }

// read is a successful command that is not a write.
func read(args ...string) contractCase { return contractCase{State: baseDunder, Args: args} }

// write is a successful write with its outcome.
func write(outcome string, args ...string) contractCase {
	return contractCase{State: baseDunder, Args: args, Outcome: outcome}
}

// fail is a command that fails with an error code.
func fail(code string, args ...string) contractCase {
	return contractCase{State: baseDunder, Args: args, Code: code}
}

const (
	pipeline = "it_2026-11-10-q4-pipeline-review"
	branch   = "it_2026-11-12-branch-numbers"
	pitch    = "it_2026-11-17-pitch-to-corporate"
	msDeal   = "dl_michael-scott-paper-company"
)

// contractCases is every case. The first block is the command line itself
// (version, usage errors, output, workspace), then one block per command.
func contractCases() []contractCase {
	var cs []contractCase
	add := func(more ...contractCase) { cs = append(cs, more...) }

	// --- The command line, output and workspace ------------------------------
	add(
		read("--version").inState(baseNone),
		fail("INVALID_VALUE", "nope").inState(baseNone),
		fail("INVALID_VALUE"),
		fail("INVALID_VALUE", "help"),
		fail("INVALID_VALUE", "task", "frobnicate"),
		fail("INVALID_VALUE", "user"),
		fail("INVALID_VALUE", "organization"),
		fail("INVALID_VALUE", "person"),
		fail("INVALID_VALUE", "person", "affiliation"),
		fail("INVALID_VALUE", "interaction"),
		fail("INVALID_VALUE", "task"),
		fail("INVALID_VALUE", "deal"),
		fail("INVALID_VALUE", "deal", "party"),
		fail("INVALID_VALUE", "project"),
		fail("INVALID_VALUE", "note"),
		fail("UNKNOWN_FIELD", "task", "list", "--owner", "us_pam"),
		fail("UNKNOWN_FIELD", "task", "create", "--title", "Fix the fax", "--delegate", "us_pam-agent"),
		fail("INVALID_VALUE", "whoami", "--output", "yaml"),
		read("whoami", "--output", "json").tty(),
		read("whoami", "--verbose"),
		fail("STORAGE_UNAVAILABLE", "check").inState(baseNone),
		fail("STORAGE_UNAVAILABLE", "person", "list").inState(baseNone),
		fail("STORAGE_UNAVAILABLE", "person", "list").inState("dunder, storage unavailable"),
		fail("INTERNAL", "person", "list").inState("dunder, storage fails"),
		fail("VERSION_CONFLICT", "person", "update", "pe_jim-halpert", "--name", "James Halpert").inState("dunder, writers keep racing"),
	)

	// --- init ----------------------------------------------------------------
	add(
		write("created", initArgs...).inState(baseNone),
		write("created", "init", "--timezone", "America/New_York", "--organization", "Dunder Mifflin Scranton", "--user", "pam").inState(baseNone).env("KRAVIS_WORKSPACE", "{ws}"),
		write("existing", initArgs...).inState(baseInit),
		fail("WORKSPACE_EXISTS", "init", "--timezone", "America/New_York", "--organization", "Dunder Mifflin Paper", "--user", "pam").inState(baseInit),
		fail("INVALID_VALUE", "init", "--timezone", "America/New_York").inState(baseNone),
		fail("INVALID_VALUE", "init", "--timezone", "Mars/Base", "--organization", "Dunder Mifflin Scranton", "--user", "pam").inState(baseNone),
	)

	// --- whoami --------------------------------------------------------------
	add(
		read("whoami"),
		read("whoami", "--actor", "us_michael"),
		read("whoami").env("KRAVIS_ACTOR", "us_michael"),
		write("updated", "whoami", "--set", "us_michael"),
		write("unchanged", "whoami", "--set", "us_pam"),
		fail("MISSING_ACTOR", "whoami").inState("dunder, no default actor"),
		fail("ACTOR_NOT_LIVE", "whoami", "--actor", "us_nobody"),
		fail("ACTOR_NOT_LIVE", "whoami", "--set", "us_nobody"),
	)

	// --- schema and check ----------------------------------------------------
	add(
		read("schema"),
		read("schema", "task"),
		read("schema", "deal"),
		fail("INVALID_VALUE", "schema", "wizard"),
		read("check"),
	)
	for _, d := range []struct {
		state string
		codes []string
	}{
		{"damaged: config.json missing", []string{"WORKSPACE_FILE_MISSING"}},
		{"damaged: config.json invalid", []string{"WORKSPACE_FILE_INVALID"}},
		{"damaged: orphan sidecar", []string{"ORPHAN_SIDECAR"}},
		{"damaged: unreadable record", []string{"UNREADABLE_RECORD", "KEY_WITHOUT_VALUE"}}, // its key finds no readable value
		{"damaged: malformed record", []string{"MALFORMED_RECORD"}},
		{"damaged: invalid record", []string{"INVALID_RECORD"}},
		{"damaged: key without value", []string{"KEY_WITHOUT_VALUE"}},
		{"damaged: value without key", []string{"VALUE_WITHOUT_KEY"}},
		{"damaged: broken reference", []string{"BROKEN_REFERENCE"}},
		{"damaged: dangling reference", []string{"DANGLING_REFERENCE"}},
		{"damaged: value not in catalog", []string{"VALUE_NOT_IN_CATALOG"}},
		{"damaged: holder mismatch", []string{"HOLDER_MISMATCH"}},
	} {
		c := read("check").inState(d.state)
		c.Findings = d.codes
		add(c)
	}

	// --- search --------------------------------------------------------------
	add(
		read("search", "halpert"),
		read("search", "vance", "--type", "person", "--type", "organization"),
		read("search", "dunder", "--all"),
		read("search", "vance.example", "--type", "organization", "--fields", "domains"),
		read("search", "vance", "--full"),
		read("search", "dunder", "--limit", "1"),
		read("search", "dunder", "--limit", "1", "--cursor", "{cursor}").after([]string{"search", "dunder", "--limit", "1"}),
		fail("INVALID_VALUE", "search", "dunder", "--cursor", "garbage"),
		read("search", "dwight", "--all-holders"),
		read("search", "halpert").inState("dunder, no default actor"),
		read("search", "warehouse").warns("NOT_LIVE_HIDDEN"),
		read("search", "zzzz-nothing-like-this"),
		fail("INVALID_VALUE", "search", "   "),
		fail("INVALID_VALUE", "search", "vance", "--type", "user"),
		fail("UNKNOWN_FIELD", "search", "vance", "--fields", "wizardry"),
	)

	add(userCases()...)
	add(organizationCases()...)
	add(personCases()...)
	add(interactionCases()...)
	add(taskCases()...)
	add(dealCases()...)
	add(projectCases()...)
	add(noteCases()...)
	add(listCases()...)
	add(widenedCases(cs)...)
	return cs
}

func userCases() []contractCase {
	return []contractCase{
		read("user", "list"),
		read("user", "list", "--all"),
		read("user", "get", "us_pam"),
		read("user", "get", "us_pam", "--full"),
		read("user", "get", "us_pam", "--fields", "person"),
		fail("NOT_FOUND", "user", "get", "us_nobody"),
		write("created", "user", "create", "darryl", "--human", "--holder", "org_dunder-mifflin-scranton", "--name", "Darryl Philbin", "--person", "pe_toby-flenderson"),
		write("created", "user", "create", "ryan-agent", "--agent", "--holder", "org_dunder-mifflin-scranton", "--handler", "us_michael"),
		write("existing", "user", "create", "michael", "--human", "--holder", "org_dunder-mifflin-scranton", "--person", "pe_michael-scott"),
		fail("INVALID_VALUE", "user", "create", "ryan-agent", "--agent", "--holder", "org_dunder-mifflin-scranton"),
		fail("INVALID_VALUE", "user", "create", "ryan-human", "--human", "--agent", "--holder", "org_dunder-mifflin-scranton"),
		write("updated", "user", "update", "us_michael", "--name", "Michael G. Scott"),
		write("unchanged", "user", "update", "us_pam", "--person", "pe_pam-beesly"),
		write("unchanged", "user", "update", "us_pam", "--human", "--holder", "org_dunder-mifflin-scranton"),
		write("unchanged", "user", "update", "us_pam-agent", "--agent", "--handler", "us_pam"),
		fail("INVALID_VALUE", "user", "update", "us_pam-agent", "--handler", "us_michael"),
		write("updated", "user", "update", "us_kevin", "--live=false"),
		fail("REFERENCED", "user", "update", "us_michael", "--live=false"),
		fail("INVALID_VALUE", "user", "update", "us_dwight", "--live=false"),
		write("deleted", "user", "delete", "us_kevin"),
		fail("REFERENCED", "user", "delete", "us_michael"),
		fail("NOT_FOUND", "user", "delete", "us_nobody"),
	}
}

func organizationCases() []contractCase {
	return []contractCase{
		read("organization", "list"),
		read("organization", "list", "--all"),
		read("organization", "get", "org_prince-family-paper"),
		read("organization", "get", "org_vance-refrigeration", "--full", "--fields", "domains"),
		fail("NOT_FOUND", "organization", "get", "org_nowhere"),
		write("created", "organization", "create", "--name", "Schrader Farms", "--own", "--domain", "schrader.example", "--domain", "beets.schrader.example"),
		write("existing", "organization", "create", "--name", "Vance Refrigeration", "--domain", "vance.example"),
		write("created", "organization", "create", "--name", "Prince Family Paper").warns("SIMILAR_EXISTS"),
		fail("DEDUP_CONFLICT", "organization", "create", "--name", "Vance Industries", "--domain", "vance.example"),
		fail("INVALID_VALUE", "organization", "create"),
		write("updated", "organization", "update", "org_sabre", "--name", "Sabre Printers", "--domain", "sabre-printers.example"),
		write("updated", "organization", "update", "org_sabre", "--remove-domain", "sabre.example"),
		write("updated", "organization", "update", "org_vance-refrigeration", "--own"),
		write("updated", "organization", "update", "org_sabre", "--live=false"),
		write("unchanged", "organization", "update", "org_sabre", "--name", "Sabre"),
		write("deleted", "organization", "delete", "org_sabre"),
		fail("REFERENCED", "organization", "delete", "org_prince-family-paper"),
		fail("NOT_FOUND", "organization", "delete", "org_nowhere"),
	}
}

func personCases() []contractCase {
	return []contractCase{
		read("person", "list"),
		read("person", "list", "--all"),
		read("person", "get", "pe_jim-halpert"),
		read("person", "get", "jim.halpert@dundermifflin.example", "--fields", "affiliations"),
		read("person", "get", "pe_jim-halpert", "--full"),
		fail("NOT_FOUND", "person", "get", "pe_nobody"),
		write("created", "person", "create", "--name", "Kelly Kapoor", "--email", "kelly.kapoor@dundermifflin.example", "--email", "kelly@kapoor.example"),
		write("existing", "person", "create", "--name", "Jim Halpert", "--email", "JIM.HALPERT@dundermifflin.example"),
		write("created", "person", "create", "--name", "Jim Halpert", "--email", "jim@halpert.example").warns("SIMILAR_EXISTS"),
		write("created", "person", "create", "--email", "info@dundermifflin.example").warns("GENERIC_ADDRESS"),
		fail("DEDUP_CONFLICT", "person", "create", "--name", "Jim Halpert", "--email", "jim.halpert@dundermifflin.example", "--email", "dwight.schrute@dundermifflin.example"),
		fail("INVALID_VALUE", "person", "create"),
		write("updated", "person", "update", "pe_jim-halpert", "--name", "James Halpert", "--email", "jim@halpert.example"),
		write("updated", "person", "update", "pe_jim-halpert", "--remove-email", "jim.halpert@dundermifflin.example", "--email", "jim@halpert.example"),
		write("updated", "person", "update", "pe_toby-flenderson", "--live=false"),
		write("unchanged", "person", "update", "pe_toby-flenderson", "--name", "Toby Flenderson"),
		write("deleted", "person", "delete", "pe_toby-flenderson"),
		fail("REFERENCED", "person", "delete", "pe_jim-halpert"),
		fail("NOT_FOUND", "person", "delete", "pe_nobody"),
		write("created", "person", "affiliation", "add", "pe_toby-flenderson", "org_sabre", "--title", "HR"),
		write("updated", "person", "affiliation", "add", "pe_bob-vance", "org_vance-refrigeration", "--title", "Founder"),
		write("deleted", "person", "affiliation", "remove", "pe_bob-vance", "org_vance-refrigeration"),
		fail("NOT_FOUND", "person", "affiliation", "remove", "pe_toby-flenderson", "org_sabre"),
	}
}

func interactionCases() []contractCase {
	return []contractCase{
		read("interaction", "list"),
		read("interaction", "list", "--participant", "pe_jim-halpert"),
		read("interaction", "list", "--direction", "inbound"),
		read("interaction", "list", "--initiated-by", "us_pam", "--with-agents"),
		read("interaction", "list", "--channel", "email", "--from", "2026-11-01", "--to", "2026-11-30"),
		read("interaction", "list", "--deal", msDeal),
		read("interaction", "list", "--project", "pr_paper-stock-audit"),
		read("interaction", "list", "--all", "--all-holders"),
		read("interaction", "get", pipeline),
		read("interaction", "get", pipeline, "--full", "--fields", "summary"),
		fail("NOT_FOUND", "interaction", "get", "it_2026-01-01-nothing"),
		fail("NOT_FOUND", "--actor", "us_dwight", "interaction", "get", pipeline),
		write("created", "interaction", "create", "--channel", "call", "--occurred-at", "2026-11-23T16:00:00Z",
			"--subject", "Call with Bob Vance", "--participant", "pe_bob-vance", "--direction", "outbound", "--initiated-by", "us_pam").in("Bob wants a quote.\n"),
		write("created", "interaction", "create", "--channel", "meeting", "--occurred-at", "2026-11-23T10:00:00Z",
			"--subject", "Audit kickoff", "--participant", "pe_jim-halpert", "--deal", msDeal, "--project", "pr_paper-stock-audit",
			"--ref", "email:message:audit-kickoff@dundermifflin.example", "--summary-file", "-").in("Kickoff of the audit.\n"),
		write("existing", "interaction", "create", "--channel", "email", "--occurred-at", "2026-11-10T14:00:00Z",
			"--subject", "Q4 pipeline review", "--participant", "pe_jim-halpert", "--participant", "pe_dwight-schrute",
			"--ref", "email:message:pipeline-review-1@dundermifflin.example", "--initiated-by", "us_pam"),
		write("created", "interaction", "create", "--channel", "email", "--occurred-at", "2026-11-23T11:00:00Z",
			"--subject", "Ticket from Angela", "--participant", "pe_angela-martin", "--ref", "zendesk:ticket:1234").warns("MULTIPLE_AFFILIATIONS", "UNKNOWN_SOURCE"),
		fail("DEDUP_CONFLICT", "interaction", "create", "--channel", "email", "--occurred-at", "2026-11-23T11:00:00Z",
			"--subject", "Two threads", "--participant", "pe_jim-halpert",
			"--ref", "email:message:pipeline-review-1@dundermifflin.example", "--ref", "email:message:branch-numbers@dundermifflin.example"),
		fail("INVALID_VALUE", "interaction", "create", "--channel", "carrier-pigeon", "--occurred-at", "2026-11-23T11:00:00Z", "--subject", "Coo"),
		fail("HOLDER_MISMATCH", "interaction", "create", "--channel", "call", "--occurred-at", "2026-11-23T11:00:00Z", "--subject", "Beets", "--participant", "pe_jim-halpert", "--deal", "dl_dwight-schrute"),
		write("updated", "interaction", "update", branch, "--subject", "Branch numbers, again", "--channel", "call", "--direction", "outbound", "--initiated-by", "us_pam"),
		write("updated", "interaction", "update", pitch, "--participant", "pe_bob-vance", "--remove-participant", "pe_michael-scott"),
		write("updated", "interaction", "update", branch, "--ref", "email:message:branch-numbers-2@dundermifflin.example", "--summary-file", "-").in("Jan asked again.\n"),
		write("updated", "interaction", "update", branch, "--remove-ref", "email:message:branch-numbers@dundermifflin.example"),
		write("updated", "interaction", "update", branch, "--deal", msDeal, "--project", "pr_desk-move"),
		write("updated", "interaction", "update", pitch, "--remove-deal", msDeal),
		write("updated", "interaction", "update", branch, "--remove-project", "pr_desk-move").after([]string{"interaction", "update", branch, "--project", "pr_desk-move"}),
		write("updated", "interaction", "update", branch, "--live=false"),
		write("unchanged", "interaction", "update", branch, "--subject", "Branch numbers"),
		fail("INVALID_VALUE", "interaction", "update", branch, "--direction", "sideways"),
		write("deleted", "interaction", "delete", branch),
		fail("REFERENCED", "interaction", "delete", pipeline),
		fail("NOT_FOUND", "interaction", "delete", "it_2026-01-01-nothing"),
	}
}

func taskCases() []contractCase {
	return []contractCase{
		read("task", "list"),
		read("task", "list", "--assignee", "us_pam", "--agent", "us_pam-agent"),
		read("task", "list", "--category", "open", "--overdue"),
		read("task", "list", "--due-before", "2026-12-31"),
		read("task", "list", "--project", "pr_paper-stock-audit"),
		read("task", "list", "--deal", msDeal),
		read("task", "list", "--related-to", "pe_jim-halpert"),
		read("task", "list", "--all", "--all-holders"),
		read("task", "get", "{task}"),
		read("task", "get", "{task}", "--full", "--fields", "description"),
		fail("NOT_FOUND", "task", "get", "tk_01jdx7v3k2f9q8w4e6r5t2y1u0"),
		fail("NOT_FOUND", "--actor", "us_dwight", "task", "get", "{task}"),
		write("created", "task", "create", "--title", "Order toner", "--assignee", "us_pam", "--agent", "us_pam-agent",
			"--due", "2026-12-02", "--purpose", "follow-up", "--related", "pe_jim-halpert", "--related", msDeal, "--description-file", "-").in("Ask Dwight for the budget first.\n"),
		write("created", "task", "create", "--title", "Hang the audit chart", "--assignee", "us_pam", "--project", "pr_paper-stock-audit"),
		write("created", "task", "create", "--title", "Send the buyout offer", "--assignee", "us_pam", "--deal", "dl_dunder-mifflin-corporate"),
		write("created", "task", "create", "--title", "Thank Jim for the pipeline review", "--assignee", "us_pam", "--interaction", pipeline, "--purpose", "ping"),
		write("existing", "task", "create", "--title", "Send Dwight the beet account terms", "--assignee", "us_pam",
			"--agent", "us_pam-agent", "--interaction", pipeline, "--due", "2026-11-20"),
		fail("INVALID_VALUE", "task", "create", "--title", "Both parents", "--assignee", "us_pam", "--project", "pr_paper-stock-audit", "--deal", msDeal),
		fail("INVALID_VALUE", "task", "create", "--title", "In a closed project", "--assignee", "us_pam", "--project", "pr_warehouse-move"),
		fail("HOLDER_MISMATCH", "task", "create", "--title", "Beets", "--assignee", "us_pam", "--related", "dl_dwight-schrute"),
		fail("NOT_FOUND", "task", "create", "--title", "Ghost", "--assignee", "us_pam", "--project", "pr_nowhere"),
		write("updated", "task", "status", "{task}", "in-progress"),
		write("unchanged", "task", "status", "{task}", "todo"),
		fail("INVALID_VALUE", "task", "status", "{task}", "pending"),
		write("updated", "task", "update", "{task}", "--title", "Send Dwight the beet terms", "--agent", "us_pam-agent", "--due", "2026-12-05",
			"--description-file", "-").in("Terms are in the shared drive.\n"),
		write("updated", "task", "update", "{task}", "--assignee", "us_michael", "--agent", ""),
		write("updated", "task", "update", "{task}", "--due", ""),
		write("updated", "task", "update", "{task}", "--related", "pe_dwight-schrute"),
		write("updated", "task", "update", "{task}", "--related", "pe_dwight-schrute", "--remove-related", "pe_dwight-schrute").after([]string{"task", "update", "{task}", "--related", "pe_dwight-schrute"}),
		write("unchanged", "task", "update", "{task}", "--title", "Send Dwight the beet account terms"),
		fail("INVALID_VALUE", "task", "update", "{task}", "--status", "done"),
		write("deleted", "task", "delete", "{task3}"),
		fail("NOT_FOUND", "--actor", "us_dwight", "task", "delete", "{task}"),
	}
}

func dealCases() []contractCase {
	return []contractCase{
		read("deal", "list"),
		read("deal", "list", "--party", "org_michael-scott-paper-company", "--stage", "engaged", "--category", "ongoing"),
		read("deal", "list", "--all", "--all-holders"),
		read("deal", "get", msDeal),
		read("deal", "get", msDeal, "--full", "--fields", "parties"),
		fail("NOT_FOUND", "deal", "get", "dl_nowhere"),
		fail("NOT_FOUND", "deal", "get", "dl_dwight-schrute"),
		write("created", "deal", "create", "--party", "pe_jim-halpert", "--role", "client", "--role", "buyer", "--holder-role", "advisor",
			"--name", "Jim's cold calls", "--stage", "engaged", "--description-file", "-").in("Jim wants a bigger book of business.\n"),
		write("created", "deal", "create", "--party", "org_vance-refrigeration", "--holder-role", "advisor"),
		fail("INVALID_VALUE", "deal", "create", "--party", "pe_jim-halpert", "--party", "pe_dwight-schrute", "--holder-role", "advisor"),
		fail("INVALID_VALUE", "deal", "create", "--party", "pe_jim-halpert", "--holder-role", "advisor", "--stage", "lost"),
		fail("INVALID_VALUE", "deal", "create", "--party", "pe_jim-halpert"),
		write("updated", "deal", "update", "dl_dunder-mifflin-corporate", "--name", "Corporate buyout", "--holder-role", "advisor", "--description-file", "-").in("Corporate wants the whole branch.\n"),
		write("unchanged", "deal", "update", "dl_dunder-mifflin-corporate", "--holder-role", "advisor"),
		fail("UNKNOWN_FIELD", "deal", "update", msDeal, "--stage", "closing"),
		write("updated", "deal", "stage", msDeal, "closing"),
		write("unchanged", "deal", "stage", msDeal, "engaged"),
		write("updated", "deal", "stage", msDeal, "lost", "--reason", "price"),
		fail("INVALID_VALUE", "deal", "stage", msDeal, "lost"),
		fail("INVALID_VALUE", "deal", "stage", msDeal, "closing", "--reason", "price"),
		fail("INVALID_VALUE", "deal", "stage", msDeal, "daydreaming"),
		write("updated", "deal", "party", "add", msDeal, "pe_jim-halpert", "--role", "client"),
		write("unchanged", "deal", "party", "add", msDeal, "pe_michael-scott", "--role", "client"),
		fail("INVALID_VALUE", "deal", "party", "add", msDeal, "org_dunder-mifflin-scranton"),
		write("updated", "deal", "party", "remove", msDeal, "pe_michael-scott", "--role", "client"),
		write("updated", "deal", "party", "remove", msDeal, "org_dunder-mifflin-corporate"),
		fail("NOT_FOUND", "deal", "party", "remove", msDeal, "pe_jim-halpert"),
		write("deleted", "deal", "delete", "dl_dunder-mifflin-corporate"),
		fail("REFERENCED", "deal", "delete", msDeal),
	}
}

func projectCases() []contractCase {
	return []contractCase{
		read("project", "list"),
		read("project", "list", "--deal", msDeal),
		read("project", "list", "--all", "--all-holders"),
		read("project", "get", "pr_paper-stock-audit"),
		read("project", "get", "pr_paper-stock-audit", "--full", "--fields", "related"),
		fail("NOT_FOUND", "project", "get", "pr_nowhere"),
		write("created", "project", "create", "--name", "Fire drill", "--deal", msDeal, "--related", "pe_dwight-schrute", "--description-file", "-").in("Dwight runs it.\n"),
		write("created", "project", "create", "--name", "Desk move").warns("SIMILAR_EXISTS"),
		fail("HOLDER_MISMATCH", "project", "create", "--name", "Beets", "--related", "dl_dwight-schrute"),
		write("updated", "project", "update", "pr_desk-move", "--name", "Desk shuffle", "--deal", msDeal, "--related", "pe_jim-halpert", "--description-file", "-").in("Everyone swaps.\n"),
		write("updated", "project", "update", "pr_desk-move", "--related", "pe_jim-halpert", "--remove-related", "pe_jim-halpert").after([]string{"project", "update", "pr_desk-move", "--related", "pe_jim-halpert"}),
		write("unchanged", "project", "update", "pr_desk-move", "--name", "Desk move"),
		fail("REFERENCED", "project", "close", "pr_paper-stock-audit"),
		write("updated", "project", "close", "pr_paper-stock-audit", "--cancel-tasks"),
		write("updated", "project", "close", "pr_desk-move"),
		write("unchanged", "project", "close", "pr_warehouse-move"),
		write("updated", "project", "reopen", "pr_warehouse-move"),
		write("unchanged", "project", "reopen", "pr_desk-move"),
		write("deleted", "project", "delete", "pr_break-room-refit"),
		fail("REFERENCED", "project", "delete", "pr_paper-stock-audit"),
	}
}

func noteCases() []contractCase {
	return []contractCase{
		read("note", "list"),
		read("note", "list", "--related-to", "pe_bob-vance"),
		read("note", "list", "--all", "--all-holders"),
		read("note", "get", "{note}"),
		read("note", "get", "{note}", "--full", "--fields", "text"),
		fail("NOT_FOUND", "note", "get", "nt_01jdx7v3k2f9q8w4e6r5t2y1u0"),
		write("created", "note", "create", "--subject", "pe_jim-halpert", "--subject", "org_vance-refrigeration", "--text-file", "-").in("Jim and Bob should meet.\n"),
		fail("INVALID_VALUE", "note", "create", "--text-file", "-").in("A note about nobody.\n"),
		fail("HOLDER_MISMATCH", "note", "create", "--subject", "dl_dwight-schrute", "--text-file", "-").in("Beets.\n"),
		write("updated", "note", "update", "{note}", "--text-file", "-", "--subject", "pe_jim-halpert").in("Prince Family Paper is a rival.\n"),
		write("updated", "note", "update", "{note}", "--remove-subject", "pe_bob-vance"),
		fail("INVALID_VALUE", "note", "update", "{note}", "--remove-subject", "pe_bob-vance", "--remove-subject", "org_prince-family-paper"),
		write("deleted", "note", "delete", "{note}"),
		fail("NOT_FOUND", "note", "delete", "nt_01jdx7v3k2f9q8w4e6r5t2y1u0"),
	}
}

// listCases walks every list command through its paging flags: --limit,
// --limit 0, a cursor that continues, and a corrupt one.
func listCases() []contractCase {
	var cs []contractCase
	for _, list := range [][]string{{"user"}, {"organization"}, {"person"}, {"interaction"}, {"task"}, {"deal"}, {"project"}, {"note"}} {
		page := append(append([]string{}, list...), "list", "--limit", "1")
		cs = append(cs,
			read(page...),
			read(append(append([]string{}, list...), "list", "--limit", "0")...),
			read(append(page, "--cursor", "{cursor}")...).after(page),
			fail("INVALID_VALUE", append(append([]string{}, list...), "list", "--cursor", "garbage")...),
			fail("INVALID_VALUE", append(append([]string{}, list...), "list", "--limit", "-1")...),
			fail("INVALID_VALUE", append(append([]string{}, list...), "list", "--limit", "0", "--cursor", "{cursor}")...).after(page),
		)
	}
	return cs
}

// widenedCases gives every command its --fields, --full and --all-holders
// flags: for each command and each of them it takes the first successful case
// of the command that does not use the flag and adds it.
func widenedCases(cs []contractCase) []contractCase {
	tree := commandTree()
	done := map[string]bool{}
	var out []contractCase
	for _, c := range cs {
		path, ok := commandOf(tree, c.Args)
		if !ok || c.Code != "" || len(c.Findings) > 0 || c.State != baseDunder {
			continue
		}
		used := flagsOf(c.Args)
		for _, f := range []string{"--fields", "--full", "--all-holders"} {
			key := path + " " + f
			if done[key] || slices.Contains(used, f) || !slices.Contains(tree[path], f) {
				continue
			}
			done[key] = true
			v := c
			v.Args = append(slices.Clone(c.Args), f)
			if f == "--fields" {
				v.Args = append(v.Args, "live")
			}
			out = append(out, v)
		}
	}
	return out
}
