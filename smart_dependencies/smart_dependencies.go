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

// Package smartdependencies provides types and tools supporting 'smart
// dependencies': dependencies that can tolerate common causal idiosyncracies
// in raw trace data.
//
// A trace Dependency represents a causal relationship between two or more
// trace Spans.  In many trace formats, a single Dependency is actually
// represented by two or more 'events' embedded within Spans -- for example,
// a send dependency from Span A at time T1 to Span B at time T2 might be
// encoded as a 'send' event in Span A at time T1 and a 'receive' event in
// Span B at time T2, with both events containing a 'message_id' field with the
// same value.  This representation strategy is efficient and easy to output at
// runtime, but imposes validity constraints on the trace data that cannot be
// enforced by trace structure alone.  For example, continuing with the 'send'/
// 'receive' example:
//
//   - A 'send' event may include a 'message_id' that no 'receive' event has,
//     or the converse.  This can represent untraced behavior, or can simply
//     be an artifact of what is knowable in the system at the time of tracing.
//   - Due to clock skew or different clock domains, a 'receive' event may
//     precede its corresponding 'send' in trace time.
//   - Multiple 'send' events may reuse the same 'message_id', either due to
//     incomplete tracing or to the impracticability of ensuring unique
//     'message_ids'.  For instance, the natural linking identifier for most
//     synchronization events is the pointer of the relevant lock, barrier, or
//     other synchronization object.  However, such objects are regularly (and
//     legitimately) reused.
//
// SmartDependencies can help build coherent traces in the presence of such
// irregularities, by deferring the construction of actual trace Dependencies
// until their validity is confirmed, and by using temporal ordering to resolve
// cases of ID reuse.
package smartdependencies

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/tracey/trace"
)

// Option defines options for smart dependencies.
type Option uint

func (sdo Option) includes(other Option) bool {
	return sdo&other == other
}

// Smart dependency options.
const (
	Defaults Option = 0
	// Sometimes it is not possible to distinguish between two different origins
	// in a traced dependence -- for example, when a single logged barrier ID is
	// reused for several different synchronization events.  In such cases,
	// AllowReuse allows a single SmartDependency to have multiple origins, each
	// of which creates its own dependency, and each destination depends upon the
	// most recent prior origin.
	AllowReuse Option = 1 << iota
	// Allow dependencies with no origin to be created.
	KeepDependenciesWithoutOrigins
	// Allow dependencies with no destination to be created.
	KeepDependenciesWithoutDestinations

	KeepDependenciesWithoutOriginsOrDestinations = KeepDependenciesWithoutOrigins | KeepDependenciesWithoutDestinations
)

type endpoint[T any, CP, SP, DP fmt.Stringer] struct {
	span              trace.Span[T, CP, SP, DP]
	waitFrom, at      T
	hasWait           bool
	dependencyOptions []trace.DependencyOption
}

// SmartDependency defines a smart dependency helper.
type SmartDependency[T any, CP, SP, DP fmt.Stringer] interface {
	WithOrigin(from trace.Span[T, CP, SP, DP], start T, options ...trace.DependencyOption) SmartDependency[T, CP, SP, DP]
	WithDestination(to trace.Span[T, CP, SP, DP], end T, options ...trace.DependencyOption) SmartDependency[T, CP, SP, DP]
	WithDestinationAfterWait(to trace.Span[T, CP, SP, DP], waitFrom, end T, options ...trace.DependencyOption) SmartDependency[T, CP, SP, DP]

	close(t trace.Trace[T, CP, SP, DP], metrics *Metrics) error
	getOptions() Option
}

type smartDependency[ID comparable, T any, CP, SP, DP fmt.Stringer] struct {
	dependencyType trace.DependencyType
	payload        DP
	hasID          bool
	id             ID
	options        Option
	origins        []*endpoint[T, CP, SP, DP]
	destinations   []*endpoint[T, CP, SP, DP]
}

