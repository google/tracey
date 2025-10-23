/*
Copyright 2025 Google Inc.

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
package drag

import (
	"fmt"
	"sort"

	"github.com/google/tracey/critical_path"
	"github.com/google/tracey/trace"
)

// A struct used to track per-elementary-span activation timings
type activity[T any, CP, SP, DP fmt.Stringer] struct {
	es                           trace.ElementarySpan[T, CP, SP, DP]
	earliestStart, latestStart   T
	earliestFinish, latestFinish T
}

func (a *activity[T, CP, SP, DP]) start() T {
	return a.earliestStart
}

func (a *activity[T, CP, SP, DP]) finish() T {
	return a.latestFinish
}

func (a *activity[T, CP, SP, DP]) payload() *activity[T, CP, SP, DP] {
	return a
}

type activityBuilder[T any, CP, SP, DP fmt.Stringer] struct {
	a                                 *activity[T, CP, SP, DP]
	pendingIncomingDependencies       int
	pendingOutgoingDependencies       int
	earliestStartSet, latestFinishSet bool
}

// Computes activities for each provided eligible elementary span, as computed
// from the provided set of origin and destination spans, and returns those
// activities, with earliestStart, earliestFinish, latestStart, and
// latestFinish populated.
func computeActivity[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	eligibleElementarySpans map[trace.ElementarySpan[T, CP, SP, DP]]struct{},
	origins, destinations []trace.ElementarySpan[T, CP, SP, DP],
) ([]*activity[T, CP, SP, DP], error) {
	if len(origins) == 0 {
		return nil, fmt.Errorf("no origin nodes defined")
	}
	if len(destinations) == 0 {
		return nil, fmt.Errorf("no destination nodes defined")
	}
	// Compute the earliest start among all origins.  We presume that this is the
	// earliest any work could happen, and that any origin could start this
	// early.
	earliestStart := origins[0].Start()
	for _, o := range origins[1:] {
		if comparator.Less(o.Start(), earliestStart) {
			earliestStart = o.Start()
		}
	}
	// Compute the latest finish among all destinations.  We presume that this is
	// the latest any work could happen, and that any destination could finish
	// this late.
	latestFinish := destinations[0].End()
	for _, d := range destinations[1:] {
		if comparator.Greater(d.End(), latestFinish) {
			latestFinish = d.End()
		}
	}
	// Returns true if the provided elementary span is eligible to have float.
	isEligible := func(es trace.ElementarySpan[T, CP, SP, DP]) bool {
		if es == nil {
			return false
		}
		if len(eligibleElementarySpans) == 0 {
			return true
		}
		_, ok := eligibleElementarySpans[es]
		return ok
	}
	ret := []*activity[T, CP, SP, DP]{}
	activityBuildersByES := make(map[trace.ElementarySpan[T, CP, SP, DP]]*activityBuilder[T, CP, SP, DP], len(eligibleElementarySpans))
	// Returns the activity for the provided elementary span, or nil if that span
	// is not in the eligible set.
	getActivityBuilder := func(es trace.ElementarySpan[T, CP, SP, DP]) *activityBuilder[T, CP, SP, DP] {
		if !isEligible(es) {
			return nil
		}
		if ab, ok := activityBuildersByES[es]; ok {
			return ab
		}
		ab := &activityBuilder[T, CP, SP, DP]{
			a: &activity[T, CP, SP, DP]{
				es: es,
			},
		}
		ret = append(ret, ab.a)
		if es.Predecessor() != nil && isEligible(es.Predecessor()) {
			ab.pendingIncomingDependencies++
		}
		if es.Incoming() != nil &&
			isEligible(es.Incoming().TriggeringOrigin()) {
			ab.pendingIncomingDependencies++
		}
		if es.Successor() != nil && isEligible(es.Successor()) {
			ab.pendingOutgoingDependencies++
		}
		if es.Outgoing() != nil {
			for _, dest := range es.Outgoing().Destinations() {
				if isEligible(dest) {
					ab.pendingOutgoingDependencies++
				}
			}
		}
		activityBuildersByES[es] = ab
		return ab
	}
	// Perform a forwards pass from all origin nodes to all destination nodes,
	// setting earliestStart and earliestFinish.
	queue := make([]*activityBuilder[T, CP, SP, DP], 0, len(origins))
	for _, o := range origins {
		ab := getActivityBuilder(o)
		if ab == nil {
			return nil, fmt.Errorf("origin node is not in the eligible set")
		}
		ab.a.earliestStart = earliestStart
		queue = append(queue, ab)
	}
	var cursor *activityBuilder[T, CP, SP, DP]
	enqueueForwards := func(es trace.ElementarySpan[T, CP, SP, DP]) {
		ab := getActivityBuilder(es)
		if ab != nil {
			ab.pendingIncomingDependencies--
			if !ab.earliestStartSet || comparator.Greater(cursor.a.earliestFinish, ab.a.earliestStart) {
				ab.earliestStartSet = true
				ab.a.earliestStart = cursor.a.earliestFinish
			}
			if ab.pendingIncomingDependencies == 0 {
				queue = append(queue, ab)
			}
		}
	}
	for len(queue) != 0 {
		cursor, queue = queue[0], queue[1:]
		if cursor.pendingIncomingDependencies != 0 {
			return nil, fmt.Errorf("forwards pass: visiting a node that still has a predecessor")
		}
		cursor.a.earliestFinish = comparator.Add(
			cursor.a.earliestStart,
			comparator.Diff(cursor.a.es.End(), cursor.a.es.Start()),
		)
		if cursor.a.es.Successor() != nil {
			enqueueForwards(cursor.a.es.Successor())
		}
		if cursor.a.es.Outgoing() != nil {
			for _, destination := range cursor.a.es.Outgoing().Destinations() {
				enqueueForwards(destination)
			}
		}
	}
	// Now perform a backwards pass from all destination nodes to all origin
	// nodes, setting latestFinish and latestStart.
	for _, d := range destinations {
		ab := getActivityBuilder(d)
		if ab == nil {
			return nil, fmt.Errorf("destination node is not in the eligible set")
		}
		ab.a.latestFinish = latestFinish
		queue = append(queue, ab)
	}
	enqueueBackwards := func(es trace.ElementarySpan[T, CP, SP, DP]) {
		ab := getActivityBuilder(es)
		if ab != nil {
			ab.pendingOutgoingDependencies--
			if !ab.latestFinishSet || comparator.Less(cursor.a.latestStart, ab.a.latestFinish) {
				ab.latestFinishSet = true
				ab.a.latestFinish = cursor.a.latestStart
			}
			if ab.pendingOutgoingDependencies == 0 {
				queue = append(queue, ab)
			}
		}
	}
	for len(queue) != 0 {
		cursor, queue = queue[0], queue[1:]
		if cursor.pendingOutgoingDependencies != 0 {
			return nil, fmt.Errorf("backwards pass: visiting a node that still has a successor")
		}
		// add(ES, start-end) == subtract(ES, end-start).
		cursor.a.latestStart = comparator.Add(
			cursor.a.latestFinish,
			comparator.Diff(cursor.a.es.Start(), cursor.a.es.End()),
		)
		if cursor.a.es.Predecessor() != nil {
			enqueueBackwards(cursor.a.es.Predecessor())
		}
		if cursor.a.es.Incoming() != nil {
			enqueueBackwards(cursor.a.es.Incoming().TriggeringOrigin())
		}
	}
	return ret, nil
}

// Given origin and destination elementary spans, computes the activities of
// all elementary spans lying on any path between origin and destination.
// Slack values computed from these activities should be interpreted as
// being entirely independent from any work not lying on a path between origin
// and destination; any dependencies from such work are not considered when
// computing activities.
func computeActivityBetweenEndpoints[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	origin, destination trace.ElementarySpan[T, CP, SP, DP],
) ([]*activity[T, CP, SP, DP], error) {
	// Find all elementary spans lying on any path between origin and
	// destination.  These are the only elementary spans that can have either
	// drag or slack for the provided endpoints.
	return computeActivity(
		comparator,
		criticalpath.FindAllCausallyReachableElementarySpansBetween(
			comparator,
			false, /* includePositiveNonTriggeringOrigins */
			origin,
			destination,
		),
		[]trace.ElementarySpan[T, CP, SP, DP]{origin},
		[]trace.ElementarySpan[T, CP, SP, DP]{destination},
	)
}

