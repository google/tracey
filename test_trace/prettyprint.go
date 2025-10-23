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

// Package testtrace provides tools for fluently constructing 'interesting'
// traces for testing.
package testtrace

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/tracey/trace"
)

// TracePrettyPrinter facilitates pretty-printing trace data for testing.
type TracePrettyPrinter[T any, CP, SP, DP fmt.Stringer] struct {
	namer                        trace.Namer[T, CP, SP, DP]
	pointPrinter                 func(T) string
	includeElementarySpanIndices bool
}

// NewPrettyPrinter returns a new TracePrettyPrinter, rendering hierarchy and
// dependency type names according to the provided namer, and rendering
// trace points with %v.
func NewPrettyPrinter[T any, CP, SP, DP fmt.Stringer](
	namer trace.Namer[T, CP, SP, DP],
) *TracePrettyPrinter[T, CP, SP, DP] {
	return &TracePrettyPrinter[T, CP, SP, DP]{
		namer: namer,
	}
}

// WithPointPrinter overrides the default '%v' trace point representation with
// the provided point-to-string converter function.
func (tpp *TracePrettyPrinter[T, CP, SP, DP]) WithPointPrinter(pp func(T) string) *TracePrettyPrinter[T, CP, SP, DP] {
	tpp.pointPrinter = pp
	return tpp
}

// WithElementarySpanIndicesIncluded specifies that prettyprinted elementary
// spans should include their index within their span.
func (tpp *TracePrettyPrinter[T, CP, SP, DP]) WithElementarySpanIndicesIncluded() *TracePrettyPrinter[T, CP, SP, DP] {
	tpp.includeElementarySpanIndices = true
	return tpp
}

func (tpp *TracePrettyPrinter[T, CP, SP, DP]) printPoint(point T) string {
	if tpp.pointPrinter == nil {
		return fmt.Sprintf("%v", point)
	}
	return tpp.pointPrinter(point)
}

type printDepMode int

const (
	sourceOnly printDepMode = iota
	destOnly
	sourceAndDest
)

func (tpp *TracePrettyPrinter[T, CP, SP, DP]) prettyPrintDependencyEndpoint(
	endpoint trace.ElementarySpan[T, CP, SP, DP],
) string {
	ret := strings.Join(trace.GetSpanDisplayPath(endpoint.Span(), tpp.namer), "/")
	if tpp.includeElementarySpanIndices {
		idx := -1
		for cursor := endpoint; cursor != nil; cursor = cursor.Predecessor() {
			idx++
		}
		ret = fmt.Sprintf("%s (#%d)", ret, idx)
	}
	return ret
}

func (tpp *TracePrettyPrinter[T, CP, SP, DP]) prettyPrintDependency(
	dep trace.Dependency[T, CP, SP, DP],
	m printDepMode,
) string {
	if dep == nil {
		return "<none>"
	}
	dests := make([]string, len(dep.Destinations()))
	for idx, dest := range dep.Destinations() {
		dests[idx] = fmt.Sprintf("%s @%s", tpp.prettyPrintDependencyEndpoint(dest), tpp.printPoint(dest.Start()))
	}
	originStr := "<unknown>"
	if len(dep.Origins()) > 0 {
		var originStrs []string
		for _, origin := range dep.Origins() {
			originStrs = append(originStrs,
				fmt.Sprintf("%s @%s", tpp.prettyPrintDependencyEndpoint(origin), tpp.printPoint(origin.End())),
			)
		}
		if len(originStrs) == 1 {
			originStr = originStrs[0]
		} else {
			originStr = fmt.Sprintf("(triggering) %s (also nontriggering %s)",
				originStrs[0],
				strings.Join(originStrs[1:], ", "),
			)
		}
	}
	destStr := "<none>"
	if len(dests) > 0 {
		destStr = strings.Join(dests, ", ")
	}
	switch m {
	case sourceOnly:
		return fmt.Sprintf("[%s from %s]",
			tpp.namer.DependencyTypes().TypeData(dep.DependencyType()).Name,
			originStr,
		)
	case destOnly:
		return fmt.Sprintf("[%s to %s]",
			tpp.namer.DependencyTypes().TypeData(dep.DependencyType()).Name,
			destStr,
		)
	default:
		return fmt.Sprintf("[%s from %s to %s]",
			tpp.namer.DependencyTypes().TypeData(dep.DependencyType()).Name,
			originStr,
			destStr,
		)
	}
}

