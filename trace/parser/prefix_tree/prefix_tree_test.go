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

package prefixtree

import (
	"testing"
)

func TestPrefixTree(t *testing.T) {
	map1 := map[string]*Token[int]{
		"as":        Keyword(1),
		"ass":       Keyword(2),
		"assert":    Keyword(3),
		"ASSERTION": Keyword(4),
		"to":        Keyword(5),
		"tom":       Keyword(6),
		"tomato":    Keyword(7),
		",":         Operator(8),
	}
	for _, test := range []struct {
		description   string
		valuesByInput map[string]*Token[int]
		input         string
		wantFound     bool
		wantLen       int
		wantVal       int
	}{{
		description:   "assertion",
		valuesByInput: map1,
		input:         "assertion",
		wantFound:     true,
		wantLen:       9,
		wantVal:       4,
	}, {
		description:   "assertionizing",
		valuesByInput: map1,
		input:         "assertionizing",
	}, {
		description:   "asserti",
		valuesByInput: map1,
		input:         "asserti",
	}, {
		description:   "blob",
		valuesByInput: map1,
		input:         "blob",
	}, {
		description:   ",and",
		valuesByInput: map1,
		input:         ",and",
		wantFound:     true,
		wantLen:       1,
		wantVal:       8,
	}} {
		t.Run(test.description, func(t *testing.T) {
			root, err := New(test.valuesByInput)
			if err != nil {
				t.Fatalf("New() returned unexpected error %s", err)
			}
			found, len, val := root.FindMaximalPrefix(test.input)
			if found != test.wantFound {
				t.Fatalf("FindMaximalPrefix returned %t, wanted %t", found, test.wantFound)
			}
			if len != test.wantLen {
				t.Errorf("FindMaximalPrefix returned len %d, wanted %d", len, test.wantLen)
			}
			if val != test.wantVal {
				t.Errorf("FindMaximalPrefix returned val %v, wanted %v", val, test.wantVal)
			}
		})
	}
}