// Returns a new SmartDependency of the specified type and with the specified
// ID.
func newWithID[ID comparable, T any, CP, SP, DP fmt.Stringer](
	dependencyType trace.DependencyType,
	payload DP,
	id ID,
	options Option,
) *smartDependency[ID, T, CP, SP, DP] {
	return &smartDependency[ID, T, CP, SP, DP]{
		dependencyType: dependencyType,
		payload:        payload,
		hasID:          true,
		id:             id,
		options:        options,
	}
}

// Returns a new SmartDependency of the specified type.
func new[ID comparable, T any, CP, SP, DP fmt.Stringer](
	dependencyType trace.DependencyType,
	payload DP,
	options Option,
) *smartDependency[ID, T, CP, SP, DP] {
	return &smartDependency[ID, T, CP, SP, DP]{
		dependencyType: dependencyType,
		payload:        payload,
		options:        options,
	}
}

// WithOrigin marks the specified point in the provided span as an origin of
// this dependency.
func (sd *smartDependency[ID, T, CP, SP, DP]) WithOrigin(
	from trace.Span[T, CP, SP, DP],
	start T,
	options ...trace.DependencyOption,
) SmartDependency[T, CP, SP, DP] {
	sd.origins = append(sd.origins, &endpoint[T, CP, SP, DP]{
		span:              from,
		at:                start,
		dependencyOptions: options,
	})
	return sd
}

// WithDestination marks the specified point in the provided span as a
// destination of this dependency.
func (sd *smartDependency[ID, T, CP, SP, DP]) WithDestination(
	to trace.Span[T, CP, SP, DP],
	end T,
	options ...trace.DependencyOption,
) SmartDependency[T, CP, SP, DP] {
	sd.destinations = append(sd.destinations, &endpoint[T, CP, SP, DP]{
		span:              to,
		at:                end,
		dependencyOptions: options,
	})
	return sd
}

// WithDestinationAfterWait marks the specified end point in the provided span
// as a destination of this dependency, after a wait beginning at the specified
// wait point.
func (sd *smartDependency[ID, T, CP, SP, DP]) WithDestinationAfterWait(
	to trace.Span[T, CP, SP, DP],
	waitFrom,
	end T,
	options ...trace.DependencyOption,
) SmartDependency[T, CP, SP, DP] {
	sd.destinations = append(sd.destinations, &endpoint[T, CP, SP, DP]{
		span:              to,
		waitFrom:          waitFrom,
		at:                end,
		hasWait:           true,
		dependencyOptions: options,
	})
	return sd
}

// Metrics aggregates metrics about dependency creation success.  Unpaired
// dependencies (those with only one side of the dependency populated) or
// dropped dependencies (those for which instantiated SmartDependencies did not
// result in trace created Dependencies) can signify causal flaws in the trace,
// and may hinder causal analyses.
type Metrics struct {
	// The count of added origins that could not be added to a dependency,
	// broken down by dependency type.
	UnpairedOriginsByType map[trace.DependencyType]uint
	// The count of added destinations that could not be added to a dependency,
	// broken down by dependency type.
	UnpairedDestinationsByType map[trace.DependencyType]uint
	// The count of fully-paired dependencies created.  A dependency is fully-
	// paired if it has an origin and at least one destination.
	PairedDependenciesByType map[trace.DependencyType]uint
	// The count of dependencies created.  Dependencies configured with neither
	// KeepDependenciesWIthoutOrigins nor KeepDependenciesWithoutDestinations
	// increment this the same as PairedDependenciesByType.
	CreatedDependenciesByType map[trace.DependencyType]uint
	// The count of dependencies that could not be created.
	DroppedDependenciesByType map[trace.DependencyType]uint
}

