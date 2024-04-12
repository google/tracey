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

package testtrace

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/tracey/trace"
)

const (
	// None can be used to specify that tests should pretty-print just a Trace's
	// Spans, without any Category hierarchy.
	None trace.HierarchyType = iota
	// Causal is the hierarchy induced by spawning.
	Causal
	// Structural is the fixed hierarchy: processes contain tasks which contain
	// regions.
	Structural
)

// HierarchyTypeNames maps test HierarchyTypes to strings.
var HierarchyTypeNames = map[trace.HierarchyType]string{
	None:       "no",
	Causal:     "causal",
	Structural: "structural",
}

// DependencyTypes supported in test traces.
const (
	Spawn trace.DependencyType = trace.FirstUserDefinedDependencyType + iota
	Send
	Signal
)

// DependencyTypeNames maps test DependencyTypes to strings.
var DependencyTypeNames = map[trace.DependencyType]string{
	trace.Call:   "call",
	trace.Return: "return",
	Spawn:        "spawn",
	Send:         "send",
	Signal:       "signal",
}

// StringPayload is a Span or Category payload that is a simple string.
type StringPayload string

// LiteralMatcher matches the provided literal path element name.
func LiteralMatcher(
	str string,
) trace.PathElementMatcher[time.Duration, StringPayload, StringPayload, StringPayload] {
	return trace.NewLiteralNameMatcher[time.Duration, StringPayload, StringPayload, StringPayload](str)
}

func (sp StringPayload) String() string {
	return string(sp)
}

type stringTraceNamer struct {
}

func (stn *stringTraceNamer) CategoryName(
	category trace.Category[time.Duration, StringPayload, StringPayload, StringPayload],
) string {
	return category.Payload().String()
}

func (stn *stringTraceNamer) CategoryUniqueID(
	category trace.Category[time.Duration, StringPayload, StringPayload, StringPayload],
) string {
	return category.Payload().String()
}

func (stn *stringTraceNamer) SpanName(
	span trace.Span[time.Duration, StringPayload, StringPayload, StringPayload],
) string {
	return span.Payload().String()
}

func (stn *stringTraceNamer) SpanUniqueID(
	span trace.Span[time.Duration, StringPayload, StringPayload, StringPayload],
) string {
	return span.Payload().String()
}

func (stn *stringTraceNamer) HierarchyTypeName(ht trace.HierarchyType) string {
	if ret, ok := HierarchyTypeNames[ht]; ok {
		return ret
	}
	return "unknown"
}

func (stn *stringTraceNamer) DependencyTypeName(dt trace.DependencyType) string {
	if ret, ok := DependencyTypeNames[dt]; ok {
		return ret
	}
	return "unknown"
}

func (stn *stringTraceNamer) MomentString(t time.Duration) string {
	return fmt.Sprintf("%v", t)
}

// TraceBuilder facilitates fluently building test traces in tests.
type TraceBuilder struct {
	err   func(error)
	trace trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload]
}

// NewTestingTraceBuilder returns a new, empty TraceBuilder.  Any errors
// encountered in trace construction yield a t.Fatalf() in the provided
// testing.T.
func NewTestingTraceBuilder(t *testing.T) *TraceBuilder {
	return NewTraceBuilderWithErrorHandler(func(err error) {
		t.Fatalf(err.Error())
	})
}

// NewTraceBuilderWithErrorHandler returns a new, empty TraceBuilder.  Any
// errors encountered in trace construction are passed to the provided error
// handler.
func NewTraceBuilderWithErrorHandler(err func(error)) *TraceBuilder {
	return &TraceBuilder{
		err: err,
		trace: trace.NewTrace[time.Duration, StringPayload, StringPayload, StringPayload](
			trace.DurationComparator,
			&stringTraceNamer{},
		),
	}
}

// Build returns the assembled Trace, and a Namer that can be used for
// prettyprinting and querying it.
func (tb *TraceBuilder) Build() (
	trace trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
) {
	return tb.trace
}

// WithRootCategories adds the provided Category hierarchy into the Trace under
// construction, returning the receiver for fluent invocation.
func (tb *TraceBuilder) WithRootCategories(
	cats ...RootCategoryFn,
) *TraceBuilder {
	for _, cat := range cats {
		cat(tb.trace)
	}
	return tb
}

// WithRootSpans adds the provided Span hierarchy into the Trace under
// construction, returning the receiver for fluent invocation.
func (tb *TraceBuilder) WithRootSpans(spans ...RootSpanFn) *TraceBuilder {
	for _, span := range spans {
		if _, err := span(tb); err != nil {
			tb.err(err)
		}
	}
	return tb
}

// OriginFn describes a function attaching an origin to a Dependency.
type OriginFn func(
	tb *TraceBuilder,
	db trace.Dependency[time.Duration, StringPayload, StringPayload, StringPayload],
) error

// DestinationFn describes a function attaching a destination to a Dependency.
type DestinationFn func(
	tb *TraceBuilder,
	db trace.Dependency[time.Duration, StringPayload, StringPayload, StringPayload],
) error

