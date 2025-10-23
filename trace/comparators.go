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

package trace

import (
	"strconv"
	"time"
)

type comparatorBase[T any] interface {
	// Returns the difference between the two arguments (e.g., a-b) as a float64.
	// This difference is transitive: if Diff(a, b) is 2 and Diff(b, c) is 2,
	// Diff(a, c) must be 4 and Diff(c, a) must be -4.  Thus, if Diff(a, b) is d,
	// then Add(a, d) must equal b, and Diff(b, Add(a, d)) must be 0.
	// Diff also acts as a comparator: if a < b, Diff(a, b) < 0; if a == b,
	// Diff(a, b) == 0, and if a > b, Diff(a, b) > 0
	Diff(a, b T) float64
	// Returns the provided magnitude, as provided by Diff(), added to a.
	Add(a T, magnitude float64) T
	// Returns the duration (diff) value represented by the provided string.
	DurationFromString(s string) (float64, error)
	// Returns the string value of the provided duration (diff).
	DurationString(d float64) string
}

type comparator[T any] struct {
	comparatorBase[T]
}

func (c *comparator[T]) Less(a, b T) bool {
	return c.Diff(a, b) < 0
}

func (c *comparator[T]) LessOrEqual(a, b T) bool {
	return c.Diff(a, b) <= 0
}

func (c *comparator[T]) Equal(a, b T) bool {
	return c.Diff(a, b) == 0
}

func (c *comparator[T]) GreaterOrEqual(a, b T) bool {
	return c.Diff(a, b) >= 0
}

func (c *comparator[T]) Greater(a, b T) bool {
	return c.Diff(a, b) > 0
}

type timeComparator struct {
}

func (tc *timeComparator) Diff(a, b time.Time) float64 {
	return float64(a.Sub(b))
}

func (tc *timeComparator) Add(a time.Time, magnitude float64) time.Time {
	return a.Add(time.Duration(magnitude))
}

func (tc *timeComparator) DurationFromString(s string) (float64, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	return float64(d), nil
}

func (tc *timeComparator) DurationString(d float64) string {
	return time.Duration(d).String()
}

// TimeComparator is a Comparator implementation for time.Time.
var TimeComparator Comparator[time.Time] = &comparator[time.Time]{&timeComparator{}}

type durationComparator struct {
}

func (dc *durationComparator) Diff(a, b time.Duration) float64 {
	return float64(a - b)
}

func (dc *durationComparator) Add(a time.Duration, magnitude float64) time.Duration {
	return a + time.Duration(magnitude)
}

func (dc *durationComparator) DurationFromString(s string) (float64, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	return float64(d), nil
}

func (dc *durationComparator) DurationString(d float64) string {
	return time.Duration(d).String()
}

// DurationComparator is a Comparator implementation for time.Duration.
var DurationComparator Comparator[time.Duration] = &comparator[time.Duration]{&durationComparator{}}

type doubleComparator struct {
}

func (dc *doubleComparator) Diff(a, b float64) float64 {
	return a - b
}

func (dc *doubleComparator) Add(a float64, magnitude float64) float64 {
	return a + magnitude
}

func (dc *doubleComparator) DurationFromString(s string) (float64, error) {
	d, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return float64(d), nil
}

func (dc *doubleComparator) DurationString(d float64) string {
	return strconv.FormatFloat(d, 'f', -1, 64)
}

// DoubleComparator is a Comparator implementation for float64.
var DoubleComparator Comparator[float64] = &comparator[float64]{&doubleComparator{}}
