package domain

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/diegomarino/kravis/internal/storage"
)

// DefaultPipeline is the MVP's single pipeline; every deal stores it.
const DefaultPipeline = "default"

// Stage categories: ongoing deals are live, won and lost ones are kept.
const (
	stageOngoing = "ongoing"
	stageLost    = "lost"
)

var stageCategories = []string{stageOngoing, "won", stageLost}

// Deal is a transaction that a holder follows through the stages of a
// pipeline. Its description lives in the sidecar.
type Deal struct {
	storage.Base
	Holder   string `json:"holder"`
	Name     string `json:"name,omitempty"`
	Pipeline string `json:"pipeline"`
	Stage    string `json:"stage"`
	// Category and Live follow the stage in the pipeline.
	Category   string `json:"category"`
	HolderRole string `json:"holder_role"`
	// LostReason is set only in the lost category.
	LostReason string  `json:"lost_reason,omitempty"`
	Parties    []Party `json:"parties,omitempty"`
	Live       bool    `json:"live"`
}

// Party is a person or organization of a deal, with its party roles.
type Party struct {
	Party string   `json:"party"`
	Roles []string `json:"roles,omitempty"`
}

var deals = newKind(kind[Deal, *Deal]{
	Type: Type{Name: "deal", Dir: "deals", Prefix: "dl_",
		Order: "name compared as its slug, else the ID; then the ID"},
	compact: []compactField{{"id", ""}, {"name", ""}, {"live", "false"}, {"holder", ""},
		{"stage", ""}, {"category", ""}, {"holder_role", ""}, {"lost_reason", ""}},
	sortKey:     func(d *Deal) string { return firstOf(d.Name, d.ID) },
	holder:      func(d *Deal) string { return d.Holder },
	live:        func(d *Deal) bool { return d.Live },
	extraFields: []string{"description"},
	rules:       dealRules,
})

// dealRules reports what is wrong inside a deal record, whatever else
// exists (DEALS-INV-01, -03..05, WORKSPACE-INV-06).
func dealRules(d *Deal) []string {
	var out []string
	if !hasID("org_", d.Holder) {
		out = append(out, "the holder "+quote(d.Holder)+" is not an organization ID (org_<slug>)")
	}
	if !slugID.MatchString(d.Pipeline) {
		out = append(out, "the pipeline "+quote(d.Pipeline)+" is not a catalog value (a slug)")
	}
	if !slugID.MatchString(d.Stage) {
		out = append(out, "the stage "+quote(d.Stage)+" is not a catalog value (a slug)")
	}
	switch {
	case !slices.Contains(stageCategories, d.Category):
		out = append(out, "the category "+quote(d.Category)+" is not one of "+strings.Join(stageCategories, ", "))
	case d.Live != (d.Category == stageOngoing):
		out = append(out, fmt.Sprintf("live is %t, but a deal in category %s is live=%t", d.Live, d.Category, d.Category == stageOngoing))
	}
	if !slugID.MatchString(d.HolderRole) {
		out = append(out, "the holder role "+quote(d.HolderRole)+" is not a catalog value (a slug)")
	}
	switch {
	case d.Category == stageLost && d.LostReason == "":
		out = append(out, "a deal in the lost category needs a lost reason")
	case d.Category != stageLost && d.LostReason != "":
		out = append(out, "the lost reason "+quote(d.LostReason)+" is set, but the deal is not in the lost category")
	case d.LostReason != "" && !slugID.MatchString(d.LostReason):
		out = append(out, "the lost reason "+quote(d.LostReason)+" is not a catalog value (a slug)")
	}
	for n, p := range d.Parties {
		switch {
		case !hasID("pe_", p.Party) && !hasID("org_", p.Party):
			out = append(out, "the party "+quote(p.Party)+" is neither a person (pe_<slug>) nor an organization (org_<slug>)")
		case p.Party == d.Holder:
			out = append(out, "the holder "+d.Holder+" is one of the deal's parties")
		case slices.ContainsFunc(d.Parties[:n], func(q Party) bool { return q.Party == p.Party }):
			out = append(out, "the party "+p.Party+" is repeated")
		}
		for i, r := range p.Roles {
			if !slugID.MatchString(r) || slices.Contains(p.Roles[:i], r) {
				out = append(out, "the party role "+quote(r)+" of "+p.Party+" is repeated or not a catalog value (a slug)")
			}
		}
	}
	return out
}