// WithDependency adds a dependency with the provided type, origin, and
// destinations into the Trace under construction, returning the receiver for
// fluent invocation.
func (tb *TraceBuilder) WithDependency(
	dependencyType trace.DependencyType,
	payload StringPayload,
	originFn OriginFn,
	destinationFns ...DestinationFn,
) *TraceBuilder {
	db := tb.trace.NewDependency(dependencyType, payload)
	if err := originFn(tb, db); err != nil {
		tb.err(err)
	}
	for _, destinationFn := range destinationFns {
		if err := destinationFn(tb, db); err != nil {
			tb.err(err)
		}
	}
	return tb
}

// WithSuspend adds a suspend over the specified interval in the specified
// span.
func (tb *TraceBuilder) WithSuspend(
	pathMatchers []string, start, end time.Duration,
	opts ...trace.SuspendOption,
) *TraceBuilder {
	matchers, err := PathMatchersFromPattern(pathMatchers)
	if err != nil {
		tb.err(err)
		return tb
	}
	spans := trace.FindSpans[time.Duration, StringPayload, StringPayload, StringPayload](
		tb.trace, tb.trace.DefaultNamer(), matchers,
	)
	if len(spans) != 1 {
		tb.err(fmt.Errorf("exactly one span must match the path patterns '%v' (got %d)", pathMatchers, len(spans)))
		return tb
	}
	spans[0].Suspend(tb.trace.Comparator(), start, end, opts...)
	return tb
}

// Origin declares the Span matching the provided path string, at the provided
// time, to be the origin of a Dependency.
func Origin(
	pathMatchers []string,
	start time.Duration,
	opts ...trace.DependencyOption,
) OriginFn {
	return func(
		tb *TraceBuilder,
		db trace.Dependency[time.Duration, StringPayload, StringPayload, StringPayload],
	) error {
		matchers, err := PathMatchersFromPattern(pathMatchers)
		if err != nil {
			return err
		}
		spans := trace.FindSpans[time.Duration, StringPayload, StringPayload, StringPayload](
			tb.trace, tb.trace.DefaultNamer(), matchers,
		)
		if len(spans) != 1 {
			return fmt.Errorf("exactly one span must match the path patterns '%v' (got %d)", pathMatchers, len(spans))
		}
		return db.SetOriginSpan(tb.trace.Comparator(), spans[0], start, opts...)
	}
}

// Destination declares the Span matching the provided path string, at the provided
// time, to be a destination of a Dependency.
func Destination(
	pathMatchers []string,
	end time.Duration,
	opts ...trace.DependencyOption,
) DestinationFn {
	return func(
		tb *TraceBuilder,
		db trace.Dependency[time.Duration, StringPayload, StringPayload, StringPayload],
	) error {
		matchers, err := PathMatchersFromPattern(pathMatchers)
		if err != nil {
			return err
		}
		spans := trace.FindSpans[time.Duration, StringPayload, StringPayload, StringPayload](
			tb.trace, tb.trace.DefaultNamer(), matchers,
		)
		if len(spans) != 1 {
			return fmt.Errorf("exactly one span must match the path pattern '%v' (got %d)", pathMatchers, len(spans))
		}
		return db.AddDestinationSpan(tb.trace.Comparator(), spans[0], end, opts...)
	}
}

// DestinationAfterWait  declares the Span matching the provided path string,
// at the provided end time, to be a destination of a Dependency after a suspended
// interval starting at the provided waitFrom time.
func DestinationAfterWait(
	pathMatchers []string,
	waitFrom, end time.Duration,
) DestinationFn {
	return func(
		tb *TraceBuilder,
		db trace.Dependency[time.Duration, StringPayload, StringPayload, StringPayload],
	) error {
		matchers, err := PathMatchersFromPattern(pathMatchers)
		if err != nil {
			return err
		}
		spans := trace.FindSpans[time.Duration, StringPayload, StringPayload, StringPayload](
			tb.trace, tb.trace.DefaultNamer(), matchers,
		)
		if len(spans) != 1 {
			return fmt.Errorf("exactly one span must match the path pattern '%v' (got %d)", pathMatchers, len(spans))
		}
		return db.AddDestinationSpanAfterWait(tb.trace.Comparator(), spans[0], waitFrom, end)
	}
}

// RootCategoryFn describes a function declaring a root Category.
type RootCategoryFn func(t trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload]) trace.Category[time.Duration, StringPayload, StringPayload, StringPayload]

// RootCategory defines a root Category, with the specified HierarchyType and
// payload, and the provided child Categories, for inclusion in a Trace.
func RootCategory(
	ht trace.HierarchyType,
	payload StringPayload,
	childCats ...CategoryFn,
) RootCategoryFn {
	return func(
		t trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
	) trace.Category[time.Duration, StringPayload, StringPayload, StringPayload] {
		ret := t.NewRootCategory(ht, payload)
		for _, childCat := range childCats {
			childCat(ret)
		}
		return ret
	}
}

