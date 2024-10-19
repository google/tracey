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

// Package antagonism provides types and functions for analyzing antagonisms
// (durations of time in which *victim* work was runnable but didn't run, while
// *antagonism* work ran instead) within traces.
package antagonism

import (
	"container/heap"
	"fmt"
	"strings"

	"github.com/google/tracey/trace"
)

// ElementarySpanner wraps a Trace elementary span for antagonism logging.
type ElementarySpanner[T any, CP, SP, DP fmt.Stringer] interface {
	ElementarySpan() trace.ElementarySpan[T, CP, SP, DP]
	aes() *antagonismElementarySpan[T, CP, SP, DP]
}

type antagonismLogger[T any, CP, SP, DP fmt.Stringer] interface {
	LogAntagonism(
		group *Group[T, CP, SP, DP],
		victims, antagonists map[ElementarySpanner[T, CP, SP, DP]]struct{},
		start, end T,
	) error
}

// Group specifies a single 'antagonism group'.
type Group[T any, CP, SP, DP fmt.Stringer] struct {
	name string
	// Exactly one of {victimFinder, victimElementarySpansFn} must be non-nil.
	victimFinder            *trace.SpanFinder[T, CP, SP, DP]
	victimElementarySpansFn func(trace.Wrapper[T, CP, SP, DP]) ([]trace.ElementarySpan[T, CP, SP, DP], error)
	// Exactly one of {antagonistFinder, antagonistElementarySpansFn} must be
	// non-nil.
	antagonistFinder            *trace.SpanFinder[T, CP, SP, DP]
	antagonistElementarySpansFn func(trace.Wrapper[T, CP, SP, DP]) ([]trace.ElementarySpan[T, CP, SP, DP], error)
}

// NewGroup returns a new antagonism Group with the specified name and span
// finder.
func NewGroup[T any, CP, SP, DP fmt.Stringer](
	name string,
) *Group[T, CP, SP, DP] {
	return &Group[T, CP, SP, DP]{
		name: name,
	}
}

// WithVictimFinder specifies that the receiver's set of victims should match
// the provided SpanFinder, returning the receiver to facilitate streaming.  If
// the receiver has already specified a victim SpanFinder or elementary span
// function, the previous value is overwritten.
func (g *Group[T, CP, SP, DP]) WithVictimFinder(
	victimFinder *trace.SpanFinder[T, CP, SP, DP],
) *Group[T, CP, SP, DP] {
	g.victimFinder = victimFinder
	g.victimElementarySpansFn = nil
	return g
}

// WithVictimElementarySpansFn specifies that the receiver's set of victims
// should be generated, as a list of ElementarySpans, by the provided function.
// If the receiver has already specified a victim SpanFinder or elementary span
// function, the previous value is overwritten.
func (g *Group[T, CP, SP, DP]) WithVictimElementarySpansFn(
	fn func(trace.Wrapper[T, CP, SP, DP]) ([]trace.ElementarySpan[T, CP, SP, DP], error),
) *Group[T, CP, SP, DP] {
	g.victimFinder = nil
	g.victimElementarySpansFn = fn
	return g
}

// WithAntagonistFinder specifies that the receiver's set of antagonists should
// match the provided SpanFinder, returning the receiver to facilitate
// streaming.  If the receiver has already specified an antagonist SpanFinder
// or elementary span function, the previous value is overwritten.
func (g *Group[T, CP, SP, DP]) WithAntagonistFinder(
	antagonistFinder *trace.SpanFinder[T, CP, SP, DP],
) *Group[T, CP, SP, DP] {
	g.antagonistFinder = antagonistFinder
	g.antagonistElementarySpansFn = nil
	return g
}

// WithAntagonistElementarySpansFn specifies that the receiver's set of
// antagonists should be generated, as a list of ElementarySpans, by the
// provided function.  If the receiver has already specified an antagonist
// SpanFinder or elementary span function, the previous value is overwritten.
func (g *Group[T, CP, SP, DP]) WithAntagonistElementarySpansFn(
	fn func(trace.Wrapper[T, CP, SP, DP]) ([]trace.ElementarySpan[T, CP, SP, DP], error),
) *Group[T, CP, SP, DP] {
	g.antagonistFinder = nil
	g.antagonistElementarySpansFn = fn
	return g
}

// Name returns the receiver's group name.
func (g *Group[T, CP, SP, DP]) Name() string {
	return g.name
}

// Analyze the provided trace for antagonisms in each provided antagonism
// group, logging the results to the provided logger.
func Analyze[T any, CP, SP, DP fmt.Stringer](
	t trace.Wrapper[T, CP, SP, DP],
	logger antagonismLogger[T, CP, SP, DP],
	groups []*Group[T, CP, SP, DP],
) error {
	a, err := newAnalyzer(t, groups)
	if err != nil {
		return err
	}
	return a.find(logger)
}

