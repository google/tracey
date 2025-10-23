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

// Package trace defines interfaces, types, and functions that facilitate
// building traces suitable for visualization, analysis, and transformation.
//
// In this package, a Trace is fundamentally collection of Spans, each of which
// has an active interval (e.g., a start and end timestamp), and which have
// causal relationships, or Dependencies, between them.  Additionally, each
// Span comprises a sequence of ElementarySpans: intervals during which
// incoming Dependencies only occur at the start, and outgoing Dependencies
// only occur at the end.  Gaps between neighboring ElementarySpans within a
// parent Span represent suspended, or idle, time, and may be introduced with
// Span.Suspend().
//
// As a special case of inter-Span relationships, this package natively
// supports blocking Call and Return Dependencies, in which one Span (the
// caller) spawns and yields to another (the callee), resuming its execution
// when the callee returns.  This models the ubiquitous blocking-call behavior
// observed in code callstacks, blocking service calls, etc.  Spans with no
// calling parents are RootSpans, and are created at the Trace level.
//
// Additional Dependency types may be user-defined and used with specific Trace
// instances.  Different Dependency types are not treated differently in this
// package, but instead are used to identify and select specific classes of
// Dependencies, for instance when defining Trace transforms.  A Dependency
// must have a single *triggering* origin endpoint -- a particular Span at a
// particular point within its running duration where the Dependency was
// causally resolved -- and one or more destination endpoints, and it defines,
// for each destination endpoint, a causal relationship with the triggering
// origin endpoint: the destination endpoint cannot occur until the origin
// endpoint has occurred.  A dependency may have multiple origins, but only one
// can be *triggering*; the rest are non-triggering.  Non-triggering origins
// are not considered causal for analysis purposes, but non-triggering origins
// may become triggering under transformation.  Dependencies are created at the
// Trace level.
//
// Traces may also define hierarchies of Categories into which RootSpans may
// be placed; when a RootSpan is placed under a Category, all its descendant
// Spans also fall under that Category.  Category hierarchies are generally not
// used for analysis, but are invaluable in visualization, where they can
// reveal the different contexts in which Spans execute.  Category
// HierarchyTypes may be user-defined and used with specific Trace
// implementations, and a single Trace implementation may support many
// HierarchyTypes; under each one, every RootSpan should have exactly one
// parent Category.  For example, a single-system user-mode execution Trace may
// offer CPU, process/thread, and execution-phase HierarchyTypes; switching
// between these in visualization helps users orient themselves in the Trace and
// spot resource contention issues.
//
// ElementarySpans are automatically created and managed by Trace
// implementations: for each Dependency endpoint, the ElementarySpan at that
// endpoint is fissioned into two at the moment of the endpoint, and the
// Dependency is linked with one of the fissioned parts -- if an origin
// endpoint, this Dependency becomes the first fissioned part's outgoing
// Dependency, and if a destination endpoint, this Dependency becomes the
// second fissioned part's incoming Dependency.  A Dependency may not be
// introduced to a Span at a point where that Span is suspended, and a Span
// suspend may not be placed over a Dependency.
//
// The order in which Dependencies are applied matters.  When multiple
// Dependencies are applied to a given Span at the same time, earlier-applied
// Dependencies are causally dependent on later-applied ones (e.g., if an
// outgoing Dependency O is applied to Span S at time T, then an incoming
// Dependency I is applied to Span S at time T, an ElementarySpan beginning
// with I and ending with O will result.)  Generally this doesn't matter, but
// can have an effect during trace transformation.
//
// Similarly, there is tension between the order in which suspends and
// Dependencies are applied to Spans (and hence the flexibility of constructing
// new traces), this library's ability to detect and report problems in the
// trace, and the simplicity of the ElementarySpan decomposition.  For
// example, suppose Span S running from time 0 to 100, into which a suspend
// is applied from 20 to 50, an incoming and outgoing Dependency at time 50,
// and another suspend applied from 50 to 80.  If during this Span's
// construction the suspends are applied before the Dependencies, those
// suspends, though abutting with no Dependencies between them, must not be
// merged, as subsequent Dependencies will be placed between them.  So, this
// library is conservative: it permits zero-duration ElementarySpans, even
// when they have no Dependencies, and does not merge abutting suspends.  This
// maximizes flexibility at the cost of simplicity -- it's possible that, after
// all Dependencies and suspends are added, Spans may contain zero-length
// ElementarySpans with no Dependencies.  To rectify this, after all suspends
// and Dependencies have been applied to a trace, the trace's Simplify method
// can remove such ElementarySpans -- however, invoking Simplify before all
// Dependencies have been added may leave subsequently-added Dependencies no
// place to go.
//
// Trace has four parameterized type arguments:
//   - T any: the type of the temporal dimension of the trace (e.g.,
//     time.Duration or time.Time).  A Comparator implementation with this type
//     must also be provided.
//   - CP fmt.Stringer: the type of Category payloads.  Category payloads
//     provide storage for Category metadata not captured in the interface, such
//     as display name, Category type, and so forth.  Category payloads must
//     implement fmt.Stringer to enable full Category serialization.
//   - SP fmt.Stringer: the type of Span payloads.  Span payloads provide
//     storage for Span metadata not captured in the interface, such as display
//     name, type, and internal structure such as trace events.  Span payloads
//     must implement fmt.Stringer to enable full Span serialization.
//   - DP fmt.Stringer: the type of Dependency payloads.  Dependency payloads
//     provide storage for Dependency metadata beyond type.  Dependency
//     payloads must implement fmt.Stringer to enable full Dependency
//     serialization.

