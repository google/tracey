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
	"strings"
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

// HierarchyTypes defines test traces' valid  hierarchy types.
var HierarchyTypes = trace.NewHierarchyTypes().
	With(None, "none", "Span-only").
	With(Structural, "structural", "structural").
	With(Causal, "causal", "causal")

// DependencyTypes supported in test traces.
const (
	Spawn trace.DependencyType = trace.FirstUserDefinedDependencyType + iota
	Send
	Signal
)

// DependencyTypes defines test traces' valid dependency types.
var DependencyTypes = trace.NewDependencyTypes().
	With(trace.Call, "call", "call").
	With(trace.Return, "return", "return").
	With(Spawn, "spawn", "spawn").
	With(Send, "send", "send").
	With(Signal, "signal", "signal")

// StringPayload is a Span or Category payload that is a simple string.
type StringPayload string

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

func (stn *stringTraceNamer) HierarchyTypes() *trace.HierarchyTypes {
	return HierarchyTypes
}

func (stn *stringTraceNamer) DependencyTypes() *trace.DependencyTypes {
	return DependencyTypes
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
		t.Fatal(err.Error())
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

// EndpointFn describes a function attaching an origin or destination to a
// Dependency.
type EndpointFn func(
	tb *TraceBuilder,
	db trace.Dependency[time.Duration, StringPayload, StringPayload, StringPayload],
) error

// WithDependency adds a dependency with the provided type, origin, and
// destinations into the Trace under construction, returning the receiver for
// fluent invocation.
func (tb *TraceBuilder) WithDependency(
	dependencyType trace.DependencyType,
	payload StringPayload,
	opts ...any,
) *TraceBuilder {
	var options []trace.DependencyOption
	var endpointFns []EndpointFn
	for _, opt := range opts {
		switch v := opt.(type) {
		case trace.DependencyOption:
			options = append(options, v)
		case EndpointFn:
			endpointFns = append(endpointFns, v)
		default:
			tb.err(fmt.Errorf("unrecognized WithDependency argument type %T", v))
			return tb
		}
	}
	db := tb.trace.NewDependency(dependencyType, payload, options...)
	for _, endpointFn := range endpointFns {
		if err := endpointFn(tb, db); err != nil {
			tb.err(err)
		}
	}
	return tb
}

func (tb *TraceBuilder) findSpans(
	pathEls []string,
) []trace.Span[time.Duration, StringPayload, StringPayload, StringPayload] {
	var cursors []trace.Span[time.Duration, StringPayload, StringPayload, StringPayload]
	if len(pathEls) == 0 {
		return cursors
	}
	for _, s := range tb.trace.RootSpans() {
		if s.Payload().String() == pathEls[0] {
			cursors = append(cursors, s)
		}
	}
	for _, pathEl := range pathEls[1:] {
		var nextCursors []trace.Span[time.Duration, StringPayload, StringPayload, StringPayload]
		for _, cursor := range cursors {
			for _, child := range cursor.ChildSpans() {
				if child.Payload().String() == pathEl {
					nextCursors = append(nextCursors, child)
				}
			}
		}
		cursors = nextCursors
	}
	return cursors
}

// WithSuspend adds a suspend over the specified interval in the specified
// span.
func (tb *TraceBuilder) WithSuspend(
	pathEls []string, start, end time.Duration,
	opts ...trace.SuspendOption,
) *TraceBuilder {
	spans := tb.findSpans(pathEls)
	if len(spans) != 1 {
		tb.err(fmt.Errorf("can't add suspend: exactly one span must match the path '%s' (got %d)", strings.Join(pathEls, "/"), len(spans)))
		return tb
	}
	spans[0].Suspend(tb.trace.Comparator(), start, end, opts...)
	return tb
}

// Origin declares the Span matching the provided path string, at the provided
// time, to be the origin of a Dependency.
func Origin(
	pathEls []string,
	start time.Duration,
	opts ...trace.DependencyEndpointOption,
) EndpointFn {
	return func(
		tb *TraceBuilder,
		db trace.Dependency[time.Duration, StringPayload, StringPayload, StringPayload],
	) error {
		spans := tb.findSpans(pathEls)
		if len(spans) != 1 {
			return fmt.Errorf("exactly one span must match the path '%s' (got %d)", strings.Join(pathEls, "/"), len(spans))
		}
		return db.SetOriginSpan(tb.trace.Comparator(), spans[0], start, opts...)
	}
}

// Destination declares the Span matching the provided path string, at the provided
// time, to be a destination of a Dependency.
func Destination(
	pathEls []string,
	end time.Duration,
	opts ...trace.DependencyEndpointOption,
) EndpointFn {
	return func(
		tb *TraceBuilder,
		db trace.Dependency[time.Duration, StringPayload, StringPayload, StringPayload],
	) error {
		spans := tb.findSpans(pathEls)
		if len(spans) != 1 {
			return fmt.Errorf("exactly one span must match the path '%s' (got %d)", strings.Join(pathEls, "/"), len(spans))
		}
		return db.AddDestinationSpan(tb.trace.Comparator(), spans[0], end, opts...)
	}
}

// DestinationAfterWait  declares the Span matching the provided path string,
// at the provided end time, to be a destination of a Dependency after a suspended
// interval starting at the provided waitFrom time.
func DestinationAfterWait(
	pathEls []string,
	waitFrom, end time.Duration,
) EndpointFn {
	return func(
		tb *TraceBuilder,
		db trace.Dependency[time.Duration, StringPayload, StringPayload, StringPayload],
	) error {
		spans := tb.findSpans(pathEls)
		if len(spans) != 1 {
			return fmt.Errorf("exactly one span must match the path '%s' (got %d)", strings.Join(pathEls, "/"), len(spans))
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
	pathEls []string
}

// FindCategory specifies a parent Category (HierarchyType and path within
// that hierarchy) for a root Span.
func FindCategory(ht trace.HierarchyType, pathEls ...string) CategorySearchSpec {
	return CategorySearchSpec{
		ht:      ht,
		pathEls: pathEls,
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
			if err := childSpan(tb.trace, ret); err != nil {
				return nil, err
			}
		}
		for _, parentCat := range parentCategories {
			var cursor trace.Category[time.Duration, StringPayload, StringPayload, StringPayload]
			pool := tb.trace.RootCategories(parentCat.ht)
			for _, pathEl := range parentCat.pathEls {
				cursor = nil
				for _, cat := range pool {
					if cat.Payload().String() == pathEl {
						cursor = cat
						break
					}
				}
				if cursor == nil {
					return nil, fmt.Errorf("failed to find parent category at %s", strings.Join(parentCat.pathEls, "/"))
				}
				pool = cursor.ChildCategories()
			}
			if err := cursor.AddRootSpan(ret); err != nil {
				return nil, err
			}
		}
		return ret, nil
	}
}

// SpanFn describes a function declaring a child Span or Mark.
type SpanFn func(
	trace trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
	parent trace.Span[time.Duration, StringPayload, StringPayload, StringPayload],
) error

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
	) error {

		ret, err := parent.NewChildSpan(t.Comparator(), start, end, payload)
		if err != nil {
			return err
		}
		for _, childSpan := range childSpans {
			if err := childSpan(t, ret); err != nil {
				return err
			}
		}
		return nil
	}
}

// Mark defines a mark at the specified moment and with the provided label
// within its parent Span.
func Mark(
	label string,
	at time.Duration,
) SpanFn {
	return func(
		t trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
		parent trace.Span[time.Duration, StringPayload, StringPayload, StringPayload],
	) error {
		return parent.Mark(t.Comparator(), label, at)
	}
}
