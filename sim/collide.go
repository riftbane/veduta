package sim

import (
	"sort"

	"github.com/riftbane/veduta/scene"
)

// Pair is an unordered pair of entity ids with A < B.
type Pair struct{ A, B uint32 }

// Contacts tracks which entity AABBs overlap from one tick to the next and reports the
// pairs that start overlapping ("collision" events). Two entities of kind static never
// collide with each other; touching boxes do not overlap (gmath.AABB.Overlaps).
type Contacts struct {
	active map[Pair]bool
	order  []*scene.Entity
}

// NewContacts returns an empty contact tracker.
func NewContacts() *Contacts { return &Contacts{active: map[Pair]bool{}} }

// Step computes the overlapping pairs of the current scene state and returns the pairs
// that were not overlapping at the previous Step, sorted by (A, B).
func (c *Contacts) Step(s *scene.Scene) []Pair {
	c.order = c.order[:0]
	for _, e := range s.Entities() {
		if e.Alive() && !e.AABB.IsEmpty() {
			c.order = append(c.order, e)
		}
	}
	// Sweep and prune along x; ties broken by id so the order is total.
	sort.Slice(c.order, func(i, j int) bool {
		a, b := c.order[i], c.order[j]
		if a.AABB.Min.X != b.AABB.Min.X {
			return a.AABB.Min.X < b.AABB.Min.X
		}
		return a.ID < b.ID
	})
	now := map[Pair]bool{}
	for i, a := range c.order {
		for _, b := range c.order[i+1:] {
			if b.AABB.Min.X >= a.AABB.Max.X {
				break
			}
			if a.Kind == scene.KindStatic && b.Kind == scene.KindStatic {
				continue
			}
			if a.AABB.Overlaps(b.AABB) {
				p := Pair{a.ID, b.ID}
				if p.A > p.B {
					p.A, p.B = p.B, p.A
				}
				now[p] = true
			}
		}
	}
	var begun []Pair
	for p := range now {
		if !c.active[p] {
			begun = append(begun, p)
		}
	}
	sortPairs(begun)
	c.active = now
	return begun
}

// Active returns the currently overlapping pairs, sorted.
func (c *Contacts) Active() []Pair {
	out := make([]Pair, 0, len(c.active))
	for p := range c.active {
		out = append(out, p)
	}
	sortPairs(out)
	return out
}

// SetActive replaces the overlapping set (snapshot restore).
func (c *Contacts) SetActive(ps []Pair) {
	c.active = make(map[Pair]bool, len(ps))
	for _, p := range ps {
		c.active[p] = true
	}
}

// Touching reports whether ids a and b overlapped at the last Step.
func (c *Contacts) Touching(a, b uint32) bool {
	if a > b {
		a, b = b, a
	}
	return c.active[Pair{a, b}]
}

func sortPairs(ps []Pair) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].A != ps[j].A {
			return ps[i].A < ps[j].A
		}
		return ps[i].B < ps[j].B
	})
}
