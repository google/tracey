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

// Package predicate provides types and functions for programmatically
// assembling predicates on trace spans and using those predicates to generate
// SpanFilters.
package predicate

import (
	"fmt"

	"github.com/google/tracey/trace"
)

// ComparisonOperator enumerates comparison operators.
type ComparisonOperator int

// Available leaf comparison operators.
const (
	UnknownComparisonOperator ComparisonOperator = iota
	Equal
	NotEqual
	GreaterThan
	GreaterThanOrEqual
	LessThan
	LessThanOrEqual
)

// MetricType enumerates available metrics.
type MetricType int

// Available metrics
const (
	UnknownMetric MetricType = iota
	TotalDuration
	TotalUnsuspendedDuration
	SelfUnsuspendedDuration
	SuspendedDuration
)

type logicalOperator int

// Available logical operators.
const (
	UnknownLogicalOperator logicalOperator = iota
	And
	Or
)

type comparandType int

// Available comparand types.
const (
	UnknownComparandType comparandType = iota
	DurationComparand
)

// Comparand describes a comparand: a literal value to be compared against.
type Comparand struct {
	comparandType   comparandType
	comparandString string
}

// Duration defines a duration-type comparand whose value is described by the
// provided string.
func Duration(str string) *Comparand {
	return &Comparand{
		comparandType:   DurationComparand,
		comparandString: str,
	}
}

type predicateType int

const (
	comparatorPredicate predicateType = iota
	logicalPredicate
)

// Predicate describes a predicate used to filter spans in span matching.
type Predicate struct {
	predicateType predicateType

	// Fields valid if predicateType == comparatorPredicate
	metricType         MetricType
	comparisonOperator ComparisonOperator
	comparand          *Comparand

	// Fields valid if predicateType == logicalPredicate
	logicalOperator logicalOperator
	left, right     *Predicate
}

// NewComparatorPredicate returns a new comparator-type Predicate in which
// the provided MetricType for each Span is compared with the provided
// Comparand using the provided ComparisonOperator.
func NewComparatorPredicate(
	metricType MetricType,
	comparisonOperator ComparisonOperator,
	comparand *Comparand,
) *Predicate {
	return &Predicate{
		predicateType:      comparatorPredicate,
		metricType:         metricType,
		comparisonOperator: comparisonOperator,
		comparand:          comparand,
	}
}

// NewLogicalPredicate returns a new logical-operator Predicate in which the
// provided left and right Predicates are compared using the provided
// logicalOperator.
func NewLogicalPredicate(
	left *Predicate,
	logicalOperator logicalOperator,
	right *Predicate,
) *Predicate {
	return &Predicate{
		predicateType:   logicalPredicate,
		left:            left,
		logicalOperator: logicalOperator,
		right:           right,
	}
}

func getSelfUnsuspendedDuration[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	span trace.Span[T, CP, SP, DP],
) float64 {
	var ret float64
	for _, es := range span.ElementarySpans() {
		ret += comparator.Diff(es.End(), es.Start())
	}
	return ret
}

func getSelfSuspendedDuration[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	span trace.Span[T, CP, SP, DP],
) float64 {
	var ret float64
	var last T = span.Start()
	for _, es := range span.ElementarySpans() {
		ret += comparator.Diff(es.Start(), last)
		last = es.End()
	}
	return ret
}

func getTotalUnsuspendedDuration[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	span trace.Span[T, CP, SP, DP],
) float64 {
	ret := getSelfUnsuspendedDuration(comparator, span)
	for _, child := range span.ChildSpans() {
		ret += getTotalUnsuspendedDuration(comparator, child)
	}
	return ret
}

func spanFilterFromComparatorPredicate[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	comparisonOperator ComparisonOperator,
	metricType MetricType,
	comparand *Comparand,
) (trace.SpanFilter[T, CP, SP, DP], error) {
	switch metricType {
	case TotalDuration, TotalUnsuspendedDuration, SelfUnsuspendedDuration, SuspendedDuration:
		if comparand.comparandType != DurationComparand {
			return nil, fmt.Errorf("expected duration comparand, but got '%v'", comparand.comparandType)
		}
		comparandVal, err := comparator.DurationFromString(comparand.comparandString)
		if err != nil {
			return nil, fmt.Errorf("invalid comparand string '%s'", comparand.comparandString)
		}
		switch comparisonOperator {
		case Equal, NotEqual, GreaterThan, GreaterThanOrEqual, LessThan, LessThanOrEqual:
		default:
			return nil, fmt.Errorf("invalid comparator '%v'", comparisonOperator)
		}
		return func(span trace.Span[T, CP, SP, DP]) (include, prune bool) {
			var metric float64
			switch metricType {
			case TotalDuration:
				metric = comparator.Diff(span.End(), span.Start())
			case TotalUnsuspendedDuration:
				metric = getTotalUnsuspendedDuration(comparator, span)
			case SelfUnsuspendedDuration:
				metric = getSelfUnsuspendedDuration(comparator, span)
			case SuspendedDuration:
				metric = getSelfSuspendedDuration(comparator, span)
			}
			switch comparisonOperator {
			case Equal:
				return metric == comparandVal, false
			case NotEqual:
				return metric != comparandVal, false
			case GreaterThan:
				return metric > comparandVal, false
			case GreaterThanOrEqual:
				return metric >= comparandVal, false
			case LessThan:
				return metric < comparandVal, false
			case LessThanOrEqual:
				return metric <= comparandVal, false
			}
			return false, false
		}, nil
	default:
		return nil, fmt.Errorf("unsupported comparator predicate metric '%v'", metricType)
	}
}

func spanFilterFromLogicalPredicate[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	logicalOperator logicalOperator,
	leftFn, rightFn trace.SpanFilter[T, CP, SP, DP],
) (trace.SpanFilter[T, CP, SP, DP], error) {
	switch logicalOperator {
	case And, Or:
		return func(span trace.Span[T, CP, SP, DP]) (include, prune bool) {
			switch logicalOperator {
			// Allow short-circuiting.
			case And:
				includeLeft, _ := leftFn(span)
				if !includeLeft {
					return false, false
				}
				return rightFn(span)
			case Or:
				includeLeft, _ := leftFn(span)
				if includeLeft {
					return true, false
				}
				return rightFn(span)
			}
			return false, false
		}, nil
	default:
		return nil, fmt.Errorf("unsupported logical operator '%v", logicalOperator)
	}
}

// BuildSpanFilter generates a SpanFilter function from the provided Predicate,
// using the provided Comparator.
func BuildSpanFilter[T any, CP, SP, DP fmt.Stringer](
	comparator trace.Comparator[T],
	predicate *Predicate,
) (trace.SpanFilter[T, CP, SP, DP], error) {
	switch predicate.predicateType {
	case comparatorPredicate:
		return spanFilterFromComparatorPredicate[T, CP, SP, DP](
			comparator,
			predicate.comparisonOperator,
			predicate.metricType,
			predicate.comparand,
		)
	case logicalPredicate:
		leftFn, err := BuildSpanFilter[T, CP, SP, DP](comparator, predicate.left)
		if err != nil {
			return nil, err
		}
		rightFn, err := BuildSpanFilter[T, CP, SP, DP](comparator, predicate.right)
		if err != nil {
			return nil, err
		}
		return spanFilterFromLogicalPredicate[T, CP, SP, DP](
			comparator,
			predicate.logicalOperator,
			leftFn, rightFn,
		)
	default:
		return nil, fmt.Errorf("unsupported predicate type '%v", predicate.predicateType)
	}
}
