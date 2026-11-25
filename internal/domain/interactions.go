package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/diegomarino/kravis/internal/storage"
)

// KeyInteractionRef is the natural key of an interaction: its holder and one
// canonical identifying source reference.
const KeyInteractionRef = "interaction-ref"

// Warning and finding codes of interactions.
const (
	WarnMultipleAffiliations = "MULTIPLE_AFFILIATIONS"

	FindingDanglingReference = "DANGLING_REFERENCE"
	FindingValueNotInCatalog = "VALUE_NOT_IN_CATALOG"
)

// Directions of an interaction.
const (
	DirectionOutbound = "outbound"
	DirectionInbound  = "inbound"
)

var directions = []string{DirectionOutbound, DirectionInbound}

// Interaction is something that happened with people or organizations
// through a channel. Its summary lives in the sidecar.
type Interaction struct {
	storage.Base
	Holder string `json:"holder"`
	// OccurredAt is an instant in UTC, RFC 3339 to the second.
	OccurredAt string `json:"occurred_at"`
	Channel    string `json:"channel"`
	Direction  string `json:"direction"`
	Subject    string `json:"subject"`
	// Participants are people (pe_…) and organizations (org_…).
	Participants storage.IDs `json:"participants,omitempty"`
	// InitiatedBy is a user of the holder when outbound, one of the
	// participants when inbound.
	InitiatedBy string      `json:"initiated_by,omitempty"`
	References  []SourceRef `json:"references,omitempty"`
	// Deals and Projects are the work the interaction belongs to, all of
	// its holder (INTERACTIONS-INV-05).
	Deals    storage.IDs `json:"deals,omitempty"`
	Projects storage.IDs `json:"projects,omitempty"`
	Live     bool        `json:"live"`
}

const instantLayout = "2006-01-02T15:04:05Z"

