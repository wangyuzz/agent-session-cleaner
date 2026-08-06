package session

import (
	"maps"
	"sort"
)

// Guide is one cell of the tree drawing to the left of a row. The glyphs are
// the display layer's business; what the shape means is decided here.
type Guide uint8

const (
	// GuideGap is empty space holding a column open.
	GuideGap Guide = iota
	// GuideTrunk continues a branch that has more siblings below it.
	GuideTrunk
	// GuideBranch introduces a child with siblings after it.
	GuideBranch
	// GuideLast introduces the final child of its parent.
	GuideLast
	// GuideSevered marks a sub-agent session whose parent is gone.
	GuideSevered
	// GuideUnknown marks a sub-agent session that never recorded a parent, so
	// it is neither rooted nor provably stranded.
	GuideUnknown
)

// Row is a session in display order, with the tree shape leading up to it.
type Row struct {
	Session Session
	Guides  []Guide
	// Orphan reports that the session that spawned this one is gone, which
	// means it can never be resumed.
	Orphan bool
	// Nested reports that this row is drawn underneath the session that spawned
	// it, rather than standing on its own at the top level.
	Nested bool
}

// Forest is a session listing with its parent/child relationships resolved.
//
// Build it once per reload and ask it questions. Sibling order, and therefore
// whatever the agent sorted by, survives inside every branch.
type Forest struct {
	sessions []Session
	index    map[string]int
	parent   map[string]string
	children map[string][]int
	roots    []int
	stranded map[string]bool
	rows     []Row
	orphans  []Session
}

// Build resolves a flat listing into a forest. The listing must be complete:
// an archived parent still exists, so leaving it out would strand its children.
func Build(sessions []Session) *Forest {
	// Session IDs are the identity used by selection, ancestry and mutation.
	// Keeping two rows with one ID would make those layers disagree about
	// which path a visible row represents, so retain the first occurrence.
	unique := make([]Session, 0, len(sessions))
	ids := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		if s.ID != "" && ids[s.ID] {
			continue
		}
		if s.ID != "" {
			ids[s.ID] = true
		}
		unique = append(unique, s)
	}
	sessions = unique

	f := &Forest{
		sessions: sessions,
		index:    make(map[string]int, len(sessions)),
		parent:   make(map[string]string, len(sessions)),
		children: make(map[string][]int),
		stranded: make(map[string]bool),
	}
	for i, s := range sessions {
		f.index[s.ID] = i
		f.parent[s.ID] = s.Parent
	}
	for i, s := range sessions {
		switch parent, known := f.index[s.Parent]; {
		case s.Parent == "":
			f.roots = append(f.roots, i)
		case !known:
			// The conversation that spawned this one is gone. It stays at the
			// top level, because there is nothing left to nest it under.
			f.stranded[s.ID] = true
			f.orphans = append(f.orphans, s)
			f.roots = append(f.roots, i)
		case parent == i:
			f.roots = append(f.roots, i)
		default:
			f.children[s.Parent] = append(f.children[s.Parent], i)
		}
	}
	f.layout()
	return f
}

// layout walks the forest once, recording every row in display order with the
// guides that lead to it.
func (f *Forest) layout() {
	f.rows = make([]Row, 0, len(f.sessions))
	seen := make([]bool, len(f.sessions))

	var walk func(at int, base []Guide, connector Guide, rooted, nested bool)
	walk = func(at int, base []Guide, connector Guide, rooted, nested bool) {
		if seen[at] {
			return // a parent cycle would otherwise never return
		}
		seen[at] = true
		s := f.sessions[at]

		guides := base
		below := base
		if !rooted {
			guides = append(append(make([]Guide, 0, len(base)+1), base...), connector)
			// A branch with siblings still to come keeps its trunk running
			// down past its own children.
			cell := GuideGap
			if connector == GuideBranch {
				cell = GuideTrunk
			}
			below = append(append(make([]Guide, 0, len(base)+1), base...), cell)
		}
		f.rows = append(f.rows, Row{
			Session: s,
			Guides:  guides,
			Orphan:  f.stranded[s.ID],
			Nested:  nested,
		})

		kin := f.children[s.ID]
		for n, child := range kin {
			connector := GuideBranch
			if n == len(kin)-1 {
				connector = GuideLast
			}
			walk(child, below, connector, false, true)
		}
	}

	for _, root := range f.roots {
		s := f.sessions[root]
		switch {
		case f.stranded[s.ID]:
			walk(root, nil, GuideSevered, false, false)
		case s.SideThread:
			walk(root, nil, GuideUnknown, false, false)
		default:
			// An ordinary conversation carries no marker, and its children
			// start at the left edge rather than one column in.
			walk(root, nil, GuideGap, true, false)
		}
	}

	// Members of a parent cycle have no root and are never reached above.
	// Show them anyway: a session nobody can see is one nobody can decide about.
	for i, s := range f.sessions {
		if !seen[i] {
			f.rows = append(f.rows, Row{Session: s})
		}
	}
}

// Rows returns every session in display order, parents before their children.
func (f *Forest) Rows() []Row { return f.rows }

// Len is how many sessions the forest holds.
func (f *Forest) Len() int { return len(f.sessions) }

// Orphans are the sub-agent sessions whose parent is no longer on disk. Such a
// session cannot be resumed by anything.
func (f *Forest) Orphans() []Session { return f.orphans }

// Stranded reports whether this session's parent is gone.
func (f *Forest) Stranded(id string) bool { return f.stranded[id] }

// Parents maps every session to the one that spawned it, whether or not that
// one is still on disk. The copy is safe to hand to a goroutine.
func (f *Forest) Parents() map[string]string {
	return maps.Clone(f.parent)
}

