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

package criticalpath

import (
	"github.com/google/tracey/trace"
)

// TypeData is a TypeEnumerationData specialized to critical path type.
type TypeData = trace.TypeEnumerationData[Type]

// CustomCriticalPathTypeData is a TypeData for custom critical path types.
var CustomCriticalPathTypeData = &TypeData{
	Type:        CustomCriticalPathType,
	Name:        "custom",
	Description: "Custom",
}

// Types specifies a set of critical path types, along with their metadata, supported by a
// particular trace.
type Types = trace.TypeEnumeration[Type]

// NewTypes creates and returns a new, empty Types instance.
func NewTypes() *Types {
	return trace.NewTypeEnumeration[Type]()
}
