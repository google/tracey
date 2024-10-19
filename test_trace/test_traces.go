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
	"time"

	"github.com/google/tracey/trace"
)

// TestNamer defines a trace.Namer for test traces constructed with this
// package.
var TestNamer = &stringTraceNamer{}

type testUnserializer struct{}

func (tu *testUnserializer) UnserializeSpanPayload(payload []byte) (StringPayload, error) {
	return StringPayload(payload), nil
}

func (tu *testUnserializer) UnserializeCategoryPayload(payload []byte) (StringPayload, error) {
	return StringPayload(payload), nil
}

func (tu *testUnserializer) UnserializeDependencyPayload(payload []byte) (StringPayload, error) {
	return StringPayload(payload), nil
}

func (tu *testUnserializer) DefaultNamer() trace.Namer[time.Duration, StringPayload, StringPayload, StringPayload] {
	return TestNamer
}

// TestUnserializer is a payload unserializer for traces constructed
// with this package.
var TestUnserializer = &testUnserializer{}

// TPP is a test prettyprinter usable with the Traces defined in this file.
var TPP = NewPrettyPrinter[time.Duration, StringPayload, StringPayload, StringPayload](
	HierarchyTypeNames, DependencyTypeNames,
)

// Paths returns the provided variadic set of strings as a string slice.
func Paths(strs ...string) []string {
	return strs
}

// Trace1 is a small but relatively complex trace featuring many different
// causality behaviors.
func Trace1() (
	trace trace.Trace[time.Duration, StringPayload, StringPayload, StringPayload],
	err error,
) {
	trace = NewTraceBuilderWithErrorHandler(func(gotErr error) {
		err = gotErr
	}).
		WithRootCategories(
			RootCategory(Structural, "p0",
				Category("t0.0",
					Category("r0.0.0"),
				),
				Category("t0.1",
					Category("r0.1.0"),
				),
			),
			RootCategory(Structural, "p1",
				Category("t1.0",
					Category("r1.0.0"),
				),
			),
			RootCategory(Causal, "p0",
				Category("t0.0",
					Category("r0.0.0"),
					Category("t0.1",
						Category("r0.1.0"),
					),
				),
				Category("p1",
					Category("t1.0",
						Category("r1.0.0"),
					),
				),
			),
		).
		WithRootSpans(
			RootSpan(0, 100, "s0.0.0",
				ParentCategories(
					FindCategory(Structural, "p0/t0.0/r0.0.0"),
					FindCategory(Causal, "p0/t0.0/r0.0.0"),
				),
				Span(10, 90, "0",
					Span(40, 70, "3"),
				),
			),
			RootSpan(30, 70, "s0.1.0",
				ParentCategories(
					FindCategory(Structural, "p0/t0.1/r0.1.0"),
					FindCategory(Causal, "p0/t0.0/t0.1/r0.1.0"),
				),
			),
			RootSpan(30, 50, "s1.0.0",
				ParentCategories(
					FindCategory(Structural, "p1/t1.0/r1.0.0"),
					FindCategory(Causal, "p0/p1/t1.0/r1.0.0"),
				),
			),
		).
		WithDependency(
			Spawn, "",
			Origin(Paths("s0.0.0/0"), 20),
			Destination(Paths("s0.1.0"), 30),
		).
		WithDependency(
			Spawn, "",
			Origin(Paths("s0.0.0/0"), 30),
			Destination(Paths("s1.0.0"), 30),
		).
		WithDependency(
			Send, "",
			Origin(Paths("s1.0.0"), 35),
			Destination(Paths("s0.1.0"), 40),
		).
		WithDependency(
			Signal, "",
			Origin(Paths("s0.1.0"), 50),
			DestinationAfterWait(Paths("s0.0.0/0/3"), 50, 60),
		).
		Build()
	trace.Simplify()
	return trace, err
}
