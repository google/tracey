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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

type payload string

func (p payload) String() string {
	return string(p)
}

func esIdx(es ElementarySpan[time.Duration, payload, payload, payload]) int {
	if es.Predecessor() == nil {
		return 0
	}
	return esIdx(es.Predecessor()) + 1
}

func esPredNumStr(es ElementarySpan[time.Duration, payload, payload, payload]) string {
	if es.Predecessor() == nil {
		return "[p-]"
	}
	predIdx := esIdx(es.Predecessor())
	return fmt.Sprintf("[p%d]", predIdx)
}

func prettyPrintSpan(span Span[time.Duration, payload, payload, payload], includeESPredNum bool, indent string) string {
	spanStrs := []string{}
	for _, es := range span.ElementarySpans() {
		incomingStr := ""
		if es.Incoming() != nil {
			incomingStr = fmt.Sprintf("(I%d) ", es.Incoming().DependencyType())
		}
		outgoingStr := ""
		if es.Outgoing() != nil {
			outgoingStr = fmt.Sprintf(" (O%d)", es.Outgoing().DependencyType())
		}
		predNumStr := ""
		if includeESPredNum {
			predNumStr = esPredNumStr(es) + " "
		}
		spanStrs = append(
			spanStrs,
			fmt.Sprintf("%s%s%s-%s%s", predNumStr, incomingStr, es.Start(), es.End(), outgoingStr),
		)
	}
	ret := []string{
		indent + "(" + span.Payload().String() + ") " + strings.Join(spanStrs, ", "),
	}
	for _, child := range span.ChildSpans() {
		ret = append(ret, prettyPrintSpan(child, includeESPredNum, indent+"  "))
	}
	return strings.Join(ret, "\n")
}

type testNamer struct {
}

func (tn *testNamer) CategoryName(category Category[time.Duration, payload, payload, payload]) string {
	return category.Payload().String()
}

func (tn *testNamer) CategoryUniqueID(category Category[time.Duration, payload, payload, payload]) string {
	return category.Payload().String()
}

func (tn *testNamer) SpanName(span Span[time.Duration, payload, payload, payload]) string {
	return span.Payload().String()
}

func (tn *testNamer) SpanUniqueID(span Span[time.Duration, payload, payload, payload]) string {
	return "id:" + span.Payload().String()
}

func (tn *testNamer) HierarchyTypeNames() map[HierarchyType]string {
	ret := map[HierarchyType]string{}
	for i := 0; i < 10; i++ {
		ret[HierarchyType(i)] = fmt.Sprintf("hierarchy %d", i)
	}
	return ret
}

func (tn *testNamer) DependencyTypeNames() map[DependencyType]string {
	ret := map[DependencyType]string{}
	for i := 0; i < 10; i++ {
		ret[DependencyType(i)] = fmt.Sprintf("hierarchy %d", i)
	}
	return ret
}

func (tn *testNamer) MomentString(t time.Duration) string {
	return fmt.Sprintf("%v", t)
}

