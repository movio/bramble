# Algorithm Definitions

This section defines Bramble's core algorithms in pseudocode form with accompanying comments.
The query planning and execution at the heart of Bramble are unfortunately non-trivial and this document should help with keeping its codebase maintainable in the future.

Algorithms are presented in a simplified form. Additional complexity is present in the implementation. Still, most of the logic should be contained in the pseudo-code and be of significant help when getting up to speed with Bramble's internals.

## Type Definitions

The following is a pseudocode representation of the data types used in this specification document.

```
union ast.Selection = ast.Field | ast.InlineFragment | ast.FragmentSpread

type ast.Field {
    Alias string
    Name string
    Arguments []ast.Argument
    Directives []ast.Directive
    SelectionSet []ast.Selection
}

type ast.InlineFragment {
    TypeCondition string
    Directives []ast.Directive
    SelectionSet []ast.Selection
}

type ast.FragmentSpread {
    Name string
    Directives []ast.Directive
    Definition ast.FragmentDefinition
}

type asr.FragmentDefinition {
	Name string
	TypeCondition string
	Directives []ast.Directive
	SelectionSet []ast.Selection
}

union ast.Type = ast.NamedType | ast.ListType

type ast.NamedType {
    Name string
    NonNull bool
}

type ast.ListType {
    Elem ast.Type
    NonNull bool
}

type ast.Definition {
    Kind        "SCALAR" | "OBJECT" | "INTERFACE" | "UNION" | "ENUM" | "INPUT_OBJECT"
    Description string
    Name        string
    Directives  []ast.Directive
    Interfaces  []string
    Fields      []ast.FieldDefinition
    Types       []string
    EnumValues  []EnumValueDefinition
}

type ast.FieldDefinition {
    Description  string
    Name         string
    Arguments    []ast.ArgumentDefinition
    DefaultValue ast.Value
    Type         ast.Type
    Directives  []ast.Directive
}

type ast.Schema {
    Query        ast.Definition
    Mutation     ast.Definition
    Subscription ast.Definition
    Types        map[string]ast.Definition
    Directives   map[string]ast.DirectiveDefinition
}

type ast.OperationDefinition {
    Operation           "query" | "mutation" | "subscription"
    Name                string
    VariableDefinitions []ast.VariableDefinition
    Directives          []ast.Directive
    SelectionSet        []ast.Selection
}

type graphql.Response {
    Errors     []Error
    Data       []byte
    Extensions map[string]interface{}
}
```

## Query Planning

The query planner uses the following additional types.

```

type QueryPlan {
    RootSteps []QueryPlanStep
}

type QueryPlanStep {
    ServiceURL     string
    ParentType     string
    SelectionSet   ast.SelectionSet
    InsertionPoint []string
    Then           []QueryPlanStep
}

type PlanningContext  {
	Operation  ast.OperationDefinition
	Schema     ast.Schema
	Locations  map[string]string
	IsBoundary map[string]bool
}

```

#### `CreateQueryPlan`

This function creates a query plan for the given query.
It's a simple convience wrapper for `CreateQueryPlanSteps` since the latter is recursive and requires more parameters.

```

function CreateQueryPlan(ctx PlanningContext) {
    panic if operation is not "Query" or "Mutation"
    return QueryPlan {
        Steps: CreateQueryPlanSteps(
            ctx,
            insertionPoint = empty slice,
            parentType = operation's root type ("Query" or "Mutation"),
            selectionSet = ctx.Operation.SelectionSet
        )
    }
}
```

#### `CreateQueryPlanSteps`

This function starts by "routing" the selection set, i.e. it partitions the selection set by the locations of
the fields within it. Then, for each `(location, selectionSet)` pair, it calls `ExtractSelectionSet`, which will compute

1. the parts of the selection set that can be resolved by the service at that location
2. the steps needed (if any) to resolve the fields removed in the previous step

```
function CreateQueryPlanSteps(ctx, insertionPoint, parentType, selectionSet) {
    result = empty slice
    for location, selectionSet in RouteSelectionSet(ctx, parentType, selectionSet) {
        selectionForLocation, childrenSteps = ExtractSelectionSet(
            ctx,
            insertionPoint,
            parentType,
            selectionSet,
            location
        )
        step = QueryPlanStep {
            location,
            parentType,
            selectionForLocation,
            insertionPoint,
            childrenSteps
        }
        append step to result
    }
    return result :: []QueryPlanStep
}
```