type antagonismElementarySpan[T any, CP, SP, DP fmt.Stringer] struct {
	es                  trace.ElementarySpan[T, CP, SP, DP]
	successor           *antagonismElementarySpan[T, CP, SP, DP]
	pendingDependencies int
}

func newAntagonismElementarySpan[T any, CP, SP, DP fmt.Stringer](
	es trace.ElementarySpan[T, CP, SP, DP],
) *antagonismElementarySpan[T, CP, SP, DP] {
	ret := &antagonismElementarySpan[T, CP, SP, DP]{
		es: es,
	}
	if es.Predecessor() != nil {
		ret.pendingDependencies++
	}
	if es.Incoming() != nil && es.Incoming().Origin() != nil {
		ret.pendingDependencies++
	}
	return ret
}

func (aes *antagonismElementarySpan[T, CP, SP, DP]) ElementarySpan() trace.ElementarySpan[T, CP, SP, DP] {
	return aes.es
}

func (aes *antagonismElementarySpan[T, CP, SP, DP]) aes() *antagonismElementarySpan[T, CP, SP, DP] {
	return aes
}

type heapOrder int

const (
	soonestStartingFirst heapOrder = iota
	soonestEndingFirst
)

// spannerHeap implements heap.Interface for a collection of
// antagonismElementarySpans.
type spannerHeap[T any, CP, SP, DP fmt.Stringer] struct {
	comparator trace.Comparator[T]
	order      heapOrder
	spanners   []ElementarySpanner[T, CP, SP, DP]
}

func newSpannerHeap[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	order heapOrder,
) *spannerHeap[T, CP, SP, DP] {
	ret := &spannerHeap[T, CP, SP, DP]{
		comparator: comparator,
		order:      order,
	}
	heap.Init(ret)
	return ret
}

func (sh *spannerHeap[T, CP, SP, DP]) Len() int {
	return len(sh.spanners)
}

func (sh *spannerHeap[T, CP, SP, DP]) Less(i, j int) bool {
	var iT, jT T
	switch sh.order {
	case soonestStartingFirst:
		iT, jT = sh.spanners[i].ElementarySpan().Start(), sh.spanners[j].ElementarySpan().Start()
	case soonestEndingFirst:
		iT, jT = sh.spanners[i].ElementarySpan().End(), sh.spanners[j].ElementarySpan().End()
	default:
		panic(fmt.Sprintf("unsupported elementary span heap order type %d", sh.order))
	}
	return sh.comparator.Less(iT, jT)
}

func (sh *spannerHeap[T, CP, SP, DP]) Swap(i, j int) {
	sh.spanners[i], sh.spanners[j] = sh.spanners[j], sh.spanners[i]
}

func (sh *spannerHeap[T, CP, SP, DP]) Push(x any) {
	es, ok := x.(*antagonismElementarySpan[T, CP, SP, DP])
	if !ok {
		panic(fmt.Sprintf("unsupported elementary span heap element %T", x))
	}
	sh.spanners = append(sh.spanners, es)
}

func (sh *spannerHeap[T, CP, SP, DP]) Pop() (x any) {
	n := len(sh.spanners)
	x = sh.spanners[n-1]
	sh.spanners = sh.spanners[0 : n-1]
	return x
}

func (sh *spannerHeap[T, CP, SP, DP]) String() string {
	var ret []string
	for _, el := range sh.spanners {
		ret = append(ret, fmt.Sprintf("%s %v-%v", el.ElementarySpan().Span().Payload(),
			el.ElementarySpan().Start(), el.ElementarySpan().End()))
	}
	return strings.Join(ret, ", ")
}

type traceGroup[T any, CP, SP, DP fmt.Stringer] struct {
	debugNamer                           trace.Namer[T, CP, SP, DP]
	group                                *Group[T, CP, SP, DP]
	victimSelection, antagonistSelection *trace.SpanSelection[T, CP, SP, DP]
	victimESs, antagonistESs             map[trace.ElementarySpan[T, CP, SP, DP]]struct{}
	victims, antagonists                 map[ElementarySpanner[T, CP, SP, DP]]struct{}
}

func (tg *traceGroup[T, CP, SP, DP]) isVictim(victim *antagonismElementarySpan[T, CP, SP, DP]) bool {
	if tg.victimESs != nil {
		_, ok := tg.victimESs[victim.es]
		return ok
	}
	return tg.victimSelection.Includes(victim.es.Span())
}

func (tg *traceGroup[T, CP, SP, DP]) isAntagonist(antagonist *antagonismElementarySpan[T, CP, SP, DP]) bool {
	if tg.antagonistESs != nil {
		_, ok := tg.antagonistESs[antagonist.es]
		return ok
	}
	return tg.antagonistSelection.Includes(antagonist.es.Span())
}

