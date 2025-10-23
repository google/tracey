/*
	Copyright 2024 Google Inc.

	Licensed under the Apache License, Version 2.0 (the "License");
	you may not use this file except in compliance with the License.
	You may obtain a copy of the License at

			http://www.apache.org/licenses/LICENSE-2.0

	Unless required by applicable law or agreed to in writing, software
	distributed under the License is distributed on an "AS IS" BASIS,
	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
	See the License for the specific language governing permissions and
	limitations under the License.
*/

// Package criticalpath defines a critical path event type and a function
// finding a critical path between two points within a trace.  A critical
// path is a 'longest' path of causal dependences connecting two points within
// a trace; when these two points bracket a latency-sensitive interval in the
// traced execution, the points along that path are of particular interest
// because they may represent optimization opportunities, since optimizing them
// could shorten the distance between the endpoints.
//
// In a schematic dependency graph of a distributed set of interdependent
// tasks, like a PERT chart, a given pair of milestone points can have multiple
// paths, each with different lengths, between them.  For such graphs, some
// paths between an endpoint pair may be longer than others, and so only some
// paths between endpoints might be critical.  However, such schematics are not
// traces: in a trace, a particular milestone can only occur at a single point
// in time.  Therefore, in a trace, *all* paths between a given pair of points
// will necessarily have the same length, and so all are potentially critical
// paths.
//
// To provide some finer control over CP selection in this situation, this
// package supports multiple critical path selection strategies, which can
// prioritize different CP characteristics (or can just ensure a relatively
// deterministic and comparable resulting CP).
package criticalpath

import (
	"fmt"
	"regexp"
	"slices"
	"sort"

	"github.com/google/tracey/trace"
)

// Type specifies a particular type (i.e., pair of endpoints) of critical path
// within a Trace.
type Type uint

const (
	// UnknownType indicates that the critical path type is unknown or unspecified.
	UnknownType Type = iota
	// CustomCriticalPathType is a critical path type in which the user specifies
	// custom endpoints.
	CustomCriticalPathType
	// SelectedElementCriticalPathType specifies the critical path from the beginning
	// to the end of a selected Span.
	SelectedElementCriticalPathType
	// FirstUserDefinedType is the critical path Type value at which specific trace libraries should
	// begin enumerating their critical path Types.
	FirstUserDefinedType
)

// Endpoint represents a Trace critical path endpoint: a Trace span, and a
// point within that span.
type Endpoint[T any, CP, SP, DP fmt.Stringer] struct {
	Span trace.Span[T, CP, SP, DP]
	At   T
}

// EndpointFromElementarySpan returns an Endpoint based on the provided
// ElementarySpan.  If atStart is true, the endpoint will be at the start of
// the ElementarySPan, otherwise it will be at its end.
func EndpointFromElementarySpan[T any, CP, SP, DP fmt.Stringer](
	es trace.ElementarySpan[T, CP, SP, DP],
	atStart bool, // if false, at the end.
) *Endpoint[T, CP, SP, DP] {
	ret := &Endpoint[T, CP, SP, DP]{
		Span: es.Span(),
	}
	if atStart {
		ret.At = es.Start()
	} else {
		ret.At = es.End()
	}
	return ret
}

// EndpointFromElementarySpanPosition returns an Endpoint based on the provided
// ElementarySpanPosition.  If atStart is true, the endpoint will be at the
// start of the ElementarySPan, otherwise it will be at its end.
func EndpointFromElementarySpanPosition[T any, CP, SP, DP fmt.Stringer](
	esp *trace.ElementarySpanPosition[T, CP, SP, DP],
) *Endpoint[T, CP, SP, DP] {
	return &Endpoint[T, CP, SP, DP]{
		Span: esp.ElementarySpan.Span(),
		At:   esp.At,
	}
}

// ElementarySpan returns the ElementarySpan in which the provided Endpoint
// lies, using the provided Comparator.
func (e *Endpoint[T, CP, SP, DP]) ElementarySpan(
	comparator trace.Comparator[T],
) trace.ElementarySpan[T, CP, SP, DP] {
	if e == nil {
		return nil
	}
	ess := e.Span.ElementarySpans()
	// Find the index of the first span ending at or after the specified point.
	idx := sort.Search(len(ess), func(x int) bool {
		return comparator.LessOrEqual(e.At, ess[x].End())
	})
	if idx == len(ess) {
		return nil
	}
	es := ess[idx]
	if comparator.LessOrEqual(es.Start(), e.At) {
		return es
	}
	return nil
}

