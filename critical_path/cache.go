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
	"sync"

	"github.com/google/tracey/trace"
)

type computedCP[T any, CP, SP, DP fmt.Stringer] struct {
	err      error
	cachedCP *Path[T, CP, SP, DP]
}

type cachedCPSet[T any, CP, SP, DP fmt.Stringer] struct {
	origin, destination trace.ElementarySpan[T, CP, SP, DP]
	computedCPs         map[string]*computedCP[T, CP, SP, DP]
}

func newCachedCPSet[T any, CP, SP, DP fmt.Stringer](
	origin, destination trace.ElementarySpan[T, CP, SP, DP],
) *cachedCPSet[T, CP, SP, DP] {
	return &cachedCPSet[T, CP, SP, DP]{
		origin:      origin,
		destination: destination,
		computedCPs: map[string]*computedCP[T, CP, SP, DP]{},
	}
}

func computeCacheKey(strategy Strategy, opts *options) string {
	return fmt.Sprintf("%d:%t", strategy, opts.includePositiveNontriggeringOrigins)
}

var errMissingEndpoint = fmt.Errorf("requested critical path lacks at least one endpoint")

func (cpSet *cachedCPSet[T, CP, SP, DP]) get(
	tr trace.Trace[T, CP, SP, DP],
	strategy Strategy,
	opts *options,
	mu *sync.Mutex,
) (*Path[T, CP, SP, DP], error) {
	cacheKey := computeCacheKey(strategy, opts)
	ccp, ok := cpSet.computedCPs[cacheKey]
	if !ok {
		mu.Lock()
		defer mu.Unlock()
		ccp = &computedCP[T, CP, SP, DP]{}
		ccp.cachedCP, ccp.err = findBetweenElementarySpans(tr, cpSet.origin, cpSet.destination, strategy, opts)
		cpSet.computedCPs[cacheKey] = ccp
	}
	return ccp.cachedCP, ccp.err
}

// Cache caches precomputed critical paths to amortize the cost of computing
// expensive ones.
type Cache[T any, CP, SP, DP fmt.Stringer] struct {
	mu                              sync.Mutex
	cpSetsByOriginAndDestinationESs map[trace.ElementarySpan[T, CP, SP, DP]]map[trace.ElementarySpan[T, CP, SP, DP]]*cachedCPSet[T, CP, SP, DP]
}

// NewCache returns a new, empty Cache.
func NewCache[T any, CP, SP, DP fmt.Stringer]() *Cache[T, CP, SP, DP] {
	return &Cache[T, CP, SP, DP]{
		cpSetsByOriginAndDestinationESs: map[trace.ElementarySpan[T, CP, SP, DP]]map[trace.ElementarySpan[T, CP, SP, DP]]*cachedCPSet[T, CP, SP, DP]{},
	}
}

func (c *Cache[T, CP, SP, DP]) getCachedCPSet(origin, destination trace.ElementarySpan[T, CP, SP, DP]) *cachedCPSet[T, CP, SP, DP] {
	cpSetsByDestinationES, ok := c.cpSetsByOriginAndDestinationESs[origin]
	if !ok {
		cpSetsByDestinationES = map[trace.ElementarySpan[T, CP, SP, DP]]*cachedCPSet[T, CP, SP, DP]{}
		c.cpSetsByOriginAndDestinationESs[origin] = cpSetsByDestinationES
	}
	cpSet, ok := cpSetsByDestinationES[destination]
	if !ok {
		cpSet = newCachedCPSet(origin, destination)
		cpSetsByDestinationES[destination] = cpSet
	}
	return cpSet
}

// GetBetweenElementarySpans returns the critical path with the specified
// elementary span endpoints, computed with the specified strategy, finding and
// caching it if necessary.  GetBetweenElementarySpans is thread-safe.
func (c *Cache[T, CP, SP, DP]) GetBetweenElementarySpans(
	tr trace.Trace[T, CP, SP, DP],
	origin, destination trace.ElementarySpan[T, CP, SP, DP],
	strategy Strategy,
	options ...Option,
) (*Path[T, CP, SP, DP], error) {
	if origin == nil || destination == nil {
		return nil, errMissingEndpoint
	}
	c.mu.Lock()
	cpSet := c.getCachedCPSet(origin, destination)
	c.mu.Unlock()
	opts := buildOptions(options...)
	return cpSet.get(tr, strategy, opts, &c.mu)
}

// GetBetweenEndpoints returns the critical path with the specified Endpoints,
// computed with the specified strategy, finding and caching it if necessary.
// GetBetweenEndpoints is thread-safe.
func (c *Cache[T, CP, SP, DP]) GetBetweenEndpoints(
	tr trace.Trace[T, CP, SP, DP],
	from, to *Endpoint[T, CP, SP, DP],
	strategy Strategy,
	options ...Option,
) (*Path[T, CP, SP, DP], error) {
	path, err := c.GetBetweenElementarySpans(
		tr.Trace(),
		from.ElementarySpan(tr.Comparator()),
		to.ElementarySpan(tr.Comparator()),
		strategy,
		options...,
	)
	if err != nil {
		return nil, err
	}
	path.Start, path.End = from.At, to.At
	return path, nil
}
