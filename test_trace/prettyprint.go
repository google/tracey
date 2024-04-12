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

// TracePrettyPrinter facilitates prettyprinting trace data for testing.
type TracePrettyPrinter[T any, CP, SP, DP fmt.Stringer] struct {
	hierarchyTypeNames  map[trace.HierarchyType]string
	dependencyTypeNames map[trace.DependencyType]string
	pointPrinter        func(T) string
}

// NewPrettyPrinter returns a new TracePrettyPrinter, rendering hierarchy and
// dependency type names according to the provided mappings, and rendering
// trace points with %v.
func NewPrettyPrinter[T any, CP, SP, DP fmt.Stringer](
	hierarchyTypeNames map[trace.HierarchyType]string,
	dependencyTypeNames map[trace.DependencyType]string,
) *TracePrettyPrinter[T, CP, SP, DP] {
	return &TracePrettyPrinter[T, CP, SP, DP]{
		hierarchyTypeNames:  hierarchyTypeNames,
		dependencyTypeNames: dependencyTypeNames,
	}
}

// WithPointPrinter overrides the default '%v' trace point representation with
// the provided point-to-string converter function.
func (tpp *TracePrettyPrinter[T, CP, SP, DP]) WithPointPrinter(pp func(T) string) *TracePrettyPrinter[T, CP, SP, DP] {
	tpp.pointPrinter = pp
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

func (tpp *TracePrettyPrinter[T, CP, SP, DP]) prettyPrintDependency(
	dep trace.Dependency[T, CP, SP, DP],
	namer trace.Namer[T, CP, SP, DP],
	m printDepMode,
) string {
	if dep == nil {
		return "<none>"
	}
	dests := make([]string, len(dep.Destinations()))
	for idx, dest := range dep.Destinations() {
		destSpanPath := strings.Join(trace.GetSpanDisplayPath(dest.Span(), namer), "/")
		dests[idx] = fmt.Sprintf("%s @%s", destSpanPath, tpp.printPoint(dest.Start()))
	}
	originStr := "<unknown>"
	if dep.Origin() != nil {
		originStr = fmt.Sprintf("%s @%s",
			strings.Join(trace.GetSpanDisplayPath(dep.Origin().Span(), namer), "/"),
			tpp.printPoint(dep.Origin().End()),
		)
	}
	destStr := "<none>"
	if len(dests) > 0 {
		destStr = strings.Join(dests, ", ")
	}
	switch m {
	case sourceOnly:
		return fmt.Sprintf("[%s from %s]",
			tpp.dependencyTypeNames[dep.DependencyType()],
			originStr,
		)
	case destOnly:
		return fmt.Sprintf("[%s to %s]",
			tpp.dependencyTypeNames[dep.DependencyType()],
			destStr,
		)
	default:
		return fmt.Sprintf("[%s from %s to %s]",
			tpp.dependencyTypeNames[dep.DependencyType()],
			originStr,
			destStr,
		)
	}
}

func (tpp *TracePrettyPrinter[T, CP, SP, DP]) prettyPrintElementarySpan(
	es trace.ElementarySpan[T, CP, SP, DP],
	namer trace.Namer[T, CP, SP, DP],
	indent string,
) string {
	ret := []string{
		fmt.Sprintf("%s%s %s -> THIS -> %s",
			indent,
			fmt.Sprintf("%s-%s", tpp.printPoint(es.Start()), tpp.printPoint(es.End())),
			tpp.prettyPrintDependency(es.Incoming(), namer, sourceOnly),
			tpp.prettyPrintDependency(es.Outgoing(), namer, destOnly),
		),
	}
	return strings.Join(ret, "\n")
}

func (tpp *TracePrettyPrinter[T, CP, SP, DP]) prettyPrintSpan(
	span trace.Span[T, CP, SP, DP],
	namer trace.Namer[T, CP, SP, DP],
	indent string,
) string {
	spanPath := strings.Join(trace.GetSpanDisplayPath(span, namer), "/")
	ret := []string{
		fmt.Sprintf("%sSpan '%s' (%s-%s) (%s)", indent, namer.SpanName(span), tpp.printPoint(span.Start()), tpp.printPoint(span.End()), spanPath),
		fmt.Sprintf("%s  Elementary spans:", indent),
	}
	for _, es := range span.ElementarySpans() {
		ret = append(ret, tpp.prettyPrintElementarySpan(es, namer, indent+"    "))
	}
	for _, child := range span.ChildSpans() {
		ret = append(ret, tpp.prettyPrintSpan(child, namer, indent+"  "))
	}
	return strings.Join(ret, "\n")
}

func (tpp *TracePrettyPrinter[T, CP, SP, DP]) prettyPrintCategory(
	cat trace.Category[T, CP, SP, DP],
	namer trace.Namer[T, CP, SP, DP],
	ht trace.HierarchyType, indent string,
) string {
	catPath := strings.Join(trace.GetCategoryDisplayPath(cat, namer), "/")
	ret := []string{
		fmt.Sprintf("%sCategory '%s' (%s)", indent, namer.CategoryName(cat), catPath),
	}
	for _, span := range cat.RootSpans() {
		ret = append(ret, tpp.prettyPrintSpan(span, namer, indent+"  "))
	}
	for _, cat := range cat.ChildCategories() {
		ret = append(ret, tpp.prettyPrintCategory(cat, namer, ht, indent+"  "))
	}
	return strings.Join(ret, "\n")
}

// PrettyPrintTrace returns the prettyprinted provided trace, using the
// provided namer, along the provided HierarchyType.
func (tpp *TracePrettyPrinter[T, CP, SP, DP]) PrettyPrintTrace(
	t trace.Trace[T, CP, SP, DP],
	ht trace.HierarchyType,
) string {
	ret := []string{
		"",
		fmt.Sprintf("Trace (%s):", tpp.hierarchyTypeNames[ht]),
	}
	for _, cat := range t.RootCategories(ht) {
		ret = append(ret, tpp.prettyPrintCategory(cat, t.DefaultNamer(), ht, "  "))
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
		ret = append(ret, tpp.prettyPrintSpan(rootSpan, namer, "  "))
	}
	return strings.Join(ret, "\n")
}