func (tpp *TracePrettyPrinter[T, CP, SP, DP]) prettyPrintElementarySpan(
	es trace.ElementarySpan[T, CP, SP, DP],
	includeMarks bool,
	indent string,
) string {
	indexStr := ""
	if tpp.includeElementarySpanIndices {
		idx := -1
		for cursor := es; cursor != nil; cursor = cursor.Predecessor() {
			idx++
		}
		indexStr = fmt.Sprintf("(#%d) ", idx)
	}
	ret := []string{
		fmt.Sprintf("%s%s%s %s -> THIS -> %s",
			indent,
			indexStr,
			fmt.Sprintf("%s-%s", tpp.printPoint(es.Start()), tpp.printPoint(es.End())),
			tpp.prettyPrintDependency(es.Incoming(), sourceOnly),
			tpp.prettyPrintDependency(es.Outgoing(), destOnly),
		),
	}
	if includeMarks {
		for _, mark := range es.Marks() {
			ret = append(ret, fmt.Sprintf("%s  '%s' @%v", indent, mark.Label(), tpp.printPoint(mark.Moment())))
		}
	}
	return strings.Join(ret, "\n")
}

// PrettyPrintElementarySpan prettyprints the provided elementary span.
func (tpp *TracePrettyPrinter[T, CP, SP, DP]) PrettyPrintElementarySpan(es trace.ElementarySpan[T, CP, SP, DP]) string {
	return tpp.namer.SpanName(es.Span()) + " " + tpp.prettyPrintElementarySpan(es, false, "")
}

func (tpp *TracePrettyPrinter[T, CP, SP, DP]) prettyPrintSpan(
	span trace.Span[T, CP, SP, DP],
	indent string,
) string {
	spanPath := strings.Join(trace.GetSpanDisplayPath(span, tpp.namer), "/")
	ret := []string{
		fmt.Sprintf("%sSpan '%s' (%s-%s) (%s)", indent, tpp.namer.SpanName(span), tpp.printPoint(span.Start()), tpp.printPoint(span.End()), spanPath),
		fmt.Sprintf("%s  Elementary spans:", indent),
	}
	for _, es := range span.ElementarySpans() {
		ret = append(ret, tpp.prettyPrintElementarySpan(es, true, indent+"    "))
	}
	for _, child := range span.ChildSpans() {
		ret = append(ret, tpp.prettyPrintSpan(child, indent+"  "))
	}
	return strings.Join(ret, "\n")
}

func (tpp *TracePrettyPrinter[T, CP, SP, DP]) prettyPrintCategory(
	cat trace.Category[T, CP, SP, DP],
	ht trace.HierarchyType, indent string,
) string {
	catPath := strings.Join(trace.GetCategoryDisplayPath(cat, tpp.namer), "/")
	ret := []string{
		fmt.Sprintf("%sCategory '%s' (%s)", indent, tpp.namer.CategoryName(cat), catPath),
	}
	for _, span := range cat.RootSpans() {
		ret = append(ret, tpp.prettyPrintSpan(span, indent+"  "))
	}
	for _, cat := range cat.ChildCategories() {
		ret = append(ret, tpp.prettyPrintCategory(cat, ht, indent+"  "))
	}
	return strings.Join(ret, "\n")
}

// PrettyPrintTrace returns the prettyprinted provided trace, using the
// provided namer, along the provided HierarchyType.
func (tpp *TracePrettyPrinter[T, CP, SP, DP]) PrettyPrintTrace(
	t trace.Trace[T, CP, SP, DP],
	ht trace.HierarchyType,
) string {
	td := tpp.namer.HierarchyTypes().TypeData(ht)
	hts := fmt.Sprintf("hierarchy type %v", ht)
	if td != nil {
		hts = td.Description
	}
	ret := []string{
		"",
		fmt.Sprintf("Trace (%s):", hts),
	}
	if ht == trace.SpanOnlyHierarchyType {
		for _, span := range t.RootSpans() {
			ret = append(ret, tpp.prettyPrintSpan(span, "  "))
		}
	} else {
		for _, cat := range t.RootCategories(ht) {
			ret = append(ret, tpp.prettyPrintCategory(cat, ht, "  "))
		}
	}
	return strings.Join(ret, "\n")
}

// PrettyPrintTraceSpans returns the prettyprinted provided trace with no
// category hierarchy.  Instead, all root spans are emitted.
func (tpp *TracePrettyPrinter[T, CP, SP, DP]) PrettyPrintTraceSpans(
	t trace.Trace[T, CP, SP, DP],
) string {
	ret := []string{
		"",
		fmt.Sprintf("Trace spans:"),
	}
	rootSpans := t.RootSpans()
	namer := t.DefaultNamer()
	sort.Slice(rootSpans, func(a, b int) bool {
		return namer.SpanName(rootSpans[a]) < namer.SpanName(rootSpans[b])
	})
	for _, rootSpan := range rootSpans {
		ret = append(ret, tpp.prettyPrintSpan(rootSpan, "  "))
	}
	return strings.Join(ret, "\n")
}