// PrettyPrintMetrics prettyprints the provided SmartDependencies closure
// metrics, using the provided trace namer and indent prefix.
func PrettyPrintMetrics[T any, CP, SP, DP fmt.Stringer](
	m *Metrics,
	namer trace.Namer[T, CP, SP, DP],
	indent string,
) string {
	ret := []string{}
	if len(m.UnpairedOriginsByType) > 0 {
		ret = append(ret, indent+"Unpaired origins by type:")
		for dt, count := range m.UnpairedOriginsByType {
			ret = append(ret, fmt.Sprintf("%s  %-20s: %d", indent, namer.DependencyTypeName(dt), count))
		}
	}
	if len(m.UnpairedDestinationsByType) > 0 {
		ret = append(ret, indent+"Unpaired destinations by type:")
		for dt, count := range m.UnpairedDestinationsByType {
			ret = append(ret, fmt.Sprintf("%s  %-20s: %d", indent, namer.DependencyTypeName(dt), count))
		}
	}
	if len(m.PairedDependenciesByType) > 0 {
		ret = append(ret, indent+"Paired dependencies by type:")
		for dt, count := range m.PairedDependenciesByType {
			ret = append(ret, fmt.Sprintf("%s  %-20s: %d", indent, namer.DependencyTypeName(dt), count))
		}
	}
	if len(m.CreatedDependenciesByType) > 0 {
		ret = append(ret, indent+"Created dependencies by type:")
		for dt, count := range m.CreatedDependenciesByType {
			ret = append(ret, fmt.Sprintf("%s  %-20s: %d", indent, namer.DependencyTypeName(dt), count))
		}
	}
	if len(m.DroppedDependenciesByType) > 0 {
		ret = append(ret, indent+"Dropped dependencies by type:")
		for dt, count := range m.DroppedDependenciesByType {
			ret = append(ret, fmt.Sprintf("%s  %-20s: %d", indent, namer.DependencyTypeName(dt), count))
		}
	}
	return strings.Join(ret, "\n")
}

func newMetrics() *Metrics {
	return &Metrics{
		UnpairedOriginsByType:      map[trace.DependencyType]uint{},
		UnpairedDestinationsByType: map[trace.DependencyType]uint{},
		PairedDependenciesByType:   map[trace.DependencyType]uint{},
		CreatedDependenciesByType:  map[trace.DependencyType]uint{},
		DroppedDependenciesByType:  map[trace.DependencyType]uint{},
	}
}

func (m *Metrics) withUnpairedOrigins(dt trace.DependencyType, count uint) *Metrics {
	if m != nil {
		m.UnpairedOriginsByType[dt] += count
	}
	return m
}

func (m *Metrics) withUnpairedDestinations(dt trace.DependencyType, count uint) *Metrics {
	if m != nil {
		m.UnpairedDestinationsByType[dt] += count
	}
	return m
}

func (m *Metrics) withPairedDependencies(dt trace.DependencyType, count uint) *Metrics {
	if m != nil {
		m.PairedDependenciesByType[dt] += count
	}
	return m
}

func (m *Metrics) withCreatedDependencies(dt trace.DependencyType, count uint) *Metrics {
	if m != nil {
		m.CreatedDependenciesByType[dt] += count
	}
	return m
}

func (m *Metrics) withDroppedDependencies(dt trace.DependencyType, count uint) *Metrics {
	if m != nil {
		m.DroppedDependenciesByType[dt] += count
	}
	return m
}

func (sd *smartDependency[ID, T, CP, SP, DP]) wrapError(err error) error {
	if err == nil {
		return nil
	}
	if sd.hasID {
		return fmt.Errorf("can't close dependency of type %v with id %v: %s", sd.dependencyType, sd.id, err)
	}
	return fmt.Errorf("can't close dependency of type %v: %s", sd.dependencyType, err)
}

func (sd *smartDependency[ID, T, CP, SP, DP]) getOptions() Option {
	return sd.options
}