#### `ExtractSelectionSet`

This function is where most of the complexity of query planning lies. It recursively traverses a selection set, filtering selections based on whether they match the given location or not, and creating steps for those fields which don't match the given location.

This function processes each selection of the selectionSet one by one.

If the selection is the `id` field of a boundary type, we know we can resolve it, since all boundary types have the `id` field.

If the field's location matches the given location, we know it will be part of the filtered result. Then,

- if the field is a leaf type, we add it to the result, the filtering is complete
- otherwise, we recursively call `ExtractSelectionSet` on that field to continue the filtering

If the field's location doesn't match the given location, we know we have to create new query plan steps to resolve that field.

Finally, if the parent type is a boundary type, we add the "id" field if necessary, as this is required to execute children steps.

```
function ExtractSelectionSet(ctx, insertionPoint, parentType, selectionSet, location) {
    result = (empty ast.SelectionSet, empty slice)
    for selection in selectionSet {
        switch on the type of selection {
        case selection is an ast.Field:
            let field = selection
            if field is the "id" field of a boundary type {
                append the field to the result's selectionSet
                continue
            }
            if location matches the location in ctx.Locations for parentType / field name {
                if the field is a leaf type {
                    append the field to the result's selectionSet
                    continue
                }
                selectionSetForLocation, childrenSteps = ExtractSelectionSet(
                    ctx,
                    insertionPoint = insertionPoint + field.Alias,
                    parentType = field.Definition.Type.Name(),
                    selectionSet = field.SelectionSet,
                    location
                )
                fieldForLocation = copy of field
                fieldForLocation.SelectionSet = selectionSetForLocation
                append fieldForLocation to the result's selectionSet
                append childrenSteps to the result's query plan steps
            } else {
                mergedWithExistingStep = false
                for step in the result steps {
                    if step.ServiceURL == location and step.InsertionPoint == insertionPoint {
                        append field to step.SelectionSet
                        mergedWithExistingStep = true
                        break
                    }
                }
                if !mergedWithExistingStep {
                    childrenSteps = createSteps(
                        ctx,
                        insertionPoint,
                        parentType,
                        selectionSet = new ast.SelectionSet containing field
                    )
                    append childrenSteps to the result's query plan steps
                }
            }
        case selection is an Inline Fragment:
            let inlineFragment = selection
            selectionSetForLocation, childrenSteps = ExtractSelectionSet(
                ctx,
                insertionPoint = insertionPoint,
                parentType = inlineFragment.TypeCondition,
                selectionSet = inlineFragment.SelectionSet,
                location
            )
            append selectionSetForLocation to the result's selectionSet
            append childrenSteps to the result's query plan steps
        case selection is an Fragment Spread:
            let fragmentSpread = selection
            selectionSetForLocation, childrenSteps = ExtractSelectionSet(
                ctx,
                insertionPoint = insertionPoint,
                parentType = fragmentSpread.Definition.TypeCondition,
                selectionSet = fragmentSpread.Definition.SelectionSet,
                location
            )
            append selectionSetForLocation to the result's selectionSet
            append childrenSteps to the result's query plan steps
        }
    }
    if parentType is a boundary type and the result selectionSet doesn't have an "id" field {
        add the "id" field to the result selectionSet, aliased to "_id"
    }
    return result :: (ast.SelectionSet, []QueryPlanStep)
}
```

#### `RouteSelectionSet`

This function "routes" the selection set, i.e. it partitions the selection set by the locations of the fields within it.

```
function RouteSelectionSet(ctx, parentType, selectionSet) {
    result = empty map
    for selection in selectionSet if not an implicit field {
        switch on the type of selection {
        case selection is an ast.Field:
            let field = selection
            location = lookup parentType / field name in ctx.Locations
            append field to result[location]
        case selection is an ast.InlineFragment:
            let inlineFragment = selection
            inner = routeSelectionSet(parentType, inlineFragment.SelectionSet)
            for location, selectionSet in inner {
                append copy of inlineFragment with SelectionSet = selectionSet to result[location]
            }
        case selection is an ast.FragmentSpread:
            let fragmentSpread = selection
            inner = routeSelectionSet(parentType, fragmentSpread.Definition.SelectionSet)
            for location, selectionSet in inner {
                for selection in selectionSet {
                    append selection to result[location]
                }
            }
        }
    }
    return result :: map[string]ast.SelectionSet
}

```

