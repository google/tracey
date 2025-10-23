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
	"fmt"
	"strings"
)

// TypeEnumerationData is a base type for trace-specific enumeration points,
// such as hierarchy types and critical path types.
type TypeEnumerationData[T comparable] struct {
	Type              T
	Name, Description string
}

// TypeEnumeration is a base type for trace-specific enumerations, such as
// hierarchy types and critical path types.
type TypeEnumeration[T comparable] struct {
	dataByType      map[T]*TypeEnumerationData[T]
	orderedTypeData []*TypeEnumerationData[T]
	// Mappings from name and description to enumeration data.  If multiple
	// enumeration data have the same name or description, the key will be
	// present but the value will be nil.
	typesByName        map[string]*TypeEnumerationData[T]
	typesByDescription map[string]*TypeEnumerationData[T]
	descriptionAliases *typeAliases
}

// NewTypeEnumeration creates and returns a new, empty TypeEnumeration instance.
func NewTypeEnumeration[T comparable]() *TypeEnumeration[T] {
	return &TypeEnumeration[T]{
		dataByType:         map[T]*TypeEnumerationData[T]{},
		typesByName:        map[string]*TypeEnumerationData[T]{},
		typesByDescription: map[string]*TypeEnumerationData[T]{},
		descriptionAliases: newTypeAliases(false),
	}
}

// With defines the specified type and metadata within the receiver, returning
// the receiver to facilitate streaming.  The first-defined type is the
// default.
func (te *TypeEnumeration[T]) With(
	t T,
	name, description string,
) *TypeEnumeration[T] {
	td := &TypeEnumerationData[T]{
		Type:        t,
		Name:        name,
		Description: description,
	}
	te.dataByType[t] = td
	if _, ok := te.typesByName[name]; ok {
		te.typesByName[name] = nil
	} else {
		te.typesByName[name] = td
	}
	if _, ok := te.typesByDescription[description]; ok {
		te.typesByDescription[description] = nil
	} else {
		te.typesByDescription[description] = td
	}
	te.orderedTypeData = append(te.orderedTypeData, td)
	return te
}

// WithDescriptionAliases declares that the provided alias strings can alias
// the provided description.
func (te *TypeEnumeration[T]) WithDescriptionAliases(description string, aliases ...string) *TypeEnumeration[T] {
	te.descriptionAliases.withAliases(description, aliases...)
	return te
}

// WithTypeData defines the provided TypeEnumerationData within the receiver.
func (te *TypeEnumeration[T]) WithTypeData(td *TypeEnumerationData[T]) *TypeEnumeration[T] {
	te.dataByType[td.Type] = td
	if _, ok := te.typesByName[td.Name]; ok {
		te.typesByName[td.Name] = nil
	} else {
		te.typesByName[td.Name] = td
	}
	if _, ok := te.typesByDescription[td.Description]; ok {
		te.typesByDescription[td.Description] = nil
	} else {
		te.typesByDescription[td.Description] = td
	}
	te.orderedTypeData = append(te.orderedTypeData, td)
	return te
}

// TypeData returns the EnumerationTypeData associated with the provided
// type in the receiver, or nil if none is found.
func (te *TypeEnumeration[T]) TypeData(t T) *TypeEnumerationData[T] {
	return te.dataByType[t]
}

// OrderedTypeData returns the enumeration type data defined in the receiver,
// in the order of their definition.
func (te *TypeEnumeration[T]) OrderedTypeData() []*TypeEnumerationData[T] {
	return te.orderedTypeData
}

// Default returns the receiver's default (i.e., first-defined)
// EnumerationTypeData.
func (te *TypeEnumeration[T]) Default() *TypeEnumerationData[T] {
	if len(te.orderedTypeData) > 0 {
		return te.orderedTypeData[0]
	}
	return nil
}

// ByName returns the type data associated with the provided name string in the
// receiver.  If there is no matching type, returns nil.  Errors if multiple
// types match the specified name.
func (te *TypeEnumeration[T]) ByName(name string) (*TypeEnumerationData[T], error) {
	ted, ok := te.typesByName[name]
	if ok && ted == nil {
		return nil, fmt.Errorf("multiple types match name '%s'", name)
	}
	if !ok {
		typeShortNames := make([]string, len(te.OrderedTypeData()))
		for idx, td := range te.OrderedTypeData() {
			typeShortNames[idx] = td.Name
		}
		return nil, fmt.Errorf("no types match name '%s' (expected one of [%v])", name, strings.Join(typeShortNames, ", "))
	}
	return ted, nil
}

// ByDescription returns the type data associated with the provided description
// string in the receiver.  If there is no matching type, returns nil.  Errors
// if multiple types match the specified description.
func (te *TypeEnumeration[T]) ByDescription(description string) (*TypeEnumerationData[T], error) {
	ted, ok := te.typesByDescription[te.descriptionAliases.lookup(description)]
	if ok && ted == nil {
		return nil, fmt.Errorf("multiple types match description '%s'", description)
	}
	return ted, nil
}

// typeAliases supports aliasing type enumeration strings, allowing primary
// strings (that is, those directly included in type enumerations) to be
// aliased by zero or more alias strings.
type typeAliases struct {
	aliasToPrimary map[string]string
	caseSensitive  bool
}

// newTypeAliases returns a new, empty TypeAliases instance.  If the provided
// boolean is true, lookups are case-sensitive; otherwise they are not.
func newTypeAliases(caseSensitive bool) *typeAliases {
	return &typeAliases{
		aliasToPrimary: map[string]string{},
		caseSensitive:  caseSensitive,
	}
}

// lookup returns the primary string aliased by the provided string, if there
// is one.  If there is no such alias defined, the provided string is returned.
// Lookup's case sensitivity is based on the receiver's.
func (ta *typeAliases) lookup(s string) string {
	ls := s
	if !ta.caseSensitive {
		ls = strings.ToUpper(s)
	}
	got, ok := ta.aliasToPrimary[ls]
	if !ok {
		return s
	}
	return got
}

// withAliases marks the provided aliases as alising the provided primary.  If
// a provided alias already aliases another primary, that is overwritten.
func (ta *typeAliases) withAliases(primary string, aliases ...string) *typeAliases {
	for _, alias := range aliases {
		if !ta.caseSensitive {
			alias = strings.ToUpper(alias)
		}
		ta.aliasToPrimary[alias] = primary
	}
	return ta
}
