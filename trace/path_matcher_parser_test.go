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
)

func TestPathElementParsing(t *testing.T) {
	tr := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	spanA := tr.NewRootSpan(0, 100, "a")
	spanB, err := spanA.NewChildSpan(DurationComparator, 10, 40, "b")
	if err != nil {
		t.Fatalf("failed to add span B to the trace")
	}
	spanC1, err := spanB.NewChildSpan(DurationComparator, 20, 30, "c")
	if err != nil {
		t.Fatalf("failed to add span C (1) to the trace")
	}
	if _, err := spanC1.NewChildSpan(DurationComparator, 22, 24, "e"); err != nil {
		t.Fatalf("failed to add span E to the trace")
	}
	if _, err := spanC1.NewChildSpan(DurationComparator, 26, 28, "/"); err != nil {
		t.Fatalf("failed to add span / to the trace")
	}
	spanD, err := spanA.NewChildSpan(DurationComparator, 60, 90, "d")
	if err != nil {
		t.Fatalf("failed to add span D to the trace")
	}
	if _, err := spanD.NewChildSpan(DurationComparator, 65, 75, "c"); err != nil {
		t.Fatalf("failed to add span C (2) to the trace")
	}
	if _, err := spanD.NewChildSpan(DurationComparator, 77, 85, "c"); err != nil {
		t.Fatalf("failed to add span C (3) to the trace")
	}
	pmp, err := NewPathMatcherParser[time.Duration, payload, payload, payload]()
	if err != nil {
		t.Fatalf("failed to build path matcher parser: %s", err)
	}
	for _, test := range []struct {
		description           string
		pathMatchersStr       string
		wantSelectedSpanPaths string
	}{{
		description:           "all cs",
		pathMatchersStr:       "**/c",
		wantSelectedSpanPaths: "a/b/c, a/d/c, a/d/c",
	}, {
		description:           "all cs another way",
		pathMatchersStr:       "*/*/c",
		wantSelectedSpanPaths: "a/b/c, a/d/c, a/d/c",
	}, {
		description:           "everything at or under a/d",
		pathMatchersStr:       "a/d/**",
		wantSelectedSpanPaths: "a/d, a/d/c, a/d/c",
	}, {
		description:           "everything at or under a/d",
		pathMatchersStr:       "a/d/**",
		wantSelectedSpanPaths: "a/d, a/d/c, a/d/c",
	}, {
		description:           "all spans whose name is a vowel",
		pathMatchersStr:       "**/([aeiou])",
		wantSelectedSpanPaths: "a, a/b/c/e",
	}, {
		description:           "everything just under a",
		pathMatchersStr:       "a/*",
		wantSelectedSpanPaths: "a/b, a/d",
	}, {
		description:           "did you really name that span '/'?",
		pathMatchersStr:       `**/\/`,
		wantSelectedSpanPaths: `a/b/c//`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			pms, err := pmp.ParsePathMatchersStr(test.pathMatchersStr)
			if err != nil {
				t.Fatalf("failed to parse path matchers string: %s", err)
			}
			sf := NewSpanFinder(&testNamer{}).WithSpanMatchers(pms...)
			spans := sf.Find(tr)
			spanPaths := []string{}
			for _, span := range spans {
				spanPaths = append(spanPaths, strings.Join(GetSpanDisplayPath(span, &testNamer{}), "/"))
			}
			gotSelectedSpanPaths := strings.Join(spanPaths, ", ")
			if gotSelectedSpanPaths != test.wantSelectedSpanPaths {
				t.Errorf("Got span paths '%s', wanted '%s'", gotSelectedSpanPaths, test.wantSelectedSpanPaths)
			}
		})
	}
}

const (
	processHierarchy = FirstUserDefinedHierarchyType + iota
	cpuHierarchy
)

func findyTrace(t *testing.T) Trace[time.Duration, payload, payload, payload] {
	tr := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	proc1 := tr.NewRootCategory(processHierarchy, "process 1")
	thread1 := proc1.NewChildCategory("thread 1")
	cpu1 := tr.NewRootCategory(cpuHierarchy, "cpu 1")
	cpu2 := tr.NewRootCategory(cpuHierarchy, "cpu 2")

	p1Root := tr.NewRootSpan(0, 20, "root")
	proc1.AddRootSpan(p1Root)
	p1t1Root := tr.NewRootSpan(20, 40, "root")
	thread1.AddRootSpan(p1t1Root)
	c1Root := tr.NewRootSpan(40, 60, "root")
	cpu1.AddRootSpan(c1Root)
	c2Root := tr.NewRootSpan(60, 100, "root")
	cpu2.AddRootSpan(c2Root)
	if _, err := c2Root.NewChildSpan(DurationComparator, 80, 100, "child"); err != nil {
		t.Fatalf("failed to add 'cpu 1 > root/child' to the trace: %s", err)
	}
	return tr
}