// Given a trace, computes the activities of all elementary spans in the trace.
// Slack values computed from these activities should be interpreted as
// 'global', reflecting all known dependencies.
func computeGlobalActivity[T any, CP, SP, DP fmt.Stringer](
	t trace.Trace[T, CP, SP, DP],
) ([]*activity[T, CP, SP, DP], error) {
	origins := []trace.ElementarySpan[T, CP, SP, DP]{}
	destinations := []trace.ElementarySpan[T, CP, SP, DP]{}
	var visit func(span trace.Span[T, CP, SP, DP])
	visit = func(span trace.Span[T, CP, SP, DP]) {
		for _, es := range span.ElementarySpans() {
			if es.Predecessor() == nil && es.Incoming() == nil {
				origins = append(origins, es)
			}
			if es.Successor() == nil && es.Outgoing() == nil {
				destinations = append(destinations, es)
			}
		}
		for _, child := range span.ChildSpans() {
			visit(child)
		}
	}
	for _, rootSpan := range t.RootSpans() {
		visit(rootSpan)
	}
	return computeActivity(
		t.Comparator(),
		nil,
		origins,
		destinations,
	)
}

type interval[T, P any] interface {
	start() T
	finish() T
	payload() P
}

func intervalCenter[T, P any](
	comparator trace.Comparator[T],
	i interval[T, P],
) T {
	width := comparator.Diff(i.finish(), i.start())
	return comparator.Add(i.start(), width/2.0)
}

