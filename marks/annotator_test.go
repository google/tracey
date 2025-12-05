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

package marks

import (
	"testing"
	"time"

	"github.com/google/tracey/trace"

	"github.com/google/go-cmp/cmp"
	testtrace "github.com/google/tracey/test_trace"
	traceparser "github.com/google/tracey/trace/parser"
)

func parseSpanPattern(t *testing.T, spanPatternStr string) *traceparser.SpanPattern {
	t.Helper()
	ret, err := traceparser.ParseSpanSpecifierPatterns(trace.SpanOnlyHierarchyType, spanPatternStr)
	if err != nil {
		t.Fatalf("Failed to parse span specifier patterh '%s': %s", spanPatternStr, err.Error())
	}
	return ret
}

func TestAnnotations(t *testing.T) {
	trace1, err := testtrace.Trace1()
	if err != nil {
		t.Fatalf("Failed to build trace1: %s", err.Error())
	}
	for _, test := range []struct {
		description  string
		tr           trace.Trace[time.Duration, testtrace.StringPayload, testtrace.StringPayload, testtrace.StringPayload]
		annotator    *Annotator
		wantTraceStr string
	}{{
		description: "trace1 with some labels",
		tr:          trace1,
		annotator: NewAnnotator().
			AnnotateOutgoing(
				parseSpanPattern(t, "s0.0.0/0"),
				testtrace.Spawn,
				"s0-spawn-",
			).
			AnnotateIncoming(
				parseSpanPattern(t, "s0.1.0"),
				testtrace.Send,
				"s0.1.0-send-",
			),
		wantTraceStr: `
Trace (Span-only):
  Span 's0.0.0' (0s-100ns) (s0.0.0)
    Elementary spans:
      0s-10ns <none> -> THIS -> [call to s0.0.0/0 @10ns]
        'start' @0s
      90ns-100ns [return from s0.0.0/0 @90ns] -> THIS -> <none>
        'end' @100ns
    Span '0' (10ns-90ns) (s0.0.0/0)
      Elementary spans:
        10ns-20ns [call from s0.0.0 @10ns] -> THIS -> [spawn to s0.1.0 @30ns]
          's0-spawn-0' @20ns
        20ns-30ns <none> -> THIS -> [spawn to s1.0.0 @30ns]
          's0-spawn-1' @30ns
        30ns-40ns <none> -> THIS -> [call to s0.0.0/0/3 @40ns]
        70ns-90ns [return from s0.0.0/0/3 @70ns] -> THIS -> [return to s0.0.0 @90ns]
      Span '3' (40ns-70ns) (s0.0.0/0/3)
        Elementary spans:
          40ns-50ns [call from s0.0.0/0 @40ns] -> THIS -> <none>
          60ns-70ns [signal from s0.1.0 @50ns] -> THIS -> [return to s0.0.0/0 @70ns]
  Span 's0.1.0' (30ns-70ns) (s0.1.0)
    Elementary spans:
      30ns-40ns [spawn from s0.0.0/0 @20ns] -> THIS -> <none>
        's0.1.0-send-0' @40ns
      40ns-50ns [send from s1.0.0 @35ns] -> THIS -> [signal to s0.0.0/0/3 @60ns]
      50ns-70ns <none> -> THIS -> <none>
  Span 's1.0.0' (30ns-50ns) (s1.0.0)
    Elementary spans:
      30ns-35ns [spawn from s0.0.0/0 @30ns] -> THIS -> [send to s0.1.0 @40ns]
      35ns-50ns <none> -> THIS -> <none>`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			if err := AnnotateTrace(test.annotator, test.tr); err != nil {
				t.Fatalf("Failed to annotate trace: %s", err.Error())
			}
			gotTraceStr := testtrace.TPP.PrettyPrintTrace(test.tr, trace.SpanOnlyHierarchyType)
			if diff := cmp.Diff(test.wantTraceStr, gotTraceStr); diff != "" {
				t.Errorf("got trace string\n%s\n, diff (-want +got) %s", gotTraceStr, diff)
			}
		})
	}
}