func TestElementarySpanBuilding(t *testing.T) {
	var trace Trace[time.Duration, payload, payload, payload]
	for _, test := range []struct {
		description string
		buildSpan   func() (Span[time.Duration, payload, payload, payload], error)
		wantErr     bool
		wantSpanStr string
	}{{
		description: "assorted dependences",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := trace.NewDependency(0, "").
				AddDestinationSpan(trace.Comparator(), span, 10); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(0, "").
				AddDestinationSpanAfterWait(trace.Comparator(), span, 40, 60); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(0, "").
				SetOriginSpan(trace.Comparator(), span, 80); err != nil {
				return nil, err
			}
			return span, nil
		},
		wantSpanStr: "() [p-] 0s-10ns, [p0] (I0) 10ns-40ns, [p1] (I0) 60ns-80ns (O0), [p2] 80ns-100ns",
	}, {
		description: "zero-duration elementary spans",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := trace.NewDependency(0, "").
				SetOriginSpan(trace.Comparator(), span, 30); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(1, "").
				AddDestinationSpan(trace.Comparator(), span, 30); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(2, "").
				AddDestinationSpan(trace.Comparator(), span, 50); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(3, "").
				SetOriginSpan(trace.Comparator(), span, 50); err != nil {
				return nil, err
			}
			return span, nil
		},
		wantSpanStr: "() [p-] 0s-30ns, [p0] (I1) 30ns-30ns (O0), [p1] 30ns-50ns (O3), [p2] (I2) 50ns-100ns",
	}, {
		description: "multiple zero-duration elementary spans",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := trace.NewDependency(0, "").
				SetOriginSpan(trace.Comparator(), span, 30); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(1, "").
				SetOriginSpan(trace.Comparator(), span, 30); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(2, "").
				SetOriginSpan(trace.Comparator(), span, 30); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(3, "").
				AddDestinationSpan(trace.Comparator(), span, 60); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(4, "").
				AddDestinationSpan(trace.Comparator(), span, 60); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(5, "").
				AddDestinationSpan(trace.Comparator(), span, 60); err != nil {
				return nil, err
			}
			return span, nil
		},
		// Later-defined events will be placed before others of the same direction
		// (incoming/outgoing) and at the same time.
		wantSpanStr: "() [p-] 0s-30ns (O2), [p0] 30ns-30ns (O1), [p1] 30ns-30ns (O0), [p2] 30ns-60ns, [p3] (I5) 60ns-60ns, [p4] (I4) 60ns-60ns, [p5] (I3) 60ns-100ns",
	}, {
		description: "child spans",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "parent")
			child, err := span.NewChildSpan(DurationComparator, 30, 60, "child")
			if err != nil {
				return nil, err
			}
			grandchild, err := child.NewChildSpan(DurationComparator, 40, 50, "grandchild")
			if err != nil {
				return nil, err
			}
			if err := trace.NewDependency(3, "").
				SetOriginSpan(trace.Comparator(), grandchild, 45); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(3, "").
				AddDestinationSpan(trace.Comparator(), span, 60); err != nil {
				return nil, err
			}
			return span, nil
		},
		wantSpanStr: `(parent) [p-] 0s-30ns (O0), [p0] 30ns-30ns, [p1] (I1) 60ns-60ns, [p2] (I3) 60ns-100ns
  (child) [p-] (I0) 30ns-40ns (O0), [p0] 40ns-40ns, [p1] (I1) 50ns-60ns (O1)
    (grandchild) [p-] (I0) 40ns-45ns (O3), [p0] 45ns-50ns (O1)`,
	}, {
		description: "suspend within a single elementary span",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			return span, span.Suspend(trace.Comparator(), 20, 80)
		},
		wantSpanStr: "() [p-] 0s-20ns, [p0] 80ns-100ns",
	}, {
		description: "dependencies are applied in definition order",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := trace.NewDependency(1, "").AddDestinationSpan(trace.Comparator(), span, 20); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(2, "").SetOriginSpan(trace.Comparator(), span, 20); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(3, "").SetOriginSpan(trace.Comparator(), span, 80); err != nil {
				return nil, err
			}
			if err := trace.NewDependency(4, "").AddDestinationSpan(trace.Comparator(), span, 80); err != nil {
				return nil, err
			}
			return span, nil
		},
		wantSpanStr: "() [p-] 0s-20ns (O2), [p0] (I1) 20ns-80ns, [p1] (I4) 80ns-80ns (O3), [p2] 80ns-100ns",
	}, {
		description: "suspends may not span incoming dependences",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := trace.NewDependency(0, "").
				AddDestinationSpan(trace.Comparator(), span, 50); err != nil {
				return nil, err
			}
			return span, span.Suspend(trace.Comparator(), 20, 80)
		},
		wantErr: true,
	}, {
		description: "suspends can fission around incoming dependences",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := trace.NewDependency(0, "").
				AddDestinationSpan(trace.Comparator(), span, 50); err != nil {
				return nil, err
			}
			if err := span.Suspend(trace.Comparator(), 20, 80, SuspendFissionsAroundElementarySpanEndpoints); err != nil {
				return nil, err
			}
			// Simplify the trace to get rid of zero-length ESs without dependencies.
			span.simplify(trace.Comparator())
			return span, nil
		},
		wantSpanStr: "() [p-] 0s-20ns, [p0] (I0) 50ns-50ns, [p1] 80ns-100ns",
	}, {
		description: "suspends may not span outgoing dependences",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := trace.NewDependency(0, "").
				SetOriginSpan(trace.Comparator(), span, 50); err != nil {
				return nil, err
			}
			return span, span.Suspend(trace.Comparator(), 20, 80)
		},
		wantErr: true,
	}, {
		description: "suspends can fission around outgoing dependences",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := trace.NewDependency(0, "").
				SetOriginSpan(trace.Comparator(), span, 50); err != nil {
				return nil, err
			}
			return span, span.Suspend(trace.Comparator(), 20, 80, SuspendFissionsAroundElementarySpanEndpoints)
		},
		wantSpanStr: "() [p-] 0s-20ns, [p0] 50ns-50ns (O0), [p1] 50ns-50ns, [p2] 80ns-100ns",
	}, {
		description: "suspends can fission around multiple elementary span endpoints",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := span.Suspend(trace.Comparator(), 80, 90); err != nil {
				return nil, err
			}
			if err := span.Suspend(trace.Comparator(), 60, 70); err != nil {
				return nil, err
			}
			if err := span.Suspend(trace.Comparator(), 40, 50); err != nil {
				return nil, err
			}
			return span, span.Suspend(trace.Comparator(), 35, 100, SuspendFissionsAroundElementarySpanEndpoints)
		},
		wantSpanStr: "() [p-] 0s-35ns, [p0] 40ns-40ns, [p1] 50ns-50ns, [p2] 60ns-60ns, [p3] 70ns-70ns, [p4] 80ns-80ns, [p5] 90ns-90ns, [p6] 100ns-100ns",
	}, {
		description: "spans must be running at incoming dependences",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := span.Suspend(trace.Comparator(), 20, 80); err != nil {
				return nil, err
			}
			return span, trace.NewDependency(0, "").
				AddDestinationSpan(trace.Comparator(), span, 50)
		},
		wantErr: true,
	}, {
		description: "incoming dependences can fission suspend ",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := span.Suspend(trace.Comparator(), 20, 80); err != nil {
				return nil, err
			}
			return span, trace.NewDependency(0, "").
				AddDestinationSpan(trace.Comparator(), span, 50, DependencyCanFissionSuspend)
		},
		wantSpanStr: "() [p-] 0s-20ns, [p0] (I0) 50ns-50ns, [p1] 80ns-100ns",
	}, {
		description: "spans must be running at outgoing dependences",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := span.Suspend(trace.Comparator(), 20, 80); err != nil {
				return nil, err
			}
			return span, trace.NewDependency(0, "").
				SetOriginSpan(trace.Comparator(), span, 50)
		},
		wantErr: true,
	}, {
		description: "outgoing dependences can fission suspend ",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			if err := span.Suspend(trace.Comparator(), 20, 80); err != nil {
				return nil, err
			}
			return span, trace.NewDependency(0, "").
				SetOriginSpan(trace.Comparator(), span, 50, DependencyCanFissionSuspend)
		},
		wantSpanStr: "() [p-] 0s-20ns, [p0] 50ns-50ns (O0), [p1] 80ns-100ns",
	}, {
		description: "suspends must be contained within spans",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			return span, span.Suspend(trace.Comparator(), 50, 150)
		},
		wantErr: true,
	}, {
		description: "incoming dependences must be contained within spans",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			return span, trace.NewDependency(0, "").
				AddDestinationSpan(trace.Comparator(), span, 150)
		},
		wantErr: true,
	}, {
		description: "outgoing dependences must be contained within spans",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			return span, trace.NewDependency(0, "").
				SetOriginSpan(trace.Comparator(), span, 150)
		},
		wantErr: true,
	}, {
		description: "adjacent suspends do not merge",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			span.Suspend(trace.Comparator(), 0, 20)
			span.Suspend(trace.Comparator(), 20, 40)
			span.Suspend(trace.Comparator(), 40, 60)
			span.Suspend(trace.Comparator(), 60, 80)
			span.Suspend(trace.Comparator(), 80, 100)
			return span, nil
		},
		wantSpanStr: "() [p-] 0s-0s, [p0] 20ns-20ns, [p1] 40ns-40ns, [p2] 60ns-60ns, [p3] 80ns-80ns, [p4] 100ns-100ns",
	}, {
		description: "adjacent suspends merge after Simplify",
		buildSpan: func() (Span[time.Duration, payload, payload, payload], error) {
			span := trace.NewRootSpan(0, 100, "")
			span.Suspend(trace.Comparator(), 0, 20)
			span.Suspend(trace.Comparator(), 20, 40)
			span.Suspend(trace.Comparator(), 40, 60)
			span.Suspend(trace.Comparator(), 60, 80)
			span.Suspend(trace.Comparator(), 80, 100)
			trace.Simplify()
			return span, nil
		},
		wantSpanStr: "() [p-] 0s-0s, [p0] 100ns-100ns",
	}} {
		t.Run(test.description, func(t *testing.T) {
			trace = NewTrace[time.Duration, payload, payload, payload](
				DurationComparator,
				&testNamer{},
			)
			span, err := test.buildSpan()
			if (err != nil) != test.wantErr {
				t.Fatalf("unexpected error status while building span: %v", err)
			}
			if err != nil {
				return
			}
			gotSpanStr := prettyPrintSpan(span, true, "")
			if diff := cmp.Diff(test.wantSpanStr, gotSpanStr); diff != "" {
				t.Errorf("Got elementary spans\n%s\ndiff (-want +got) %s", gotSpanStr, diff)
			}
		})
	}
}