type intervalTreeNode[T, P any] struct {
	center                              T
	left, right                         *intervalTreeNode[T, P]
	intervalsByStart, intervalsByFinish []interval[T, P]
}

// newIntervalTree builds a new centered interval tree from the provided list
// of intervals, sorted by their center point increasing, and returns the root
// node of that tree.
func newIntervalTree[T, P any](
	comparator trace.Comparator[T],
	intervalsSortedByCenter []interval[T, P],
) *intervalTreeNode[T, P] {
	if len(intervalsSortedByCenter) == 0 {
		return nil
	}
	midIdx := len(intervalsSortedByCenter) / 2
	root := &intervalTreeNode[T, P]{
		center: intervalCenter(comparator, intervalsSortedByCenter[midIdx]),
	}
	var leftIntervalsSortedByCenter, rightIntervalsSortedByCenter []interval[T, P]
	for _, i := range intervalsSortedByCenter {
		switch {
		case comparator.Less(i.finish(), root.center):
			leftIntervalsSortedByCenter = append(leftIntervalsSortedByCenter, i)
		case comparator.Greater(i.start(), root.center):
			rightIntervalsSortedByCenter = append(rightIntervalsSortedByCenter, i)
		default:
			root.intervalsByStart = append(root.intervalsByStart, i)
			root.intervalsByFinish = append(root.intervalsByFinish, i)
		}
	}
	sort.Slice(root.intervalsByStart, func(a, b int) bool {
		return comparator.Less(
			root.intervalsByStart[a].start(),
			root.intervalsByStart[b].start(),
		)
	})
	sort.Slice(root.intervalsByFinish, func(a, b int) bool {
		return comparator.Less(
			root.intervalsByFinish[a].finish(),
			root.intervalsByFinish[b].finish(),
		)
	})
	root.left = newIntervalTree(comparator, leftIntervalsSortedByCenter)
	root.right = newIntervalTree(comparator, rightIntervalsSortedByCenter)
	return root
}

