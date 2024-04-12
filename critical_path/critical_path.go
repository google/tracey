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
	"slices"
	"sort"

	"github.com/google/tracey/trace"
)

// Type specifies a particular type (i.e., pair of endpoints) of critical path
// within a Trace.
type Type string

const (
	// SelectedElementCriticalPath specifies the critical path from the beginning
	// to the end of a selected Span.
	SelectedElementCriticalPath Type = "specified element"
)

// Endpoint represents a Trace critical path endpoint: a Trace span, and a
// point within that span.
type Endpoint[T any, CP, SP, DP fmt.Stringer] struct {
	Span trace.Span[T, CP, SP, DP]
	At   T
}

// Strategy specifies a particular critical path selection strategy.
type Strategy int

const (
	// PreferCausal specifies that causal edges (i.e., Dependencies) should be
	// traversed preferentially to edges to an elementary span's predecessor
	// within its span (i.e., the elementary span just before it in their parent
	// Span).
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
)

// elementarySpanAt returns the ElementarySpan in which the provided Endpoint
// lies, using the provided Comparator.
func elementarySpanAt[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	endpoint *Endpoint[T, CP, SP, DP],
) (trace.ElementarySpan[T, CP, SP, DP], bool) {
	ess := endpoint.Span.ElementarySpans()
	// Find the index of the first span ending at or after the specified point.
	idx := sort.Search(len(ess), func(x int) bool {
		return comparator.LessOrEqual(endpoint.At, ess[x].End())
	})
	if idx == len(ess) {
		return nil, false
	}
	es := ess[idx]
	if comparator.LessOrEqual(es.Start(), endpoint.At) {
		return es, true
	}
	return nil, false
}

// Find seeks a critical path between the two specified endpoints.
func Find[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	strategy Strategy,
	from, to *Endpoint[T, CP, SP, DP],
) ([]trace.ElementarySpan[T, CP, SP, DP], error) {
	switch strategy {
	case PreferCausal, PreferPredecessor, PreferMostProximate, PreferLeastProximate:
		return greedyFind[T, CP, SP, DP](comparator, strategy, from, to)
	case PreferMostWork, PreferLeastWork:
		return exactFind[T, CP, SP, DP](comparator, strategy, from, to)
	default:
		return nil, fmt.Errorf("unsupported critical path strategy")
	}
}

// Seeks a critical path between the two specified events using a greedy
// heuristic: searching recursively backwards from its endpoint (here, 'to'),
// at each step pushing the current event's predecessors onto a stack in
// increasing temporal order, then popping the next event from the stack, until
// the current event is the start point (here, `from`).
func greedyFind[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	strategy Strategy,
	from, to *Endpoint[T, CP, SP, DP],
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
	origin, ok := elementarySpanAt(comparator, from)
	if !ok {
		return nil, fmt.Errorf("can't find critical path: origin span is not running at start point %v", from.At)
	}
	destination, ok := elementarySpanAt(comparator, to)
	if !ok {
		return nil, fmt.Errorf("can't find critical path: destination span is not running at start point %v", from.At)
	}
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
			rev := []trace.ElementarySpan[T, CP, SP, DP]{}
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
		// Add predecessors in order into the stack.
		inSpanPredecessor := thisStep.elementarySpan.Predecessor()
		var causalPredecessor trace.ElementarySpan[T, CP, SP, DP]
		if thisStep.elementarySpan.Incoming() != nil {
			causalPredecessor = thisStep.elementarySpan.Incoming().Origin()
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
func findReachable[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	direction direction,
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
			if thisES.Outgoing() != nil {
				for _, dest := range thisES.Outgoing().Destinations() {
					enqueue(dest)
				}
			}
		} else {
			if thisES.Predecessor() != nil {
				enqueue(thisES.Predecessor())
			}
			if thisES.Incoming() != nil {
				enqueue(thisES.Incoming().Origin())
			}
		}
	}
	return ret
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
	comparator trace.Comparator[T],
	strategy Strategy,
	from, to *Endpoint[T, CP, SP, DP],
) ([]trace.ElementarySpan[T, CP, SP, DP], error) {
	origin, ok := elementarySpanAt(comparator, from)
	if !ok {
		return nil, fmt.Errorf("can't find critical path: origin span is not running at start point %v", from.At)
	}
	destination, ok := elementarySpanAt(comparator, to)
	if !ok {
		return nil, fmt.Errorf("can't find critical path: destination span is not running at start point %v", from.At)
	}
	// Find all ElementarySpan lying on any path between 'from' and 'to'.
	backwardsSweep := findReachable(comparator, backwards, origin, destination, nil)
	onPath := findReachable(comparator, forwards, origin, destination, backwardsSweep)
	// Returns true if the provided ElementarySpan is on a path between origin
	// and destination.
	isOnPath := func(es trace.ElementarySpan[T, CP, SP, DP]) bool {
		_, ok := onPath[es]
		return ok
	}
	type esState struct {
		es                   trace.ElementarySpan[T, CP, SP, DP]
		bestWeight           float64
		bestPredecessor      *esState
		remainingDeps        int
		outgoingDepsResolved bool
		visited              bool
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
				ess.remainingDeps++
			}
			if es.Incoming() != nil && isOnPath(es.Incoming().Origin()) {
				ess.remainingDeps++
			}
			if ess.remainingDeps == 0 {
				// The best weight for a ElementarySpan with no incoming dependencies
				// is just its duration.
				ess.bestWeight = comparator.Diff(es.End(), es.Start())
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
	// Resolves a dependency from pred to succ.  If this dependency edge yields
	// a better path weight for succ, update's succ's best predecessor and
	// weight.  If, after this dependency is resolved, succ has no more pending
	// dependencies, it is enqueued in the work queue; this has the effect of
	// enqueueing ElementarySpans in topological-sort order.
	resolveDep := func(pred, succ *esState) {
		if succ == nil {
			return
		}
		newWeight := pred.bestWeight + comparator.Diff(succ.es.End(), succ.es.Start())
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
		if succ.remainingDeps > 0 {
			succ.remainingDeps--
			if succ.remainingDeps == 0 {
				queue = append(queue, succ)
			}
		}
	}
	// Resolves all outgoing dependencies from ess.  Idempotent.
	resolveDeps := func(ess *esState) {
		if !ess.outgoingDepsResolved {
			resolveDep(ess, getESState(ess.es.Successor()))
			if ess.es.Outgoing() != nil {
				for _, dest := range ess.es.Outgoing().Destinations() {
					resolveDep(ess, getESState(dest))
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
		if thisESS.remainingDeps != 0 {
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
			fmt.Printf("Cycle exists along possible critical paths; attempting to resolve")
			earliestUnvisited := []*esState{}
			for _, ess := range statesByES {
				if !ess.visited {
					if len(earliestUnvisited) == 0 ||
						comparator.Less(ess.es.Start(), earliestUnvisited[0].es.Start()) {
						earliestUnvisited = []*esState{ess}
					} else if comparator.Equal(ess.es.Start(), earliestUnvisited[0].es.Start()) {
						earliestUnvisited = append(earliestUnvisited, ess)
					}
				}
			}
			for _, ess := range earliestUnvisited {
				ess.remainingDeps = 0
			}
			queue = earliestUnvisited
		}
	}
	// Working backwards from the destination's esState, construct the best path.
	path := []trace.ElementarySpan[T, CP, SP, DP]{}
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
	// The path was constructed back-to-front, so reverse it.
	slices.Reverse(path)
	return path, nil
}