package trace

import (
	"fmt"
)

// Comparator describes types able to compute a float difference between two
// instances of the parameterized type, and to add such differences to
// instances of the parameterized type.  A Comparator must be defined for any
// type used as T in Trace.
type Comparator[T any] interface {
	comparatorBase[T]
	// Returns true iff a < b.
	Less(a, b T) bool
	// Returns true iff a <= b.
	LessOrEqual(a, b T) bool
	// Returns true iff a == b.
	Equal(a, b T) bool
	// Returns true iff a >= b.
	GreaterOrEqual(a, b T) bool
	// Returns true iff a > b.
	Greater(a, b T) bool
}

// DependencyType defines a particular type of trace Dependency.
type DependencyType uint

const (
	// Call Dependencies occur when one Span calls into another.  The calling
	// Span is suspended until the called Span returns.
	Call DependencyType = iota
	// Return Dependencies occur when a called Span returns to its caller,
	// unsuspending it.
	Return

	// FirstUserDefinedDependencyType is the DependencyType value at which
	// specific trace libraries should begin enumerating their DependencyTypes.
	FirstUserDefinedDependencyType
)

// HierarchyType enumerates a trace's available hierarchy types: the different
// kinds of nontemporal Category structures into which the trace's temporal
// elements fit.  For example, a Span representing some single-threaded
// execution may be displayed in a 'swim lane' for its thread, which is in
// turn nested under its process; alternatively, it may be displayed in a lane
// for its CPU, which is nested under its die, socket, and NUMA node.
//
// HierarchyType and Trace Category are related: each Category belongs to
// exactly one HierarchyType.  When providing a Trace implementation, consider
// defining a HierarchyType for each hierarchy of Categories you'd like to
// offer in the trace view.
type HierarchyType uint

const (
	// SpanOnlyHierarchyType designates a hierarchy with no categories, and only
	// spans.  Root spans form the top level of this hierarchy, and each non-root
	// span is nested under its parent span.
	SpanOnlyHierarchyType HierarchyType = iota
	// FirstUserDefinedHierarchyType is the HierarchyType value at which
	// specific trace libraries should begin enumerating their HierarchyTypes.
	FirstUserDefinedHierarchyType
)