// Strategy specifies a particular critical path selection strategy.
type Strategy uint

const (
	// PreferCausal specifies that causal edges (i.e., Dependencies) should be
	// traversed preferentially to edges to an elementary span's predecessor
	// within its span (i.e., the elementary span just before it in their parent
	// Span).  If a dependency has multiple origins, the most-recently-resolving
	// one is chosen.
	PreferCausal Strategy = iota
	// PreferPredecessor specifies that edges to predecessors should be traversed
	// preferentially to causal edges.
	PreferPredecessor
	// PreferMostProximate specifies that, if an elementary span has multiple
	// predecessors (i.e., one causal and one in-span predecessor), the one
	// ending most recently is traversed preferentially.
	PreferMostProximate
	// PreferLeastProximate specifies that, if an elementary span has multiple
	// predecessors, the one ending least recently is traversed preferentially.
	PreferLeastProximate
	// PreferMostWork specifies that the critical path with the most work (that
	// is, the largest total elementary span duration, and the least scheduling
	// delay) should be returned.  An exact algorithm, it can be expensive to
	// compute.
	PreferMostWork
	// PreferLeastWork specifies that the critical path with the least work (that
	// is, the smallest total elementary span duration, and the most scheduling
	// delay) should be returned.  An exact algorithm, it can be expensive to
	// compute.
	PreferLeastWork
	// PreferTemporalMostWork specifies that the critical path with the most work
	// (that is, the largest total elementary span duration, and the least
	// scheduling delay) should be returned, but causal edges should be ignored.
	// It does this by ignoring all causal edges and redefining edges as a
	// temporal "happens before" relationship, even if they are not causal. This
	// is useful for finding the critical path through a trace that does not have
	// enough causal information to provide a meaningful most work CP.
	PreferTemporalMostWork
)

type options struct {
	// Whether the pathing algorithm should consider non-negative, non-triggering
	// origins.  Under multiple-origin-OR semantics, a dependency may have
	// multiple origins, of which the earliest is the 'triggering' one -- the one
	// that caused the dependency to be satisfied.  If a pathing algorithm is
	// constrained to only traverse the triggering origin, then the resulting
	// critical path can reveal scheduling opportunity -- places where work
	// started later than it causally could have.  Meanwhile, a pathing algorithm
	// permitted to traverse any non-negative origin edges can instead find long
	// chains of causally-related work.
	includePositiveNontriggeringOrigins bool
}

// Option instances are options to critical path pathing algorithms.
type Option func(opts *options)

// IncludePositiveNontriggeringOrigins specifies that a critical path pathing
// algorithm should traverse all non-negative origin edges, not just the
// triggering ones.
func IncludePositiveNontriggeringOrigins() Option {
	return func(opts *options) {
		opts.includePositiveNontriggeringOrigins = true
	}
}

// ExcludePositiveNontriggeringOrigins specifies that a critical path pathing
// algorithm should traverse only non-negative triggering origin edges, not
// non-triggering ones.
func ExcludePositiveNontriggeringOrigins() Option {
	return func(opts *options) {
		opts.includePositiveNontriggeringOrigins = false
	}
}

func buildOptions(opts ...Option) *options {
	ret := &options{
		includePositiveNontriggeringOrigins: false,
	}
	for _, opt := range opts {
		opt(ret)
	}
	return ret
}

// FindBetweenEndpoints seeks a critical path between the two specified endpoints.
func FindBetweenEndpoints[T any, CP, SP, DP fmt.Stringer](
	tr trace.Trace[T, CP, SP, DP],
	from, to *Endpoint[T, CP, SP, DP],
	strategy Strategy,
	options ...Option,
) (*Path[T, CP, SP, DP], error) {
	origin := from.ElementarySpan(tr.Comparator())
	if origin == nil {
		return nil, fmt.Errorf("can't find critical path: origin span is not running at start point %v", from.At)
	}
	destination := to.ElementarySpan(tr.Comparator())
	if destination == nil {
		return nil, fmt.Errorf("can't find critical path: destination span is not running at start point %v", from.At)
	}
	path, err := FindBetweenElementarySpans(tr, origin, destination, strategy, options...)
	if err != nil {
		return nil, err
	}
	path.Start, path.End = from.At, to.At
	return path, nil
}

// Path represents a single critical path.
type Path[T any, CP, SP, DP fmt.Stringer] struct {
	Start, End   T
	CriticalPath []trace.ElementarySpan[T, CP, SP, DP]
}

