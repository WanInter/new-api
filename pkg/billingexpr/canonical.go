package billingexpr

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// CanonicalBillingField describes a stable, provider-produced billing value.
// Paths must be rooted at billing (for example billing.duration_seconds).
// EnumValues are represented as strings so this contract can be sent directly
// to administration UIs without losing integer precision or string enums.
type CanonicalBillingField struct {
	Path       string
	Type       string
	Required   bool
	EnumValues []string
}

// ValidateCanonicalBillingSchema verifies field declarations before they are
// exposed to administrators or used to validate a provider-produced input.
func ValidateCanonicalBillingSchema(fields []CanonicalBillingField) error {
	_, err := canonicalFieldMap(fields)
	return err
}

// ValidateCanonicalBillingInput verifies that body contains only a canonical
// billing object and conforms to the declared provider contract. It is used at
// relay time, after a task adaptor has resolved aliases and upstream defaults.
func ValidateCanonicalBillingInput(body []byte, fields []CanonicalBillingField) error {
	fieldMap, err := canonicalFieldMap(fields)
	if err != nil {
		return err
	}

	var root map[string]any
	if err := common.Unmarshal(body, &root); err != nil {
		return fmt.Errorf("decode canonical billing input: %w", err)
	}
	if len(root) != 1 {
		return fmt.Errorf("canonical billing input may contain only the billing object")
	}
	billingValue, ok := root["billing"]
	if !ok {
		return fmt.Errorf("canonical billing input has no billing object")
	}
	billing, ok := billingValue.(map[string]any)
	if !ok {
		return fmt.Errorf("canonical billing input has invalid billing object")
	}

	for key := range billing {
		path := "billing." + key
		if _, ok := fieldMap[path]; !ok {
			return fmt.Errorf("canonical billing input contains undeclared field %q", path)
		}
	}
	for path, field := range fieldMap {
		key := strings.TrimPrefix(path, "billing.")
		value, exists := billing[key]
		if !exists || value == nil {
			if field.Required {
				return fmt.Errorf("canonical billing input is missing required field %q", path)
			}
			continue
		}
		if err := validateCanonicalFieldValue(path, field, value); err != nil {
			return err
		}
	}
	return nil
}

// ValidateCanonicalBillingExpression ensures an expression can only inspect
// declared canonical fields. It intentionally permits ordinary token variables
// (p, c, len, ...) because they are independent of request-body semantics.
func ValidateCanonicalBillingExpression(exprStr string, fields []CanonicalBillingField) error {
	_, err := canonicalExpressionParamPaths(exprStr, fields)
	return err
}

// ValidateCanonicalBillingExpressionMatrix additionally executes an expression
// for every declared enum combination. This catches a missing branch before an
// administrator enables model-level dynamic billing.
func ValidateCanonicalBillingExpressionMatrix(exprStr string, fields []CanonicalBillingField) error {
	usedPaths, err := canonicalExpressionParamPaths(exprStr, fields)
	if err != nil {
		return err
	}

	fieldMap, err := canonicalFieldMap(fields)
	if err != nil {
		return err
	}
	used := make(map[string]struct{}, len(usedPaths))
	for _, path := range usedPaths {
		used[path] = struct{}{}
	}

	matrixFields := make([]CanonicalBillingField, 0, len(fields))
	for _, field := range fields {
		_, referenced := used[field.Path]
		if referenced && len(field.EnumValues) == 0 {
			return fmt.Errorf("canonical billing field %q is referenced by the expression but has no enumerable values", field.Path)
		}
		if field.Required || referenced {
			if len(field.EnumValues) == 0 {
				return fmt.Errorf("required canonical billing field %q has no enumerable values", field.Path)
			}
			matrixFields = append(matrixFields, field)
		}
	}

	for _, input := range buildCanonicalBillingMatrix(matrixFields) {
		body, err := common.Marshal(map[string]any{"billing": input})
		if err != nil {
			return fmt.Errorf("marshal canonical billing matrix: %w", err)
		}
		if err := ValidateCanonicalBillingInput(body, fields); err != nil {
			return err
		}
		cost, _, err := RunExprWithRequest(exprStr, TokenParams{}, RequestInput{Body: body})
		if err != nil {
			return fmt.Errorf("canonical billing matrix evaluation failed: %w", err)
		}
		if math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 {
			return fmt.Errorf("canonical billing matrix produced invalid cost %v", cost)
		}
	}

	// Keep fieldMap in this function so schema validation remains coupled to
	// expression validation even when no field is referenced by the expression.
	if len(fieldMap) == 0 {
		return fmt.Errorf("canonical billing schema has no fields")
	}
	return nil
}

func canonicalExpressionParamPaths(exprStr string, fields []CanonicalBillingField) ([]string, error) {
	fieldMap, err := canonicalFieldMap(fields)
	if err != nil {
		return nil, err
	}
	_, body := ParseExprVersion(strings.TrimSpace(exprStr))
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("billing expression is empty")
	}
	tree, err := parser.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse billing expression: %w", err)
	}

	visitor := canonicalExpressionVisitor{
		allowedFields: fieldMap,
		paths:         make(map[string]struct{}),
	}
	ast.Walk(&tree.Node, &visitor)
	if visitor.err != nil {
		return nil, visitor.err
	}
	paths := make([]string, 0, len(visitor.paths))
	for path := range visitor.paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

type canonicalExpressionVisitor struct {
	allowedFields map[string]CanonicalBillingField
	paths         map[string]struct{}
	err           error
}