func TestSpanFinderParsing(t *testing.T) {
	tr := findyTrace(t)
	pmp, err := NewPathMatcherParser[time.Duration, payload, payload, payload]()
	if err != nil {
		t.Fatalf("failed to build path matcher parser: %s", err)
	}
	for _, test := range []struct {
		description           string
		spanFinderStr         string
		hierarchyType         HierarchyType
		wantSelectedSpanPaths string
	}{{
		description:           "** > root, process hierarchy",
		spanFinderStr:         "** > root",
		hierarchyType:         processHierarchy,
		wantSelectedSpanPaths: "process 1 > root, process 1/thread 1 > root",
	}, {
		description:           "** > root, cpu hierarchy",
		spanFinderStr:         "** > root",
		hierarchyType:         cpuHierarchy,
		wantSelectedSpanPaths: "cpu 1 > root, cpu 2 > root",
	}, {
		description:           "** > **, cpu hierarchy",
		spanFinderStr:         "** > **",
		hierarchyType:         cpuHierarchy,
		wantSelectedSpanPaths: "cpu 1 > root, cpu 2 > root, cpu 2 > root/child",
	}, {
		description:           "process 1 > root, process hierarchy",
		spanFinderStr:         "process 1 > root",
		hierarchyType:         processHierarchy,
		wantSelectedSpanPaths: "process 1 > root",
	}} {
		t.Run(test.spanFinderStr, func(t *testing.T) {
			sf, err := pmp.ParseSpanFinderStr(&testNamer{}, test.hierarchyType, test.spanFinderStr)
			if err != nil {
				t.Fatalf("failed to parse span finder string: %s", err)
			}
			spans := sf.Find(tr)
			spanPaths := []string{}
			for _, span := range spans {
				rootSpan := span
				for ; rootSpan.ParentSpan() != nil; rootSpan = rootSpan.ParentSpan() {
				}
				thisPath := strings.Join(
					GetCategoryDisplayPath(
						rootSpan.(RootSpan[time.Duration, payload, payload, payload]).
							ParentCategory(test.hierarchyType),
						&testNamer{},
					),
					"/",
				)
				thisPath += " > " + strings.Join(GetSpanDisplayPath(span, &testNamer{}), "/")
				spanPaths = append(spanPaths, thisPath)
			}
			gotSelectedSpanPaths := strings.Join(spanPaths, ", ")
			if gotSelectedSpanPaths != test.wantSelectedSpanPaths {
				t.Errorf("Got span paths '%s', wanted '%s'", gotSelectedSpanPaths, test.wantSelectedSpanPaths)
			}
		})
	}
}

func TestTracePositionParsing(t *testing.T) {
	tr := findyTrace(t)
	pmp, err := NewPathMatcherParser[time.Duration, payload, payload, payload]()
	if err != nil {
		t.Fatalf("failed to build path matcher parser: %s", err)
	}
	for _, test := range []struct {
		description      string
		tracePositionStr string
		hierarchyType    HierarchyType
		wantPositions    string
	}{{
		description:      "** > root, process hierarchy, 0%",
		tracePositionStr: "** > root at 0%",
		hierarchyType:    processHierarchy,
		wantPositions:    "process 1 > root @0s-20ns, process 1/thread 1 > root @20ns-40ns",
	}, {
		description:      "** > root, cpu hierarchy, 0%",
		tracePositionStr: "** > root at 0%",
		hierarchyType:    cpuHierarchy,
		wantPositions:    "cpu 1 > root @40ns-60ns, cpu 2 > root @60ns-80ns",
	}, {
		description:      "** > **, cpu hierarchy, 100%",
		tracePositionStr: "** > ** at 100%",
		hierarchyType:    cpuHierarchy,
		wantPositions:    "cpu 1 > root @40ns-60ns, cpu 2 > root @100ns-100ns, cpu 2 > root/child @80ns-100ns",
	}, {
		description:      "cpu 2 > root, cpu hierarchy, 50%",
		tracePositionStr: "cpu 2 > root @50%",
		hierarchyType:    cpuHierarchy,
		wantPositions:    "cpu 2 > root @60ns-80ns",
	}} {
		t.Run(test.description, func(t *testing.T) {
			p, err := pmp.ParseTracePositionStr(&testNamer{}, test.hierarchyType, test.tracePositionStr)
			if err != nil {
				t.Fatalf("failed to parse trace position string: %s", err)
			}
			ess := p.Find(tr)
			positions := []string{}
			for _, es := range ess {
				rootSpan := es.Span()
				for ; rootSpan.ParentSpan() != nil; rootSpan = rootSpan.ParentSpan() {
				}
				thisPosition := strings.Join(
					GetCategoryDisplayPath(
						rootSpan.(RootSpan[time.Duration, payload, payload, payload]).
							ParentCategory(test.hierarchyType),
						&testNamer{},
					),
					"/",
				)
				thisPosition += " > " + strings.Join(GetSpanDisplayPath(es.Span(), &testNamer{}), "/")
				thisPosition += fmt.Sprintf(" @%v-%v", es.Start(), es.End())
				positions = append(positions, thisPosition)
			}
			gotPositions := strings.Join(positions, ", ")
			if gotPositions != test.wantPositions {
				t.Errorf("Got positions '%s', wanted '%s'", gotPositions, test.wantPositions)
			}
		})
	}
}