// FindMarkers returns all markers matching the provided regexp in any
// on-critical-path elementary span.  Note that matching markers that appear
// before the path's Start or after the path's End will be produced.
func (p *Path[T, CP, SP, DP]) FindMarkers(markLabelRE *regexp.Regexp) []string {
	var ret []string
	for _, cp := range p.CriticalPath {
		for _, mark := range cp.Marks() {
			if markLabelRE.MatchString(mark.Label()) {
				ret = append(ret, mark.Label())
			}
		}
	}
	return ret
}

// FindBetweenElementarySpans seeks a critical path between the two specified
// ElementarySpans.
func FindBetweenElementarySpans[T any, CP, SP, DP fmt.Stringer](
	tr trace.Trace[T, CP, SP, DP],
	origin, destination trace.ElementarySpan[T, CP, SP, DP],
	strategy Strategy,
	options ...Option,
) (*Path[T, CP, SP, DP], error) {
	opts := buildOptions(options...)
	return findBetweenElementarySpans(tr, origin, destination, strategy, opts)
}

func findBetweenElementarySpans[T any, CP, SP, DP fmt.Stringer](
	tr trace.Trace[T, CP, SP, DP],
	origin, destination trace.ElementarySpan[T, CP, SP, DP],
	strategy Strategy,
	opts *options,
) (*Path[T, CP, SP, DP], error) {
	var cp []trace.ElementarySpan[T, CP, SP, DP]
	var err error
	switch strategy {
	case PreferCausal, PreferPredecessor, PreferMostProximate, PreferLeastProximate:
		cp, err = greedyFind[T, CP, SP, DP](
			tr.Comparator(), strategy, opts, origin, destination,
		)
	case PreferMostWork, PreferLeastWork:
		cp, err = exactFind[T, CP, SP, DP](
			tr, strategy, opts, origin, destination,
		)
	case PreferTemporalMostWork:
		cp, err = nonCausalExactFind[T, CP, SP, DP](tr, origin, destination)
	default:
		err = fmt.Errorf("unsupported critical path strategy")
	}
	if err != nil {
		return nil, err
	}
	if len(cp) == 0 {
		return nil, fmt.Errorf("no critical path found")
	}
	return &Path[T, CP, SP, DP]{
		Start:        cp[0].Start(),
		End:          cp[len(cp)-1].End(),
		CriticalPath: cp,
	}, nil
}

// Seeks a critical path between the two specified events using a greedy
// heuristic: searching recursively backwards from its endpoint (here, 'to'),
// at each step pushing the current event's predecessors onto a stack in
// increasing temporal order, then popping the next event from the stack, until
// the current event is the start point (here, `from`).
func greedyFind[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	strategy Strategy,
	opts *options,
	origin, destination trace.ElementarySpan[T, CP, SP, DP],
) ([]trace.ElementarySpan[T, CP, SP, DP], error) {
	// A step in the critical path search.
	type step struct {
		// The event at this step
		elementarySpan trace.ElementarySpan[T, CP, SP, DP]
		// The index, in steps, of the successor of this step.
		successorPos int
	}
	// Track visited events.  Visiting the same event a second time cannot yield
	// a different outcome than the first, and the first instance will be on the
	// better critical path, so all visits after the first are immediately
	// pruned.
	visitedEvents := map[trace.ElementarySpan[T, CP, SP, DP]]struct{}{}
	// Always start the backwards search at `to`.
	steps := []step{{destination, -1}}
	// Initialize the stack with the `to` step.
	stack := []int{0}
	for len(stack) > 0 {
		// Pop the position of the next step from the stack.
		thisPos := stack[len(stack)-1]
		stack = stack[0 : len(stack)-1]
		thisStep := steps[thisPos]
		// If the current step's event is `from`, we've found a critical path.
		// Walk forward along `successor_pos` links to build this path.
		if thisStep.elementarySpan == origin {
			cur := thisPos
			var rev []trace.ElementarySpan[T, CP, SP, DP]
			for cur >= 0 {
				rev = append(rev, steps[cur].elementarySpan)
				cur = steps[cur].successorPos
			}
			return rev, nil
		}
		// If we've already visited this event, continue.
		if _, ok := visitedEvents[thisStep.elementarySpan]; ok {
			continue
		}
		visitedEvents[thisStep.elementarySpan] = struct{}{}
		// Find the relevant predecessors.
		inSpanPredecessor := thisStep.elementarySpan.Predecessor()
		var causalPredecessor trace.ElementarySpan[T, CP, SP, DP]
		if thisStep.elementarySpan.Incoming() != nil {
			if opts.includePositiveNontriggeringOrigins {
				for _, origin := range thisStep.elementarySpan.Incoming().Origins() {
					if comparator.LessOrEqual(origin.End(), thisStep.elementarySpan.Start()) {
						if causalPredecessor == nil ||
							(strategy == PreferLeastProximate && comparator.Less(origin.End(), causalPredecessor.End())) ||
							(strategy != PreferLeastProximate && comparator.GreaterOrEqual(origin.End(), causalPredecessor.End())) {
							causalPredecessor = origin
						}
					}
				}
			} else {
				causalPredecessor = thisStep.elementarySpan.Incoming().TriggeringOrigin()
			}
		}
		var preds []trace.ElementarySpan[T, CP, SP, DP]
		if inSpanPredecessor != nil && causalPredecessor != nil {
			causalEndsBeforeInSpan := comparator.Less(
				causalPredecessor.End(),
				inSpanPredecessor.End(),
			)
			if (strategy == PreferMostProximate && !causalEndsBeforeInSpan) ||
				(strategy == PreferLeastProximate && causalEndsBeforeInSpan) ||
				(strategy == PreferCausal) {
				preds = []trace.ElementarySpan[T, CP, SP, DP]{
					inSpanPredecessor, causalPredecessor,
				}
			} else {
				preds = []trace.ElementarySpan[T, CP, SP, DP]{
					causalPredecessor, inSpanPredecessor,
				}
			}
		} else if inSpanPredecessor != nil {
			preds = []trace.ElementarySpan[T, CP, SP, DP]{inSpanPredecessor}
		} else if causalPredecessor != nil {
			preds = []trace.ElementarySpan[T, CP, SP, DP]{causalPredecessor}
		}

		for _, pred := range preds {
			if comparator.Less(thisStep.elementarySpan.Start(), pred.End()) {
				// Reject this negative edge.
				continue
			}
			predPos := len(steps)
			steps = append(steps, step{
				elementarySpan: pred,
				successorPos:   thisPos,
			})
			stack = append(stack, predPos)
		}
	}
	return nil, nil
}

