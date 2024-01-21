package bramble

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
)

func indentPrefix(sb *strings.Builder, level int, suffix ...string) (int, error) {
	sb.WriteString("\n")

	var err error
	total, count := 0, 0
	for i := 0; i <= level; i++ {
		count, err = sb.WriteString("  ")
		total += count
		if err != nil {
			return total, err
		}
	}
	for _, str := range suffix {
		count, err = sb.WriteString(str)
		total += count
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func formatDocument(ctx context.Context, schema *ast.Schema, operationType string, selectionSet ast.SelectionSet) (string, map[string]interface{}) {
	operation, vars := formatOperation(ctx, selectionSet)
	return strings.ToLower(operationType) + " " + operation + formatSelectionSet(ctx, schema, selectionSet), vars
}

func formatOperation(ctx context.Context, selection ast.SelectionSet) (string, map[string]interface{}) {
	sb := strings.Builder{}

	if !graphql.HasOperationContext(ctx) {
		return "", nil
	}
	operationCtx := graphql.GetOperationContext(ctx)

	variables := selectionSetVariables(selection)
	variableNames := map[string]struct{}{}
	for _, s := range variables {
		variableNames[s] = struct{}{}
	}

	var arguments []string
	usedVariables := map[string]interface{}{}
	for _, variableDefinition := range operationCtx.Operation.VariableDefinitions {
		if _, exists := variableNames[variableDefinition.Variable]; !exists {
			continue
		}

		for varName, varValue := range operationCtx.Variables {
			if varName == variableDefinition.Variable {
				usedVariables[varName] = varValue
			}
		}

		argument := fmt.Sprintf("$%s: %s", variableDefinition.Variable, variableDefinition.Type.String())
		arguments = append(arguments, argument)
	}

	sb.WriteString(operationCtx.OperationName)
	if len(arguments) == 0 {
		return sb.String(), nil
	}

	sb.WriteString("(")
	sb.WriteString(strings.Join(arguments, ","))
	sb.WriteString(")")

	return sb.String(), usedVariables
}

func selectionSetVariables(selectionSet ast.SelectionSet) []string {
	var vars []string
	for _, s := range selectionSet {
		switch selection := s.(type) {
		case *ast.Field:
			vars = append(vars, directiveListVariables(selection.Directives)...)
			vars = append(vars, argumentListVariables(selection.Arguments)...)
			vars = append(vars, selectionSetVariables(selection.SelectionSet)...)
		case *ast.InlineFragment:
			vars = append(vars, directiveListVariables(selection.Directives)...)
			vars = append(vars, selectionSetVariables(selection.SelectionSet)...)
		case *ast.FragmentSpread:
			vars = append(vars, directiveListVariables(selection.Directives)...)
		}
	}

	return vars
}

func directiveListVariables(directives ast.DirectiveList) []string {
	var output []string
	for _, d := range directives {
		output = append(output, argumentListVariables(d.Arguments)...)
	}

	return output
}

func argumentListVariables(arguments ast.ArgumentList) []string {
	var output []string
	for _, a := range arguments {
		output = append(output, valueVariables(a.Value)...)
	}

	return output
}

func valueVariables(a *ast.Value) []string {
	var output []string
	switch a.Kind {
	case ast.Variable:
		output = append(output, a.Raw)
	default:
		for _, child := range a.Children {
			output = append(output, valueVariables(child.Value)...)
		}
	}

	return output
}

func formatSelectionSelectionSet(sb *strings.Builder, schema *ast.Schema, vars map[string]interface{}, level int, selectionSet ast.SelectionSet) {
	sb.WriteString(" {")
	formatSelection(sb, schema, vars, level+1, selectionSet)
	indentPrefix(sb, level, "}")
}

func formatSelection(sb *strings.Builder, schema *ast.Schema, vars map[string]interface{}, level int, selectionSet ast.SelectionSet) {
	for _, selection := range selectionSet {
		indentPrefix(sb, level)
		switch selection := selection.(type) {
		case *ast.Field:
			if selection.Alias != selection.Name {
				sb.WriteString(selection.Alias)
				sb.WriteString(": ")
				sb.WriteString(selection.Name)
			} else {
				sb.WriteString(selection.Alias)
			}
			formatArgumentList(sb, schema, vars, selection.Arguments)
			for _, d := range selection.Directives {
				sb.WriteString(" @")
				sb.WriteString(d.Name)
				formatArgumentList(sb, schema, vars, d.Arguments)
			}
			if len(selection.SelectionSet) > 0 {
				formatSelectionSelectionSet(sb, schema, vars, level, selection.SelectionSet)
			}
		case *ast.InlineFragment:
			fmt.Fprintf(sb, "... on %v", selection.TypeCondition)
			formatSelectionSelectionSet(sb, schema, vars, level, selection.SelectionSet)
		case *ast.FragmentSpread:
			sb.WriteString("...")
			sb.WriteString(selection.Name)
		}
	}
}

func formatArgumentList(sb *strings.Builder, schema *ast.Schema, vars map[string]interface{}, args ast.ArgumentList) {
	if len(args) > 0 {
		sb.WriteString("(")
		for i, arg := range args {
			if i != 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(sb, "%s: %s", arg.Name, formatArgument(schema, arg.Value, vars))
		}
		sb.WriteString(")")
	}
}

func formatSelectionSet(ctx context.Context, schema *ast.Schema, selection ast.SelectionSet) string {
	vars := map[string]interface{}{}
	if reqctx := graphql.GetOperationContext(ctx); reqctx != nil {
		vars = reqctx.Variables
	}

	sb := strings.Builder{}

	sb.WriteString("{")
	formatSelection(&sb, schema, vars, 0, selection)
	sb.WriteString("\n}")

	return sb.String()
}

var multipleSpacesRegex = regexp.MustCompile(`\s+`)

func formatSelectionSetSingleLine(ctx context.Context, schema *ast.Schema, selection ast.SelectionSet) string {
	return multipleSpacesRegex.ReplaceAllString(formatSelectionSet(ctx, schema, selection), " ")
}

func formatArgument(schema *ast.Schema, v *ast.Value, vars map[string]interface{}) string {
	if schema == nil {
		// this is to allow tests to pass to due the MarshalJSON comparator not having access
		// to the schema
		return v.String()
	}

	// this is a mix between v.String() and v.Raw(vars) as we need a string value with variables replaced

	if v == nil {
		return "<nil>"
	}
	switch v.Kind {
	case ast.Variable:
		return "$" + v.Raw
	case ast.IntValue, ast.FloatValue, ast.EnumValue, ast.BooleanValue, ast.NullValue:
		return v.Raw
	case ast.StringValue, ast.BlockValue:
		return strconv.Quote(v.Raw)
	case ast.ListValue:
		var val []string
		for _, elem := range v.Children {
			val = append(val, formatArgument(schema, elem.Value, vars))
		}
		return "[" + strings.Join(val, ",") + "]"
	case ast.ObjectValue:
		var val []string
		for _, elem := range v.Children {
			val = append(val, elem.Name+":"+formatArgument(schema, elem.Value, vars))
		}
		return "{" + strings.Join(val, ",") + "}"
	default:
		panic(fmt.Errorf("unknown value kind %d", v.Kind))
	}
}

func formatSchema(schema *ast.Schema) string {
	buf := bytes.NewBufferString("")
	f := formatter.NewFormatter(buf)
	f.FormatSchema(schema)
	return buf.String()
}
