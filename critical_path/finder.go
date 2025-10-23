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

package criticalpath

import (
	"fmt"

	"github.com/google/tracey/trace"
	traceparser "github.com/google/tracey/trace/parser"
)

// Endpoints represents a {start, end} pair of Endpoints within a trace.
type Endpoints[T any, CP, SP, DP fmt.Stringer] struct {
	Start, End *Endpoint[T, CP, SP, DP]
}

// Trace defines a trace.Wrapper able to also return critical path endpoints.
type Trace[T any, CP, SP, DP fmt.Stringer] interface {
	trace.Wrapper[T, CP, SP, DP]
	GetEndpoints(td *TypeData) (*Endpoints[T, CP, SP, DP], bool)
	SupportedTypeData() []*TypeData
}

// Finder finds a particular critical path of a particular type in a Trace.
type Finder struct {
	cpTypeData                                           *TypeData
	cpStrategy                                           Strategy
	opts                                                 []Option
	customStartPositionPattern, customEndPositionPattern *traceparser.PositionPattern
}

// NewFinder returns a new Finder.
func NewFinder(
	cpTypeData *TypeData,
	hierarchyType trace.HierarchyType,
	customStartPositionString, customEndPositionString string,
	cpStrategy Strategy,
	opts ...Option,
) (*Finder, error) {
	ret := &Finder{
		cpTypeData: cpTypeData,
		cpStrategy: cpStrategy,
		opts:       opts,
	}
	var err error
	if customStartPositionString != "" {
		ret.customStartPositionPattern, err = traceparser.ParsePositionSpecifiers(
			hierarchyType, customStartPositionString,
		)
		if err != nil {
			return nil, err
		}
	}
	if customEndPositionString != "" {
		ret.customEndPositionPattern, err = traceparser.ParsePositionSpecifiers(
			hierarchyType, customEndPositionString,
		)
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}

// Returns the receiver's endpoints within the provided trace, or an error if
// this is not possible.
func endpoints[T any, CP, SP, DP fmt.Stringer](
	f *Finder,
	tr Trace[T, CP, SP, DP],
) (*Endpoints[T, CP, SP, DP], error) {
	if f.cpTypeData.Type == CustomCriticalPathType {
		var start, end *Endpoint[T, CP, SP, DP]
		if f.customStartPositionPattern != nil {
			startPosFinder, err := traceparser.NewPositionFinder(
				f.customStartPositionPattern,
				tr.Trace(),
			)
			if err != nil {
				return nil, err
			}
			startESPS := startPosFinder.FindPositions()
			if len(startESPS) == 0 {
				return nil, fmt.Errorf("cannot find custom start position")
			}
			if len(startESPS) > 1 {
				return nil, fmt.Errorf("found more than one custom start position")
			}
			start = EndpointFromElementarySpanPosition(startESPS[0])
		}
		if f.customEndPositionPattern != nil {
			endPosFinder, err := traceparser.NewPositionFinder(
				f.customEndPositionPattern,
				tr.Trace(),
			)
			if err != nil {
				return nil, err
			}
			endESPS := endPosFinder.FindPositions()
			if len(endESPS) == 0 {
				return nil, fmt.Errorf("cannot find custom end position")
			}
			if len(endESPS) > 1 {
				return nil, fmt.Errorf("found more than one custom end position")
			}
			end = EndpointFromElementarySpanPosition(endESPS[0])
		}
		return &Endpoints[T, CP, SP, DP]{
			Start: start,
			End:   end,
		}, nil
	}
	ret, ok := tr.GetEndpoints(f.cpTypeData)
	if !ok {
		return nil, fmt.Errorf("could not find endpoints for critical path type '%s'", f.cpTypeData.Name)
	}
	return ret, nil
}

// Find returns the receiver's configured critical path, and its temporal
// bounds, from the specified trace.  If the provided cpCache is non-nil, it
// is used to fetch the critical path.
func Find[T any, CP, SP, DP fmt.Stringer](
	f *Finder,
	tr Trace[T, CP, SP, DP],
	cpCache *Cache[T, CP, SP, DP],
) (
	cp *Path[T, CP, SP, DP],
	err error,
) {
	eps, err := endpoints(f, tr)
	if err != nil {
		return cp, err
	}
	if cpCache != nil {
		cp, err = cpCache.GetBetweenEndpoints(tr.Trace(), eps.Start, eps.End, f.cpStrategy, f.opts...)
	} else {
		cp, err = FindBetweenEndpoints(tr.Trace(), eps.Start, eps.End, f.cpStrategy, f.opts...)
	}
	return cp, err
}