type direction bool

const (
	forwards  direction = true
	backwards direction = false
)

// Returns the set of all ElementarySpans reachable by scanning forwards from
// one endpoint towards the other (but not including any elementary span lying
// temporally past the distal endpoint.)  If the provided limitMap is non-nil,
// nodes not present in that map will also not be traversed.
// The direction of scan is provided by the 'direction' argument.  If it is
// 'forwards', the scan proceeds from the origin along successors and outgoing
// dependency edges to all downstream elementary spans starting no later than
// the destination.  If 'backwards', the scan proceeds from the destination
// along predecessors and incoming dependency edges to all upstream elementary
// spans ending no earlier than the origin.
//
// findReachable can be applied twice to find all elementary spans lying on any
// path between origin and destination.  The first pass proceeds in one
// direction with a nil limitMap; the second pass proceeds in the opposite
// direction with the return value of the first pass as its limitMap.  Because
// an incoming dependency only has one origin, but an outgoing dependency can
// have many destinations, performing the backwards scan first may minimize
// the expense of this algorithm.
func findCausallyReachable[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	direction direction,
	includePositiveNontriggeringOrigins bool,
	origin, destination trace.ElementarySpan[T, CP, SP, DP],
	limitMap map[trace.ElementarySpan[T, CP, SP, DP]]struct{},
) map[trace.ElementarySpan[T, CP, SP, DP]]struct{} {
	inLimitMap := func(es trace.ElementarySpan[T, CP, SP, DP]) bool {
		if limitMap == nil {
			return true
		}
		_, ok := limitMap[es]
		return ok
	}
	inRange := func(es trace.ElementarySpan[T, CP, SP, DP]) bool {
		if direction == forwards {
			return comparator.LessOrEqual(es.Start(), destination.Start())
		}
		return comparator.GreaterOrEqual(es.End(), origin.End())
	}
	queue := make([]trace.ElementarySpan[T, CP, SP, DP], 0, len(limitMap))
	enqueue := func(es trace.ElementarySpan[T, CP, SP, DP]) {
		if es == nil {
			return
		}
		if inRange(es) && inLimitMap(es) {
			queue = append(queue, es)
		}
	}
	ret := make(map[trace.ElementarySpan[T, CP, SP, DP]]struct{}, len(limitMap))
	if direction == forwards {
		queue = append(queue, origin)
	} else {
		queue = append(queue, destination)
	}
	for len(queue) != 0 {
		thisES := queue[0]
		queue = queue[1:]
		if _, ok := ret[thisES]; ok {
			continue
		}
		ret[thisES] = struct{}{}
		if direction == forwards {
			if thisES.Successor() != nil {
				enqueue(thisES.Successor())
			}
			if thisES.Outgoing() != nil &&
				(includePositiveNontriggeringOrigins ||
					thisES.Outgoing().TriggeringOrigin() == thisES) {
				for _, dest := range thisES.Outgoing().Destinations() {
					if comparator.LessOrEqual(thisES.End(), dest.Start()) {
						enqueue(dest)
					}
				}
			}
		} else {
			if thisES.Predecessor() != nil {
				enqueue(thisES.Predecessor())
			}
			if thisES.Incoming() != nil {
				if includePositiveNontriggeringOrigins {
					for _, origin := range thisES.Incoming().Origins() {
						if comparator.LessOrEqual(origin.End(), thisES.Start()) {
							enqueue(origin)
						}
					}
				} else {
					if comparator.LessOrEqual(thisES.Incoming().TriggeringOrigin().End(), thisES.Start()) {
						enqueue(thisES.Incoming().TriggeringOrigin())
					}
				}
			}
		}
	}
	return ret
}