func (v *canonicalExpressionVisitor) Visit(node *ast.Node) {
	if v.err != nil {
		return
	}
	call, ok := (*node).(*ast.CallNode)
	if !ok {
		return
	}
	callee, ok := call.Callee.(*ast.IdentifierNode)
	if !ok {
		return
	}
	switch callee.Value {
	case "header":
		v.err = fmt.Errorf("canonical billing expressions may not use header()")
	case "param":
		if len(call.Arguments) != 1 {
			v.err = fmt.Errorf("param() must receive exactly one literal canonical field path")
			return
		}
		pathNode, ok := call.Arguments[0].(*ast.StringNode)
		if !ok {
			v.err = fmt.Errorf("param() path must be a string literal")
			return
		}
		path := strings.TrimSpace(pathNode.Value)
		if !strings.HasPrefix(path, "billing.") {
			v.err = fmt.Errorf("param(%q) is not a canonical billing field", path)
			return
		}
		if _, ok := v.allowedFields[path]; !ok {
			v.err = fmt.Errorf("param(%q) is not declared by the canonical billing schema", path)
			return
		}
		v.paths[path] = struct{}{}
	}
}

func canonicalFieldMap(fields []CanonicalBillingField) (map[string]CanonicalBillingField, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("canonical billing schema has no fields")
	}
	result := make(map[string]CanonicalBillingField, len(fields))
	for _, field := range fields {
		field.Path = strings.TrimSpace(field.Path)
		field.Type = strings.ToLower(strings.TrimSpace(field.Type))
		if !strings.HasPrefix(field.Path, "billing.") || strings.TrimPrefix(field.Path, "billing.") == "" || strings.Contains(strings.TrimPrefix(field.Path, "billing."), ".") {
			return nil, fmt.Errorf("invalid canonical billing field path %q", field.Path)
		}
		switch field.Type {
		case "number", "string", "boolean":
		default:
			return nil, fmt.Errorf("canonical billing field %q has unsupported type %q", field.Path, field.Type)
		}
		if _, exists := result[field.Path]; exists {
			return nil, fmt.Errorf("canonical billing schema declares %q more than once", field.Path)
		}
		result[field.Path] = field
	}
	return result, nil
}

func validateCanonicalFieldValue(path string, field CanonicalBillingField, value any) error {
	normalized, nonZero, err := canonicalFieldValue(field.Type, value)
	if err != nil {
		return fmt.Errorf("canonical billing field %q: %w", path, err)
	}
	if field.Required && !nonZero {
		return fmt.Errorf("canonical billing field %q must not be empty or zero", path)
	}
	if len(field.EnumValues) == 0 {
		return nil
	}
	for _, allowed := range field.EnumValues {
		allowedValue, err := canonicalEnumFieldValue(field.Type, allowed)
		if err == nil && normalized == allowedValue {
			return nil
		}
	}
	return fmt.Errorf("canonical billing field %q has unsupported value %q", path, normalized)
}

// canonicalEnumFieldValue normalizes schema values, which are transported as
// strings for UI compatibility. Request values still pass through
// canonicalFieldValue and must match the field's declared JSON type.
func canonicalEnumFieldValue(fieldType string, value string) (string, error) {
	value = strings.TrimSpace(value)
	switch fieldType {
	case "string":
		if value == "" {
			return "", fmt.Errorf("must not be empty")
		}
		return value, nil
	case "boolean":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return "", err
		}
		return strconv.FormatBool(parsed), nil
	case "number":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return "", fmt.Errorf("must be a finite number")
		}
		return strconv.FormatFloat(parsed, 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("has unsupported type %q", fieldType)
	}
}

func canonicalFieldValue(fieldType string, value any) (normalized string, nonZero bool, err error) {
	switch fieldType {
	case "string":
		text, ok := value.(string)
		if !ok {
			return "", false, fmt.Errorf("must be a string")
		}
		text = strings.TrimSpace(text)
		return text, text != "", nil
	case "boolean":
		boolean, ok := value.(bool)
		if !ok {
			return "", false, fmt.Errorf("must be a boolean")
		}
		return strconv.FormatBool(boolean), true, nil
	case "number":
		floatValue, ok := canonicalNumber(value)
		if !ok || math.IsNaN(floatValue) || math.IsInf(floatValue, 0) {
			return "", false, fmt.Errorf("must be a finite number")
		}
		return strconv.FormatFloat(floatValue, 'f', -1, 64), floatValue != 0, nil
	default:
		return "", false, fmt.Errorf("has unsupported type %q", fieldType)
	}
}

func canonicalNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int8:
		return float64(number), true
	case int16:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint8:
		return float64(number), true
	case uint16:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	default:
		return 0, false
	}
}

func buildCanonicalBillingMatrix(fields []CanonicalBillingField) []map[string]any {
	if len(fields) == 0 {
		return []map[string]any{{}}
	}
	result := []map[string]any{{}}
	for _, field := range fields {
		key := strings.TrimPrefix(field.Path, "billing.")
		values := make([]any, 0, len(field.EnumValues)+1)
		if !field.Required {
			values = append(values, nil)
		}
		for _, value := range field.EnumValues {
			values = append(values, canonicalMatrixValue(field.Type, value))
		}
		next := make([]map[string]any, 0, len(result)*len(values))
		for _, current := range result {
			for _, value := range values {
				copyValue := make(map[string]any, len(current)+1)
				for existingKey, existingValue := range current {
					copyValue[existingKey] = existingValue
				}
				if value != nil {
					copyValue[key] = value
				}
				next = append(next, copyValue)
			}
		}
		result = next
	}
	return result
}

func canonicalMatrixValue(fieldType, value string) any {
	switch fieldType {
	case "number":
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err == nil {
			return parsed
		}
	case "boolean":
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err == nil {
			return parsed
		}
	}
	return strings.TrimSpace(value)
}
