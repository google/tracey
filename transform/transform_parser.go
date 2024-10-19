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

package transform

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/tracey/trace"
)

// ApplyTransformationTemplate parses a trace transformation template, applying
// the specified transformations to the provided Transform object.
//
// Supported transformation specifications include:
//
//	SCALE SPANS(<path matchers>) BY <float factor> ;
//	SCALE DEPENDENCIES(<dependency type>)
//	  FROM SPANS(<path matchers>) TO SPANS(<path matchers>)
//	  BY <float factor> ;
//	ADD DEPENDENCY(<dependency type>)
//	  FROM POSITION(<path matchers> AT <float percentage>)
//	  TO POSITIONS(<path matchers> AT <float percentage>) ;
//	REMOVE DEPENDENCIES(<dependency type>)
//	  FROM SPANS(<path matchers>) TO SPANS(<path matchers>) ;
//	APPLY CONCURRENCY <int concurrency> TO SPANS(<path matchers>) ;
//	START SPANS(<path matchers>) AS EARLY AS POSSIBLE ;
//	SHRINK NONBLOCKING DEPENDENCIES TO SPANS(<path matchers>) ;
//
// Statements beginning (possibly after whitespace) with # and ending with ;
// are treated as comments and ignored.
func ApplyTransformationTemplate[T any, CP, SP, DP fmt.Stringer](
	namer trace.Namer[T, CP, SP, DP],
	hierarchyType trace.HierarchyType,
	t *Transform[T, CP, SP, DP],
	template string,
) error {
	ttp := &transformTemplateParser[T, CP, SP, DP]{
		namer:         namer,
		hierarchyType: hierarchyType,
		t:             t,
	}
	pmp, err := trace.NewPathMatcherParser[T, CP, SP, DP]()
	if err != nil {
		return err
	}
	ttp.pmp = pmp
	offset := 0
	for remainder := template; len(remainder) > 0; {
		var re *regexp.Regexp
		var handler matchHandler
		switch {
		case commentRE.MatchString(remainder):
			re = commentRE
		// check scaleDependenciesRE before scaleSpansRE, with which it can alias.
		case scaleDependenciesRE.MatchString(remainder):
			re = scaleDependenciesRE
			handler = ttp.scaleDependenciesHandler
		case scaleSpansRE.MatchString(remainder):
			re = scaleSpansRE
			handler = ttp.scaleSpansHandler
		case addDependenciesRE.MatchString(remainder):
			re = addDependenciesRE
			handler = ttp.addDependenciesHandler
		case removeDependenciesRE.MatchString(remainder):
			re = removeDependenciesRE
			handler = ttp.removeDependenciesHandler
		case applyConcurrencyRE.MatchString(remainder):
			re = applyConcurrencyRE
			handler = ttp.applyConcurrencyHandler
		case startSpansAsEarlyAsPossibleRE.MatchString(remainder):
			re = startSpansAsEarlyAsPossibleRE
			handler = ttp.startSpansAsEarlyAsPossibleHandler
		case shrinkNonblockingDependenciesRE.MatchString(remainder):
			re = shrinkNonblockingDependenciesRE
			handler = ttp.shrinkNonblockingDependenciesHandler
		case whitespaceRE.MatchString(remainder):
			re = whitespaceRE
			handler = func(matches map[string]string) error {
				return nil
			}
		default:
			return fmt.Errorf("failed to parse template at position %d", offset)
		}
		matches := re.FindStringSubmatch(remainder)
		matchNames := re.SubexpNames()
		if len(matches) != len(matchNames) {
			return fmt.Errorf("unexpected error: match count doesn't equal match name count")
		}
		m := map[string]string{}
		for idx, name := range matchNames {
			m[name] = matches[idx]
		}
		offsetDelta := len(remainder) - len(m["remainder"])
		if offsetDelta == 0 {
			return fmt.Errorf("failed to parse template at position %d", offset)
		}
		offset += offsetDelta
		var ok bool
		remainder, ok = m["remainder"]
		if !ok {
			return fmt.Errorf("unexpected error: can't find template remainder")
		}
		if handler != nil {
			if err := handler(m); err != nil {
				return err
			}
		}
	}
	return nil
}

type matchHandler func(matches map[string]string) error

type transformTemplateParser[T any, CP, SP, DP fmt.Stringer] struct {
	namer         trace.Namer[T, CP, SP, DP]
	t             *Transform[T, CP, SP, DP]
	hierarchyType trace.HierarchyType
	pmp           *trace.PathMatcherParser[T, CP, SP, DP]
}

func (ttp *transformTemplateParser[T, CP, SP, DP]) spanFinder(
	spanFinderStr string) (*trace.SpanFinder[T, CP, SP, DP], error) {
	if spanFinderStr != "*" &&
		spanFinderStr != "**" &&
		strings.ToUpper(spanFinderStr) != "ALL" &&
		strings.ToUpper(spanFinderStr) != "ANY" {
		return ttp.pmp.ParseSpanFinderStr(ttp.namer, ttp.hierarchyType, spanFinderStr)
	}
	return nil, nil
}

