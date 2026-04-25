package expr

import (
	"fmt"
	"sort"
	"strings"
)

// Set builds a JS assignment expression.
func Set(signalPath string, jsValue string) string {
	return signalPath + " = " + jsValue
}

// SetArray builds a JS array assignment.
func SetArray(signalPath string, items []string) string {
	return signalPath + " = [" + strings.Join(items, ", ") + "]"
}

// Toggle builds a JS boolean toggle expression.
func Toggle(signalPath string) string {
	return signalPath + " = !" + signalPath
}

// Seq joins multiple JS statements with "; " (DataStar's statement separator).
func Seq(parts ...string) string {
	return strings.Join(parts, "; ")
}

// Not negates a JS expression.
func Not(exp string) string {
	return "!" + exp
}

// Eq builds a strict equality check.
func Eq(a, b string) string {
	return a + " === " + b
}

// NotEq builds a strict inequality check.
func NotEq(a, b string) string {
	return a + " !== " + b
}

// Ternary builds a JS ternary expression.
func Ternary(condition, trueVal, falseVal string) string {
	return condition + " ? " + trueVal + " : " + falseVal
}

// When executes action(s) only if condition is true (no-op otherwise).
func When(condition string, actions ...string) string {
	body := strings.Join(actions, ", ")
	return condition + " ? (" + body + ") : void 0"
}

// Branch pairs a condition with a body for use in Match.
type Branch struct {
	Cond string
	Then string
}

// Match builds a chained ternary (like if/else-if/else).
func Match(branches []Branch, fallback string) string {
	if len(branches) == 0 {
		return fallback
	}
	if fallback == "" {
		fallback = "void 0"
	}

	var b strings.Builder
	for _, br := range branches {
		b.WriteString(br.Cond)
		b.WriteString(" ? (")
		b.WriteString(br.Then)
		b.WriteString(") : ")
	}
	b.WriteString(fallback)
	return b.String()
}

// IfBranch pairs a condition with multiple statements for use in IfChain.
type IfBranch struct {
	Cond string
	Body []string
}

// IfChain builds a full if / else-if / else chain.
func IfChain(branches []IfBranch, fallback ...string) string {
	if len(branches) == 0 {
		if len(fallback) > 0 {
			return strings.Join(fallback, "; ")
		}
		return ""
	}

	var b strings.Builder
	for i, br := range branches {
		if i > 0 {
			b.WriteString(" else ")
		}
		b.WriteString("if (")
		b.WriteString(br.Cond)
		b.WriteString(") { ")
		b.WriteString(strings.Join(br.Body, "; "))
		b.WriteString(" }")
	}
	if len(fallback) > 0 {
		b.WriteString(" else { ")
		b.WriteString(strings.Join(fallback, "; "))
		b.WriteString(" }")
	}
	return b.String()
}

// GetComputeExp builds a data-computed object entry: {"name": () => expression}.
func GetComputeExp(signalName string, expression string) string {
	signalName = strings.TrimPrefix(signalName, "$")
	return "{\"" + signalName + "\": () => " + expression + "}"
}

// EvaluateExp appends raw JS to a signal reference.
func EvaluateExp(signalRef string, expression string) string {
	return signalRef + expression
}

// DataClass builds a JS object literal for the data-class attribute.
func DataClass(classConditions map[string]string) string {
	if len(classConditions) == 0 {
		return "{}"
	}

	keys := make([]string, 0, len(classConditions))
	for k := range classConditions {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, len(keys))
	for i, className := range keys {
		escaped := strings.ReplaceAll(className, "'", "\\'")
		parts[i] = fmt.Sprintf("'%s': %s", escaped, classConditions[className])
	}

	return "{" + strings.Join(parts, ", ") + "}"
}
