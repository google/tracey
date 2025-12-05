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

package stitch

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	tt "github.com/google/tracey/test_trace"
	"github.com/google/tracey/trace"
)

func opts(opts ...Option[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload]) []Option[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload] {
	return opts
}

func dep(
	dependencyType trace.DependencyType,
	opts ...DependencyOption,
) Option[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload] {
	return Dependency[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload](dependencyType, opts...)
}

func TestStitch(t *testing.T) {
	tpp := tt.NewPrettyPrinter(tt.TestNamer)
	for _, test := range []struct {
		description   string
		options       []Option[time.Duration, tt.StringPayload, tt.StringPayload, tt.StringPayload]
		hierarchyType trace.HierarchyType
		wantTraceStr  string
	}{{
		description: "RPC call stitching",
		options: opts(
			// RPC caller trace.
			Trace(
				tt.NewTestingTraceBuilder(t).
					WithRootCategories(
						tt.RootCategory(tt.Structural, "client"),
					).
					WithRootSpans(
						tt.RootSpan(0, 100, "foo",
							tt.ParentCategories(
								tt.FindCategory(tt.Structural, "client"),
							),
							tt.Span(10, 90, "rpc_caller"),
						),
					).
					WithSuspend(tt.Paths("foo", "rpc_caller"), 20, 80).
					Build(),
			),
			// RPC handler trace.
			Trace(
				tt.NewTestingTraceBuilder(t).
					WithRootCategories(
						tt.RootCategory(tt.Structural, "server"),
					).
					WithRootSpans(
						tt.RootSpan(20, 80, "rpc_handler",
							tt.ParentCategories(
								tt.FindCategory(tt.Structural, "server"),
							),
							tt.Span(30, 70, "bar"),
						),
					).
					Build(),
			),
			// Stitch in the 'call' edge from caller to handler.
			dep(
				tt.Spawn,
				Origins("foo/rpc_caller @49.999%"),
				Destinations("rpc_handler @0%"),
			),
			// Stitch in the 'response' edge from handler back to caller.
			dep(
				tt.Signal,
				Origins("rpc_handler @100%"),
				Destinations("foo/rpc_caller @50.001%"),
			),
		),
		hierarchyType: tt.Structural,
		wantTraceStr: `
Trace (structural):
  Category 'client' (client)
    Span 'foo' (0s-100ns) (foo)
      Elementary spans:
        0s-10ns <none> -> THIS -> [call to foo/rpc_caller @10ns]
        90ns-100ns [return from foo/rpc_caller @90ns] -> THIS -> <none>
      Span 'rpc_caller' (10ns-90ns) (foo/rpc_caller)
        Elementary spans:
          10ns-19ns [call from foo @10ns] -> THIS -> [spawn to rpc_handler @20ns]
          19ns-20ns <none> -> THIS -> <none>
          80ns-90ns [signal from rpc_handler @80ns] -> THIS -> [return to foo @90ns]
  Category 'server' (server)
    Span 'rpc_handler' (20ns-80ns) (rpc_handler)
      Elementary spans:
        20ns-30ns [spawn from foo/rpc_caller @19ns] -> THIS -> [call to rpc_handler/bar @30ns]
        70ns-80ns [return from rpc_handler/bar @70ns] -> THIS -> [signal to foo/rpc_caller @80ns]
      Span 'bar' (30ns-70ns) (rpc_handler/bar)
        Elementary spans:
          30ns-70ns [call from rpc_handler @30ns] -> THIS -> [return to rpc_handler @70ns]`,
	}} {
		t.Run(test.description, func(t *testing.T) {
			tr, err := Stitch(test.options...)
			if err != nil {
				t.Fatalf("Failed to stitch traces: %v", err)
			}
			gotTraceStr := tpp.PrettyPrintTrace(tr, test.hierarchyType)
			if diff := cmp.Diff(test.wantTraceStr, gotTraceStr); diff != "" {
				t.Errorf("Got stitched trace:%s\ndiff (-want +got) %s", gotTraceStr, diff)
			}
		})
	}
}