// CategoryFn describes a function declaring a child Category.
type CategoryFn func(parent trace.Category[time.Duration, StringPayload, StringPayload, StringPayload]) trace.Category[time.Duration, StringPayload, StringPayload, StringPayload]

// Category defines a child category, with the specified payload and the
// provided child Categories, for inclusion in a Trace.
func Category(payload StringPayload, childCats ...CategoryFn) CategoryFn {
	return func(
		parent trace.Category[time.Duration, StringPayload, StringPayload, StringPayload],
	) trace.Category[time.Duration, StringPayload, StringPayload, StringPayload] {
		ret := parent.NewChildCategory(payload)
		for _, childCat := range childCats {
			childCat(ret)
		}
		return ret
	}
}

// CategorySearchSpec defines a path string of a Category, and the
// HierarchyType it should be found under in a Trace.
type CategorySearchSpec struct {
	ht      trace.HierarchyType
	pathStr string
}

// FindCategory specifies a parent Category (HierarchyType and path within
// that hierarchy) for a root Span.
func FindCategory(ht trace.HierarchyType, pathStr string) CategorySearchSpec {
	return CategorySearchSpec{
		ht:      ht,
		pathStr: pathStr,
	}
}

// ParentCategories specifies the parent Categories for a root Span.
func ParentCategories(parentCats ...CategorySearchSpec) []CategorySearchSpec {
	return parentCats
}

// RootSpanFn describes a function declaring a root Span.
type RootSpanFn func(tb *TraceBuilder) (
	trace.Span[time.Duration, StringPayload, StringPayload, StringPayload],
	error,
)

// PathMatchersFromPattern returns a slice of PathElementMatchers formed by
// splitting the provided path string on slashes, then creating a
// LiteralNameMatcher for each path element.
func PathMatchersFromPattern(pathMatchers []string) (
	[][]trace.PathElementMatcher[time.Duration, StringPayload, StringPayload, StringPayload],
	error,
) {
	pmp, err := trace.NewPathMatcherParser[time.Duration, StringPayload, StringPayload, StringPayload]()
	if err != nil {
		return nil, err
	}
	ret := make([][]trace.PathElementMatcher[time.Duration, StringPayload, StringPayload, StringPayload], len(pathMatchers))
	for idx, pathMatcher := range pathMatchers {
		pems, err := pmp.ParsePathMatcherStr(pathMatcher)
		if err != nil {
			return nil, err
		}
		ret[idx] = pems
	}
	return ret, nil
}

// RootSpan defines a root Span, with the specified endpoints, payload,
// parent Categories, and child Spans, for inclusion in a Trace.
func RootSpan(
	start, end time.Duration,
	payload StringPayload,
	parentCategories []CategorySearchSpec,
	childSpans ...SpanFn,
) RootSpanFn {
	return func(tb *TraceBuilder) (
		trace.Span[time.Duration, StringPayload, StringPayload, StringPayload], error) {
		ret := tb.trace.NewRootSpan(start, end, payload)
		for _, childSpan := range childSpans {
			if _, err := childSpan(tb.trace, ret); err != nil {
				return nil, err
			}
		}
		pmp, err := trace.NewPathMatcherParser[time.Duration, StringPayload, StringPayload, StringPayload]()
		if err != nil {
			return nil, err
		}
		for _, parentCat := range parentCategories {
			matchers, err := pmp.ParsePathMatcherStr(parentCat.pathStr)
			if err != nil {
				return nil, err
			}
			cats := trace.FindCategories[time.Duration, StringPayload, StringPayload, StringPayload](
				tb.trace, tb.trace.DefaultNamer(),
				parentCat.ht,
				[][]trace.PathElementMatcher[time.Duration, StringPayload, StringPayload, StringPayload]{matchers},
			)
			if len(cats) != 1 {
				return nil, fmt.Errorf("exactly one category must match the path pattern '%s' (got %d)", parentCat.pathStr, len(cats))
			}
			if err := cats[0].AddRootSpan(ret); err != nil {
				return nil, err
			}
		}
		return ret, nil
	}
}

// SpanFn describes a function declaring a child Span.
type SpanFn func(
	trace trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
	parent trace.Span[time.Duration, StringPayload, StringPayload, StringPayload],
) (
	trace.Span[time.Duration, StringPayload, StringPayload, StringPayload],
	error,
)

// Span defines a child Span, with the specified endpoints, payload,
// and child Spans, for inclusion in a Trace.
func Span(
	start, end time.Duration,
	payload StringPayload,
	childSpans ...SpanFn,
) SpanFn {
	return func(
		t trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
		parent trace.Span[time.Duration, StringPayload, StringPayload, StringPayload],
	) (
		trace.Span[time.Duration, StringPayload, StringPayload, StringPayload],
		error,
	) {
		ret, err := parent.NewChildSpan(t.Comparator(), start, end, payload)
		if err != nil {
			return nil, err
		}
		for _, childSpan := range childSpans {
			if _, err := childSpan(t, ret); err != nil {
				return nil, err
			}
		}
		return ret, nil
	}
}