// FindAllCausallyReachableElementarySpansBetween returns the set of all
// ElementarySpans that lie on any causal path between the provided endpoints.
func FindAllCausallyReachableElementarySpansBetween[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	includePositiveNontriggeringOrigins bool,
	origin, destination trace.ElementarySpan[T, CP, SP, DP],
) map[trace.ElementarySpan[T, CP, SP, DP]]struct{} {
	backwardsSweep := findCausallyReachable(comparator, backwards, includePositiveNontriggeringOrigins, origin, destination, nil)
	return findCausallyReachable(comparator, forwards, includePositiveNontriggeringOrigins, origin, destination, backwardsSweep)
}

// Seeks a critical path between the two specified events using an exact
// algorithm: it first finds all elementary spans between the provided
// endpoints, then traverses that set in topological order, updating each
// elementary spans' successors for which it offers a better path.  Finally,
// working backwards from the destination, each elementary span on the best
// path is recorded.  This is a specialized implementation of the method in
// https://en.wikipedia.org/wiki/Longest_path_problem#Acyclic_graphs, modified
// to support point-to-point paths instead of just end-to-end ones.
func exactFind[T any, CP, SP, DP fmt.Stringer](
	tr trace.Trace[T, CP, SP, DP],
	strategy Strategy,
	opts *options,
	origin, destination trace.ElementarySpan[T, CP, SP, DP],
) ([]trace.ElementarySpan[T, CP, SP, DP], error) {
	// Find all ElementarySpan lying on any path between 'from' and 'to'.
	onPath := FindAllCausallyReachableElementarySpansBetween(tr.Comparator(), opts.includePositiveNontriggeringOrigins, origin, destination)
	// Returns true if the provided ElementarySpan is on a path between origin
	// and destination.
	isOnPath := func(es trace.ElementarySpan[T, CP, SP, DP]) bool {
		if es == nil {
			return false
		}
		_, ok := onPath[es]
		return ok
	}
	type esState struct {
		es                    trace.ElementarySpan[T, CP, SP, DP]
		bestWeight            float64
		bestPredecessor       *esState
		remainingIncomingDeps int
		outgoingDepsResolved  bool
		visited               bool
	}
	statesByES := make(map[trace.ElementarySpan[T, CP, SP, DP]]*esState, len(onPath))
	// A work queue of ElementarySpan states.  esState instances are enqueued
	// here in topological-sort order.
	queue := make([]*esState, 0, len(onPath))
	// Returns the esState for the provided ElementarySpan, creating it if
	// necessary.
	getESState := func(es trace.ElementarySpan[T, CP, SP, DP]) *esState {
		ess, ok := statesByES[es]
		if !ok {
			if !isOnPath(es) {
				return nil
			}
			ess = &esState{
				es: es,
			}
			if es.Predecessor() != nil && isOnPath(es.Predecessor()) {
				ess.remainingIncomingDeps++
			}
			if es.Incoming() != nil {
				if opts.includePositiveNontriggeringOrigins {
					for _, origin := range es.Incoming().Origins() {
						if isOnPath(origin) &&
							tr.Comparator().LessOrEqual(origin.End(), es.Start()) {
							ess.remainingIncomingDeps++
						}
					}
				} else {
					if isOnPath(es.Incoming().TriggeringOrigin()) &&
						tr.Comparator().LessOrEqual(es.Incoming().TriggeringOrigin().End(), es.Start()) {
						ess.remainingIncomingDeps++
					}
				}
			}
			if ess.remainingIncomingDeps == 0 {
				// The best weight for a ElementarySpan with no incoming dependencies
				// is just its duration.
				ess.bestWeight = tr.Comparator().Diff(es.End(), es.Start())
				// This ES is ready to process.
				queue = append(queue, ess)
			}
			statesByES[es] = ess
		}
		return ess
	}
	// Build an esState for each elementary span between 'from' and 'to'.
	for es := range onPath {
		getESState(es)
	}
	negativeEdgeWarningCount := 0
	var negativeEdgeWarningExample string
	// Resolves a dependency from pred to succ.  If this dependency edge yields
	// a better path weight for succ, update's succ's best predecessor and
	// weight.  If, after this dependency is resolved, succ has no more pending
	// dependencies, it is enqueued in the work queue; this has the effect of
	// enqueueing ElementarySpans in topological-sort order.
	resolveDep := func(pred, succ *esState) {
		if succ == nil || !tr.Comparator().LessOrEqual(pred.es.End(), succ.es.Start()) {
			return
		}
		dependencyEdgeLength := tr.Comparator().Diff(succ.es.Start(), pred.es.End())
		if dependencyEdgeLength >= 0 {
			newWeight := pred.bestWeight + tr.Comparator().Diff(succ.es.End(), succ.es.Start())
			replaceWeight := succ.bestPredecessor == nil
			if !replaceWeight {
				if strategy == PreferLeastWork {
					replaceWeight = newWeight < succ.bestWeight
				} else {
					replaceWeight = newWeight > succ.bestWeight
				}
			}
			if replaceWeight {
				succ.bestPredecessor = pred
				succ.bestWeight = newWeight
			}
		} else {
			if negativeEdgeWarningCount == 0 {
				negativeEdgeWarningExample = fmt.Sprintf("Negative dependency edge detected in critical path algorithm (%s @%v -> %s @%v, diff=%.6fms).  The resulting critical path may be suboptimal.",
					tr.DefaultNamer().SpanName(pred.es.Span()), pred.es.End(),
					tr.DefaultNamer().SpanName(succ.es.Span()), succ.es.Start(),
					dependencyEdgeLength/1e6,
				)
			}
			negativeEdgeWarningCount++
		}
		if succ.remainingIncomingDeps > 0 {
			succ.remainingIncomingDeps--
			if succ.remainingIncomingDeps == 0 {
				queue = append(queue, succ)
			}
		}
	}
	// Resolves all outgoing dependencies from ess.  Idempotent.
	resolveDeps := func(ess *esState) {
		if !ess.outgoingDepsResolved {
			resolveDep(ess, getESState(ess.es.Successor()))
			if ess.es.Outgoing() != nil {
				if opts.includePositiveNontriggeringOrigins || ess.es.Outgoing().TriggeringOrigin() == ess.es {
					for _, dest := range ess.es.Outgoing().Destinations() {
						resolveDep(ess, getESState(dest))
					}
				}
			}
			ess.outgoingDepsResolved = true
		}
	}
	// Iterate through all on-path ElementarySpans, starting with ones with no
	// on-path dependencies.  For each, resolve its outgoing dependencies.
	count := 0
	for len(queue) > 0 {
		thisESS := queue[0]
		queue = queue[1:]
		if thisESS.remainingIncomingDeps != 0 {
			return nil, fmt.Errorf("internal error finding critical path: expected all incoming dependencies to be resolved")
		}
		resolveDeps(thisESS)
		thisESS.visited = true
		count++
		// If the queue is empty but not all ESs between 'from' and 'to' have been
		// visited, a cycle exists among unvisited nodes.  This renders the
		// critical path formally unsolvable, but we can resolve the earliest
		// unvisited ESs to try to break the cycle.
		if len(queue) == 0 && count != len(onPath) {
			// Cycle exists along possible critical paths; attempting to resolve.
			var earliestUnvisited []*esState
			for _, ess := range statesByES {
				if !ess.visited {
					if len(earliestUnvisited) == 0 ||
						tr.Comparator().Less(ess.es.Start(), earliestUnvisited[0].es.Start()) {
						earliestUnvisited = []*esState{ess}
					} else if tr.Comparator().Equal(ess.es.Start(), earliestUnvisited[0].es.Start()) {
						earliestUnvisited = append(earliestUnvisited, ess)
					}
				}
			}
			for _, ess := range earliestUnvisited {
				ess.remainingIncomingDeps = 0
			}
			queue = earliestUnvisited
		}
	}
	// Working backwards from the destination's esState, construct the best path.
	var path []trace.ElementarySpan[T, CP, SP, DP]
	cursor := getESState(destination)
	for {
		if cursor == nil {
			return nil, fmt.Errorf("no path found between critical path endpoints")
		}
		path = append(path, cursor.es)
		if cursor.es == origin {
			break
		}
		cursor = cursor.bestPredecessor
	}
	if negativeEdgeWarningCount > 0 {
		fmt.Printf(
			"%d negative edges encountered while constructing critical path.  Example: %s",
			negativeEdgeWarningCount, negativeEdgeWarningExample,
		)
	}
	// The path was constructed back-to-front, so reverse it.
	slices.Reverse(path)
	return path, nil
}

