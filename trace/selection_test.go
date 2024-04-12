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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestSpanSelectionFullExpansion(t *testing.T) {
	trace := testTrace1(t)
	for _, test := range []struct {
		description          string
		matchers             [][]PathElementMatcher[time.Duration, payload, payload, payload]
		wantMatchingSpansStr string // Lexically sorted for stability.
	}{{
		description: "find span by literal path",
		matchers: matchers(
			pathMatcher(
				matchLiteralName("a"),
				matchLiteralName("b"),
				matchLiteralName("c"),
			),
		),
		wantMatchingSpansStr: `
c: 20ns-30ns`,
	}, {
		description: "find **/c",
		matchers: matchers(
			pathMatcher(
				matchGlobstar,
				matchLiteralName("c"),
			),
		),
		wantMatchingSpansStr: `
c: 20ns-30ns
c: 50ns-90ns`,
	}, {
		description: "find a/c/**",
		matchers: matchers(
			pathMatcher(
				matchLiteralName("a"),
				matchLiteralName("c"),
				matchGlobstar,
			),
		),
		wantMatchingSpansStr: `
c: 50ns-90ns
d: 60ns-80ns
e: 65ns-75ns`,
	}, {
		description: "find a/*/*",
		matchers: matchers(
			pathMatcher(
				matchLiteralName("a"),
				matchStar,
				matchStar,
			),
		),
		wantMatchingSpansStr: `
c: 20ns-30ns
d: 60ns-80ns`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			selection := SelectSpans[time.Duration, payload, payload, payload](trace, &testNamer{}, test.matchers)
			matchingSpans := selection.Spans()
			gotMatchingSpansStrs := []string{}
			for _, span := range matchingSpans {
				gotMatchingSpansStrs = append(
					gotMatchingSpansStrs,
					fmt.Sprintf("%s: %v-%v", span.Payload(), span.Start(), span.End()),
				)
			}
			sort.Strings(gotMatchingSpansStrs)
			gotMatchingSpansStr := "\n" + strings.Join(gotMatchingSpansStrs, "\n")
			if diff := cmp.Diff(test.wantMatchingSpansStr, gotMatchingSpansStr); diff != "" {
				t.Errorf("Got trace spans\n%s\ndiff (-want +got) %s", gotMatchingSpansStr, diff)
			}
		})
	}
}

func TestSpanSelectionIncludes(t *testing.T) {
	trace := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	span := trace.NewRootSpan(0, 100, "parent")
	child, err := span.NewChildSpan(DurationComparator, 30, 60, "child")
	if err != nil {
		t.Errorf(err.Error())
	}
	grandchild, err := child.NewChildSpan(DurationComparator, 40, 50, "grandchild")
	if err != nil {
		t.Errorf(err.Error())
	}
	ss := SelectSpans[time.Duration, payload, payload, payload](
		trace,
		&testNamer{},
		matchers(pathMatcher(
			matchLiteralName("parent"),
			matchStar,
			matchStar,
		),
		),
	)
	if !ss.Includes(grandchild) {
		t.Errorf("expected grandchild to match, but it didn't")
	}
}

func testTrace2(t *testing.T) Trace[time.Duration, payload, payload, payload] {
	t.Helper()
	trace := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	a := trace.NewRootCategory(0, "a")
	ab := a.NewChildCategory("b")
	ab.NewChildCategory("c")
	ac := a.NewChildCategory("c")
	acd := ac.NewChildCategory("d")
	acd.NewChildCategory("e")
	return trace
}

func TestCategorySelectionFullExpansion(t *testing.T) {
	trace := testTrace2(t)
	for _, test := range []struct {
		description               string
		matchers                  [][]PathElementMatcher[time.Duration, payload, payload, payload]
		wantMatchingCategoriesStr string // Lexically sorted for stability.
	}{{
		description: "find category by literal path",
		matchers: matchers(
			pathMatcher(
				matchLiteralName("a"),
				matchLiteralName("b"),
				matchLiteralName("c"),
			),
		),
		wantMatchingCategoriesStr: `c`,
	}, {
		description: "find **/c",
		matchers: matchers(
			pathMatcher(
				matchGlobstar,
				matchLiteralName("c"),
			),
		),
		wantMatchingCategoriesStr: `c,c`,
	}, {
		description: "find a/c/**",
		matchers: matchers(
			pathMatcher(
				matchLiteralName("a"),
				matchLiteralName("c"),
				matchGlobstar,
			),
		),
		wantMatchingCategoriesStr: `c,d,e`,
	}, {
		description: "find a/*/*",
		matchers: matchers(
			pathMatcher(
				matchLiteralName("a"),
				matchStar,
				matchStar,
			),
		),
		wantMatchingCategoriesStr: `c,d`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			selection := SelectCategories[time.Duration, payload, payload, payload](trace, &testNamer{}, 0, test.matchers)
			matchingCategories := selection.Categories(0)
			gotMatchingCategoriesStrs := []string{}
			for _, category := range matchingCategories {
				gotMatchingCategoriesStrs = append(
					gotMatchingCategoriesStrs,
					fmt.Sprintf("%s", category.Payload()),
				)
			}
			sort.Strings(gotMatchingCategoriesStrs)
			gotMatchingCategoriessStr := strings.Join(gotMatchingCategoriesStrs, ",")
			if diff := cmp.Diff(test.wantMatchingCategoriesStr, gotMatchingCategoriessStr); diff != "" {
				t.Errorf("Got trace categories\n%s\ndiff (-want +got) %s", gotMatchingCategoriessStr, diff)
			}
		})
	}
}

func TestCategorySelectionIncludes(t *testing.T) {
	trace := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	span := trace.NewRootCategory(0, "parent")
	child := span.NewChildCategory("child")
	grandchild := child.NewChildCategory("grandchild")
	ss := SelectCategories[time.Duration, payload, payload, payload](
		trace,
		&testNamer{},
		0,
		matchers(pathMatcher(
			matchLiteralName("parent"),
			matchStar,
			matchStar,
		),
		),
	)
	if !ss.Includes(grandchild) {
		t.Errorf("expected grandchild to match, but it didn't")
	}
}

func TestDependencySelection(t *testing.T) {
	trace := NewTrace[time.Duration, payload, payload, payload](DurationComparator, &testNamer{})
	span := trace.NewRootSpan(0, 100, "parent")
	child, err := span.NewChildSpan(DurationComparator, 30, 60, "child")
	if err != nil {
		t.Errorf(err.Error())
	}
	grandchild, err := child.NewChildSpan(DurationComparator, 40, 50, "grandchild")
	if err != nil {
		t.Errorf(err.Error())
	}
	if err := trace.NewDependency(3, "").
		SetOriginSpan(trace.Comparator(), grandchild, 45); err != nil {
		t.Errorf(err.Error())
	}
	if err := trace.NewDependency(3, "").
		AddDestinationSpan(trace.Comparator(), span, 60); err != nil {
		t.Errorf(err.Error())
	}

	sd := SelectDependencies[time.Duration, payload, payload, payload](
		trace,
		&testNamer{},
		matchers(literalNameMatchers("parent", "child", "grandchild")),
		matchers(literalNameMatchers("parent")),
		3,
	)
	if !sd.Includes(grandchild, span, 3) {
		t.Errorf("expected child->grandchild dependency of type 3, but didn't find it")
	}
}
