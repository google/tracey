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

package trace

import (
	"errors"
	"fmt"
	"strings"
)

type checkHelper struct {
	logAll  bool // True if all errors should be logged to stdout.
	lastErr error
}

func (ch *checkHelper) error(err error) {
	if ch.logAll {
		fmt.Println(err.Error())
	}
	ch.lastErr = err
}

type esData[T any, CP, SP, DP fmt.Stringer] struct {
	es           ElementarySpan[T, CP, SP, DP]
	predecessors map[ElementarySpan[T, CP, SP, DP]]struct{}
}

func newESData[T any, CP, SP, DP fmt.Stringer](es ElementarySpan[T, CP, SP, DP]) *esData[T, CP, SP, DP] {
	return &esData[T, CP, SP, DP]{
		es: es,
	}
}

func (esd *esData[T, CP, SP, DP]) addPred(t Trace[T, CP, SP, DP], pred ElementarySpan[T, CP, SP, DP]) {
	if esd.predecessors == nil {
		esd.predecessors = map[ElementarySpan[T, CP, SP, DP]]struct{}{}
	}
	esd.predecessors[pred] = struct{}{}
}

func (esd *esData[T, CP, SP, DP]) removePred(t Trace[T, CP, SP, DP], pred ElementarySpan[T, CP, SP, DP]) bool {
	if esd.predecessors == nil {
		return false
	}
	_, ok := esd.predecessors[pred]
	delete(esd.predecessors, pred)
	if len(esd.predecessors) == 0 {
		esd.predecessors = nil
	}
	return ok
}

func getESIdxInSpan[T any, CP, SP, DP fmt.Stringer](es ElementarySpan[T, CP, SP, DP]) int {
	esIdx := -1
	for cursor := es; cursor != nil; cursor, esIdx = cursor.Predecessor(), esIdx+1 {
	}
	return esIdx
}

func findCycles[T any, CP, SP, DP fmt.Stringer](
	t Trace[T, CP, SP, DP],
	entryESs []ElementarySpan[T, CP, SP, DP],
	ch *checkHelper,
) (visitedESs int) {
	esDataByES := map[ElementarySpan[T, CP, SP, DP]]*esData[T, CP, SP, DP]{}
	var getESData func(es ElementarySpan[T, CP, SP, DP]) *esData[T, CP, SP, DP]
	getESData = func(es ElementarySpan[T, CP, SP, DP]) *esData[T, CP, SP, DP] {
		esd, ok := esDataByES[es]
		if !ok {
			visitedESs++
			esd = &esData[T, CP, SP, DP]{
				es: es,
			}
			if es.Span() == nil {
				ch.error(fmt.Errorf("elementary span has no parent span"))
			}
			esDataByES[es] = esd
			if es.Predecessor() != nil {
				esd.addPred(t, es.Predecessor())
			}
			if es.Incoming() != nil {
				for _, origin := range es.Incoming().Origins() {
					if t.Comparator().LessOrEqual(origin.End(), es.Start()) {
						esd.addPred(t, origin)
					}
				}
			}
		}
		return esd
	}
	queue := make([]*esData[T, CP, SP, DP], len(entryESs))
	for idx, entryES := range entryESs {
		queue[idx] = getESData(entryES)
	}
	removePred := func(es, pred ElementarySpan[T, CP, SP, DP]) bool {
		esd := getESData(es)
		if !esd.removePred(t, pred) {
			return false
		}
		if len(esd.predecessors) == 0 {
			queue = append(queue, esd)
		}
		return true
	}
	var thisESD *esData[T, CP, SP, DP]
	for len(queue) > 0 {
		thisESD, queue = queue[0], queue[1:]
		if thisESD.es.Successor() != nil {
			if !removePred(thisESD.es.Successor(), thisESD.es) {
				ch.error(fmt.Errorf("internal error: failed to remove expected predecessor elementary span from predecessor list"))
				return 0
			}
		}
		outgoing := thisESD.es.Outgoing()
		if outgoing != nil {
			for _, destination := range outgoing.Destinations() {
				if t.Comparator().LessOrEqual(thisESD.es.End(), destination.Start()) {
					if !removePred(destination, thisESD.es) {
						ch.error(fmt.Errorf("internal error: failed to remove expected origin elementary span from predecessor list"))
						return 0
					}
				}
			}
		}
		delete(esDataByES, thisESD.es)
	}
	if len(esDataByES) > 0 {
		errorStrs := []string{"dependency cycle exists among elementary spans:"}
		outstandingESMap := map[ElementarySpan[T, CP, SP, DP]]struct{}{}
		for _, esd := range esDataByES {
			outstandingESMap[esd.es] = struct{}{}
		}
		for _, esd := range esDataByES {
			errorStrs = append(
				errorStrs,
				fmt.Sprintf("  %s:%d (%v-%v)", t.DefaultNamer().SpanName(esd.es.Span()), getESIdxInSpan(esd.es), esd.es.Start(), esd.es.End()),
			)
			for pred := range esd.predecessors {
				if _, ok := outstandingESMap[pred]; ok {
					errorStrs = append(
						errorStrs,
						fmt.Sprintf("    [B] %s:%d (%v-%v)", t.DefaultNamer().SpanName(pred.Span()), getESIdxInSpan(pred), pred.Start(), pred.End()),
					)
				} else {
					errorStrs = append(
						errorStrs,
						fmt.Sprintf("        %s:%d (%v-%v)", t.DefaultNamer().SpanName(pred.Span()), getESIdxInSpan(pred), pred.Start(), pred.End()),
					)
				}
			}
		}
		ch.error(errors.New(strings.Join(errorStrs, "\n")))
		return 0
	}
	return visitedESs
}