// findTemporallyReachable returns the set of all non-zero duration
// ElementarySpans in tr that start after or at the same time as
// start and end before or at the same time as end.
func findTemporallyReachable[T any, CP, SP, DP fmt.Stringer](
	tr trace.Trace[T, CP, SP, DP],
	start, end T,
) []trace.ElementarySpan[T, CP, SP, DP] {
	comparator := tr.Comparator()
	hasDuration := func(es trace.ElementarySpan[T, CP, SP, DP]) bool {
		return comparator.Less(es.Start(), es.End())
	}
	inRange := func(es trace.ElementarySpan[T, CP, SP, DP]) bool {
		return comparator.GreaterOrEqual(es.Start(), start) && comparator.LessOrEqual(es.End(), end)
	}
	esAfterRange := func(es trace.ElementarySpan[T, CP, SP, DP]) bool {
		return comparator.Greater(es.End(), end)
	}
	spanAfterRange := func(span trace.Span[T, CP, SP, DP]) bool {
		return comparator.Greater(span.Start(), end)
	}

	// Visit all ESs in the trace and return the ones that are in the range.
	ret := make([]trace.ElementarySpan[T, CP, SP, DP], 0)
	var visitSpan func(span trace.Span[T, CP, SP, DP])
	visitSpan = func(span trace.Span[T, CP, SP, DP]) {
		if spanAfterRange(span) {
			return
		}
		for _, es := range span.ElementarySpans() {
			// Skip ESs that are after the range and any descendants, they will
			// also be after the range.
			if esAfterRange(es) {
				break
			}
			if hasDuration(es) && inRange(es) {
				ret = append(ret, es)
			}
		}
		for _, child := range span.ChildSpans() {
			visitSpan(child)
		}
	}
	for _, rootSpan := range tr.RootSpans() {
		visitSpan(rootSpan)
	}
	return ret
}

