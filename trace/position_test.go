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

func TestTracePositionFinding(t *testing.T) {
	tr := testTrace1(t)
	for _, test := range []struct {
		description     string
		trace           Trace[time.Duration, payload, payload, payload]
		spanFinder      *SpanFinder[time.Duration, payload, payload, payload]
		fractionThrough float64
		wantESs         string
	}{{
		description: "a/b @100%",
		trace:       testTrace1(t),
		spanFinder: NewSpanFinder(&testNamer{}).WithSpanMatchers(
			matchers(literalNameMatchers("a", "b"))...,
		),
		fractionThrough: 1.0,
		wantESs:         `a/b @30ns-40ns`,
	}, {
		description: "**/c @0%",
		trace:       testTrace1(t),
		spanFinder: NewSpanFinder(&testNamer{}).WithSpanMatchers(
			matchers(pathMatcher(matchGlobstar, matchLiteralName("c")))...,
		),
		fractionThrough: 0.0,
		wantESs: `a/c @50ns-60ns
a/b/c @20ns-30ns`,
	}, {
		description: "**/d @30%",
		trace:       testTrace1(t),
		spanFinder: NewSpanFinder(&testNamer{}).WithSpanMatchers(
			matchers(pathMatcher(matchGlobstar, matchLiteralName("d")))...,
		),
		fractionThrough: 0.3,
		wantESs:         `a/c/d @60ns-65ns`,
	}, {
		description: "**/d @70%",
		trace:       testTrace1(t),
		spanFinder: NewSpanFinder(&testNamer{}).WithSpanMatchers(
			matchers(pathMatcher(matchGlobstar, matchLiteralName("d")))...,
		),
		fractionThrough: 0.7,
		wantESs:         `a/c/d @75ns-80ns`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			pos := NewPosition(test.spanFinder, test.fractionThrough)
			ess := pos.Find(tr)
			var gotESsStrs []string
			for _, es := range ess {
				gotESsStrs = append(
					gotESsStrs,
					fmt.Sprintf(
						"%s @%v-%v",
						strings.Join(
							GetSpanDisplayPath(
								es.Span(),
								&testNamer{},
							),
							"/",
						),
						es.Start(), es.End(),
					),
				)
			}
			gotESs := strings.Join(gotESsStrs, "\n")
			if diff := cmp.Diff(test.wantESs, gotESs); diff != "" {
				t.Errorf("Found positions:\n%s\ndiff (-want +got) %s", gotESs, diff)
			}
		})
	}
}
