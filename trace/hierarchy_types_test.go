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
)

func TestDefaultTypes(t *testing.T) {
	hTypes := NewHierarchyTypes().
		With(HierarchyType(0), "cpus", "CPUs").
		With(HierarchyType(1), "procsAndThreads", "Processes and threads")
	defaultType := hTypes.Default()
	if defaultType.Type != 0 {
		t.Fatalf("Default hierarchy type was '%s', expected '%s'", defaultType.Name, hTypes.TypeData(0).Name)
	}
}

func TestDescriptionAliases(t *testing.T) {
	hTypes := NewHierarchyTypes().
		With(HierarchyType(0), "cpus", "CPUs").
		WithDescriptionAliases("CPUs", "central processing units").
		With(HierarchyType(1), "procsAndThreads", "Processes and threads").
		WithDescriptionAliases("Processes and threads", "processes", "threads")
	if ht, err := hTypes.ByDescription("central processing units"); err != nil {
		t.Fatalf("Failed to find hierarchy type by alias")
	} else if ht.Type != HierarchyType(0) {
		t.Errorf("Lookup by alias found wrong hierarchy type")
	}
}