// findIntersecting invokes the provided addIntersectingCallback on every
// interval in the receiver that intersects with the provided moment.
func (it *intervalTreeNode[T, P]) findIntersecting(
	comparator trace.Comparator[T],
	moment T,
	addIntersecting func(interval[T, P]),
) {
	if it == nil {
		return
	}
	switch d := comparator.Diff(moment, it.center); {
	case d == 0:
		// moment *is* the center of this node, so all intervals overlapping the
		// center can be added.
		for _, ival := range it.intervalsByStart {
			addIntersecting(ival)
		}
	case d < 0:
		// moment is before the center of this node, so add all intervals starting
		// before moment (and not also ending before it) then recurse to the left.
		for _, ival := range it.intervalsByStart {
			if comparator.GreaterOrEqual(ival.start(), moment) {
				break
			}
			if comparator.Greater(ival.finish(), moment) {
				addIntersecting(ival)
			}
		}
		it.left.findIntersecting(comparator, moment, addIntersecting)
	default:
		// moment is after the center of this node, so add all intervals ending
		// after moment (and not also start after it) then recurse to the right.
		for idx := len(it.intervalsByFinish) - 1; idx > 0; idx-- {
			ival := it.intervalsByFinish[idx]
			if comparator.LessOrEqual(ival.finish(), moment) {
				break
			}
			if comparator.Less(ival.start(), moment) {
				addIntersecting(ival)
			}
		}
		it.right.findIntersecting(comparator, moment, addIntersecting)
	}
}

type endpoint[T, P any] struct {
	moment T
	i      interval[T, P]
}

// intersectionFinder supports finding intersections between sets of intervals
// and both single points and other intervals.
type intersectionFinder[T, P any] struct {
	comparator      trace.Comparator[T]
	intervalTree    *intervalTreeNode[T, P]
	sortedEndpoints []*endpoint[T, P]
}

func newIntersectionFinder[T, P any](
	comparator trace.Comparator[T],
	intervals []interval[T, P],
) *intersectionFinder[T, P] {
	intervalsSortedByCenter := make(
		[]interval[T, P], 0, len(intervals),
	)
	sortedEndpoints := make(
		[]*endpoint[T, P], 0, 2*len(intervals),
	)
	for _, i := range intervals {
		intervalsSortedByCenter = append(intervalsSortedByCenter, i)
		sortedEndpoints = append(
			sortedEndpoints,
			&endpoint[T, P]{i.start(), i},
			&endpoint[T, P]{i.finish(), i},
		)
	}
	sort.Slice(intervalsSortedByCenter, func(a, b int) bool {
		return comparator.Less(
			intervalCenter(comparator, intervalsSortedByCenter[a]),
			intervalCenter(comparator, intervalsSortedByCenter[b]),
		)
	})
	sort.Slice(sortedEndpoints, func(a, b int) bool {
		return comparator.Less(
			sortedEndpoints[a].moment, sortedEndpoints[b].moment,
		)
	})
	return &intersectionFinder[T, P]{
		comparator:      comparator,
		intervalTree:    newIntervalTree(comparator, intervalsSortedByCenter),
		sortedEndpoints: sortedEndpoints,
	}
}