// interactionForm is what an interaction ID looks like after it_: the date
// it occurred in the workspace timezone, then the subject as a slug.
var interactionForm = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(-[a-z0-9]+(-[a-z0-9]+)*)?$`)

var interactions = newKind(kind[Interaction, *Interaction]{
	Type: Type{Name: "interaction", Dir: "interactions", Prefix: "it_",
		Order: "occurred_at, newest first; then the ID",
		// The month folder is the month of the date in the ID.
		group: func(id string) string {
			if len(id) < 10 {
				return ""
			}
			return id[3:10]
		}},
	compact: []compactField{{"id", ""}, {"subject", ""}, {"live", "false"}, {"holder", ""},
		{"occurred_at", ""}, {"channel", ""}, {"direction", ""}, {"participants", "[]"}},
	sortKey:     func(i *Interaction) string { return i.OccurredAt },
	holder:      func(i *Interaction) string { return i.Holder },
	desc:        true,
	form:        interactionForm,
	live:        func(i *Interaction) bool { return i.Live },
	keyKinds:    []string{KeyInteractionRef},
	extraFields: []string{"summary"},
	keys:        interactionKeys,
	rules:       interactionRules,
})

// interactionKeys are the record's natural keys: one per identifying
// reference.
func interactionKeys(i *Interaction) []storage.Key {
	var keys []storage.Key
	for _, r := range i.References {
		if identifying(r) {
			keys = append(keys, storage.Key{Kind: KeyInteractionRef, Value: refKey(i.Holder, r)})
		}
	}
	return keys
}

func refKey(holder string, r SourceRef) string { return "interaction|" + holder + "|" + r.String() }

// interactionRules reports what is wrong inside an interaction record,
// whatever else exists (INTERACTIONS-INV-01..03, 06).
func interactionRules(i *Interaction) []string {
	var out []string
	if !hasID("org_", i.Holder) {
		out = append(out, "the holder "+quote(i.Holder)+" is not an organization ID (org_<slug>)")
	}
	if t, err := time.Parse(instantLayout, i.OccurredAt); err != nil || t.Format(instantLayout) != i.OccurredAt {
		out = append(out, "occurred_at "+quote(i.OccurredAt)+" is not a UTC instant such as 2026-11-23T17:02:00Z")
	}
	if !slugID.MatchString(i.Channel) {
		out = append(out, "the channel "+quote(i.Channel)+" is not a catalog value (a slug)")
	}
	if !slices.Contains(directions, i.Direction) {
		out = append(out, "the direction "+quote(i.Direction)+" is not outbound or inbound")
	}
	if strings.TrimSpace(i.Subject) == "" {
		out = append(out, "an interaction needs a subject")
	}
	if len(i.Participants) == 0 {
		out = append(out, "an interaction needs at least one participant")
	}
	for n, p := range i.Participants {
		if !hasID("pe_", p) && !hasID("org_", p) {
			out = append(out, "the participant "+quote(p)+" is neither a person (pe_<slug>) nor an organization (org_<slug>)")
		}
		if slices.Contains(i.Participants[:n], p) {
			out = append(out, "the participant "+p+" is repeated")
		}
	}
	switch {
	case i.InitiatedBy == "":
	case i.Direction == DirectionOutbound && !hasID("us_", i.InitiatedBy):
		out = append(out, "an outbound interaction is initiated by a user (us_<key>), not "+quote(i.InitiatedBy))
	case i.Direction == DirectionInbound && !slices.Contains(i.Participants, i.InitiatedBy):
		out = append(out, "an inbound interaction is initiated by one of its participants, and "+quote(i.InitiatedBy)+" is not")
	}
	for n, d := range i.Deals {
		if !hasID("dl_", d) || slices.Contains(i.Deals[:n], d) {
			out = append(out, "the deal "+quote(d)+" is repeated or not a deal ID (dl_<slug>)")
		}
	}
	for n, p := range i.Projects {
		if !hasID("pr_", p) || slices.Contains(i.Projects[:n], p) {
			out = append(out, "the project "+quote(p)+" is repeated or not a project ID (pr_<slug>)")
		}
	}
	return append(out, refRules(i.References)...)
}

func trimAll(in []string) []string {
	var out []string
	for _, s := range in {
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

// checkWork checks the deals and projects an interaction is linked to: they
// exist and belong to its holder (INTERACTIONS-INV-05).
func (ws *Workspace) checkWork(holder string, dealRefs, projectRefs []string) (ds, ps []string, err error) {
	for _, d := range trimAll(dealRefs) {
		if _, err := ws.holderDeal(holder, "deal", d); err != nil {
			return nil, nil, err
		}
		ds = append(ds, d)
	}
	for _, p := range trimAll(projectRefs) {
		if _, err := ws.holderProject(holder, "project", p); err != nil {
			return nil, nil, err
		}
		ps = append(ps, p)
	}
	return sortedIDs(ds), sortedIDs(ps), nil
}

// checkChannel refuses a channel that is not in the catalog of the
// holder's configuration. stale says the channel was not just given: it is
// the one the interaction already had.
func (ws *Workspace) checkChannel(holder, channel string, stale bool) error {
	allowed := ws.ConfigFor(holder).Catalogs.Channels
	if slices.Contains(allowed, channel) {
		return nil
	}
	e := invalidValue("channel", "use one of the allowed channels", "the channel %q is not in the channel catalog", channel).
		with("allowed", allowed).with("value", channel)
	if stale {
		e.Message = fmt.Sprintf("the channel %q is no longer in the channel catalog, so this update must replace it", channel)
		e.Hint = "pass --channel with one of the allowed channels"
	}
	return e
}

// parseInstant reads an RFC 3339 instant, in any offset, to a UTC one.
func parseInstant(field, s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return t, invalidValue(field, "use RFC 3339, such as 2026-11-23T17:02:00Z or 2026-11-23T18:02:00+01:00",
			"%q is not an RFC 3339 date and time", s).with("value", s)
	}
	return t.UTC().Truncate(time.Second), nil
}

// location is the workspace timezone, which civil dates are computed in.
func (ws *Workspace) location() (*time.Location, error) {
	return time.LoadLocation(ws.Config.Timezone)
}

// interactionBase is the ID after it_: the date plus the subject as ASCII,
// cut at about 40 characters.
func interactionBase(date, subject string) string {
	s := Slug(subject)
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	if s == "" {
		return date
	}
	return date + "-" + s
}

// participantID turns a person ID, a person's email or an organization ID
// into the ID, without looking at the record.
func (ws *Workspace) participantID(field, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	switch {
	case strings.HasPrefix(ref, "org_"):
		return ref, nil
	case strings.HasPrefix(ref, "pe_") || strings.Contains(ref, "@"):
		id, err := ws.ResolvePerson(ref)
		if err != nil {
			return "", notFoundIn(field, err)
		}
		return id, nil
	}
	return "", invalidValue(field, "give a person ID (pe_…) or email, or an organization ID (org_…)",
		"%q is neither a person nor an organization", ref).with("value", ref)
}

// participants resolves the participants given: each must exist. A person
// with exactly one affiliation adds that organization; with several, nothing
// is added and MULTIPLE_AFFILIATIONS lists them. given holds the IDs asked
// for; all adds the organizations that came with them.
func (ws *Workspace) participants(refs []string) (given, all []string, warnings []Warning, err error) {
	for _, ref := range refs {
		id, err := ws.participantID("participant", ref)
		if err != nil {
			return nil, nil, nil, err
		}
		given = append(given, id)
		all = append(all, id)
		if strings.HasPrefix(id, "org_") {
			if _, err := loadLinked(ws, "participant", organizations, id); err != nil {
				return nil, nil, nil, err
			}
			continue
		}
		p, err := loadLinked(ws, "participant", people, id)
		if err != nil {
			return nil, nil, nil, err
		}
		orgs := make([]string, len(p.Affiliations))
		for n, a := range p.Affiliations {
			orgs[n] = a.Organization
		}
		switch {
		case len(orgs) == 1:
			all = append(all, orgs[0])
		case len(orgs) > 1:
			warnings = append(warnings, Warning{Code: WarnMultipleAffiliations,
				Message: fmt.Sprintf("%s has %d affiliations, so no organization was added as a participant; add the right one with --participant", id, len(orgs)),
				Details: map[string]any{"person": id, "organizations": orgs}})
		}
	}
	return sortedIDs(given), sortedIDs(all), warnings, nil
}

func sortedIDs(ids []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(ids)))
}

// initiator checks the initiator given against the direction: a user of the
// holder when outbound, one of the participants when inbound
// (INTERACTIONS-INV-03, WORKSPACE-INV-02). It returns the ID.
func (ws *Workspace) initiator(holder, direction, ref string, participants []string) (string, error) {
	ref = strings.TrimSpace(ref)
	if direction == DirectionOutbound {
		if !strings.HasPrefix(ref, "us_") {
			return "", invalidValue("initiated-by", "give the ID of a user of the holder, such as us_<key>",
				"an outbound interaction is initiated by a user of its holder, not by %q", ref).with("value", ref)
		}
		u, err := loadLinked(ws, "initiated-by", users, ref)
		if err != nil {
			return "", err
		}
		if u.Holder != holder {
			return "", newError(CodeHolderMismatch, "choose a user of the interaction's holder",
				"%s works for %s, not for %s", ref, u.Holder, holder).with("field", "initiated-by")
		}
		return ref, nil
	}
	if strings.HasPrefix(ref, "us_") {
		return "", invalidValue("initiated-by", "give one of the participants",
			"an inbound interaction is initiated by one of its participants, not by the user %s", ref).with("value", ref)
	}
	id, err := ws.participantID("initiated-by", ref)
	if err != nil {
		return "", err
	}
	if !slices.Contains(participants, id) {
		return "", invalidValue("initiated-by", "add it with --participant, or choose one of the participants",
			"an inbound interaction is initiated by one of its participants, and %s is not", id).with("value", id)
	}
	return id, nil
}

// InteractionInput is what interaction create takes.
type InteractionInput struct {
	Channel      string   `json:"channel" jsonschema_extras:"x-cli=--channel"`
	OccurredAt   string   `json:"occurred_at" jsonschema:"format=date-time,description=RFC 3339" jsonschema_extras:"x-cli=--occurred-at"`
	Subject      string   `json:"subject" jsonschema:"pattern=\\S" jsonschema_extras:"x-cli=--subject"`
	Participants []string `json:"participants" jsonschema:"minItems=1,description=person IDs or emails: organization IDs" jsonschema_extras:"x-cli=--participant (repeatable)"`
	Refs         []string `json:"refs,omitempty" jsonschema:"description=<source>:<kind>:<id>[@<scope>]: see the skill" jsonschema_extras:"x-cli=--ref (repeatable)"`
	Direction    string   `json:"direction,omitempty" jsonschema_extras:"x-cli=--direction"` // "" is outbound
	InitiatedBy  string   `json:"initiated_by,omitempty" jsonschema:"description=a user of the holder if outbound: a participant if inbound" jsonschema_extras:"x-cli=--initiated-by"`
	Summary      string   `json:"summary,omitempty" jsonschema_extras:"x-cli=--summary-file"`
	Deals        []string `json:"deals,omitempty" jsonschema:"description=deals of your holder it belongs to" jsonschema_extras:"x-cli=--deal (repeatable)"`
	Projects     []string `json:"projects,omitempty" jsonschema:"description=projects of your holder it belongs to" jsonschema_extras:"x-cli=--project (repeatable)"`
}

// CreateInteraction records an interaction under the actor's holder, or
// returns the one that already has the same identifying reference
// (INTERACTIONS-INV-07). Data that contradicts it is DEDUP_CONFLICT, and so
// are identifying references that belong to two interactions.
func (ws *Workspace) CreateInteraction(actorID string, in InteractionInput, opt ViewOptions) (*Result, error) {
	if err := interactions.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	holder := actor.Holder
	channel := strings.TrimSpace(in.Channel)
	if err := ws.checkChannel(holder, channel, false); err != nil {
		return nil, err
	}
	direction := firstOf(strings.TrimSpace(in.Direction), DirectionOutbound)
	if !slices.Contains(directions, direction) {
		return nil, InvalidFlag("direction", directions, "%q is not a direction", direction)
	}
	at, err := parseInstant("occurred-at", in.OccurredAt)
	if err != nil {
		return nil, err
	}
	subject := strings.TrimSpace(in.Subject)
	if subject == "" {
		return nil, invalidValue("subject", "give --subject", "an interaction needs a subject")
	}
	if len(in.Participants) == 0 {
		return nil, invalidValue("participant", "give at least one --participant: a person ID or email, or an organization ID",
			"an interaction needs at least one participant")
	}
	given, all, warnings, err := ws.participants(in.Participants)
	if err != nil {
		return nil, err
	}
	rec := &Interaction{Holder: holder, OccurredAt: at.Format(instantLayout), Channel: channel,
		Direction: direction, Subject: subject, Participants: all, Live: true}
	if in.InitiatedBy != "" {
		if rec.InitiatedBy, err = ws.initiator(holder, direction, in.InitiatedBy, all); err != nil {
			return nil, err
		}
	}
	refs, refWarnings, err := parseRefs(in.Refs)
	if err != nil {
		return nil, err
	}
	rec.References = refs
	warnings = append(warnings, refWarnings...)
	if rec.Deals, rec.Projects, err = ws.checkWork(holder, in.Deals, in.Projects); err != nil {
		return nil, err
	}
	if in.Summary != "" {
		rec.Text = &in.Summary
	}
	if err := ws.splitRefs(rec); err != nil {
		return nil, err
	}

	loc, err := ws.location()
	if err != nil {
		return nil, err
	}
	touched := touchedIf("occurred_at", true, "channel", true, "direction", true, "subject", true, "participants", true,
		"initiated_by", rec.InitiatedBy != "", "references", len(refs) > 0, "summary", in.Summary != "",
		"deals", len(rec.Deals) > 0, "projects", len(rec.Projects) > 0)
	ex, similar, err := interactions.create(ws, actorID, rec, interactionBase(at.In(loc).Format("2006-01-02"), subject))
	if errors.Is(err, storage.ErrKeyConflict) {
		return nil, ws.refConflict(rec, "", err)
	}
	if err != nil {
		return nil, err
	}
	if ex != nil {
		if err := sameInteraction(ex, rec, given); err != nil {
			return nil, err
		}
		return ws.finish(opt, false)(interactions.result(OutcomeExisting, ex, nil, opt, warnings...))
	}
	// The existing record may be another holder's: its ID is not ours to show.
	if similar != "" {
		if other, _, err := interactions.load(ws, similar); err == nil && other.Holder == holder {
			warnings = append(warnings, similarExists("interaction", rec.ID, similar))
		}
	}
	return ws.finish(opt, in.Summary != "")(interactions.result(OutcomeCreated, rec, touched, opt, warnings...))
}

// splitRefs is DEDUP_CONFLICT when the identifying references of rec
// already belong to two interactions of its holder.
func (ws *Workspace) splitRefs(rec *Interaction) error {
	owners := map[string]string{} // owner -> the reference that points to it
	for _, k := range interactionKeys(rec) {
		switch owner, err := ws.Store.KeyOwner(k); {
		case errors.Is(err, storage.ErrNotFound):
		case err != nil:
			return err
		default:
			owners[owner] = k.Value
		}
	}
	if len(owners) < 2 {
		return nil
	}
	ids := slices.Sorted(maps.Keys(owners))
	return newError(CodeDedupConflict, "the references belong to different interactions: update one of them, or delete the duplicate",
		"the identifying references given belong to %s and %s, which are different interactions", ids[0], ids[1]).
		with("field", "ref").with("interactions", ids)
}

// refConflict names the identifying reference of rec whose key belongs to
// another record than id.
func (ws *Workspace) refConflict(rec *Interaction, id string, cause error) error {
	for _, k := range interactionKeys(rec) {
		owner, err := ws.Store.KeyOwner(k)
		if err == nil && owner != id && owner != rec.ID {
			e := newError(CodeDedupConflict, fmt.Sprintf("kravis interaction get %s shows it", owner),
				"a reference given already identifies %s", owner).with("id", owner).with("field", "ref")
			e.Cause = cause
			return e
		}
	}
	return cause
}

// sameInteraction is DEDUP_CONFLICT when the data a create sent contradicts
// the interaction it found: its date, channel, direction or initiator
// differ, or a participant or reference it gave is missing there. A create
// never changes a record.
func sameInteraction(ex, rec *Interaction, given []string) error {
	var clashes []clash
	for _, d := range []struct{ field, have, want string }{
		{"occurred-at", ex.OccurredAt, rec.OccurredAt}, {"channel", ex.Channel, rec.Channel},
		{"direction", ex.Direction, rec.Direction},
	} {
		clashes = append(clashes, clashing(d.field, d.have != d.want, d.want, "%s already exists with %s %q, not %q", ex.ID, d.field, d.have, d.want))
	}
	clashes = append(clashes, clashing("initiated-by", rec.InitiatedBy != "" && rec.InitiatedBy != ex.InitiatedBy, rec.InitiatedBy,
		"%s was initiated by %q, not %q", ex.ID, ex.InitiatedBy, rec.InitiatedBy))
	for _, p := range given {
		clashes = append(clashes, clashing("participant", !slices.Contains(ex.Participants, p), p, "%s does not have the participant %s", ex.ID, p))
	}
	for _, d := range rec.Deals {
		clashes = append(clashes, clashing("deal", !slices.Contains(ex.Deals, d), d, "%s is not linked to the deal %s", ex.ID, d))
	}
	for _, p := range rec.Projects {
		clashes = append(clashes, clashing("project", !slices.Contains(ex.Projects, p), p, "%s is not linked to the project %s", ex.ID, p))
	}
	for _, r := range rec.References {
		clashes = append(clashes, clashing("ref", !slices.Contains(ex.References, r), r.String(), "%s does not have the reference %s", ex.ID, r))
	}
	return sameData("interaction", ex.ID, clashes...)
}

// InteractionChange is what interaction update takes; nil leaves a field as
// it is. The date and the holder never change.
type InteractionChange struct {
	Channel            *string
	Subject            *string
	Direction          *string
	InitiatedBy        *string // "" clears it
	Summary            *string // "" clears it
	Live               *bool
	Participants       []string
	RemoveParticipants []string
	Refs               []string
	RemoveRefs         []string
	Deals              []string
	RemoveDeals        []string
	Projects           []string
	RemoveProjects     []string
}

// UpdateInteraction changes an interaction of the actor's holder; another
// holder's is NOT_FOUND (WORKSPACE-INV-02). Participants and references are
// added and removed, and the deduplication keys follow: an identifying
// reference that belongs to another interaction is DEDUP_CONFLICT. An
// interaction whose channel left the catalog must get a new one.
func (ws *Workspace) UpdateInteraction(actorID, id string, ch InteractionChange, opt ViewOptions) (*Result, error) {
	if err := interactions.checkFields(opt); err != nil {
		return nil, err
	}
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	if _, _, err := interactions.loadIn(ws, actor.Holder, id); err != nil {
		return nil, err
	}
	if ch.Channel != nil {
		c := strings.TrimSpace(*ch.Channel)
		ch.Channel = &c
	}
	if ch.Direction != nil && !slices.Contains(directions, *ch.Direction) {
		return nil, InvalidFlag("direction", directions, "%q is not a direction", *ch.Direction)
	}
	_, addAll, warnings, err := ws.participants(ch.Participants)
	if err != nil {
		return nil, err
	}
	var drop []string
	for _, r := range ch.RemoveParticipants {
		p, err := ws.participantID("remove-participant", r)
		if err != nil {
			return nil, err
		}
		drop = append(drop, p)
	}
	addRefs, refWarnings, err := parseRefs(ch.Refs)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, refWarnings...)
	dropRefs, _, err := parseRefs(ch.RemoveRefs)
	if err != nil {
		return nil, err
	}

	addDeals, addProjects, err := ws.checkWork(actor.Holder, ch.Deals, ch.Projects)
	if err != nil {
		return nil, err
	}
	dropDeals, dropProjects := trimAll(ch.RemoveDeals), trimAll(ch.RemoveProjects)
	touched := touchedIf("deals", len(addDeals)+len(dropDeals) > 0, "projects", len(addProjects)+len(dropProjects) > 0,
		"channel", ch.Channel != nil, "subject", ch.Subject != nil, "direction", ch.Direction != nil,
		"initiated_by", ch.InitiatedBy != nil, "summary", ch.Summary != nil, "live", ch.Live != nil,
		"participants", len(addAll)+len(drop) > 0, "references", len(addRefs)+len(dropRefs) > 0)
	rec, changed, err := interactions.updateIn(ws, actor.Holder, actorID, id, func(r *Interaction) error {
		if ch.Channel != nil {
			r.Channel = *ch.Channel
		}
		if err := ws.checkChannel(r.Holder, r.Channel, ch.Channel == nil); err != nil {
			return err
		}
		if ch.Subject != nil {
			r.Subject = strings.TrimSpace(*ch.Subject)
		}
		if ch.Direction != nil {
			r.Direction = *ch.Direction
		}
		ps := sortedIDs(slices.Concat(r.Participants, addAll))
		r.Participants = slices.DeleteFunc(ps, func(p string) bool { return slices.Contains(drop, p) })
		if ch.InitiatedBy != nil {
			r.InitiatedBy = ""
			if strings.TrimSpace(*ch.InitiatedBy) != "" {
				if r.InitiatedBy, err = ws.initiator(r.Holder, r.Direction, *ch.InitiatedBy, r.Participants); err != nil {
					return err
				}
			}
		}
		refs := sortRefs(slices.Concat(r.References, addRefs))
		r.References = slices.DeleteFunc(refs, func(x SourceRef) bool { return slices.Contains(dropRefs, x) })
		r.Deals = slices.DeleteFunc(sortedIDs(slices.Concat(r.Deals, addDeals)), func(x string) bool { return slices.Contains(dropDeals, x) })
		r.Projects = slices.DeleteFunc(sortedIDs(slices.Concat(r.Projects, addProjects)), func(x string) bool { return slices.Contains(dropProjects, x) })
		if ch.Live != nil {
			r.Live = *ch.Live
		}
		if ch.Summary != nil {
			cur, err := ws.Store.Text(interactions.Path(id))
			if err != nil {
				return err
			}
			if cur != *ch.Summary {
				r.Text = ch.Summary
			}
		}
		return nil
	})
	if errors.Is(err, storage.ErrKeyConflict) {
		return nil, ws.refConflict(&Interaction{Holder: actor.Holder, References: addRefs}, id, err)
	}
	if err != nil {
		return nil, err
	}
	return ws.finish(opt, ch.Summary != nil)(interactions.result(outcome(changed), rec, touched, opt, warnings...))
}

// DeleteInteraction deletes an interaction of the actor's holder that no
// record refers to; another holder's is NOT_FOUND.
func (ws *Workspace) DeleteInteraction(actorID, id string, opt ViewOptions) (*Result, error) {
	actor, err := ws.Actor(actorID)
	if err != nil {
		return nil, err
	}
	var text string
	if wantsSummary(opt) {
		if _, _, err := interactions.loadIn(ws, actor.Holder, id); err != nil {
			return nil, err
		}
		if text, err = ws.Store.Text(interactions.Path(id)); err != nil {
			return nil, err
		}
	}
	res, err := interactions.removeIn(ws, actor.Holder, id, opt)
	if err != nil {
		return nil, err
	}
	res.Data = withText(res.Data, text)
	return res, nil
}

// InteractionFilter is what interaction list narrows by.
type InteractionFilter struct {
	// Holder limits the list to one holder's interactions; "" is no limit.
	Holder      string
	Participant string // a person ID or email, or an organization ID
	Direction   string
	InitiatedBy string // a user, a person or an organization
	WithAgents  bool   // with a human user: also what their agents initiated
	Channel     string
	From, To    string // a date in the workspace timezone, or an instant
	Deal        string // only those linked to this deal
	Project     string // only those linked to this project
}

// bound reads a --from or --to: a date is a whole day in the workspace
// timezone, an instant is that second. It returns the first and the last
// second of what the bound names, as stored instants.
func (ws *Workspace) bound(field, s string) (first, last string, err error) {
	if d, err := time.Parse("2006-01-02", strings.TrimSpace(s)); err == nil {
		loc, err := ws.location()
		if err != nil {
			return "", "", err
		}
		start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
		next := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, loc)
		return start.UTC().Format(instantLayout), next.UTC().Add(-time.Second).Format(instantLayout), nil
	}
	t, err := parseInstant(field, s)
	if err != nil {
		return "", "", invalidValue(field, "use a date such as 2026-11-23 or an RFC 3339 instant", "%q is not a date or an instant", s).with("value", s)
	}
	return t.Format(instantLayout), t.Format(instantLayout), nil
}

// ListInteractions returns a page of interactions, newest first.
func (ws *Workspace) ListInteractions(f InteractionFilter, opt ListOptions) (*ListResult, error) {
	if f.Direction != "" && !slices.Contains(directions, f.Direction) {
		return nil, InvalidFlag("direction", directions, "%q is not a direction", f.Direction)
	}
	var participant, initiator string
	var err error
	if f.Participant != "" {
		if participant, err = ws.participantID("participant", f.Participant); err != nil {
			return nil, err
		}
	}
	initiators := []string{}
	if f.InitiatedBy != "" {
		initiators = []string{strings.TrimSpace(f.InitiatedBy)}
		if !strings.HasPrefix(initiators[0], "us_") && !strings.HasPrefix(initiators[0], "org_") {
			if initiators[0], err = ws.participantID("initiated-by", initiators[0]); err != nil {
				return nil, err
			}
		}
		initiator = initiators[0]
		if f.WithAgents {
			u, err := loadLinked(ws, "initiated-by", users, initiator)
			switch {
			case err != nil:
				return nil, err
			case u.Kind != KindHuman:
				return nil, invalidValue("with-agents", "give a human user to --initiated-by", "--with-agents takes the agents of a human user, and %s is an agent", initiator)
			}
			agents, err := ws.agentsOf(initiator)
			if err != nil {
				return nil, err
			}
			initiators = append(initiators, agents...)
		}
	} else if f.WithAgents {
		return nil, invalidValue("with-agents", "add --initiated-by <human user>", "--with-agents needs --initiated-by")
	}
	var from, to string
	if f.From != "" {
		if from, _, err = ws.bound("from", f.From); err != nil {
			return nil, err
		}
	}
	if f.To != "" {
		if _, to, err = ws.bound("to", f.To); err != nil {
			return nil, err
		}
	}
	keep := func(r *Interaction) bool {
		return (participant == "" || slices.Contains(r.Participants, participant)) &&
			(f.Direction == "" || r.Direction == f.Direction) &&
			(initiator == "" || slices.Contains(initiators, r.InitiatedBy)) &&
			(f.Channel == "" || r.Channel == f.Channel) &&
			(f.Deal == "" || slices.Contains(r.Deals, f.Deal)) &&
			(f.Project == "" || slices.Contains(r.Projects, f.Project)) &&
			(from == "" || r.OccurredAt >= from) &&
			(to == "" || r.OccurredAt <= to)
	}
	res, err := interactions.list(ws, f.Holder, opt, keep, applied("participant", participant, "direction", f.Direction,
		"initiated_by", initiator, "with_agents", f.WithAgents, "channel", f.Channel, "from", from, "to", to, "deal", f.Deal, "project", f.Project))
	if err != nil {
		return nil, err
	}
	if wantsSummary(opt.View) {
		for n, v := range res.Data {
			if res.Data[n], err = ws.addSummary(v); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

// agentsOf lists the agents that a human handles.
func (ws *Workspace) agentsOf(id string) ([]string, error) {
	rows, err := users.scan(ws)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if r.rec.Kind == KindAgent && r.rec.Handler == id {
			out = append(out, r.id)
		}
	}
	return out, nil
}

// GetResult is what a get returns: the view and what Kravis filtered by.
type GetResult struct {
	Data    View
	Filters map[string]any
}

// GetInteraction returns the view of an interaction. A read limited to a
// holder finds only that holder's interactions.
func (ws *Workspace) GetInteraction(holder, id string, opt ViewOptions) (*GetResult, error) {
	v, err := interactions.getIn(ws, holder, id, opt)
	if err == nil && wantsSummary(opt) {
		v, err = ws.addSummary(v)
	}
	if err != nil {
		return nil, err
	}
	return &GetResult{Data: v, Filters: holderFilter(holder)}, nil
}

func wantsSummary(opt ViewOptions) bool {
	return opt.Full || slices.Contains(opt.Fields, "summary")
}

// withText adds the summary to a view, when there is one.
func withText(v View, text string) View {
	if text == "" {
		return v
	}
	raw, _ := json.Marshal(text)
	return append(v, storage.Field{Name: "summary", Value: raw})
}

// addSummary reads the summary of the interaction a view shows.
func (ws *Workspace) addSummary(v View) (View, error) {
	for _, f := range v {
		if f.Name == "id" {
			var id string
			if json.Unmarshal(f.Value, &id) != nil || !interactions.validID(id) {
				break
			}
			text, err := ws.Store.Text(interactions.Path(id))
			return withText(v, text), err
		}
	}
	return v, nil
}

// finish adds the summary to a write's result when the options ask for it
// (--fields summary, --full) or the write touched it:
// finish(opt, touched)(interactions.result(…)).
func (ws *Workspace) finish(opt ViewOptions, touched bool) func(*Result, error) (*Result, error) {
	return func(res *Result, err error) (*Result, error) {
		if err != nil || (!touched && !wantsSummary(opt)) {
			return res, err
		}
		res.Data, err = ws.addSummary(res.Data)
		return res, err
	}
}

// interactionsReferringTo finds the interactions that have id as their
// holder, a participant or their initiator.
func interactionsReferringTo(ws *Workspace, id string) ([]string, error) {
	rows, err := interactions.scan(ws)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if r.rec.Holder == id || r.rec.InitiatedBy == id || slices.Contains(r.rec.Participants, id) ||
			slices.Contains(r.rec.Deals, id) || slices.Contains(r.rec.Projects, id) {
			out = append(out, r.id)
		}
	}
	return out, nil
}

// interactionsOf finds the interactions whose holder is id.
func interactionsOf(ws *Workspace, id string) ([]string, error) {
	rows, err := interactions.scan(ws)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if r.rec.Holder == id {
			out = append(out, r.id)
		}
	}
	return out, nil
}

// auditInteractionLinks reports what interactions refer to: records that do
// not exist, a holder that is not an own organization, an initiator of
// another holder, and a channel the catalog no longer has.
func auditInteractionLinks(st *storage.Store, add addFinding) error {
	its, err := loadForCheck(st, interactions)
	if err != nil {
		return err
	}
	if len(its) == 0 {
		return nil
	}
	us, err := loadForCheck(st, users)
	if err != nil {
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
	ds, err := loadForCheck(st, deals)
	if err != nil {
		return err
	}
	ps, err := loadForCheck(st, projects)
	if err != nil {
		return err
	}
	var cfg *Config // nil when config.json cannot be read: check reports that
	if data, _, err := st.Raw(ConfigPath); err == nil {
		c := Config{}
		if json.Unmarshal(data, &c) == nil && c.Validate() == nil {
			cfg = &c
		}
	}
	for _, id := range slices.Sorted(maps.Keys(its)) {
		it := its[id]
		switch o, ok := orgs[it.rec.Holder]; {
		case !hasID("org_", it.rec.Holder):
		case !ok:
			add(FindingDanglingReference, it.path, "the holder %s does not exist", it.rec.Holder)
		case !o.rec.Own:
			add(FindingInvalidRecord, it.path, "the holder %s is not an own organization", it.rec.Holder)
		}
		for _, p := range it.rec.Participants {
			_, isPerson := pe[p]
			_, isOrg := orgs[p]
			if (hasID("pe_", p) && !isPerson) || (hasID("org_", p) && !isOrg) {
				add(FindingDanglingReference, it.path, "the participant %s does not exist", p)
			}
		}
		for _, d := range it.rec.Deals {
			if hasID("dl_", d) {
				auditHolderLink(add, it.path, "deal", d, it.rec.Holder, ds)
			}
		}
		for _, p := range it.rec.Projects {
			if hasID("pr_", p) {
				auditHolderLink(add, it.path, "project", p, it.rec.Holder, ps)
			}
		}
		if hasID("us_", it.rec.InitiatedBy) {
			switch u, ok := us[it.rec.InitiatedBy]; {
			case !ok:
				add(FindingDanglingReference, it.path, "the initiator %s does not exist", it.rec.InitiatedBy)
			case u.rec.Holder != it.rec.Holder:
				add(FindingInvalidRecord, it.path, "the initiator %s works for %s, not for the holder %s", it.rec.InitiatedBy, u.rec.Holder, it.rec.Holder)
			}
		}
		if cfg != nil && slugID.MatchString(it.rec.Channel) &&
			!slices.Contains(cfg.forHolder(it.rec.Holder).Catalogs.Channels, it.rec.Channel) {
			add(FindingValueNotInCatalog, it.path, "the channel %q is not in the channel catalog for holder %s; the next update must replace it",
				it.rec.Channel, it.rec.Holder)
		}
	}
	return nil
}
