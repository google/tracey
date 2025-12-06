The Tracey libraries support causal trace analysis, but what is causal tracing?

Under most execution tracing systems, such as
[OpenTelemetry's tracing](https://opentelemetry.io/docs/concepts/signals/traces/),
a *trace* comprises a collection of *spans*, which represent units of work at
the tracing granularity.  Spans can be anything, but generally represent an
operation which is meaningful at the source or system level, such as a function,
an RPC, or a pipeline phase.  Spans generally include a context (i.e., a root
request ID, an RPC id, a parent span ID), a temporal extent (i.e., a start and
end moment), and a set of *events*, each of which occurs at a moment or for some
duration during its span's temporal extent.

Tracing spans alone is adequate for many kinds of performance analysis: such
traces can show with certainty what ran, and for each running span, where and
when it ran.  However, they can't show *why* the span ran then -- critical
information for understanding why a given operation took as long as it took, or
where opportunity to speed things up might exist.  To understand *why* work ran
when it did requires *causal tracing*.

Causal tracing extends span-only tracing, adding information about the *causal
relationships* between spans.  For example, in the following parallel execution,

```
lock <- NewLock().acquire()

function foo() {
    // starting at T=0ms
    work_for(5ms)
    lock.release() // at T=5ms
}

function bar() {
    // starting at T=0ms
    lock.acquire() // ending at T=5ms
    work_for(5ms)  
    lock.release() // ending at T=10ms
}

parallel_run(foo, bar)
```

a span-only trace would show a `foo` span running from T=0ms to T=5ms, and a
`bar` span running from T=0ms to T=10ms.  However, a causal trace might also
show two additional facts: that `bar` was suspended between T=0ms and T=5ms, and
that the execution starting at T=5ms within `bar` could not start until the
execution in `foo` at T=5ms completed: in other words, a dependency edge exists
between `foo` at T=5ms and `bar` at T=5ms.  Tracing systems which include
information about span suspensions and inter-span dependencies are *causal
tracing systems*, and the traces they produce are *causal traces*.

In terms of actionable latency evidence, causal traces are substantially more
powerful than traces without causality.  For the example execution, causal
information clearly reveals why the operation took as long as it took -- `foo`
and `bar` were forced to serialize -- and what could be done to improve latency.
Specifically, adding causality to traces enables many new latency analyses.
Among them:

*   [*critical path analysis*](./critical_path_analysis.md): find sequential
    paths through traced execution that can explain why the execution took as
    long as it did;
*   [*antagonism analysis*](./antagonism_analysis.md): find places where work is
    delayed due to resource contention, rather than due to dependencies on
    earlier work;
*   [*trace transformation*](./transformation.md): understand how the traced
    execution would have changed under different conditions.  For example,
    estimate the effect of an optimization or an added feature by modeling that
    optimization or feature, rather than actually implementing, deploying, and
    tracing the change.

# How Tracey models causal traces

The causal tracing data model, defined in [model.go](../trace/model.go),
includes a top-level `Trace` type containing:

*   Zero or more *`Category` hierarchies*, representing different ways of
    grouping and arranging the `Trace`'s `Span`s.  Different category
    hierarchies can provide different ways of understanding the context of the
    `Trace`'s `Span`s.  For example, for a trace of multiple processes on a
    single node, appropriate `Category` hierarchies might include:
    * One showing traced execution broken down by CPU topology, with nested
      `Category` instances for NUMA node, CPU complex, core, and hyperthread;
      and
    * Another showing traced execution broken down by processes and task, with
      `Category` instances for each process (nested under the task that spawned
      it, if there is one) and for each task (nested under the task that spawned
      it, or its parent process if it is the root.)

    Each `Trace` has a list of supported `HierarchyType`s, and for each such
    `HierarchyType`, a list of `RootCategory` instances within that type.
    `Category` instances (including `RootCategory` instances) have parent and
    child `Category` instances, a `HierarchyType`, a parameterized payload
    (which includes information like naming), and zero or more `RootSpan`s.

    Categories are generally not necessary for analysis, but are essential for
    visualization, especially for traces of large and complex systems.
*   A set of *`Span` hierarchies*, accessed via a list of `RootSpan` instances.
    `Span` hierarchies generally reflect nested yielding execution: a parent
    `Span` yields to its children, and suspends until they return.

    `Span` instances (including `RootSpan` instances) have a temporal extent,
    parent and child `Span` instances, a parameterized payload, a `RootSpan`
    instance, and a set of `ElementarySpan`s.  `RootSpan` instances also have at
    most one parent `Category` for each `HierarchyType` in the trace.
*   A set of *`Dependency`* instances, representing causal dependencies beteween
    origin and destination `Span`s.  Each `Dependency` has a `DependencyType`, a
    parameterized payload, and zero or more origin and destination
    `ElementarySpan`s.

## Elementary spans

Within a `Trace`, each `Span` contains one or more non-overlapping
`ElementarySpan`s.  An `ElementarySpan` represents an interval within its parent
`Span` during which neither the `Span`'s causal view of the rest of the `Trace`,
nor the rest of the `Trace`'s view of the `Span`, can change.  Specifically,

*    Every `Dependency` destination within a `Span` starts an `ElementarySpan`
     within that `Span`;
*    Every `Dependency` origin within a `Span` ends an `ElementarySpan` within
     that `Span`; and
*    Every `ElementarySpan` within a `Span` has at most one incoming
     `Dependency`, for which it is a destination, and at most one outgoing
     `Dependency`, for which it is an origin.

Each `ElementarySpan` instance has a (possibly zero-duration) temporal extent
which lies fully within their parent `Span` and overlaps no other
`ElementarySpan`'s temporal extent within that parent.  It also has a parent
`Span`, zero or more `Mark`s (labelled moments within the trace), a
`Predecessor` and a `Successor` `ElementarySpan` within its parent `Span`, and
an `Incoming` and an `Outgoing` dependency.  `Predecessor`, `Successor`,
`Incoming`, and `Outgoing` may be nil.  If a nonzero temporal gap exists between
an `ElementarySpan` and its successor, that gap represents time during which the
parent `Span` was suspended or blocked.

`ElementarySpans` are generally created automatically as `Dependency` instances
and suspend intervals are added to a `Span` (for very large or complex traces,
using the [MutableTrace](../trace/mutable_model.go) interface can be more
efficient).

Within a `Trace`, the `ElementarySpans` should form a forest of directed acyclic
graphs -- directed because causality is directional; acyclic because cause
should not precede effect (but note that the interface does not preclude
negative dependency edges, and in some cases they can be valid; see
[below](#multiple-origins-or-destinations)).  Many causal trace analysis
techniques devolve to graph algorithms over the `ElementarySpan` graph.

## Multiple origins or destinations

The example execution above produces a `signal`-type `Dependency` with one
origin (`foo` at T=5ms) and one destination (`bar` a T=5ms).  Such
single-origin, single-destination behavior is common, but not required.

A `Dependency` can have multiple destinations.  This models when one event
causally affects multiple `Span`s, such as when a single barrier release allows
several threads to continue.  It's preferable that this be modeled with one
origin, instead of an origin for each destination, because with the latter, each
origin after the first would also causally depend on its predecessors.

Interestingly, a `Dependency` can also have multiple origins.  This models when
multiple `Span`s participate in causally affecting one or more other `Span`s.
For example, if a `Span` is blocked on multiple different events, and all of
those events must complete before the blocked `Span` may proceed, then each of
those events is a separate origin to the one dependency; this is
'multiple-origin AND semantics'.  Likewise, 'multiple-origin AND semantics' have
destination `Span`s blocked on several different events, and *any* of those
events must complete before the blocked `Span`s may proceed; this arises, for
instance, when considering the causal relationships between the computation that
produces a cached value (the destination) and that cached value's consumers (the
origins).