// stages are the stages of a holder's pipeline.
func (ws *Workspace) stages(holder, pipeline string) []Status {
	return ws.ConfigFor(holder).Catalogs.Pipelines[pipeline].Stages
}

func stageNames(stages []Status) []string {
	out := make([]string, len(stages))
	for i, s := range stages {
		out[i] = s.Name
	}
	return out
}

// checkStage refuses a stage that is not in the holder's pipeline and
// returns its category (WORKSPACE-AC-13). stale says the stage was not just
// given: it is the one the deal already has.
func (ws *Workspace) checkStage(holder, pipeline, stage string, stale bool) (string, error) {
	stages := ws.stages(holder, pipeline)
	if i := slices.IndexFunc(stages, func(s Status) bool { return s.Name == stage }); i >= 0 {
		return stages[i].Category, nil
	}
	e := invalidValue("stage", "use one of the allowed stages", "the stage %q is not in the pipeline %s", stage, pipeline).
		with("allowed", stageNames(stages)).with("value", stage)
	if stale {
		e.Message = fmt.Sprintf("the stage %q is no longer in the pipeline %s, so this deal must get another one first", stage, pipeline)
		e.Hint = "run kravis deal stage <deal> <stage> with one of the allowed stages"
	}
	return "", e
}

// checkCatalog refuses a value that is not in a catalog of the holder
// (WORKSPACE-AC-13). hint, when set, replaces the default one: the value
// was already on the record and must be replaced.
func checkCatalog(field, what, value string, allowed []string, hint string) error {
	if slices.Contains(allowed, value) {
		return nil
	}
	e := invalidValue(field, "use one of the allowed values", "the %s %q is not in the %s catalog", what, value, what).
		with("allowed", allowed).with("value", value)
	if hint != "" {
		e.Message = fmt.Sprintf("the %s %q is no longer in the %s catalog, so this deal must replace it first", what, value, what)
		e.Hint = hint
	}
	return e
}