func TestCategories(t *testing.T) {
	for _, test := range []struct {
		description     string
		buildTrace      func() (Trace[time.Duration, payload, payload, payload], error)
		wantTraceCatStr string
	}{{
		description: "two hierarchies",
		buildTrace: func() (Trace[time.Duration, payload, payload, payload], error) {
			trace := NewTrace[time.Duration, payload, payload, payload](
				DurationComparator,
				&testNamer{},
			)
			a := trace.NewRootCategory(0, "a")
			a.NewChildCategory("b").NewChildCategory("c")
			a.NewChildCategory("d").NewChildCategory("e")
			trace.NewRootCategory(0, "f")
			trace.NewRootCategory(1, "g")
			h := trace.NewRootCategory(1, "h")
			h.NewChildCategory("i").NewChildCategory("j")
			h.NewChildCategory("k").NewChildCategory("l")
			return trace, nil
		},
		wantTraceCatStr: `
hierarchy 0
  /a
  /a/b
  /a/b/c
  /a/d
  /a/d/e
  /f
hierarchy 1
  /g
  /h
  /h/i
  /h/i/j
  /h/k
  /h/k/l`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			trace, err := test.buildTrace()
			if err != nil {
				t.Fatalf("failed to build trace: %v", err)
			}
			gotTraceCatStrs := []string{}
			var visit func(cat Category[time.Duration, payload, payload, payload], prefix string)
			visit = func(cat Category[time.Duration, payload, payload, payload], prefix string) {
				prefix = prefix + "/" + cat.Payload().String()
				gotTraceCatStrs = append(gotTraceCatStrs, "  "+prefix)
				for _, childCat := range cat.ChildCategories() {
					visit(childCat, prefix)
				}
			}
			for _, ht := range trace.HierarchyTypes() {
				gotTraceCatStrs = append(
					gotTraceCatStrs,
					fmt.Sprintf("hierarchy %d", ht),
				)
				for _, rootCat := range trace.RootCategories(ht) {
					visit(rootCat, "")
				}
			}
			gotTraceCatStr := "\n" + strings.Join(gotTraceCatStrs, "\n")
			if diff := cmp.Diff(test.wantTraceCatStr, gotTraceCatStr); diff != "" {
				t.Errorf("Got trace categories\n%s\ndiff (-want +got) %s", gotTraceCatStr, diff)
			}
		})
	}
}