// intersectingIntervals invokes the provided addIntersecting function on each
// interval in the receiver that overlaps the provided interval.
//
// The provided addIntersecting function may receive duplicates, and will
// receive the provided interval as an intersection.  Otherwise non-overlapping
// intervals that share an endpoint (e.g., [0, 10] and [10, 20], which share
// 10) are considered intersecting.  If any of these behaviors is not desired,
// the provided addIntersecting function may apply filters.
func (f *intersectionFinder[T, P]) intersectingIntervals(
	i interval[T, P],
	addIntersecting func(interval[T, P]),
) error {
	//  An interval I has an extent from I.start to I.finish.
	//
	// Given a 'query' interval Q, we wish to find all intervals which intersect
	// Q.  There are two ways for an interval J to intersect with Q:
	// 1. An endpoint of J lies within Q:
	//     [    Q    ]
	//         [    J    ]
	// 2. J completely encloses Q:
	//     [    J    ]
	//      [  Q  ]
	// We can capture case 1 using sortedEndpoints: all endpoints lying between
	// Q.start and Q.finish are of intersecting intervals.
	// In case 2, all moments in Q lie inside of J, so we can use intervalTree to
	// find all intervals containing any moment in Q (e.g., Q.start).
	// The union of all intervals found with both methods contains all intervals
	// intersecting with Q.
	firstIdx := sort.Search(len(f.sortedEndpoints), func(idx int) bool {
		return f.comparator.GreaterOrEqual(f.sortedEndpoints[idx].moment, i.start())
	})
	lastIdx := sort.Search(len(f.sortedEndpoints), func(idx int) bool {
		return f.comparator.Less(i.finish(), f.sortedEndpoints[idx].moment)
	})
	if firstIdx > lastIdx {
		return fmt.Errorf("can't find intersecting intervals: interval is negative or interval endpoint slice isn't sorted")
	}
	for _, ep := range f.sortedEndpoints[firstIdx:lastIdx] {
		addIntersecting(ep.i)
	}
	f.intervalTree.findIntersecting(f.comparator, i.start(), addIntersecting)
	return nil
}

// intersectingIntervalAccumulator is a helper type for accumulating
// intersecting intervals; its addIntersecting method can be used as the
// addIntersecting argument to intersectionFinder.
type intersectingIntervalAccumulator[T any, P comparable] struct {
	comparator                 trace.Comparator[T]
	intersectee                interval[T, P]
	intersecteeIsInstantaneous bool
	intersecting               map[interval[T, P]]struct{}
}

func newIntersectingIntervalAccumulator[T any, P comparable](
	comparator trace.Comparator[T],
	intersectee interval[T, P],
) *intersectingIntervalAccumulator[T, P] {
	return &intersectingIntervalAccumulator[T, P]{
		comparator:                 comparator,
		intersectee:                intersectee,
		intersecteeIsInstantaneous: comparator.Equal(intersectee.start(), intersectee.finish()),
		intersecting:               map[interval[T, P]]struct{}{},
	}
}

func (iia *intersectingIntervalAccumulator[T, P]) addIntersecting(ival interval[T, P]) {
	// Intervals don't intersect...
	if ival.payload() == iia.intersectee.payload() || // with themselves
		iia.comparator.Equal(ival.start(), iia.intersectee.finish()) || // if they have zero overlap
		iia.comparator.Equal(ival.finish(), iia.intersectee.start()) { // if they have zero overlap
	} else {
		iia.intersecting[ival] = struct{}{}
	}
}

func (iia *intersectingIntervalAccumulator[T, P]) get() map[interval[T, P]]struct{} {
	return iia.intersecting
}

// Finder finds drag and slack of provided trace ElementarySpans.
type Finder[T any, CP, SP, DP fmt.Stringer] struct {
	slackActivityMap   map[trace.ElementarySpan[T, CP, SP, DP]]*activity[T, CP, SP, DP]
	comparator         trace.Comparator[T]
	intersectionFinder *intersectionFinder[T, *activity[T, CP, SP, DP]]
}

func newFinder[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	activities []*activity[T, CP, SP, DP],
) *Finder[T, CP, SP, DP] {
	intervals := make([]interval[T, *activity[T, CP, SP, DP]], 0, len(activities))
	for _, a := range activities {
		intervals = append(intervals, a)
	}
	slackActivityMap := make(map[trace.ElementarySpan[T, CP, SP, DP]]*activity[T, CP, SP, DP], len(intervals))
	for _, a := range intervals {
		slackActivityMap[a.payload().es] = a.payload()
	}
	return &Finder[T, CP, SP, DP]{
		slackActivityMap:   slackActivityMap,
		comparator:         comparator,
		intersectionFinder: newIntersectionFinder(comparator, intervals),
	}
}