// Check checks the provided trace for violated invariants which would likely
// compromise its correctness, returning an arbitrary violated invariant.
// Invariants tested by this function include:
//   - The trace's ElementarySpans must record the proper Successor() and
//     Predecessor();
//   - The trace's ElementarySpans must not overlap any other ElementarySpan
//     within their Span;
//   - The trace's ElementarySpans must not end before they begin;
//   - The trace's Dependencies must have at least one origin and at least one
//     destination;
//   - For each Dependency in the trace, each of its origin ElementarySpans
//     must have that dependency as their Outgoing(), and each of its
//     destination ElementarySpans must have that dependency as their
//     Incoming().
//
// If logAll is true, all errors are also logged to stderr.
func Check[T any, CP, SP, DP fmt.Stringer](
	t Trace[T, CP, SP, DP],
	logAll bool,
) error {
	ch := &checkHelper{
		logAll: logAll,
	}
	deps := map[Dependency[T, CP, SP, DP]]struct{}{}
	var entryESs []ElementarySpan[T, CP, SP, DP]
	var visitSpan func(span Span[T, CP, SP, DP])
	totalESs := 0
	visitSpan = func(span Span[T, CP, SP, DP]) {
		var lastES ElementarySpan[T, CP, SP, DP]
		if span.ElementarySpans()[0].Incoming() == nil {
			entryESs = append(entryESs, span.ElementarySpans()[0])
		}
		totalESs += len(span.ElementarySpans())
		for idx, es := range span.ElementarySpans() {
			if lastES != nil {
				if lastES.Successor() != es {
					ch.error(
						fmt.Errorf("span '%s' ES %d is not succeeded by ES %d",
							t.DefaultNamer().SpanName(span), idx-1, idx,
						),
					)
				}
				if es.Predecessor() != lastES {
					ch.error(
						fmt.Errorf("span '%s' ES %d is not preceded by ES %d",
							t.DefaultNamer().SpanName(span), idx, idx-1,
						),
					)
				}
				if t.Comparator().Less(es.Start(), lastES.End()) {
					ch.error(
						fmt.Errorf("adjacent ESs %d and %d in span %s overlap", idx, idx-1, t.DefaultNamer().SpanName(span)),
					)
				}
			}
			if lastES != nil {
				if t.Comparator().Less(es.End(), es.Start()) {
					ch.error(
						fmt.Errorf("negative-duration ES %d in span %s", idx, t.DefaultNamer().SpanName(span)),
					)
				}
			}
			if es.Incoming() != nil {
				deps[es.Incoming()] = struct{}{}
			}
			if es.Outgoing() != nil {
				deps[es.Outgoing()] = struct{}{}
			}
			lastES = es
		}
		for _, child := range span.ChildSpans() {
			visitSpan(child)
		}
	}
	for _, rootSpan := range t.RootSpans() {
		visitSpan(rootSpan)
	}
	if visitedESs := findCycles(t, entryESs, ch); ch.lastErr == nil && visitedESs != totalESs {
		// findCycle found no problems searching from entryESs, but it didn't visit
		// all elementary spans.  This means there's at least one cycle among
		// elementary spans that's unreachable from any entry span.
		ch.error(fmt.Errorf("unreachable cycles exist among trace elementary spans (%d visited ESs, %d total)", visitedESs, totalESs))
	}
	for dep := range deps {
		depStr := func() string {
			var origins, destinations []string
			for _, origin := range dep.Origins() {
				origins = append(origins, t.DefaultNamer().SpanName(origin.Span()))
			}
			for _, destination := range dep.Destinations() {
				destinations = append(destinations, t.DefaultNamer().SpanName(destination.Span()))
			}
			return fmt.Sprintf(
				"dependency of type %d from [%s] to [%s]",
				dep.DependencyType(),
				strings.Join(origins, ", "),
				strings.Join(destinations, ", "),
			)
		}
		if len(dep.Origins()) == 0 {
			ch.error(
				fmt.Errorf("%s has no origins",
					depStr(),
				),
			)
		}
		if len(dep.Destinations()) == 0 {
			ch.error(
				fmt.Errorf("%s has no destinations",
					depStr(),
				),
			)
		}
		for _, o := range dep.Origins() {
			if o.Outgoing() != dep {
				ch.error(
					fmt.Errorf("%s is not symmetrical; dependency origin %s recorded a different outgoing dependency",
						depStr(), t.DefaultNamer().SpanName(o.Span()),
					),
				)
			}
		}
		if dep.TriggeringOrigin() == nil {
			ch.error(fmt.Errorf("%s has no triggering origin", depStr()))
			continue
		}
		firstMoment := dep.TriggeringOrigin().End()
		for _, d := range dep.Destinations() {
			if d.Incoming() != dep {
				ch.error(
					fmt.Errorf("%s is not symmetrical; dependency destination %s recorded a different incoming dependency",
						depStr(), t.DefaultNamer().SpanName(d.Span()),
					),
				)
			}
			if t.Comparator().Less(d.Start(), firstMoment) {
				ch.error(
					fmt.Errorf("%s is negative from Span %s @%v to Span %s @%v",
						depStr(),
						t.DefaultNamer().SpanName(dep.TriggeringOrigin().Span()), dep.TriggeringOrigin().End(),
						t.DefaultNamer().SpanName(d.Span()), d.Start(),
					),
				)
			}
		}
	}
	return ch.lastErr
}