// Namer describes types which can provide names for trace elements: categories
// and spans.
type Namer[T any, CP, SP, DP fmt.Stringer] interface {
	// Returns the pattern-matchable name of the provided category.  This can be
	// computed from anything in the category, such as its payload.  This is
	// invoked frequently, so it should be inexpensive to compute or cached.
	CategoryName(category Category[T, CP, SP, DP]) string
	// Returns a unique identifier for the provided category.  This must be
	// unique at least among the category's siblings and deterministically
	// determined.
	CategoryUniqueID(category Category[T, CP, SP, DP]) string
	// Returns a string representing the pattern-matchable name of the provided
	// span.  This string must be stable, be the same for identical spans, and
	// unique among all the span's siblings.  It can be computed from anything in
	// the span, such as its payload.  This is invoked frequently, so it should
	// be inexpensive to compute or cached.
	SpanName(span Span[T, CP, SP, DP]) string
	// Returns a unique identifier for the provided span.  This must be
	// unique at least among the span's siblings and deterministically
	// determined.
	SpanUniqueID(span Span[T, CP, SP, DP]) string
	// Returns the available HierarchyTypes.
	HierarchyTypes() *HierarchyTypes
	// Returns a human-readable name for the provided DependencyType.
	DependencyTypes() *DependencyTypes
	// Returns a human-readable representation of the provided moment.
	MomentString(t T) string
}

// Wrapper wraps a raw Trace and may provide arbitrary trace-type-specific
// members and methods, such as type-specific critical path endpoints, trace
// context information, and so on.  Analysis tools that might depend on this
// trace-type-specific information should work with Wrappers instead of raw
// Traces.
type Wrapper[T any, CP, SP, DP fmt.Stringer] interface {
	Trace() Trace[T, CP, SP, DP]
}

// Trace is an entire trace (as defined above): a set of potentially-nested Spans
// interconnected with Dependencies, and zero or more Category hierarchies.
type Trace[T any, CP, SP, DP fmt.Stringer] interface {
	// All traces serve as their own Wrappers, so that analysis tools can
	// flexibly work with Traces directly or with their wrappers.
	Wrapper[T, CP, SP, DP]
	// Creates and returns a new root Category of the specified HierarchyType,
	// and with the specified payload, under the receiver.
	NewRootCategory(ht HierarchyType, payload CP) Category[T, CP, SP, DP]
	// Creates and returns a new RootSpan under the Trace.  The same RootSpan
	// may appear in all Category hierarchies -- indeed, each RootSpan should
	// appear in all hierarchies, to avoid some Spans being locatable only in
	// certain hierarchies -- but may appear under only one Category for each
	// HierarchyType.)
	NewRootSpan(start, end T, payload SP) RootSpan[T, CP, SP, DP]
	// Creates and returns a new (but incomplete) Dependency of the specified
	// type.  The returned Dependency is intended to be completed by the caller
	// or, in some cases, an incomplete Dependency might be warranted to show
	// where traces are lacking information, such as a Dependency on an untraced
	// RPC.
	NewDependency(dependencyType DependencyType, payload DP, dependencyOptions ...DependencyOption) Dependency[T, CP, SP, DP]
	// Simplifies the receiver's Spans by merging abutting ElementarySpans and
	// suspend intervals where no Dependencies intervene.  This optional method
	// only simplifies the trace, and does not affect the semantics of the trace.
	// Because it can remove ElementarySpan endpoints where Dependencies could
	// eventually be added (e.g., with SmartDependencies), this should be invoked
	// after all Dependences and suspends have been fully added.
	Simplify()
	// Returns the receiver's Comparator.
	Comparator() Comparator[T]
	// Returns the receiver's default Namer.
	DefaultNamer() Namer[T, CP, SP, DP]
	// Returns the Trace's supported HierarchyTypes.
	HierarchyTypes() []HierarchyType
	// Returns the DependencyTypes observed within the Trace.
	DependencyTypes() []DependencyType
	// Returns the root Categories of the specified HierarchyType under the
	// trace.
	RootCategories(ht HierarchyType) []Category[T, CP, SP, DP]
	// Returns the trace's RootSpans.
	RootSpans() []RootSpan[T, CP, SP, DP]
}