// Get returns a session by id.
func (f *Forest) Get(id string) (Session, bool) {
	at, ok := f.index[id]
	if !ok {
		return Session{}, false
	}
	return f.sessions[at], true
}

// Descendants are the sub-agent sessions below this one, deepest first, so a
// cascade never has to step over a session whose parent it already removed.
func (f *Forest) Descendants(id string) []Session {
	seen := map[string]bool{id: true}
	var found []Session
	var walk func(string)
	walk = func(node string) {
		for _, at := range f.children[node] {
			child := f.sessions[at]
			if seen[child.ID] {
				continue
			}
			seen[child.ID] = true
			walk(child.ID)
			found = append(found, child)
		}
	}
	walk(id)
	return found
}

// Ancestors are the sessions this one hangs from, nearest first. The walk
// stops at the first parent that is no longer on disk.
func (f *Forest) Ancestors(id string) []Session {
	seen := map[string]bool{id: true}
	var found []Session
	node, ok := f.Get(id)
	for ok && node.Parent != "" && !seen[node.Parent] {
		seen[node.Parent] = true
		node, ok = f.Get(node.Parent)
		if ok {
			found = append(found, node)
		}
	}
	return found
}

// Cascade is everything one action on s really touches, sub-agents first.
//
// An agent's own command line only ever acts on the session it is given, which
// is how deleting a conversation leaves its sub-agents behind as orphans. A
// side thread exists only because of the conversation that spawned it, so it
// goes wherever that conversation goes — and it goes first, so that a failure
// part-way through leaves the parent standing rather than a fresh orphan.
//
// keep may be nil; otherwise only descendants it accepts come along.
func (f *Forest) Cascade(s Session, keep func(Session) bool) []Session {
	kin := f.Descendants(s.ID)
	targets := make([]Session, 0, len(kin)+1)
	for _, child := range kin {
		if keep == nil || keep(child) {
			targets = append(targets, child)
		}
	}
	return append(targets, s)
}

// WithDescendants is the same cascade for a whole set at once, deduplicated
// and still deepest-first within each family. Sweeping a set of parents without
// their sub-agents would manufacture exactly the orphans the next key along has
// to clean up.
func (f *Forest) WithDescendants(targets []Session) []Session {
	seen := make(map[string]bool, len(targets))
	ordered := make([]Session, 0, len(targets))
	for _, target := range targets {
		for _, item := range f.Cascade(target, nil) {
			if !seen[item.ID] {
				seen[item.ID] = true
				ordered = append(ordered, item)
			}
		}
	}
	return ordered
}

// Branches splits a batch into groups that cannot interfere with one another.
//
// Everything in a group sits on one branch and keeps the order it arrived in —
// sub-agents before the session that spawned them — so a session is still only
// ever acted on after its own descendants. Separate groups share no ancestry,
// which is what makes running them at the same time safe.
//
// The whole forest is consulted, not just the batch: a generation left out of
// it (an already-archived session, say) must not make a grandparent and
// grandchild look like two unrelated branches.
func (f *Forest) Branches(targets []Session) [][]Session {
	among := make(map[string]bool, len(targets))
	for _, target := range targets {
		among[target.ID] = true
	}

	type ranked struct {
		depth   int
		session Session
	}
	grouped := make(map[string][]ranked)
	var order []string
	for _, target := range targets {
		top, depth := f.ancestry(target.ID, among)
		if _, seen := grouped[top]; !seen {
			order = append(order, top)
		}
		grouped[top] = append(grouped[top], ranked{depth, target})
	}

	branches := make([][]Session, 0, len(order))
	for _, top := range order {
		members := grouped[top]
		// Deepest first, and stably, so the guarantee holds whatever order the
		// batch arrived in rather than only for the callers that sort.
		sort.SliceStable(members, func(i, j int) bool {
			return members[i].depth > members[j].depth
		})
		branch := make([]Session, len(members))
		for i, member := range members {
			branch[i] = member.session
		}
		branches = append(branches, branch)
	}
	return branches
}

// ancestry finds the topmost batch member this session hangs from, and how far
// below it that is.
func (f *Forest) ancestry(id string, among map[string]bool) (string, int) {
	top, depth := id, 0
	path := []string{id}
	position := map[string]int{id: 0}
	node, climbed := id, 0

	for {
		parent, known := f.parent[node]
		if !known || parent == "" {
			return top, depth
		}
		if at, looped := position[parent]; looped {
			// A corrupt parent cycle has no top, but its members still share
			// state and must never run concurrently. One canonical key puts
			// the whole cycle, and anything hanging from it, in one group.
			return f.cycleKey(path[at:], path, among)
		}
		position[parent] = len(path)
		path = append(path, parent)
		node, climbed = parent, climbed+1
		if among[parent] {
			top, depth = parent, climbed
		}
	}
}

func (f *Forest) cycleKey(cycle, path []string, among map[string]bool) (string, int) {
	key := ""
	for _, member := range cycle {
		if among[member] && (key == "" || member < key) {
			key = member
		}
	}
	if key == "" {
		key = cycle[0]
		for _, member := range cycle {
			if member < key {
				key = member
			}
		}
	}
	for at, member := range path {
		if member == key {
			return key, at
		}
	}
	return key, len(path)
}

// SortByRecency orders a listing newest first, which is how every agent's
// sessions are presented.
//
// Sessions started within the same second are common — a conversation and the
// sub-agent it spawns — and their recorded times are only accurate to one. The
// id breaks the tie so the list does not reshuffle between two reads of the
// same directory.
func SortByRecency(sessions []Session) {
	sort.Slice(sessions, func(i, j int) bool {
		a, b := sessions[i].RecencyAt(), sessions[j].RecencyAt()
		if a.Equal(b) {
			return sessions[i].ID < sessions[j].ID
		}
		return a.After(b)
	})
}