func (ttp *transformTemplateParser[T, CP, SP, DP]) lookupDependencyTypes(dependencyTypeStr string) ([]trace.DependencyType, error) {
	want := strings.ToUpper(dependencyTypeStr)
	if dependencyTypeStr != "*" &&
		dependencyTypeStr != "**" &&
		strings.ToUpper(dependencyTypeStr) != "ALL" {
		for dt, dts := range ttp.namer.DependencyTypeNames() {
			if strings.ToUpper(dts) == want {
				return []trace.DependencyType{dt}, nil
			}
		}
		return nil, fmt.Errorf("unrecognized dependency type '%s'", dependencyTypeStr)
	}
	return nil, nil
}

func (ttp *transformTemplateParser[T, CP, SP, DP]) scaleSpansHandler(matches map[string]string) error {
	spanFinder, err := ttp.spanFinder(matches["spans"])
	if err != nil {
		return fmt.Errorf("failed to parse path matchers string '%s': %s", matches["spans"], err)
	}
	var factor float64
	n, err := fmt.Sscanf(matches["factor"], "%f", &factor)
	if n == 0 {
		return fmt.Errorf("failed to parse span scaling factor '%s' as float", matches["factor"])
	}
	if err != nil {
		return fmt.Errorf("failed to parse span scaling factor '%s' as float: %s", matches["factor"], err)
	}
	ttp.t.WithSpansScaledBy(spanFinder, factor)
	return nil
}

func (ttp *transformTemplateParser[T, CP, SP, DP]) scaleDependenciesHandler(matches map[string]string) error {
	originSpanFinder, err := ttp.spanFinder(matches["origin_spans"])
	if err != nil {
		return fmt.Errorf("failed to parse origin span path matchers string '%s': %s", matches["spans"], err)
	}
	destinationSpanFinder, err := ttp.spanFinder(matches["destination_spans"])
	if err != nil {
		return fmt.Errorf("failed to parse destination span path matchers string '%s': %s", matches["spans"], err)
	}
	var factor float64
	n, err := fmt.Sscanf(matches["factor"], "%f", &factor)
	if n == 0 {
		return fmt.Errorf("failed to parse dependency scaling factor '%s' as float", matches["factor"])
	}
	if err != nil {
		return fmt.Errorf("failed to parse dependency scaling factor '%s' as float: %s", matches["factor"], err)
	}
	dts, err := ttp.lookupDependencyTypes(matches["dependency_types"])
	if err != nil {
		return err
	}
	ttp.t.WithDependenciesScaledBy(originSpanFinder, destinationSpanFinder, dts, factor)
	return nil
}

func (ttp *transformTemplateParser[T, CP, SP, DP]) addDependenciesHandler(matches map[string]string) error {
	originPos, err := ttp.pmp.ParseTracePositionStr(ttp.namer, ttp.hierarchyType, matches["origin_position"])
	if err != nil {
		return fmt.Errorf("failed to parse origin position: %s", err)
	}
	destinationPos, err := ttp.pmp.ParseTracePositionStr(ttp.namer, ttp.hierarchyType, matches["destination_positions"])
	if err != nil {
		return fmt.Errorf("failed to parse destination position: %s", err)
	}
	dts, err := ttp.lookupDependencyTypes(matches["dependency_type"])
	if err != nil {
		return err
	}
	if len(dts) != 1 {
		return fmt.Errorf("exactly one added dependency type must be specified")
	}
	ttp.t.WithAddedDependencies(
		originPos, destinationPos,
		dts[0],
		0,
	)
	return nil
}

func (ttp *transformTemplateParser[T, CP, SP, DP]) removeDependenciesHandler(matches map[string]string) error {
	originSpanFinder, err := ttp.spanFinder(matches["origin_spans"])
	if err != nil {
		return fmt.Errorf("failed to parse origin span path matchers string '%s': %s", matches["spans"], err)
	}
	destinationSpanFinder, err := ttp.spanFinder(matches["destination_spans"])
	if err != nil {
		return fmt.Errorf("failed to parse destination span path matchers string '%s': %s", matches["spans"], err)
	}
	dts, err := ttp.lookupDependencyTypes(matches["dependency_types"])
	if err != nil {
		return err
	}
	ttp.t.WithRemovedDependencies(originSpanFinder, destinationSpanFinder, dts)
	return nil
}

type concurrencyLimiter[T any, CP, SP, DP fmt.Stringer] struct {
	allowedConcurrency, currentConcurrency int
}

func (cl *concurrencyLimiter[T, CP, SP, DP]) SpanStarting(span trace.Span[T, CP, SP, DP], isSelected bool) {
	if isSelected {
		cl.currentConcurrency++
	}
}

func (cl *concurrencyLimiter[T, CP, SP, DP]) SpanEnding(span trace.Span[T, CP, SP, DP], isSelected bool) {
	if isSelected {
		cl.currentConcurrency--
	}
}

func (cl *concurrencyLimiter[T, CP, SP, DP]) SpanCanStart(span trace.Span[T, CP, SP, DP]) bool {
	return cl.currentConcurrency < cl.allowedConcurrency
}