// Category is a non-temporal grouping of temporal elements, generally visually
// represented at a discrete y-axis lane.  Categories may nest, and may contain
// child Spans.
type Category[T any, CP, SP, DP fmt.Stringer] interface {
	// Returns the payload of this Category.
	Payload() CP
	// Returns the HierarchyType of this Category.
	HierarchyType() HierarchyType
	// Returns the parent of this Category, or nil if it is a root.
	Parent() Category[T, CP, SP, DP]
	// Returns the children of this Category.
	ChildCategories() []Category[T, CP, SP, DP]
	// Returns the RootSpans under this Category.
	RootSpans() []RootSpan[T, CP, SP, DP]
	// Creates and returns a new child Category under this one, with the
	// provided payload.
	NewChildCategory(payload CP) Category[T, CP, SP, DP]
	// Adds the provided RootSpan under this Category.
	AddRootSpan(rs RootSpan[T, CP, SP, DP]) error
	// Replaces the Category's payload with the provided one.
	UpdatePayload(newPayload CP)
}

type timeRange[T any] interface {
	Start() T
	End() T
}

// SuspendOption defines options applied when creating suspends.
type SuspendOption uint64

// Options applied when creating suspends.  May be ORed together.
const (
	DefaultSuspendOptions SuspendOption = 0
)

// Non-default suspend options.
const (
	// If present, a suspend may cross ElementarySpan boundaries.  If it does
	// so, any endpoint it crosses becomes its own zero-duration ElementarySpan,
	// preserving incoming or outgoing Dependencies.
	SuspendFissionsAroundElementarySpanEndpoints SuspendOption = 1 << iota
	// If present, a suspend may cross marks.  If it does so, any mark it crosses
	// becomes its own zero-duration ElementarySpan.
	SuspendFissionsAroundMarks
)

// Span is a temporal element of a trace, whose temporal extent includes its
// endpoints, that is fully contained within a single Category.  Spans may have
// child Spans (during whose execution the parent is suspended).  Causal
// relationships between Spans are specified with Dependencies.
type Span[T any, CP, SP, DP fmt.Stringer] interface {
	timeRange[T]
	// Creates and returns a new child Span under this one, with the provided
	// timespan and payload.
	NewChildSpan(comparator Comparator[T], start, end T, payload SP) (Span[T, CP, SP, DP], error)
	// Returns this Span's payload.
	Payload() SP
	// Returns this Span's parent Span, or nil if it is a RootSpan.
	ParentSpan() Span[T, CP, SP, DP]
	// Returns the RootSpan ancestral to this span, which may be the receiver.
	RootSpan() RootSpan[T, CP, SP, DP]
	// Returns this Span's children.
	ChildSpans() []Span[T, CP, SP, DP]
	// Marks the provided span at the specified moment with the provided label.
	Mark(comparator Comparator[T], label string, moment T, options ...MarkOption) error
	// Marks this Span as non-running over the specified interval.  Returns an
	// error if the proposed suspend is incompatible with the provided options.
	Suspend(comparator Comparator[T], start, end T, options ...SuspendOption) error
	// Returns the set of ElementarySpans in the receiver, in increasing
	// temporal order.  The returned ElementarySpans represent time during which
	// this Span was not suspended; gaps between ElementarySpans represent time
	// during which the Span was suspended, waiting for some Dependency to
	// resolve.  The returned ElementarySpans may abut but should never overlap.
	ElementarySpans() []ElementarySpan[T, CP, SP, DP]
	// Replaces the Span's payload with the provided one.
	UpdatePayload(newPayload SP)

	// Simplifies this span and its children, as described for Trace.Simplify.
	// The existence of this private function in this template ensures that only
	// implementations of these interfaces provided in this package can exist.
	simplify(Comparator[T])
}

// RootSpan is a top-level Span: one without a parent Span.  Instead, it has
// a set of parent Categories, up to one per hierarchy type.
type RootSpan[T any, CP, SP, DP fmt.Stringer] interface {
	Span[T, CP, SP, DP]

	ParentCategory(ht HierarchyType) Category[T, CP, SP, DP]
}