type nonCausalEsState[T any, CP, SP, DP fmt.Stringer] struct {
	es            trace.ElementarySpan[T, CP, SP, DP]
	bestWeight    float64
	bestSuccessor *nonCausalEsState[T, CP, SP, DP]
}

// LongestPath is a recursive function that finds the longest path
// from origin to destination based on the adjacency map adj. Weight
// of an ES is its duration.
func longestPath[T any, CP, SP, DP fmt.Stringer](
	origin trace.ElementarySpan[T, CP, SP, DP],
	destination trace.ElementarySpan[T, CP, SP, DP],
	comparator trace.Comparator[T],
	visited map[trace.ElementarySpan[T, CP, SP, DP]]*nonCausalEsState[T, CP, SP, DP],
	adj map[trace.ElementarySpan[T, CP, SP, DP]][]trace.ElementarySpan[T, CP, SP, DP],
) *nonCausalEsState[T, CP, SP, DP] {
	if origin == nil {
		return nil
	}
	// No need to visit the same ES twice due to dynamic programming.
	if _, ok := visited[origin]; ok {
		return visited[origin]
	}
	dur := comparator.Diff(origin.End(), origin.Start())
	// Base case: origin is the destination bestWeight is the duration.
	if origin == destination {
		visited[origin] = &nonCausalEsState[T, CP, SP, DP]{
			es:            origin,
			bestWeight:    dur,
			bestSuccessor: nil,
		}
		return visited[origin]
	}
	// Recursive case: find the best successor in the adjacency map
	// and update the current origin's bestWeight.
	var bs *nonCausalEsState[T, CP, SP, DP]
	for _, des := range adj[origin] {
		nextDest := longestPath(des, destination, comparator, visited, adj)
		if nextDest == nil {
			continue
		}
		// If no best successor has been found, or if the next destination
		// has a longer path to the destination, use it as the best successor.
		if bs == nil || bs.bestWeight < nextDest.bestWeight {
			bs = nextDest
		}
	}
	bw := dur
	if bs != nil {
		bw += bs.bestWeight
	}
	visited[origin] = &nonCausalEsState[T, CP, SP, DP]{
		es:            origin,
		bestWeight:    bw,
		bestSuccessor: bs,
	}
	return visited[origin]
}

