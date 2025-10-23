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
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	for _, test := range []struct {
		description string
		buildTrace  func(t *testing.T) Trace[time.Duration, payload, payload, payload]
		wantError   bool
	}{{
		description: "OK trace",
		buildTrace: func(t *testing.T) Trace[time.Duration, payload, payload, payload] {
			trace := NewTrace[time.Duration, payload, payload, payload](
				DurationComparator,
				&testNamer{},
			)
			a := trace.NewRootSpan(0, 100, "")
			b, err := a.NewChildSpan(DurationComparator, 30, 70, "")
			if err != nil {
				t.Fatal(err.Error())
			}
			c := trace.NewRootSpan(40, 70, "")
			spawn := trace.NewDependency(FirstUserDefinedDependencyType, "")
			if err := spawn.SetOriginSpan(DurationComparator, b, 35); err != nil {
				t.Error(err.Error())
			}
			if err := spawn.AddDestinationSpan(DurationComparator, c, 40); err != nil {
				t.Error(err.Error())
			}
			ret := trace.NewDependency(FirstUserDefinedDependencyType+1, "")
			if err := ret.SetOriginSpan(DurationComparator, c, 70); err != nil {
				t.Error(err.Error())
			}
			if err := ret.AddDestinationSpan(DurationComparator, b, 70); err != nil {
				t.Error(err.Error())
			}
			return trace
		},
		wantError: false,
	}, {
		description: "partial dependency",
		buildTrace: func(t *testing.T) Trace[time.Duration, payload, payload, payload] {
			trace := NewTrace[time.Duration, payload, payload, payload](
				DurationComparator,
				&testNamer{},
			)
			a := trace.NewRootSpan(0, 100, "")
			if err := trace.NewDependency(FirstUserDefinedDependencyType, "").
				SetOriginSpan(DurationComparator, a, 50); err != nil {
				t.Error(err.Error())
			}
			return trace
		},
		wantError: true,
	}, {
		description: "negative dependency edge",
		buildTrace: func(t *testing.T) Trace[time.Duration, payload, payload, payload] {
			trace := NewTrace[time.Duration, payload, payload, payload](
				DurationComparator,
				&testNamer{},
			)
			a := trace.NewRootSpan(0, 100, "")
			b := trace.NewRootSpan(0, 100, "")
			dep := trace.NewDependency(FirstUserDefinedDependencyType, "")
			if err := dep.SetOriginSpan(DurationComparator, a, 70); err != nil {
				t.Error(err.Error())
			}
			if err := dep.AddDestinationSpan(DurationComparator, b, 30); err != nil {
				t.Error(err.Error())
			}
			return trace
		},
		wantError: true,
	}, {
		description: "cycle reachable from entry elementary spans",
		buildTrace: func(t *testing.T) Trace[time.Duration, payload, payload, payload] {
			trace := NewMutableTrace[time.Duration, payload, payload, payload](
				DurationComparator,
				&testNamer{},
			)
			a0 := NewMutableElementarySpan[time.Duration, payload, payload, payload]().WithStart(0).WithEnd(0)
			a1 := NewMutableElementarySpan[time.Duration, payload, payload, payload]().WithStart(0).WithEnd(0)
			if _, err := trace.NewMutableRootSpan([]MutableElementarySpan[time.Duration, payload, payload, payload]{a0, a1}, "A"); err != nil {
				t.Error(err.Error())
			}
			b0 := NewMutableElementarySpan[time.Duration, payload, payload, payload]().WithStart(0).WithEnd(0)
			b1 := NewMutableElementarySpan[time.Duration, payload, payload, payload]().WithStart(0).WithEnd(0)
			if _, err := trace.NewMutableRootSpan([]MutableElementarySpan[time.Duration, payload, payload, payload]{b0, b1}, "B"); err != nil {
				t.Error(err.Error())
			}
			trace.NewMutableDependency(FirstUserDefinedDependencyType).
				WithOriginElementarySpan(DurationComparator, b1).
				WithDestinationElementarySpan(a1)
			trace.NewMutableDependency(FirstUserDefinedDependencyType).
				WithOriginElementarySpan(DurationComparator, a1).
				WithDestinationElementarySpan(b0)
			return trace
		},
		wantError: true,
	}, {
		description: "cycle unreachable from entry elementary spans",
		buildTrace: func(t *testing.T) Trace[time.Duration, payload, payload, payload] {
			trace := NewMutableTrace[time.Duration, payload, payload, payload](
				DurationComparator,
				&testNamer{},
			)
			a0 := NewMutableElementarySpan[time.Duration, payload, payload, payload]().WithStart(0).WithEnd(0)
			a1 := NewMutableElementarySpan[time.Duration, payload, payload, payload]().WithStart(0).WithEnd(0)
			if _, err := trace.NewMutableRootSpan([]MutableElementarySpan[time.Duration, payload, payload, payload]{a0, a1}, "A"); err != nil {
				t.Error(err.Error())
			}
			b0 := NewMutableElementarySpan[time.Duration, payload, payload, payload]().WithStart(0).WithEnd(0)
			b1 := NewMutableElementarySpan[time.Duration, payload, payload, payload]().WithStart(0).WithEnd(0)
			if _, err := trace.NewMutableRootSpan([]MutableElementarySpan[time.Duration, payload, payload, payload]{b0, b1}, "B"); err != nil {
				t.Error(err.Error())
			}
			trace.NewMutableDependency(FirstUserDefinedDependencyType).
				WithOriginElementarySpan(DurationComparator, b1).
				WithDestinationElementarySpan(b0)
			return trace
		},
		wantError: true,
	}} {
		t.Run(test.description, func(t *testing.T) {
			tr := test.buildTrace(t)
			checkErr := Check(tr, true)
			if (checkErr != nil) != test.wantError {
				t.Errorf("Check() returned unexpected error %v", checkErr)
			}
		})
	}
}