// Mark represents a labeled position within a trace.
type Mark[T any] interface {
	Label() string
	Moment() T
}

// MarkOption specifies options applied when creating dependencies.
type MarkOption uint64

// Options applied when creating dependency endpoints.  May be ORed together.
const (
	DefaultMarkOptions MarkOption = 0
)

// Non-default mark options.
const (
	// If present, this mark can fission a preexisting suspend
	// region.
	MarkCanFissionSuspend MarkOption = 1 << iota
)

// ElementarySpan is a temporal portion of a Span during which the Span's view
// of the rest of the Trace, and the rest of the Trace's view of the Span, is
// causally unchanged.  ElementarySpans are comparable to compiler basic
// blocks or logical clocks: they provide a natural granularity for causality
// analysis -- the level of causality recorded in the trace, if any part of an
// ElementarySpan is executed, the whole thing will be executed.  Like basic
// blocks, ElementarySpans have many analysis applications: each piece of a
// critical path, except possibly the endpoints, is a whole ElementarySpan;
// headroom metrics like drag or slack are computed on a per-ElementarySpan
// basis, and ElementarySpans are central to trace transformations.  Within a
// Span, every incoming Dependency marks the start of a new ElementarySpan, and
// every outgoing Dependency marks the end of one.  ElementarySpans are
// automatically created within their parents with the SetOriginSpan,
// AddDestinationSpan, and AddDestinationSpanAfterWait methods of Dependencies.
type ElementarySpan[T any, CP, SP, DP fmt.Stringer] interface {
	timeRange[T]
	// This ElementarySpan's parent Span.
	Span() Span[T, CP, SP, DP]
	// Returns the marks in this ElementarySpan, if any.
	Marks() []Mark[T]
	// The ElementarySpan preceding this one in its parent Span.  nil for the
	// first ElementarySpan within a Span.
	Predecessor() ElementarySpan[T, CP, SP, DP]
	// The ElementarySpan succeeding this one in its parent Span.  nil for the
	// last ElementarySpan within a Span.
	Successor() ElementarySpan[T, CP, SP, DP]
	// The Dependency, if any, at the start of this ElementarySpan.
	// If non-nil, this ElementarySpan will be among Incoming.Destinations.
	Incoming() Dependency[T, CP, SP, DP] // nil if none.
	// The Dependency, if any, at the end of this ElementarySpan.
	// If non-nil, this ElementarySpan will be in Outgoing.Origins(); if this is
	// the Dependency's triggering origin, this ElementarySpan will be
	// Outgoing.TriggeringOrigin().
	Outgoing() Dependency[T, CP, SP, DP] // nil if none.
}

// DependencyEndpointOption specifies options applied when creating dependencies.
type DependencyEndpointOption uint64

// Options applied when creating dependency endpoints.  May be ORed together.
const (
	DefaultDependencyEndpointOptions DependencyEndpointOption = 0
)

// Non-default dependency endpoint options.
const (
	// If present, this dependency endpoint can fission a preexisting suspend
	// region.
	DependencyEndpointCanFissionSuspend DependencyEndpointOption = 1 << iota
	// If present, this dependency endpoint should be placed prior to other
	// previously-added dependency endpoints in the same span at the same time.
	// Note that the first incoming dependency within a span at a given moment
	// will always occur before the last outgoing one in that span at that
	// moment.  Incompatible with PlaceDependencyEndpointAsLateAsPossible.
	PlaceDependencyEndpointAsEarlyAsPossible
	// If present this dependency endpoint should be placed after other
	// previously-added dependency endpoints in the same span at the same time.
	// Note that the first incoming dependency within a span at a given moment
	// will always occur before the last outgoing one in that span at that
	// moment.  Incompatible with PlaceDependencyEndpointAsEarlyAsPossible.
	PlaceDependencyEndpointAsLateAsPossible
)

// DependencyOption records dependency options.
type DependencyOption int

const (
	// DefaultDependencyOptions is the default set of dependency properties.
	DefaultDependencyOptions DependencyOption = 0
)

