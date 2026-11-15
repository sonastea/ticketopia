package appmcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// argumentSchemas holds each registered tool's input schema, so calls with
// unknown, missing or mistyped arguments are refused in the API's
// "field: message" wording before the SDK's own schema validation, whose
// messages describe the schema rather than what to send.
type argumentSchemas map[string]*jsonschema.Schema

// addTool registers a tool and records its input schema for checkArguments.
// The schema is inferred as the SDK infers it; the SDK still validates
// everything this check lets through.
func addTool[In, Out any](server *mcp.Server, d Deps, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	if d.arguments != nil {
		schema, err := jsonschema.ForType(reflect.TypeFor[In](), &jsonschema.ForOptions{})
		if err != nil {
			panic(err)
		}
		d.arguments[tool.Name] = schema
	}
	mcp.AddTool(server, tool, handler)
}

// checkArguments refuses a tools/call whose arguments don't fit the tool,
// naming every problem field, joined by "; " in name order. Calls to unknown
// tools and well-formed calls pass through unchanged.
func checkArguments(schemas argumentSchemas) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if !ok || call.Params == nil {
				return next(ctx, method, req)
			}
			schema, ok := schemas[call.Params.Name]
			if !ok {
				return next(ctx, method, req)
			}
			if problem := argumentProblem(call.Params.Name, schema, call.Params.Arguments); problem != "" {
				var result mcp.CallToolResult
				result.SetError(errors.New(problem))
				return &result, nil
			}
			return next(ctx, method, req)
		}
	}
}

func argumentProblem(tool string, schema *jsonschema.Schema, raw json.RawMessage) string {
	args := map[string]json.RawMessage{}
	if trimmed := strings.TrimSpace(string(raw)); trimmed != "" && trimmed != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "arguments: send the arguments as a JSON object of named values"
		}
	}
	problems := map[string]string{}
	var unknown []string
	for name, value := range args {
		property, ok := schema.Properties[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		if expected := schemaTypes(property); len(expected) > 0 && !slices.Contains(expected, jsonType(value)) {
			problems[name] = typeHint(name, property, expected)
		} else if problem := itemProblem(property, value); problem != "" {
			problems[name] = problem
		}
	}
	// Unknown arguments share one entry, so the tool's arguments are listed once.
	if len(unknown) > 0 {
		slices.Sort(unknown)
		problems[strings.Join(unknown, ", ")] = unknownArgument(tool, schema, len(unknown))
	}
	for _, name := range schema.Required {
		if _, ok := args[name]; !ok {
			message := "required"
			if property := schema.Properties[name]; property != nil && property.Description != "" {
				message += " (" + property.Description + ")"
			}
			problems[name] = message
		}
	}
	names := make([]string, 0, len(problems))
	for name := range problems {
		names = append(names, name)
	}
	slices.Sort(names)
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = name + ": " + problems[name]
	}
	return strings.Join(parts, "; ")
}

// itemProblem words a list whose items have the wrong type, such as numbers
// in a list of IDs.
func itemProblem(property *jsonschema.Schema, raw json.RawMessage) string {
	if property.Items == nil {
		return ""
	}
	expected := schemaTypes(property.Items)
	if len(expected) == 0 {
		return ""
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	for _, item := range items {
		if !slices.Contains(expected, jsonType(item)) {
			message := "use a list of " + plural(expected)
			if property.Description != "" {
				message += " (" + property.Description + ")"
			}
			return message
		}
	}
	return ""
}

func plural(expected []string) string {
	words := map[string]string{"string": "strings", "integer": "integers", "number": "numbers",
		"boolean": "true or false values", "array": "lists", "object": "objects"}
	var kinds []string
	for _, kind := range expected {
		if word, ok := words[kind]; ok {
			kinds = append(kinds, word)
		}
	}
	return strings.Join(kinds, " or ")
}

func unknownArgument(tool string, schema *jsonschema.Schema, count int) string {
	if len(schema.Properties) == 0 {
		return tool + " takes no arguments"
	}
	noun := "not an argument"
	if count > 1 {
		noun = "not arguments"
	}
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	slices.Sort(names)
	return noun + " of " + tool + "; use " + strings.Join(names, ", ")
}

// typeHint words a mistyped argument as the API words the same field. Every
// tool's limit is 1 to 100, as on /api/v1.
func typeHint(name string, property *jsonschema.Schema, expected []string) string {
	if name == "limit" {
		return "use an integer from 1 to 100"
	}
	words := map[string]string{"string": "a string", "integer": "an integer", "number": "a number",
		"boolean": "true or false", "array": "a list", "object": "an object"}
	var kinds []string
	for _, kind := range expected {
		if word, ok := words[kind]; ok {
			kinds = append(kinds, word)
		}
	}
	message := "use " + strings.Join(kinds, " or ")
	if property.Description != "" {
		message += " (" + property.Description + ")"
	}
	return message
}

// schemaTypes lists the JSON types a property accepts; integer values also
// satisfy number. Null is left out of hints but accepted when allowed.
func schemaTypes(s *jsonschema.Schema) []string {
	types := s.Types
	if s.Type != "" {
		types = []string{s.Type}
	}
	out := slices.Clone(types)
	if slices.Contains(types, "number") {
		out = append(out, "integer")
	}
	slices.SortStableFunc(out, func(a, b string) int {
		// Keep null last so hints name the useful type first.
		return boolInt(a == "null") - boolInt(b == "null")
	})
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func jsonType(raw json.RawMessage) string {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return ""
	}
	switch v := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case json.Number:
		if _, err := v.Int64(); err == nil {
			return "integer"
		}
		if f, err := v.Float64(); err == nil && f == float64(int64(f)) {
			return "integer"
		}
		return "number"
	}
	return ""
}