// NewGlobalFinder returns a new Finder operating on the entire provided trace.
// It computes the slack activity for every ElementarySpan in the trace, which
// it can use to quickly compute any ElementarySpan's global slack or global
// drag.
func NewGlobalFinder[T any, CP, SP, DP fmt.Stringer](
	tr trace.Trace[T, CP, SP, DP],
) (*Finder[T, CP, SP, DP], error) {
	globalActivity, err := computeGlobalActivity(tr)
	if err != nil {
		return nil, fmt.Errorf("failed to construct drag finder: %w", err)
	}
	return newFinder(tr.Comparator(), globalActivity), nil
}

// NewEndpointFinder returns a new Finder operating on the subtrace consisting
// of all paths that lie between the provided pair of ElementarySpans.  It
// computes the slack activity of only that subtrace, and uses that to then
// compute slack or drag relative to those endpoints.
func NewEndpointFinder[T any, CP, SP, DP fmt.Stringer](
	tr trace.Trace[T, CP, SP, DP],
	origin, destination trace.ElementarySpan[T, CP, SP, DP],
) (*Finder[T, CP, SP, DP], error) {
	activityBetweenEndpoints, err := computeActivityBetweenEndpoints(
		tr.Comparator(), origin, destination,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to construct drag finder: %w", err)
	}
	return newFinder(tr.Comparator(), activityBetweenEndpoints), nil
}

func (f *Finder[T, CP, SP, DP]) slack(a *activity[T, CP, SP, DP]) float64 {
	return f.comparator.Diff(a.finish(), a.start()) -
		f.comparator.Diff(a.payload().es.End(), a.payload().es.Start())
}

// Slack returns the slack of the provided ElementarySpan.
func (f *Finder[T, CP, SP, DP]) Slack(es trace.ElementarySpan[T, CP, SP, DP]) (float64, error) {
	ival, ok := f.slackActivityMap[es]
	if !ok {
		return 0, fmt.Errorf("can't find elementary span drag: no activities for provided elementary span")
	}
	return f.slack(ival.payload()), nil
}

type esInterval[T any, CP, SP, DP fmt.Stringer] struct {
	a *activity[T, CP, SP, DP]
}

func (esi *esInterval[T, CP, SP, DP]) start() T {
	return esi.a.es.Start()
}

func (esi *esInterval[T, CP, SP, DP]) finish() T {
	return esi.a.es.End()
}

func (esi *esInterval[T, CP, SP, DP]) payload() *activity[T, CP, SP, DP] {
	return esi.a
}

func (f *Finder[T, CP, SP, DP]) esInterval(es trace.ElementarySpan[T, CP, SP, DP]) (interval[T, *activity[T, CP, SP, DP]], error) {
	ival, ok := f.slackActivityMap[es]
	if !ok {
		return nil, fmt.Errorf("can't find elementary span drag: no activities for provided elementary span")
	}
	return &esInterval[T, CP, SP, DP]{ival.payload()}, nil
}

// Drag returns the drag of the provided ElementarySpan, reckoned against the
// receiving Finder's slack activity.  An ElementarySpan ES's drag is ES's
// duration, or the minimum slack among all ElementarySpans parallel to ES.
// A span A is parallel to ES if (A.EarliestStart, A.LatestFinish) has nonzero
// overlap with (ES.Start, ES.End).
func (f *Finder[T, CP, SP, DP]) Drag(
	es trace.ElementarySpan[T, CP, SP, DP],
) (float64, error) {
	ival, err := f.esInterval(es)
	if err != nil {
		return 0, err
	}
	iia := newIntersectingIntervalAccumulator(f.comparator, ival)
	if err := f.intersectionFinder.intersectingIntervals(ival, iia.addIntersecting); err != nil {
		return 0, fmt.Errorf("can't compute drag: %w", err)
	}
	ivalDrag := f.comparator.Diff(ival.payload().es.End(), ival.payload().es.Start())
	for intersectingIval := range iia.get() {
		totalSlack := f.slack(intersectingIval.payload())
		if totalSlack < ivalDrag {
			ivalDrag = totalSlack
		}
	}
	return ivalDrag, nil
}