func (ttp *transformTemplateParser[T, CP, SP, DP]) applyConcurrencyHandler(matches map[string]string) error {
	spanFinder, err := ttp.spanFinder(matches["spans"])
	if err != nil {
		return fmt.Errorf("failed to parse origin span path matchers string '%s': %s", matches["spans"], err)
	}
	concurrency, err := strconv.Atoi(matches["concurrency"])
	if err != nil {
		return fmt.Errorf("failed to parse concurrency '%s' as integer: %s", matches["concurrency"], err)
	}
	ttp.t.WithSpansGatedBy(spanFinder, func() SpanGater[T, CP, SP, DP] {
		return &concurrencyLimiter[T, CP, SP, DP]{
			allowedConcurrency: concurrency,
		}
	})
	return nil
}

func (ttp *transformTemplateParser[T, CP, SP, DP]) startSpansAsEarlyAsPossibleHandler(matches map[string]string) error {
	spanFinder, err := ttp.spanFinder(matches["spans"])
	if err != nil {
		return fmt.Errorf("failed to parse origin span path matchers string '%s': %s", matches["spans"], err)
	}
	ttp.t.WithSpansStartingAsEarlyAsPossible(spanFinder)
	return nil
}

func (ttp *transformTemplateParser[T, CP, SP, DP]) shrinkNonblockingDependenciesHandler(matches map[string]string) error {
	spanFinder, err := ttp.spanFinder(matches["spans"])
	if err != nil {
		return fmt.Errorf("failed to parse origin span path matchers string '%s': %s", matches["spans"], err)
	}
	dts, err := ttp.lookupDependencyTypes(matches["dependency_types"])
	if err != nil {
		return err
	}
	ttp.t.WithShrinkableIncomingDependencies(spanFinder, dts, 0)
	return nil
}

var (
	commentRE    = transformRegex(hash, anything("_"))
	scaleSpansRE = transformRegex(
		scale, spans("spans"),
		by, float("factor"),
	)
	scaleDependenciesRE = transformRegex(
		scale, dependencies("dependency_types"),
		from, spans("origin_spans"),
		to, spans("destination_spans"),
		by, float("factor"),
	)
	addDependenciesRE = transformRegex(
		add, dependency("dependency_type"),
		from, position("origin_position"),
		to, positions("destination_positions"),
	)
	removeDependenciesRE = transformRegex(
		remove, dependencies("dependency_types"),
		from, spans("origin_spans"),
		to, spans("destination_spans"),
	)
	applyConcurrencyRE = transformRegex(
		apply, concurrency, integer("concurrency"),
		to, spans("spans"),
	)
	startSpansAsEarlyAsPossibleRE = transformRegex(
		start, spans("spans"),
		as, early, as, possible,
	)
	shrinkNonblockingDependenciesRE = transformRegex(
		shrink, nonblocking, dependencies("dependency_types"), to, spans("spans"),
	)
	whitespaceRE = regexp.MustCompile(`\s*` + remainder)
)

func anything(name string) string {
	return "(?P<" + name + ">.+?)"
}

func keyword(str string) string {
	return "(?i:" + str + ")"
}

func pathMatcher(name string) string {
	return anything(name)
}

func spans(name string) string {
	return keyword("SPANS") + `\(` + pathMatcher(name) + `\)`
}

func position(name string) string {
	return keyword("POSITION") + `\(` + anything(name) + `\)`
}

func positions(name string) string {
	return keyword("POSITIONS") + `\(` + anything(name) + `\)`
}

func float(name string) string {
	return "(?P<" + name + ">[+-]?([0-9]*[.])?[0-9])"
}

func dependencyType(name string) string {
	return anything(name)
}

func dependencies(name string) string {
	return keyword("DEPENDENCIES") + `\(` + dependencyType(name) + `\)`
}

func dependency(name string) string {
	return keyword("DEPENDENCY") + `\(` + dependencyType(name) + `\)`
}

func integer(name string) string {
	return "(?P<" + name + ">[0-9]*)"
}

func moment(name string) string {
	return anything(name)
}

func buildRegex(fragments ...string) string {
	return `^\s*` + strings.Join(fragments, `\s+`) + `\s*\;` + remainder
}

func transformRegex(fragments ...string) *regexp.Regexp {
	return regexp.MustCompile(buildRegex(fragments...))
}

var (
	remainder   = `(?P<remainder>(?s:.*))$`
	hash        = keyword("#")
	scale       = keyword("SCALE")
	by          = keyword("BY")
	from        = keyword("FROM")
	to          = keyword("TO")
	add         = keyword("ADD")
	remove      = keyword("REMOVE")
	apply       = keyword("APPLY")
	concurrency = keyword("CONCURRENCY")
	start       = keyword("START")
	as          = keyword("AS")
	early       = keyword("EARLY")
	possible    = keyword("POSSIBLE")
	shrink      = keyword("SHRINK")
	nonblocking = keyword("NONBLOCKING")
)