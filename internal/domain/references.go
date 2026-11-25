package domain

import (
	"maps"
	"slices"
	"strings"

	"github.com/diegomarino/kravis/internal/storage"
)

// Finding code for a reference to a record that does not exist.
const FindingBrokenReference = "BROKEN_REFERENCE"

// stored is a record read for check, with the path of its file.
type stored[P any] struct {
	path string
	rec  P
}

// loadForCheck reads every record of the type that decodes and sits at its
// own path; the rest is reported by the type's own audit.
func loadForCheck[T any, P interface {
	*T
	storage.Record
}](st *storage.Store, k *kind[T, P]) (map[string]stored[P], error) {
	paths, err := st.List("records/" + k.Dir + "/")
	if err != nil {
		return nil, err
	}
	out := map[string]stored[P]{}
	for _, p := range paths {
		if !strings.HasSuffix(p, ".json") {
			continue
		}
		data, _, err := st.Raw(p)
		rec := P(new(T))
		if err != nil || storage.Decode(data, rec) != nil || k.Path(rec.Meta().ID) != p {
			continue
		}
		out[rec.Meta().ID] = stored[P]{p, rec}
	}
	return out, nil
}

// auditUserLinks reports what users refer to: records that do not exist
// (BROKEN_REFERENCE) and records that cannot be what is meant
// (INVALID_RECORD): a holder that is not an own organization, a handler that
// is not a live human of the agent's holder. It also reports a holder left
// without a live human (IDENTITY-INV-06), which two retirements that race
// can leave.
func auditUserLinks(st *storage.Store, add addFinding) error {
	us, err := loadForCheck(st, users)
	if err != nil {
		return err
	}
	orgs, err := loadForCheck(st, organizations)
	if err != nil {
		return err
	}
	// A holder with users but no live human is stranded; so is every holder
	// when no live human is left anywhere, since then nobody can create one.
	liveHuman, anyLive := map[string]bool{}, false
	for _, u := range us {
		live := u.rec.Kind == KindHuman && u.rec.Live
		liveHuman[u.rec.Holder] = liveHuman[u.rec.Holder] || live
		anyLive = anyLive || live
	}
	for _, id := range slices.Sorted(maps.Keys(orgs)) {
		live, hasUsers := liveHuman[id]
		if o := orgs[id]; o.rec.Own && !live && (hasUsers || !anyLive) {
			add(FindingInvalidRecord, o.path, "the holder has no live human to act for it")
		}
	}
	pe, err := loadForCheck(st, people)
	if err != nil {
		return err
	}
	for _, id := range slices.Sorted(maps.Keys(us)) {
		u := us[id]
		switch o, ok := orgs[u.rec.Holder]; {
		case !hasID("org_", u.rec.Holder):
		case !ok:
			add(FindingBrokenReference, u.path, "the holder %s does not exist", u.rec.Holder)
		case !o.rec.Own:
			add(FindingInvalidRecord, u.path, "the holder %s is not an own organization", u.rec.Holder)
		}
		if u.rec.Kind == KindAgent && hasID("us_", u.rec.Handler) {
			switch h, ok := us[u.rec.Handler]; {
			case !ok:
				add(FindingBrokenReference, u.path, "the handler %s does not exist", u.rec.Handler)
			case h.rec.Kind != KindHuman:
				add(FindingInvalidRecord, u.path, "the handler %s is not a human", u.rec.Handler)
			case h.rec.Holder != u.rec.Holder:
				add(FindingInvalidRecord, u.path, "the handler %s works for %s, not for %s", u.rec.Handler, h.rec.Holder, u.rec.Holder)
			case u.rec.Live && !h.rec.Live:
				add(FindingInvalidRecord, u.path, "the agent is live but its handler %s is not", u.rec.Handler)
			}
		}
		if hasID("pe_", u.rec.Person) {
			if _, ok := pe[u.rec.Person]; !ok {
				add(FindingBrokenReference, u.path, "the person %s does not exist", u.rec.Person)
			}
		}
	}
	return nil
}

// auditPersonLinks reports the affiliations that point to an organization
// that does not exist (BROKEN_REFERENCE).
func auditPersonLinks(st *storage.Store, add addFinding) error {
	orgs, err := loadForCheck(st, organizations)
	if err != nil {
		return err
	}
	pe, err := loadForCheck(st, people)
	if err != nil {
		return err
	}
	for _, id := range slices.Sorted(maps.Keys(pe)) {
		for _, a := range pe[id].rec.Affiliations {
			if _, ok := orgs[a.Organization]; hasID("org_", a.Organization) && !ok {
				add(FindingBrokenReference, pe[id].path, "the affiliation is to %s, which does not exist", a.Organization)
			}
		}
	}
	return nil
}