// close completes the Dependency.
func (sd *smartDependency[ID, T, CP, SP, DP]) close(t trace.Trace[T, CP, SP, DP], metrics *Metrics) error {
	var lastOrigin *endpoint[T, CP, SP, DP]
	var lastDestinations []*endpoint[T, CP, SP, DP]
	update := func() error {
		if (lastOrigin != nil || sd.options.includes(KeepDependenciesWithoutOrigins)) &&
			(len(lastDestinations) > 0 || sd.options.includes(KeepDependenciesWithoutDestinations)) {
			dep := t.NewDependency(sd.dependencyType, sd.payload)
			metrics.withCreatedDependencies(sd.dependencyType, 1)
			if lastOrigin != nil && len(lastDestinations) > 0 {
				metrics.withPairedDependencies(sd.dependencyType, 1)
			}
			if lastOrigin != nil {
				if err := dep.SetOriginSpan(
					t.Comparator(),
					lastOrigin.span,
					lastOrigin.at,
					lastOrigin.dependencyOptions...,
				); err != nil {
					return err
				}
			}
			for _, lastDestination := range lastDestinations {
				if lastDestination.hasWait {
					if err := dep.AddDestinationSpanAfterWait(
						t.Comparator(),
						lastDestination.span,
						lastDestination.waitFrom,
						lastDestination.at,
						lastDestination.dependencyOptions...,
					); err != nil {
						return err
					}
				} else {
					if err := dep.AddDestinationSpan(
						t.Comparator(),
						lastDestination.span,
						lastDestination.at,
						lastDestination.dependencyOptions...,
					); err != nil {
						return err
					}
				}
			}
		} else {
			if lastOrigin != nil {
				metrics.
					withUnpairedOrigins(sd.dependencyType, 1)
			}
			metrics.
				withUnpairedDestinations(sd.dependencyType, uint(len(lastDestinations)))
			if lastOrigin != nil || len(lastDestinations) > 0 {
				metrics.withDroppedDependencies(sd.dependencyType, 1)
			}
		}
		lastOrigin, lastDestinations = nil, nil
		return nil
	}
	if sd.options.includes(AllowReuse) {
		sort.Slice(sd.origins, func(a, b int) bool {
			return t.Comparator().Less(sd.origins[a].at, sd.origins[b].at)
		})
		sort.Slice(sd.destinations, func(a, b int) bool {
			return t.Comparator().Less(sd.destinations[a].at, sd.destinations[b].at)
		})
		for len(sd.origins) > 0 || len(sd.destinations) > 0 {
			switch {
			case len(sd.origins) > 0 && len(sd.destinations) > 0:
				if t.Comparator().Less(sd.origins[0].at, sd.destinations[0].at) {
					// Pop and process an origin.
					if err := update(); err != nil {
						return sd.wrapError(err)
					}
					lastOrigin, sd.origins = sd.origins[0], sd.origins[1:]
				} else {
					lastDestinations = append(lastDestinations, sd.destinations[0])
					sd.destinations = sd.destinations[1:]
				}
			case len(sd.origins) > 0:
				if err := update(); err != nil {
					return sd.wrapError(err)
				}
				lastOrigin, sd.origins = sd.origins[0], sd.origins[1:]
			default:
				lastDestinations = append(lastDestinations, sd.destinations...)
				sd.destinations = nil
			}
		}
	} else {
		if len(sd.origins) > 1 {
			return sd.wrapError(fmt.Errorf("multiple origins specified but reuse is not allowed"))
		}
		if len(sd.origins) > 0 {
			lastOrigin, sd.origins = sd.origins[0], nil
		}
		lastDestinations, sd.destinations = sd.destinations, nil
	}
	return sd.wrapError(update())
}

// SmartDependencies manages a collection of SmartDependency instances,
// supporting dependency lookup and closure.
type SmartDependencies[ID comparable, T any, CP, SP, DP fmt.Stringer] struct {
	t                      trace.Trace[T, CP, SP, DP]
	indexedDepsByTypeAndID map[trace.DependencyType]map[ID]SmartDependency[T, CP, SP, DP]
	unindexedDeps          []SmartDependency[T, CP, SP, DP]
	depsInDefinitionOrder  []SmartDependency[T, CP, SP, DP]
}