func (tg *traceGroup[T, CP, SP, DP]) setAntagonist(antagonist *antagonismElementarySpan[T, CP, SP, DP]) {
	if tg.isAntagonist(antagonist) {
		tg.antagonists[antagonist] = struct{}{}
	}
}

func (tg *traceGroup[T, CP, SP, DP]) setVictim(victim *antagonismElementarySpan[T, CP, SP, DP]) {
	if tg.isVictim(victim) {
		tg.victims[victim] = struct{}{}
	}
}

func (tg *traceGroup[T, CP, SP, DP]) unsetVictim(victim *antagonismElementarySpan[T, CP, SP, DP]) {
	if tg.isVictim(victim) {
		delete(tg.victims, victim)
	}
}

func (tg *traceGroup[T, CP, SP, DP]) retireAntagonist(antagonist *antagonismElementarySpan[T, CP, SP, DP]) {
	if tg.isAntagonist(antagonist) {
		delete(tg.antagonists, antagonist)
	}
}

type analyzer[T any, CP, SP, DP fmt.Stringer] struct {
	comparator                          trace.Comparator[T]
	debugNamer                          trace.Namer[T, CP, SP, DP]
	traceGroups                         []*traceGroup[T, CP, SP, DP]
	running, runnable                   *spannerHeap[T, CP, SP, DP]
	totalESs, retiredESs, backwardsDeps int
	aes                                 map[trace.ElementarySpan[T, CP, SP, DP]]*antagonismElementarySpan[T, CP, SP, DP]
}