func (ws *Workspace) checkRoles(holder string, roles []string) ([]string, error) {
	var out []string
	for _, r := range roles {
		r = strings.TrimSpace(r)
		if err := checkCatalog("role", "party role", r, ws.ConfigFor(holder).Catalogs.PartyRoles, ""); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return sortedIDs(out), nil
}

// partyOf resolves a party given as a person ID or email, or an
// organization ID, to an existing record that is not the holder
// (DEALS-INV-05).
func (ws *Workspace) partyOf(holder, ref string) (string, error) {
	id, err := ws.participantID("party", ref)
	if err != nil {
		return "", err
	}
	if id == holder {
		return "", invalidValue("party", "the holder takes part through --holder-role; give the other side's people or organizations",
			"%s is the deal's holder, and the holder is never one of its parties", id).with("value", id)
	}
	if strings.HasPrefix(id, "org_") {
		_, err = loadLinked(ws, "party", organizations, id)
	} else {
		_, err = loadLinked(ws, "party", people, id)
	}
	return id, err
}

// DealInput is what deal create takes.
type DealInput struct {
	Party       string
	Roles       []string
	HolderRole  string
	Name        string
	Stage       string // "" is the first stage of the pipeline
	Description string
}

// CreateDeal creates a deal of the actor's holder with its first party.
// A deal has no natural key: each create makes a new one, dl_<party>,
// then dl_<party>-2… without a warning.
func (ws *Workspace) CreateDeal(actorID string, in DealInput, opt ViewOptions) (*Result, error) {
	if err := deals.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	holder := actor.Holder
	cat := ws.ConfigFor(holder).Catalogs
	party, err := ws.partyOf(holder, in.Party)
	if err != nil {
		return nil, err
	}
	roles, err := ws.checkRoles(holder, in.Roles)
	if err != nil {
		return nil, err
	}
	holderRole := strings.TrimSpace(in.HolderRole)
	if holderRole == "" {
		return nil, invalidValue("holder_role", "give --holder-role <party role>: the part your holder plays in the deal",
			"a deal needs its holder's party role").with("allowed", cat.PartyRoles)
	}
	if err := checkCatalog("holder_role", "party role", holderRole, cat.PartyRoles, ""); err != nil {
		return nil, err
	}
	rec := &Deal{Holder: holder, Name: strings.TrimSpace(in.Name), Pipeline: DefaultPipeline, HolderRole: holderRole,
		Parties: []Party{{Party: party, Roles: roles}}}
	rec.Stage = strings.TrimSpace(in.Stage)
	if rec.Stage == "" {
		if stages := ws.stages(holder, DefaultPipeline); len(stages) > 0 {
			rec.Stage = stages[0].Name
		}
	}
	if rec.Category, err = ws.checkStage(holder, rec.Pipeline, rec.Stage, false); err != nil {
		return nil, err
	}
	if rec.Category == stageLost {
		return nil, invalidValue("stage", "create the deal in another stage, then run kravis deal stage <deal> <stage> --reason <lost reason>",
			"a deal cannot be created in the lost stage %q: losing it takes a lost reason", rec.Stage).with("value", rec.Stage)
	}
	rec.Live = rec.Category == stageOngoing
	if in.Description != "" {
		rec.Text = &in.Description
	}
	if _, _, err := deals.create(ws, actorID, rec, strings.TrimPrefix(strings.TrimPrefix(party, "pe_"), "org_")); err != nil {
		return nil, err
	}
	touched := touchedIf("parties", true, "description", in.Description != "")
	return ws.finishTask(opt, in.Description != "")(deals.result(OutcomeCreated, rec, touched, opt))
}

// DealChange is what deal update takes; nil leaves a field as it is. The
// stage and the parties change only through their transitions.
type DealChange struct {
	Name        *string // "" clears it
	HolderRole  *string
	Description *string // "" clears it
}

// freshDeal refuses a deal that holds a value its catalogs no longer have
// and that this update does not replace, and recomputes its category and
// live flag (WORKSPACE-INV-05, -06).
func (ws *Workspace) freshDeal(d *Deal, replacesHolderRole bool) error {
	cat, err := ws.checkStage(d.Holder, d.Pipeline, d.Stage, true)
	if err != nil {
		return err
	}
	d.Category, d.Live = cat, cat == stageOngoing
	c := ws.ConfigFor(d.Holder).Catalogs
	if d.LostReason != "" {
		if err := checkCatalog("reason", "lost reason", d.LostReason, c.LostReasons,
			"run kravis deal stage <deal> <stage> --reason <lost reason> with one of the allowed reasons"); err != nil {
			return err
		}
	}
	if !replacesHolderRole {
		if err := checkCatalog("holder_role", "party role", d.HolderRole, c.PartyRoles,
			"pass --holder-role with one of the allowed party roles"); err != nil {
			return err
		}
	}
	for _, p := range d.Parties {
		for _, r := range p.Roles {
			if err := checkCatalog("role", "party role", r, c.PartyRoles,
				"run kravis deal party remove <deal> "+p.Party+" --role "+r+", then add an allowed role"); err != nil {
				return err
			}
		}
	}
	return nil
}

// UpdateDeal changes the name, the holder's party role or the description
// of a deal of the actor's holder; another holder's is NOT_FOUND. It never
// changes the stage (DEALS-INV-02), and a value that left its catalog must
// be replaced first.
func (ws *Workspace) UpdateDeal(actorID, id string, ch DealChange, opt ViewOptions) (*Result, error) {
	if err := deals.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	if _, _, err := deals.loadIn(ws, actor.Holder, id); err != nil {
		return nil, err
	}
	var holderRole string
	if ch.HolderRole != nil {
		holderRole = strings.TrimSpace(*ch.HolderRole)
		if err := checkCatalog("holder_role", "party role", holderRole, ws.ConfigFor(actor.Holder).Catalogs.PartyRoles, ""); err != nil {
			return nil, err
		}
	}
	touched := touchedIf("name", ch.Name != nil, "holder_role", ch.HolderRole != nil, "description", ch.Description != nil)
	rec, changed, err := deals.updateIn(ws, actor.Holder, actorID, id, func(d *Deal) error {
		if err := ws.freshDeal(d, ch.HolderRole != nil); err != nil {
			return err
		}
		if ch.Name != nil {
			d.Name = strings.TrimSpace(*ch.Name)
		}
		if ch.HolderRole != nil {
			d.HolderRole = holderRole
		}
		if ch.Description != nil {
			cur, err := ws.Store.Text(deals.Path(id))
			if err != nil {
				return err
			}
			if cur != *ch.Description {
				d.Text = ch.Description
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ws.finishTask(opt, ch.Description != nil)(deals.result(outcome(changed), rec, touched, opt))
}

// SetDealStage moves a deal of the actor's holder to any stage of its
// pipeline (DEALS-INV-02). A lost stage needs a lost reason of the catalog,
// and any other stage clears it (DEALS-INV-03). The same stage and reason
// change nothing.
func (ws *Workspace) SetDealStage(actorID, id, stage, reason string, opt ViewOptions) (*Result, error) {
	if err := deals.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	d, _, err := deals.loadIn(ws, actor.Holder, id)
	if err != nil {
		return nil, err
	}
	stage, reason = strings.TrimSpace(stage), strings.TrimSpace(reason)
	cat, err := ws.checkStage(actor.Holder, d.Pipeline, stage, false)
	if err != nil {
		return nil, err
	}
	reasons := ws.ConfigFor(actor.Holder).Catalogs.LostReasons
	switch {
	case cat == stageLost && reason == "":
		return nil, invalidValue("reason", "pass --reason with one of the allowed lost reasons",
			"moving a deal to the lost stage %q needs a lost reason", stage).with("allowed", reasons)
	case cat == stageLost:
		if err := checkCatalog("reason", "lost reason", reason, reasons, ""); err != nil {
			return nil, err
		}
	case reason != "":
		return nil, invalidValue("reason", "drop --reason: only a stage of the lost category takes one",
			"the stage %q is %s, and only a lost stage has a lost reason", stage, cat).with("value", reason)
	}
	rec, changed, err := deals.updateIn(ws, actor.Holder, actorID, id, func(d *Deal) error {
		d.Stage, d.Category, d.Live, d.LostReason = stage, cat, cat == stageOngoing, reason
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ws.finishTask(opt, false)(deals.result(outcome(changed), rec, []string{"stage", "category", "lost_reason"}, opt))
}

// AddDealParty adds a party to a deal of the actor's holder, or the roles
// it lacks to a party already there (DEALS-INV-05).
func (ws *Workspace) AddDealParty(actorID, id, ref string, roles []string, opt ViewOptions) (*Result, error) {
	if err := deals.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	if _, _, err := deals.loadIn(ws, actor.Holder, id); err != nil {
		return nil, err
	}
	party, err := ws.partyOf(actor.Holder, ref)
	if err != nil {
		return nil, err
	}
	if roles, err = ws.checkRoles(actor.Holder, roles); err != nil {
		return nil, err
	}
	rec, changed, err := deals.updateIn(ws, actor.Holder, actorID, id, func(d *Deal) error {
		i := slices.IndexFunc(d.Parties, func(p Party) bool { return p.Party == party })
		if i < 0 {
			d.Parties = append(d.Parties, Party{Party: party})
			slices.SortFunc(d.Parties, func(a, b Party) int { return strings.Compare(a.Party, b.Party) })
			i = slices.IndexFunc(d.Parties, func(p Party) bool { return p.Party == party })
		}
		d.Parties[i].Roles = sortedIDs(slices.Concat(d.Parties[i].Roles, roles))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deals.result(outcome(changed), rec, []string{"parties"}, opt)
}

// RemoveDealParty removes a party from a deal of the actor's holder, or
// only one of its roles. A party or role that is not there is NOT_FOUND.
func (ws *Workspace) RemoveDealParty(actorID, id, ref, role string, opt ViewOptions) (*Result, error) {
	if err := deals.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	if _, _, err := deals.loadIn(ws, actor.Holder, id); err != nil {
		return nil, err
	}
	party, err := ws.participantID("party", ref)
	if err != nil {
		return nil, err
	}
	role = strings.TrimSpace(role)
	rec, changed, err := deals.updateIn(ws, actor.Holder, actorID, id, func(d *Deal) error {
		i := slices.IndexFunc(d.Parties, func(p Party) bool { return p.Party == party })
		switch {
		case i < 0:
			return newError(CodeNotFound, "kravis deal get <deal> --fields parties shows the parties",
				"%s is not a party of %s", party, id).with("field", "party").with("id", party)
		case role == "":
			d.Parties = slices.Delete(d.Parties, i, i+1)
		case !slices.Contains(d.Parties[i].Roles, role):
			return newError(CodeNotFound, "kravis deal get <deal> --fields parties shows the parties and their roles",
				"%s has no party role %q in %s", party, role, id).with("field", "role").with("value", role)
		default:
			d.Parties[i].Roles = slices.DeleteFunc(d.Parties[i].Roles, func(r string) bool { return r == role })
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deals.result(outcome(changed), rec, []string{"parties"}, opt)
}

// DeleteDeal deletes a deal of the actor's holder that no record refers
// to; another holder's is NOT_FOUND.
func (ws *Workspace) DeleteDeal(actorID, id string, opt ViewOptions) (*Result, error) {
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	var text string
	if wantsText(opt, "description") {
		if _, _, err := deals.loadIn(ws, actor.Holder, id); err != nil {
			return nil, err
		}
		if text, err = ws.Store.Text(deals.Path(id)); err != nil {
			return nil, err
		}
	}
	res, err := deals.removeIn(ws, actor.Holder, id, opt)
	if err != nil {
		return nil, err
	}
	res.Data = withField(res.Data, "description", text)
	return res, nil
}

// DealFilter is what deal list narrows by.
type DealFilter struct {
	// Holder limits the list to one holder's deals; "" is no limit.
	Holder   string
	Party    string
	Stage    string
	Category string
}

// ListDeals returns a page of deals by name. Live deals only, unless All,
// a stage or a category is given.
func (ws *Workspace) ListDeals(f DealFilter, opt ListOptions) (*ListResult, error) {
	if f.Category != "" {
		if !slices.Contains(stageCategories, f.Category) {
			return nil, InvalidFlag("category", stageCategories, "%q is not a deal stage category", f.Category)
		}
		opt.All = true
	}
	if f.Stage != "" {
		if _, err := ws.checkStage(f.Holder, DefaultPipeline, f.Stage, false); err != nil {
			return nil, err
		}
		opt.All = true
	}
	if f.Party != "" {
		id, err := ws.participantID("party", f.Party)
		if err != nil {
			return nil, err
		}
		f.Party = id
	}
	keep := func(d *Deal) bool {
		return (f.Party == "" || slices.ContainsFunc(d.Parties, func(p Party) bool { return p.Party == f.Party })) &&
			(f.Stage == "" || d.Stage == f.Stage) &&
			(f.Category == "" || d.Category == f.Category)
	}
	print := fmt.Sprintf("holder=%s|party=%s|stage=%s|category=%s", f.Holder, f.Party, f.Stage, f.Category)
	res, err := deals.list(ws, f.Holder, opt, keep, print)
	if err != nil {
		return nil, err
	}
	if wantsText(opt.View, "description") {
		for n, v := range res.Data {
			if res.Data[n], err = ws.addText(v, "description"); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

// GetDeal returns the view of a deal. A read limited to a holder finds only
// that holder's deals.
func (ws *Workspace) GetDeal(holder, id string, opt ViewOptions) (*GetResult, error) {
	v, err := deals.getIn(ws, holder, id, opt)
	if err == nil && wantsText(opt, "description") {
		v, err = ws.addText(v, "description")
	}
	if err != nil {
		return nil, err
	}
	return &GetResult{Data: v, Filters: holderFilter(holder)}, nil
}

// dealsReferringTo finds the deals that have id as their holder or a party.
func dealsReferringTo(ws *Workspace, id string) ([]string, error) {
	return scanDeals(ws, func(d *Deal) bool {
		return d.Holder == id || slices.ContainsFunc(d.Parties, func(p Party) bool { return p.Party == id })
	})
}

// dealsOf finds the deals whose holder is id.
func dealsOf(ws *Workspace, id string) ([]string, error) {
	return scanDeals(ws, func(d *Deal) bool { return d.Holder == id })
}

func scanDeals(ws *Workspace, match func(*Deal) bool) ([]string, error) {
	rows, err := deals.scan(ws)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if match(r.rec) {
			out = append(out, r.id)
		}
	}
	slices.Sort(out)
	return out, nil
}

// auditDealLinks reports what deals refer to: a holder or a party that does
// not exist (BROKEN_REFERENCE), a holder that is not an own organization,
// and a stage, party role or lost reason that the catalog no longer has, or
// a category or live flag that the catalog contradicts.
func auditDealLinks(st *storage.Store, add addFinding) error {
	ds, err := loadForCheck(st, deals)
	if err != nil || len(ds) == 0 {
		return err
	}
	orgs, err := loadForCheck(st, organizations)
	if err != nil {
		return err
	}
	pe, err := loadForCheck(st, people)
	if err != nil {
		return err
	}
	cfg := configForCheck(st)
	for _, id := range slices.Sorted(maps.Keys(ds)) {
		d := ds[id]
		switch o, ok := orgs[d.rec.Holder]; {
		case !hasID("org_", d.rec.Holder):
		case !ok:
			add(FindingBrokenReference, d.path, "the holder %s does not exist", d.rec.Holder)
		case !o.rec.Own:
			add(FindingInvalidRecord, d.path, "the holder %s is not an own organization", d.rec.Holder)
		}
		for _, p := range d.rec.Parties {
			_, isOrg := orgs[p.Party]
			_, isPerson := pe[p.Party]
			if (hasID("org_", p.Party) && !isOrg) || (hasID("pe_", p.Party) && !isPerson) {
				add(FindingBrokenReference, d.path, "the party %s does not exist", p.Party)
			}
		}
		if cfg == nil {
			continue
		}
		c := cfg.forHolder(d.rec.Holder).Catalogs
		notIn := func(what, value string) {
			add(FindingValueNotInCatalog, d.path, "the %s %q is not in the %s catalog for holder %s; the next edit must replace it",
				what, value, what, d.rec.Holder)
		}
		if slugID.MatchString(d.rec.Stage) {
			stages := c.Pipelines[d.rec.Pipeline].Stages
			i := slices.IndexFunc(stages, func(s Status) bool { return s.Name == d.rec.Stage })
			switch {
			case i < 0:
				add(FindingValueNotInCatalog, d.path, "the stage %q is not in the pipeline %s for holder %s; the deal must get another stage",
					d.rec.Stage, d.rec.Pipeline, d.rec.Holder)
			case stages[i].Category != d.rec.Category:
				add(FindingInvalidRecord, d.path, "the category %q disagrees with the catalog, which puts %q in %q",
					d.rec.Category, d.rec.Stage, stages[i].Category)
			}
		}
		if slugID.MatchString(d.rec.HolderRole) && !slices.Contains(c.PartyRoles, d.rec.HolderRole) {
			notIn("party role", d.rec.HolderRole)
		}
		if slugID.MatchString(d.rec.LostReason) && !slices.Contains(c.LostReasons, d.rec.LostReason) {
			notIn("lost reason", d.rec.LostReason)
		}
		for _, p := range d.rec.Parties {
			for _, r := range p.Roles {
				if slugID.MatchString(r) && !slices.Contains(c.PartyRoles, r) {
					notIn("party role", r)
				}
			}
		}
	}
	return nil
}