// nonCausalExactFind finds the non-causal critical path between the two
// specified ElementarySpans. Instead of using causal dependencies to
// find a path, edges are simply established between two ESs if one ends
// before the other starts. The algorithm is based on the longest path
// problem and dynamic programming.
func nonCausalExactFind[T any, CP, SP, DP fmt.Stringer](
	tr trace.Trace[T, CP, SP, DP],
	origin, destination trace.ElementarySpan[T, CP, SP, DP],
) ([]trace.ElementarySpan[T, CP, SP, DP], error) {
	// Returns true if oes ends before or at the time that des starts.
	happensBefore := func(oes trace.ElementarySpan[T, CP, SP, DP], des trace.ElementarySpan[T, CP, SP, DP]) bool {
		if oes == nil || des == nil || oes == des {
			return false
		}
		return tr.Comparator().LessOrEqual(oes.End(), des.Start())
	}
	if !happensBefore(origin, destination) {
		return nil, fmt.Errorf("origin %s ends before destination %s starts",
			tr.DefaultNamer().SpanName(origin.Span()), tr.DefaultNamer().SpanName(destination.Span()))
	}

	// Find all ElementarySpan temporally between origin and destination.
	between := findTemporallyReachable(tr, origin.End(), destination.Start())
	between = append(between, origin)
	between = append(between, destination)
	// Sort ESs by start time, then by end time to make the ADJ list building
	// more efficient.
	slices.SortFunc(between, func(a, b trace.ElementarySpan[T, CP, SP, DP]) int {
		if tr.Comparator().Less(a.Start(), b.Start()) {
			return -1
		}
		if tr.Comparator().Less(b.Start(), a.Start()) {
			return 1
		}
		if tr.Comparator().Less(a.End(), b.End()) {
			return -1
		}
		if tr.Comparator().Less(b.End(), a.End()) {
			return 1
		}
		return 0
	})

	// Build adjacency map for ES happening before other ESs
	adj := make(map[trace.ElementarySpan[T, CP, SP, DP]][]trace.ElementarySpan[T, CP, SP, DP], len(between))
	for i, es1 := range between[:len(between)-1] {
		var firstEsAfter trace.ElementarySpan[T, CP, SP, DP]
		for _, es2 := range between[i+1:] {
			if !happensBefore(es1, es2) {
				continue
			}
			if firstEsAfter == nil || tr.Comparator().Less(es2.End(), firstEsAfter.End()) {
				firstEsAfter = es2
			}
			if es2 != firstEsAfter && happensBefore(firstEsAfter, es2) {
				// We can skip additional ESs that start after the first ES that
				// finishes after es1. This is due to the triangle inequality.
				// We approximate the first ES that finishes after es1 by updating
				// firstEsAfter as necessary.
				break
			}
			adj[es1] = append(adj[es1], es2)
		}
	}

	// Run the longest path algorithm to find the best path.
	visited := make(map[trace.ElementarySpan[T, CP, SP, DP]]*nonCausalEsState[T, CP, SP, DP], len(between))
	cursor := longestPath(origin, destination, tr.Comparator(), visited, adj)

	// Working forwards from the origin's nonCausalEsState, construct the best path.
	var path []trace.ElementarySpan[T, CP, SP, DP]
	cpWorkDuration := 0.0
	currentTime := cursor.es.Start()
	for {
		if cursor == nil {
			return nil, fmt.Errorf("no path found between critical path endpoints")
		}
		if tr.Comparator().Less(cursor.es.Start(), currentTime) {
			return nil, fmt.Errorf("overlapping ESs found in critical path")
		}
		currentTime = cursor.es.End()
		cpWorkDuration += tr.Comparator().Diff(cursor.es.End(), cursor.es.Start())
		path = append(path, cursor.es)
		if cursor.es == destination {
			break
		}
		if cursor.bestSuccessor == nil {
			return nil, fmt.Errorf("no path found between critical path endpoints")
		}
		cursor = cursor.bestSuccessor
	}
	if len(path) < 2 {
		return nil, fmt.Errorf("no path found between critical path endpoints")
	}
	return path, nil
}