## Query Execution

The `Execute` function is straightforward, it simply iterates over each root step in the query plan, and executes them in turn. The implementation does this in parallel, but this is omitted in the pseudo-code for simplicity.

```
function Execute(ctx, queryPlan, resultPtr) {
    for step in queryPlan.RootSteps {
        ExecuteRootStep(ctx, step, resultPtr)
    }
}
```

The `ExecuteRootStep` function executes a single step of the query plan, along with its children steps, if any.

The operation document is simply composed of the operation type and the step's selection set.

Once the document is constructed, we invoke the remote GraphQL service with the document and store the response at the given result pointer.

Finally, for each child step in `step.Then`, we call `ExecuteChildStep`.

```
function ExecuteRootStep(ctx, step, resultPtr) {
    operationType = if step.ParentType == "Mutation" then "mutation" else "query"
    if id is the empty string {
        document = "${operationType} ${step.SelectionSet}"
    }
    execute document at URL step.ServiceURL and write response to resultPtr
    for childStep in step.Then {
        ExecuteChildStep(ctx, childStep, resultPtr)
    }
}
```

The `ExecuteChildStep` function execute a single child step, along with its
children steps, if any.

First we build the corresponding insertion slice. This is a slice containing
all the target elements for the operation (where we need to insert the data).
They are represented by the id of the element along with a pointer to a
structure that can receive JSON document. See `buildInsertionSlice` below.

Then we build the document: one boundary query per insertion target. To avoid
conflict we alias each query with an id.

Once we have the document is constructed we invoke the remote GraphQL service
and store the response into each corresponding target.

Finally recursively call `ExecuteChildStep` for each child step in
`step.Then`.

```
function ExecuteChildStep(ctx, step, resultPtr) {
    targets = buildInsertionSlice(step, resultPtr)
    queries = []
    for target in targets {
        query = """
            {
                ${id}: $boundaryQuery(id: ${target.Id}) {
                    ${step.SelectionSet}
                }
            }
        """
        append query to queries
    }
    document = "{ ${queries} }"
    execute document at URL step.ServiceURL and write response to resultPtr
    for childStep in step.Then {
        ExecuteChildStep(ctx, childStep, resultPtr)
    }
}
```

The `buildInsertionSlice` algorithm traverses the structure pointed by
`resultPtr`, along the path described by `insertionPoint`. It returns a slice
of pointers to JSON results along with the id of the element.
Those pointers indicate where data should be written by a step that has the
corresponding insertion point.

First, if the insertion point is empty, it means that we have reached the end of the path, and `resultPtr` points to the destination we were looking for. If this destination is a `map`, we return a singleton slice of that map. If this destination is a slice then we call `buildInsertionSlice` recursively on each element of that slice in order to ensure that the returned slice is not nested (`resultPtr` may be a list of lists, in which case the resulting slice must be flattened).

Finally, if the insertion point is not empty, we consider whether `resultPtr` is a map or a slice.
<br/>
If it's a map, we look up the insertion point's first item in that map and call `buildInsertionSlice` recursively on that value, passing a new insertion point to the recursive call that skips that first element.
<br/>
If `resultPtr` is a slice we perform the same operation as described above, i.e. we call `buildInsertionSlice` recursively on each element of that slice in order to ensure that the returned slice is not nested.

```
function buildInsertionSlice(insertionPoint, resultPtr) {
    if insertionPoint is empty {
        switch on the type of resultPtr {
        case resultPtr is a slice:
            newResultPtr = empty slice
            for element in resultPtr {
                for newElement in buildInsertionSlice(insertionPoint, element) {
                    append newElement to newResultPtr
                }
            }
            return newResultPtr
        case resultPtr is a map:
            id = resultPtr["id"] || resultPtr["_id"]
            return [ (id, resultPtr) ]
        }
    }

    switch on the type of resultPtr {
        case resultPtr is a slice:
            newResultPtr = empty slice
            for element in resultPtr {
                for newElement in buildInsertionSlice(insertionPoint, element) {
                    append newElement to newResultPtr
                }
            }
            return newResultPtr
        case resultPtr is a map:
            return buildInsertionSlice(insertionPoint[1:], resultPtr[insertionPoint[0]])
    }
}
```