func TestMutable(t *testing.T) {
	for _, test := range []struct {
		description       string
		buildTrace        func() (Trace[time.Duration, payload, payload, payload], error)
		wantTraceSpansStr string
	}{{
		description: "single-span trace",
		buildTrace: func() (Trace[time.Duration, payload, payload, payload], error) {
			ret := NewMutableTrace[time.Duration, payload, payload, payload](
				DurationComparator,
				&testNamer{},
			)
			_, err := ret.NewMutableRootSpan(
				[]MutableElementarySpan[time.Duration, payload, payload, payload]{
					NewMutableElementarySpan[time.Duration, payload, payload, payload](nil).
						WithStart(0).
						WithEnd(100),
				},
				"",
			)
			return ret, err
		},
		wantTraceSpansStr: `
() [p-] 0s-100ns`,
	}, {
		description: "trace with deps",
		buildTrace: func() (Trace[time.Duration, payload, payload, payload], error) {
			ret := NewMutableTrace[time.Duration, payload, payload, payload](
				DurationComparator,
				&testNamer{},
			)
			originES := NewMutableElementarySpan[time.Duration, payload, payload, payload](nil).
				WithStart(0).
				WithEnd(50)
			if _, err := ret.NewMutableRootSpan(
				[]MutableElementarySpan[time.Duration, payload, payload, payload]{
					originES,
					NewMutableElementarySpan[time.Duration, payload, payload, payload](originES).
						WithStart(50).
						WithEnd(100),
				},
				"",
			); err != nil {
				return nil, err
			}
			startES := NewMutableElementarySpan[time.Duration, payload, payload, payload](nil).
				WithStart(0).
				WithEnd(50)
			destinationES := NewMutableElementarySpan[time.Duration, payload, payload, payload](startES).
				WithStart(50).
				WithEnd(100)
			if _, err := ret.NewMutableRootSpan(
				[]MutableElementarySpan[time.Duration, payload, payload, payload]{
					startES,
					destinationES,
				},
				"",
			); err != nil {
				return nil, err
			}
			ret.NewMutableDependency(0).
				WithOriginElementarySpan(originES).
				WithDestinationElementarySpan(destinationES)
			return ret, nil
		},
		wantTraceSpansStr: `
() [p-] 0s-50ns (O0), [p0] 50ns-100ns
() [p-] 0s-50ns, [p0] (I0) 50ns-100ns`,
	}, {
		description: "child spans",
		buildTrace: func() (Trace[time.Duration, payload, payload, payload], error) {
			ret := NewMutableTrace[time.Duration, payload, payload, payload](
				DurationComparator,
				&testNamer{},
			)
			parentCallES := NewMutableElementarySpan[time.Duration, payload, payload, payload](nil).
				WithStart(0).
				WithEnd(30)
			childReturnES := NewMutableElementarySpan[time.Duration, payload, payload, payload](parentCallES).
				WithStart(60).
				WithEnd(60)
			destinationES := NewMutableElementarySpan[time.Duration, payload, payload, payload](childReturnES).
				WithStart(60).
				WithEnd(100)
			parent, err := ret.NewMutableRootSpan(
				[]MutableElementarySpan[time.Duration, payload, payload, payload]{
					parentCallES, childReturnES, destinationES,
				},
				"parent",
			)
			if err != nil {
				return nil, err
			}
			childCallES := NewMutableElementarySpan[time.Duration, payload, payload, payload](nil).
				WithStart(30).
				WithEnd(40)
			childEndES := NewMutableElementarySpan[time.Duration, payload, payload, payload](childCallES).
				WithStart(50).
				WithEnd(60)
			child, err := parent.NewMutableChildSpan(
				[]MutableElementarySpan[time.Duration, payload, payload, payload]{
					childCallES, childEndES,
				},
				"child",
			)
			if err != nil {
				return nil, err
			}
			originES := NewMutableElementarySpan[time.Duration, payload, payload, payload](nil).
				WithStart(40).
				WithEnd(45)
			grandchildEndES := NewMutableElementarySpan[time.Duration, payload, payload, payload](originES).
				WithStart(45).
				WithEnd(50)
			if _, err := child.NewMutableChildSpan(
				[]MutableElementarySpan[time.Duration, payload, payload, payload]{
					originES, grandchildEndES,
				},
				"grandchild",
			); err != nil {
				return nil, err
			}
			ret.NewMutableDependency(Call).
				WithOriginElementarySpan(parentCallES).
				WithDestinationElementarySpan(childCallES)
			ret.NewMutableDependency(Return).
				WithOriginElementarySpan(childEndES).
				WithDestinationElementarySpan(childReturnES)
			ret.NewMutableDependency(Call).
				WithOriginElementarySpan(childCallES).
				WithDestinationElementarySpan(originES)
			ret.NewMutableDependency(Return).
				WithOriginElementarySpan(grandchildEndES).
				WithDestinationElementarySpan(childEndES)
			ret.NewMutableDependency(3).
				WithOriginElementarySpan(originES).
				WithDestinationElementarySpan(destinationES)
			return ret, nil
		},
		wantTraceSpansStr: `
(parent) [p-] 0s-30ns (O0), [p0] (I1) 60ns-60ns, [p1] (I3) 60ns-100ns
  (child) [p-] (I0) 30ns-40ns (O0), [p0] (I1) 50ns-60ns (O1)
    (grandchild) [p-] (I0) 40ns-45ns (O3), [p0] 45ns-50ns (O1)`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			trace, err := test.buildTrace()
			if err != nil {
				t.Fatalf("failed to build trace: %s", err)
			}
			gotTraceSpansStrs := []string{}
			for _, span := range trace.RootSpans() {
				gotTraceSpansStrs = append(gotTraceSpansStrs, prettyPrintSpan(span, true, ""))
			}
			gotTraceSpansStr := "\n" + strings.Join(gotTraceSpansStrs, "\n")
			if diff := cmp.Diff(test.wantTraceSpansStr, gotTraceSpansStr); diff != "" {
				t.Errorf("Got trace spans\n%s\ndiff (-want +got) %s", gotTraceSpansStr, diff)
			}
		})
	}
}

