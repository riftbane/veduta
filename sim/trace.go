package sim

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"sort"

	"github.com/riftbane/veduta/v2/scene"
)

// Built-in trace events.
const (
	EventSpawn              = "spawn"
	EventDespawn            = "despawn"
	EventCollision          = "collision"
	EventSceneLoad          = "scene_load"
	EventInvariantViolation = "invariant_violation"
)

// Event is one trace event. Fields are encoded next to the reserved "event" key.
type Event struct {
	Name   string
	Fields map[string]any
}

// Recorder builds the trace: one canonical JSON object per tick,
//
//	{"entities":[...],"events":[...],"tick":n}
//
// written to an optional writer (a .jsonl file) and hashed with SHA-256. The hash covers
// exactly the bytes written, newline included, so it can be recomputed from the file.
type Recorder struct {
	w       io.Writer
	h       hash.Hash
	pending []Event
	counts  map[string]int // cumulative event counts
	line    []byte
	ticks   int
}

// NewRecorder returns a recorder writing to w (nil: hash only).
func NewRecorder(w io.Writer) *Recorder {
	return &Recorder{w: w, h: sha256.New(), counts: map[string]int{}}
}

// Emit adds an event to the current tick.
func (r *Recorder) Emit(name string, fields map[string]any) {
	r.pending = append(r.pending, Event{Name: name, Fields: fields})
}

// Pending returns the events emitted so far in the current tick.
func (r *Recorder) Pending() []Event { return r.pending }

// EndTick writes the record for tick with the given entity summaries and starts the
// next tick.
func (r *Recorder) EndTick(tick uint64, entities []map[string]any) error {
	evs := make([]any, len(r.pending))
	for i, e := range r.pending {
		m := make(map[string]any, len(e.Fields)+1)
		for k, v := range e.Fields {
			m[k] = v
		}
		m["event"] = e.Name
		evs[i] = m
		r.counts[e.Name]++
	}
	ents := make([]any, len(entities))
	for i, e := range entities {
		ents[i] = e
	}
	rec := map[string]any{"tick": tick, "events": evs, "entities": ents}
	var err error
	r.line, err = AppendCanonical(r.line[:0], rec)
	if err != nil {
		return fmt.Errorf("trace tick %d: %w", tick, err)
	}
	r.line = append(r.line, '\n')
	r.h.Write(r.line)
	if r.w != nil {
		if _, err := r.w.Write(r.line); err != nil {
			return fmt.Errorf("trace tick %d: %w", tick, err)
		}
	}
	r.pending = r.pending[:0]
	r.ticks++
	return nil
}

// Hash returns the hex SHA-256 of the trace written so far.
func (r *Recorder) Hash() string { return hex.EncodeToString(r.h.Sum(nil)) }

// Count returns how many events named name were recorded in completed ticks.
func (r *Recorder) Count(name string) int { return r.counts[name] }

// Counts returns a copy of all cumulative event counts.
func (r *Recorder) Counts() map[string]int {
	out := make(map[string]int, len(r.counts))
	for k, v := range r.counts {
		out[k] = v
	}
	return out
}

// CountNames returns the recorded event names in sorted order.
func (r *Recorder) CountNames() []string {
	names := make([]string, 0, len(r.counts))
	for k := range r.counts {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Ticks returns the number of ticks recorded.
func (r *Recorder) Ticks() int { return r.ticks }

// Summary returns the trace summary of a live entity of s: id, name, kind, position
// (world), rotation_deg (local Euler angles, R = Ry·Rx·Rz), scale (local), visible, tags,
// model, material, parent (name, "" for none), and — when present — aabb ({min, max},
// world) and state. These keys are what scenario paths address (position.x,
// aabb.max.y, state.score, …; see asset.ExpectPaths).
func Summary(s *scene.Scene, e *scene.Entity) map[string]any {
	parent := ""
	if p := s.Get(e.Parent); p != nil && e.Parent != 0 {
		parent = p.Name
	}
	m := map[string]any{
		"id":           e.ID,
		"name":         e.Name,
		"kind":         e.Kind,
		"position":     e.WorldPosition(),
		"rotation_deg": e.Transform.Rotation.EulerDeg(),
		"scale":        e.Transform.Scale,
		"visible":      e.Visible,
		"tags":         append([]string{}, e.Tags...),
		"model":        e.Model,
		"material":     e.Material,
		"parent":       parent,
	}
	if !e.AABB.IsEmpty() {
		m["aabb"] = map[string]any{"min": e.AABB.Min, "max": e.AABB.Max}
	}
	if e.Frame != 0 { // left out at 0, so traces without sprite sheets keep their hashes
		m["frame"] = e.Frame
	}
	if e.Anim != "" { // left out when none, likewise
		m["anim"] = e.Anim
	}
	if e.State != nil {
		m["state"] = e.State
	}
	return m
}

// Summaries returns the summaries of all live entities of s in id order.
func Summaries(s *scene.Scene) []map[string]any {
	var out []map[string]any
	for _, e := range s.Entities() {
		if e.Alive() {
			out = append(out, Summary(s, e))
		}
	}
	return out
}