func newAnalyzer[T any, CP, SP, DP fmt.Stringer](
	t trace.Wrapper[T, CP, SP, DP],
	groups []*Group[T, CP, SP, DP],
) (*analyzer[T, CP, SP, DP], error) {
	a := &analyzer[T, CP, SP, DP]{
		comparator:  t.Trace().Comparator(),
		debugNamer:  t.Trace().DefaultNamer(),
		traceGroups: make([]*traceGroup[T, CP, SP, DP], len(groups)),
		running:     newSpannerHeap[T, CP, SP, DP](t.Trace().Comparator(), soonestEndingFirst),
		runnable:    newSpannerHeap[T, CP, SP, DP](t.Trace().Comparator(), soonestStartingFirst),
		aes:         map[trace.ElementarySpan[T, CP, SP, DP]]*antagonismElementarySpan[T, CP, SP, DP]{},
	}
	for idx, group := range groups {
		tg := &traceGroup[T, CP, SP, DP]{
			debugNamer:          a.debugNamer,
			group:               group,
			victims:             map[ElementarySpanner[T, CP, SP, DP]]struct{}{},
			antagonists:         map[ElementarySpanner[T, CP, SP, DP]]struct{}{},
			victimSelection:     trace.SelectSpans(t.Trace(), group.victimFinder),
			antagonistSelection: trace.SelectSpans(t.Trace(), group.antagonistFinder),
		}
		if group.victimFinder != nil {
			tg.victimSelection = trace.SelectSpans(t.Trace(), group.victimFinder)
		} else if group.victimElementarySpansFn != nil {
			ess, err := group.victimElementarySpansFn(t)
			if err != nil {
				return nil, err
			}
			tg.victimESs = make(map[trace.ElementarySpan[T, CP, SP, DP]]struct{}, len(ess))
			for _, es := range ess {
				tg.victimESs[es] = struct{}{}
			}
		} else {
			return nil, fmt.Errorf("group '%s' has not defined a victim span finder or victim elementary span function", group.Name())
		}
		if group.antagonistFinder != nil {
			tg.antagonistSelection = trace.SelectSpans(t.Trace(), group.antagonistFinder)
		} else if group.antagonistElementarySpansFn != nil {
			ess, err := group.antagonistElementarySpansFn(t)
			if err != nil {
				return nil, err
			}
			tg.antagonistESs = make(map[trace.ElementarySpan[T, CP, SP, DP]]struct{}, len(ess))
			for _, es := range ess {
				tg.antagonistESs[es] = struct{}{}
			}
		} else {
			return nil, fmt.Errorf("group '%s' has not defined an antagonist span finder or antagonist elementary span function", group.Name())
		}
		a.traceGroups[idx] = tg
	}
	var visitSpan func(span trace.Span[T, CP, SP, DP]) error
	visitSpan = func(span trace.Span[T, CP, SP, DP]) error {
		var lastAES *antagonismElementarySpan[T, CP, SP, DP]
		for _, es := range span.ElementarySpans() {
			a.totalESs++
			aes := newAntagonismElementarySpan(es)
			a.aes[es] = aes
			if aes.pendingDependencies == 0 {
				if err := a.setRunnable(aes); err != nil {
					return err
				}
			}
			if lastAES != nil {
				lastAES.successor = aes
			}
			lastAES = aes
		}
		for _, child := range span.ChildSpans() {
			if err := visitSpan(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, rootSpan := range t.Trace().RootSpans() {
		if err := visitSpan(rootSpan); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *analyzer[T, CP, SP, DP]) find(
	logger antagonismLogger[T, CP, SP, DP],
) error {
	lastTimeValid := false
	var lastTime, nextTime T
	var nextEndingRunning, nextStartingRunnable *antagonismElementarySpan[T, CP, SP, DP]
	for a.running.Len() > 0 || a.runnable.Len() > 0 || nextEndingRunning != nil || nextStartingRunnable != nil {
		// Find the next ending elementary span in `running`, and the next starting
		// one in `runnable`.
		if nextEndingRunning == nil && a.running.Len() > 0 {
			nextEndingRunning = heap.Pop(a.running).(*antagonismElementarySpan[T, CP, SP, DP])
		}
		if nextStartingRunnable == nil && a.runnable.Len() > 0 {
			nextStartingRunnable = heap.Pop(a.runnable).(*antagonismElementarySpan[T, CP, SP, DP])
		}
		var drawFromRunning bool
		if nextEndingRunning != nil && nextStartingRunnable != nil {
			// Prefer to draw from running if there's a tie.
			drawFromRunning = a.comparator.LessOrEqual(nextEndingRunning.es.End(), nextStartingRunnable.es.Start())
		} else if nextEndingRunning != nil {
			drawFromRunning = true
		} else if nextStartingRunnable != nil {
			drawFromRunning = false
		} else {
			return fmt.Errorf("expected at least one elementary span to be running or runnable, but none was")
		}
		if drawFromRunning {
			nextTime = nextEndingRunning.es.End()
		} else {
			nextTime = nextStartingRunnable.es.Start()
		}
		if lastTimeValid && !a.comparator.Equal(lastTime, nextTime) {
			if !a.comparator.Greater(lastTime, nextTime) {
				// Backwards dependency edges result in apparently-negative antagonism
				// durations.  Don't report those, and warn about the backwards edges.
				for _, g := range a.traceGroups {
					logger.LogAntagonism(
						g.group, g.victims, g.antagonists, lastTime, nextTime,
					)
				}
			}
		}
		if !drawFromRunning {
			if err := a.setRunning(nextStartingRunnable); err != nil {
				return err
			}
			nextStartingRunnable = nil
		}
		if drawFromRunning {
			if err := a.retire(nextEndingRunning); err != nil {
				return err
			}
			nextEndingRunning = nil
		}
		lastTime = nextTime
		lastTimeValid = true
	}
	if a.retiredESs != a.totalESs {
		return fmt.Errorf("After antagonism analysis, %d spans remain blocked, indicating causal errors in the trace", a.totalESs-a.retiredESs)
	}
	if a.backwardsDeps > 0 {
		fmt.Printf("Warning: Trace had %d backwards dependency edges (i.e., effect preceded cause).  This can skew antagonism analysis.\n", a.backwardsDeps)
	}
	return nil
}

func (a *analyzer[T, CP, SP, DP]) setRunnable(aes *antagonismElementarySpan[T, CP, SP, DP]) error {
	heap.Push(a.runnable, aes)
	for _, g := range a.traceGroups {
		g.setVictim(aes)
	}
	return nil
}

func (a *analyzer[T, CP, SP, DP]) setRunning(nextStartingRunnable *antagonismElementarySpan[T, CP, SP, DP]) error {
	heap.Push(a.running, nextStartingRunnable)
	for _, g := range a.traceGroups {
		g.unsetVictim(nextStartingRunnable)
		g.setAntagonist(nextStartingRunnable)
	}
	return nil
}

func (a *analyzer[T, CP, SP, DP]) resolveDependency(
	aes *antagonismElementarySpan[T, CP, SP, DP],
) error {
	if aes == nil {
		return nil
	}
	aes.pendingDependencies--
	if aes.pendingDependencies == 0 {
		if err := a.setRunnable(aes); err != nil {
			return err
		}
	}
	return nil
}

func (a *analyzer[T, CP, SP, DP]) retire(
	nextEndingRunning *antagonismElementarySpan[T, CP, SP, DP],
) error {
	a.retiredESs++
	for _, g := range a.traceGroups {
		g.retireAntagonist(nextEndingRunning)
	}
	if err := a.resolveDependency(nextEndingRunning.successor); err != nil {
		return err
	}
	if nextEndingRunning.ElementarySpan().Outgoing() != nil {
		for _, destES := range nextEndingRunning.ElementarySpan().Outgoing().Destinations() {
			if a.comparator.Greater(nextEndingRunning.ElementarySpan().End(), destES.Start()) {
				a.backwardsDeps++
			}
			if err := a.resolveDependency(a.aes[destES]); err != nil {
				return err
			}
		}
	}
	return nil
}