func TestPayloads(t *testing.T) {
	// Confirm that conventionally-built traces' payloads work.
	trace := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	cat := trace.NewRootCategory(0, "cat0")
	if cat.Payload() != "cat0" {
		t.Errorf("category payload = '%s', wanted '%s'", cat.Payload(), "cat0")
	}
	span := trace.NewRootSpan(0, 10, "span0")
	if span.Payload() != "span0" {
		t.Errorf("span payload = '%s', wanted '%s'", span.Payload(), "span0")
	}
	dep := trace.NewDependency(FirstUserDefinedDependencyType, "dep0")
	if dep.Payload() != "dep0" {
		t.Errorf("dep payload = '%s', wanted '%s'", dep.Payload(), "dep0")
	}

	// Confirm that mutable traces' payloads work.
	mtrace := NewMutableTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	es := NewMutableElementarySpan[time.Duration, payload, payload, payload](nil)
	mspan, err := mtrace.NewMutableRootSpan(
		[]MutableElementarySpan[time.Duration, payload, payload, payload]{
			es,
		}, "span1",
	)
	if err != nil {
		t.Fatalf("NewMutableRootSpan yielded unexpected error %s", err)
	}
	if mspan.Payload() != "span1" {
		t.Errorf("mspan payload = '%s', wanted '%s'", mspan.Payload(), "span1")
	}
	mdep := mtrace.NewMutableDependency(FirstUserDefinedDependencyType).WithPayload("dep1")
	if mdep.Payload() != "dep1" {
		t.Errorf("mdep payload = '%s', wanted '%s'", mdep.Payload(), "dep1")
	}
}
