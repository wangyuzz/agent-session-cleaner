package session

import "maps"

// Selection is the set of sessions picked out for one action to be applied to
// all of them. A non-empty selection *is* multi-select mode: there is no
// separate flag, so the mode cannot be on with nothing in it.
//
// The invariant the whole type exists to keep: what the rows show is exactly
// what the keys will act on.
type Selection struct {
	ids map[string]bool
}

// Len is how many sessions are picked out.
func (s *Selection) Len() int { return len(s.ids) }

// Empty reports whether multi-select is off.
func (s *Selection) Empty() bool { return len(s.ids) == 0 }

// Has reports whether this session is picked out.
func (s *Selection) Has(id string) bool { return s.ids[id] }

// Clear drops everything, leaving multi-select mode.
func (s *Selection) Clear() { s.ids = nil }

// Clone returns an independent copy of the selection.
func (s Selection) Clone() Selection { return Selection{ids: maps.Clone(s.ids)} }

// Toggle picks a session out, or releases it.
//
// A sub-agent session exists only because of the conversation that spawned it
// and goes wherever that conversation goes, so picking a session picks its
// whole subtree. Releasing one releases the chain it hangs from as well: an
// ancestor left standing would take this one along regardless, and the row
// would be claiming it had been spared when it had not. Siblings are left
// alone — they strand nothing.
func (s *Selection) Toggle(f *Forest, id string) {
	if s.ids == nil {
		s.ids = make(map[string]bool)
	}
	if s.ids[id] {
		delete(s.ids, id)
		for _, kin := range f.Descendants(id) {
			delete(s.ids, kin.ID)
		}
		for _, ancestor := range f.Ancestors(id) {
			delete(s.ids, ancestor.ID)
		}
		return
	}
	s.ids[id] = true
	for _, kin := range f.Descendants(id) {
		s.ids[kin.ID] = true
	}
}

// Remove releases specific sessions, for the ones an action has settled.
func (s *Selection) Remove(ids ...string) {
	for _, id := range ids {
		delete(s.ids, id)
	}
}

// Retain drops whatever is no longer in the listing. Anything deleted since
// the selection was made is not selectable, and would otherwise keep
// multi-select mode on with nothing in it.
func (s *Selection) Retain(f *Forest) {
	for id := range s.ids {
		if _, ok := f.Get(id); !ok {
			delete(s.ids, id)
		}
	}
	if len(s.ids) == 0 {
		s.ids = nil
	}
}

// Picked returns the chosen sessions in display order.
func (s *Selection) Picked(f *Forest) []Session {
	if s.Empty() {
		return nil
	}
	picked := make([]Session, 0, len(s.ids))
	for _, row := range f.Rows() {
		if s.ids[row.Session.ID] {
			picked = append(picked, row.Session)
		}
	}
	return picked
}

// Scope is everything an action on this selection really touches: the picked
// sessions plus their sub-agents, deepest first.
//
// A picked session drags its sub-agents along even when they were taken out of
// the selection by hand, because deleting it would strand them either way.
func (s *Selection) Scope(f *Forest) []Session {
	return f.WithDescendants(s.Picked(f))
}