const (
	multipleOrigins DependencyOption = 1 << iota
	andSemantics
	orSemantics
)

// Non-default dependency options.
const (
	// Indicates that this dependency may have several origins, and those origins
	// have AND-semantics (meaning that the latest-resolving origin is the causal
	// trigger).
	// AND-origin semantics reflect code blocked on all of several events, for
	// example when code blocks until one file descriptor is readable and another
	// is writeable.  Noting these semantics allows them to be preserved through
	// trace transformation.
	MultipleOriginsWithAndSemantics = multipleOrigins | andSemantics
	// Indicates that this dependency may have several origins, and those origins
	// have OR-semantics (meaning that the earliest-resolving origin is the
	// causal trigger).
	// OR-origin semantics reflect code blocked on any of several events.  An
	// example is spawning work whose results will be cached or memoized: such a
	// memoized result may be consumed in several places, but the work is only
	// actually performed when its first consumer requests it.  Noting these
	// semantics allows them to be preserved through trace transformation.
	MultipleOriginsWithOrSemantics = multipleOrigins | orSemantics
)

// Includes returns true iff the receiver includes the provided bitmap mask.
func (do DependencyOption) Includes(props DependencyOption) bool {
	return do&props == props
}

// Dependency represents a causal dependency between two or more Spans.  Each
// Dependency has a type, a single origin (a point within an origin Span), and
// one or more destinations (points within destination Spans).  Relative to a
// particular destination, a Dependency also has a duration -- the interval
// between the origin point and that destination's point; this duration is
// often referred to as 'scheduling delay', since it can represent time in
// which the destination could have been running, but wasn't.
type Dependency[T any, CP, SP, DP fmt.Stringer] interface {
	// The type of this Dependency.
	DependencyType() DependencyType
	// The triggering origin of this Dependency.  If nil, this Dependency is
	// incomplete.  If non-nil, this Dependency will be
	// TriggeringOrigin().Outgoing().
	TriggeringOrigin() ElementarySpan[T, CP, SP, DP]
	// All possible origins of this Dependency.  A dependency may only have one
	// *triggering* origin, representing the point in the trace where the
	// dependency was, causally, fully resolved, and returned by
	// TriggeringOrigin(). However, there may be other dependency origins which
	// are not triggering, but may become so under transformation.
	Origins() []ElementarySpan[T, CP, SP, DP]
	// Returns this Dependency's Options.
	Options() DependencyOption
	// The destinations of this Dependency.  If nil, this Dependency is
	// incomplete.  If non-nil, this Dependency will be dest.Incoming() for each
	// dest in Destinations().
	Destinations() []ElementarySpan[T, CP, SP, DP]
	// Returns this Dependency's payload.
	Payload() DP
	// Sets the origin Span of the Dependency, possibly adding a new
	// ElementarySpan in that origin Span ending at the start point.  Returns an
	// error if the specified point does not lie within a running portion of the
	// Span.
	SetOriginSpan(
		comparator Comparator[T],
		from Span[T, CP, SP, DP],
		start T,
		options ...DependencyEndpointOption,
	) error
	// Adds a destination Span into the Dependency, possibly adding a new
	// ElementarySpan in that destination Span starting at the end point.
	// Returns an error if the specified point does not lie within a running
	// portion of the Span.
	AddDestinationSpan(
		comparator Comparator[T],
		to Span[T, CP, SP, DP],
		end T,
		options ...DependencyEndpointOption,
	) error
	// Adds a destination Span into the Dependency, possibly adding a new
	// ElementarySpan in that destination Span starting at the end point, and
	// introducing a suspended interval in the destination Span between the wait
	// point and the end point.  Returns an error if the specified end point does
	// not lie within a running portion of the Span, or if the wait would overlap
	// an existing Dependency in the Span.
	AddDestinationSpanAfterWait(
		comparator Comparator[T],
		from Span[T, CP, SP, DP],
		waitFrom,
		end T,
		options ...DependencyEndpointOption,
	) error
}