// New returns a new, empty SmartDependencies set.
func New[ID comparable, T any, CP, SP, DP fmt.Stringer](t trace.Trace[T, CP, SP, DP]) *SmartDependencies[ID, T, CP, SP, DP] {
	return &SmartDependencies[ID, T, CP, SP, DP]{
		t:                      t,
		indexedDepsByTypeAndID: map[trace.DependencyType]map[ID]SmartDependency[T, CP, SP, DP]{},
	}
}

// NewUnindexed returns a new unindexed SmartDependency of the specified type.
// Unindexed SmartDependencies are not indexed by IDs, making it the caller's
// responsibility to save them.
func (sd *SmartDependencies[ID, T, CP, SP, DP]) NewUnindexed(
	dependencyType trace.DependencyType,
	payload DP,
	options Option,
) SmartDependency[T, CP, SP, DP] {
	dep := new[ID, T, CP, SP, DP](dependencyType, payload, options)
	sd.unindexedDeps = append(sd.unindexedDeps, dep)
	sd.depsInDefinitionOrder = append(sd.depsInDefinitionOrder, dep)
	return dep
}

// GetIndexed returns the SmartDependency of the specified type indexed under
// the provided ID, creating it with the specified options if it doesn't
// already exist.  Returns true if the provided SmartDependency was created
// during this call.  Returns an error if it already exists but with different
// options.
func (sd *SmartDependencies[ID, T, CP, SP, DP]) GetIndexed(
	dependencyType trace.DependencyType,
	payload DP,
	id ID,
	options Option,
) (dep SmartDependency[T, CP, SP, DP], created bool, err error) {
	depsByID, ok := sd.indexedDepsByTypeAndID[dependencyType]
	if !ok {
		depsByID = map[ID]SmartDependency[T, CP, SP, DP]{}
		sd.indexedDepsByTypeAndID[dependencyType] = depsByID
	}
	dep, ok = depsByID[id]
	if !ok {
		dep = newWithID[ID, T, CP, SP, DP](dependencyType, payload, id, options)
		created = true
		sd.depsInDefinitionOrder = append(sd.depsInDefinitionOrder, dep)
		depsByID[id] = dep
	} else {
		if dep.getOptions() != options {
			return nil, false, fmt.Errorf("dependency %d:%v already exists with different options (old %d, new %d)", dependencyType, id, dep.getOptions(), options)
		}
	}
	return dep, created, nil
}

// Close closes all contained SmartDependencies and clears the receiver.  Upon
// an error, fails fast and returns that error.
func (sd *SmartDependencies[ID, T, CP, SP, DP]) Close() error {
	defer func() {
		sd.indexedDepsByTypeAndID = map[trace.DependencyType]map[ID]SmartDependency[T, CP, SP, DP]{}
		sd.unindexedDeps = nil
	}()
	for _, dep := range sd.depsInDefinitionOrder {
		if err := dep.close(sd.t, nil); err != nil {
			return err
		}
	}
	return nil
}

// CloseWithMetrics closes all contained SmartDependencies and clears the
// receiver, returning metrics about dependency creation success.  Upon an
// error, fails fast and returns that error and nil metrics.
func (sd *SmartDependencies[ID, T, CP, SP, DP]) CloseWithMetrics() (*Metrics, error) {
	defer func() {
		sd.indexedDepsByTypeAndID = map[trace.DependencyType]map[ID]SmartDependency[T, CP, SP, DP]{}
		sd.unindexedDeps = nil
	}()
	metrics := newMetrics()
	for _, dep := range sd.depsInDefinitionOrder {
		if err := dep.close(sd.t, metrics); err != nil {
			return nil, err
		}
	}
	return metrics, nil
}
