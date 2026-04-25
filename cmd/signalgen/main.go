package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"text/template"
	"unicode"

	"gopkg.in/yaml.v3"
)

// fileConfig is loaded from signalgen.yml in consuming projects.
type fileConfig struct {
	Input         string `yaml:"input"`
	Output        string `yaml:"output"`
	InputDir      string `yaml:"inputDir"`
	OutputFile    string `yaml:"outputFile"`
	SourcePackage string `yaml:"sourcePackage"`
}

func (c fileConfig) resolvedInput() string {
	if c.Input != "" {
		return c.Input
	}
	return c.InputDir
}

func (c fileConfig) resolvedOutput() string {
	if c.Output != "" {
		return c.Output
	}
	return c.OutputFile
}

// SignalDef represents a parsed signal definition extracted from a *Signals struct.
type SignalDef struct {
	Name     string
	RouteID  string
	Data     []FieldDef
	UI       []FieldDef
	Computed []FieldDef
	Ids      []FieldDef
}

// FieldDef represents a single field within a signal section.
type FieldDef struct {
	GoName         string
	JSONName       string
	GoType         string
	HasExplicitTag bool
}

// GenContext is the top-level context passed to the code template.
type GenContext struct {
	Package string
	Signals []SignalDef
}

func main() {
	configPath := flag.String("config", "signalgen.yml", "Path to config file")
	inputDirFlag := flag.String("input", "", "Directory containing signal definition .go files")
	outputFileFlag := flag.String("output", "", "Output file path")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fatalf("load config: %v", err)
	}

	inputDir := firstNonEmpty(*inputDirFlag, cfg.resolvedInput(), ".")
	outputFile := firstNonEmpty(*outputFileFlag, cfg.resolvedOutput(), filepath.Join(inputDir, "signals_gen.go"))

	fmt.Printf("signalgen: scanning %s\n", inputDir)

	files, err := listSourceFiles(inputDir)
	if err != nil {
		fatalf("read input directory: %v", err)
	}
	if len(files) == 0 {
		fmt.Println("  No Go source files found")
		return
	}

	fset := token.NewFileSet()
	const parseMode = parser.SkipObjectResolution

	var defs []SignalDef
	pkgNames := make(map[string]struct{})
	for _, filePath := range files {
		file, err := parser.ParseFile(fset, filePath, nil, parseMode)
		if err != nil {
			fatalf("parse error in %s: %v", filepath.Base(filePath), err)
		}

		pkgNames[file.Name.Name] = struct{}{}
		fileDefs := extractSignalDefs(file)
		if len(fileDefs) > 0 {
			fmt.Printf("  Found %d signal definition(s) in %s\n", len(fileDefs), filepath.Base(filePath))
		}
		defs = append(defs, fileDefs...)
	}

	pkgName, err := singlePackageName(pkgNames)
	if err != nil {
		fatalf("%v", err)
	}

	sort.Slice(defs, func(i, j int) bool {
		if defs[i].Name == defs[j].Name {
			return defs[i].RouteID < defs[j].RouteID
		}
		return defs[i].Name < defs[j].Name
	})

	if err := ensureUniqueDefNames(defs); err != nil {
		fatalf("%v", err)
	}

	if len(defs) == 0 {
		fmt.Println("  No signal definitions found (structs ending in 'Signals' with Data/UI/Ids sections)")
		return
	}

	hasErrors := false
	for _, def := range defs {
		if errs := validate(def); len(errs) > 0 {
			fmt.Fprintf(os.Stderr, "Validation errors in %sSignals:\n", def.Name)
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "  x %s\n", e)
			}
			hasErrors = true
		}
	}
	if hasErrors {
		os.Exit(1)
	}

	ctx := GenContext{
		Package: pkgName,
		Signals: defs,
	}

	var buf bytes.Buffer
	if err := genTmpl.Execute(&buf, ctx); err != nil {
		fatalf("template execution failed: %v", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		fatalf("gofmt failed: %v\n\nRaw output:\n%s", err, buf.String())
	}

	if err := os.WriteFile(outputFile, formatted, 0644); err != nil {
		fatalf("write failed: %v", err)
	}

	fmt.Printf("Generated %s (%d signal type(s))\n", outputFile, len(defs))
	for _, def := range defs {
		fmt.Printf("  - %s (route: %q, data: %d, ui: %d, computed: %d, ids: %d)\n",
			def.Name, def.RouteID, len(def.Data), len(def.UI), len(def.Computed), len(def.Ids))
	}
}

func loadConfig(path string) (fileConfig, error) {
	var cfg fileConfig

	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func fatalf(formatStr string, args ...any) {
	fmt.Fprintf(os.Stderr, "signalgen: "+formatStr+"\n", args...)
	os.Exit(1)
}

func listSourceFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !isSignalSourceFile(entry) {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}

	sort.Strings(files)
	return files, nil
}

func isSignalSourceFile(entry fs.DirEntry) bool {
	if entry.IsDir() {
		return false
	}

	name := entry.Name()
	if !strings.HasSuffix(name, ".go") {
		return false
	}
	if strings.HasSuffix(name, "_gen.go") || strings.HasSuffix(name, "_test.go") {
		return false
	}
	return true
}

func singlePackageName(pkgNames map[string]struct{}) (string, error) {
	if len(pkgNames) == 0 {
		return "", fmt.Errorf("no Go packages parsed")
	}

	names := make([]string, 0, len(pkgNames))
	for name := range pkgNames {
		names = append(names, name)
	}
	sort.Strings(names)

	if len(names) > 1 {
		return "", fmt.Errorf("multiple packages found in input: %s", strings.Join(names, ", "))
	}

	return names[0], nil
}

func ensureUniqueDefNames(defs []SignalDef) error {
	seen := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		if _, ok := seen[def.Name]; ok {
			return fmt.Errorf("duplicate signal definition %q; each *Signals type must be unique", def.Name+"Signals")
		}
		seen[def.Name] = struct{}{}
	}
	return nil
}

func extractSignalDefs(file *ast.File) []SignalDef {
	var defs []SignalDef

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			name := typeSpec.Name.Name
			if !strings.HasSuffix(name, "Signals") {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			baseName := strings.TrimSuffix(name, "Signals")
			if baseName == "" {
				continue
			}

			def := SignalDef{
				Name:    baseName,
				RouteID: toLowerFirst(baseName),
			}

			for _, field := range structType.Fields.List {
				if len(field.Names) == 0 {
					continue
				}
				sectionName := field.Names[0].Name
				innerStruct, ok := field.Type.(*ast.StructType)
				if !ok {
					continue
				}

				switch sectionName {
				case "Data":
					def.Data = extractFields(innerStruct, "Data")
				case "UI":
					def.UI = extractFields(innerStruct, "UI")
				case "Computed":
					def.Computed = extractFields(innerStruct, "Computed")
				case "Ids":
					def.Ids = extractFields(innerStruct, "Ids")
				}
			}

			if len(def.Data) == 0 && len(def.UI) == 0 && len(def.Computed) == 0 && len(def.Ids) == 0 {
				continue
			}

			defs = append(defs, def)
		}
	}

	return defs
}

func extractFields(st *ast.StructType, section string) []FieldDef {
	var fields []FieldDef

	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			continue
		}

		goName := f.Names[0].Name
		goType := typeToString(f.Type)

		var jsonName string
		var hasTag bool

		if f.Tag != nil {
			rawTag := strings.Trim(f.Tag.Value, "`")
			extracted, tagPresent := extractJSONTag(rawTag)
			hasTag = tagPresent
			jsonName = extracted
		}

		switch section {
		case "Data":
			if jsonName == "" && !hasTag {
				jsonName = toLowerFirst(goName)
			}
		case "UI":
			jsonName = toLowerFirst(goName)
		case "Computed":
			jsonName = toLowerFirst(goName)
		case "Ids":
			jsonName = ""
		}

		fields = append(fields, FieldDef{
			GoName:         goName,
			JSONName:       jsonName,
			GoType:         goType,
			HasExplicitTag: hasTag,
		})
	}

	return fields
}

func extractJSONTag(tag string) (string, bool) {
	jsonTag, ok := reflect.StructTag(tag).Lookup("json")
	if !ok {
		return "", false
	}
	if idx := strings.Index(jsonTag, ","); idx != -1 {
		jsonTag = jsonTag[:idx]
	}
	if jsonTag == "-" {
		return "", true
	}
	return jsonTag, true
}

func typeToString(exp ast.Expr) string {
	switch t := exp.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeToString(t.Elt)
		}
		return fmt.Sprintf("[%s]%s", t.Len, typeToString(t.Elt))
	case *ast.StarExpr:
		return "*" + typeToString(t.X)
	case *ast.MapType:
		return "map[" + typeToString(t.Key) + "]" + typeToString(t.Value)
	case *ast.SelectorExpr:
		return typeToString(t.X) + "." + t.Sel.Name
	default:
		return "any"
	}
}

func validate(def SignalDef) []string {
	var errs []string

	for _, f := range def.Data {
		if !f.HasExplicitTag {
			errs = append(errs, fmt.Sprintf("Data.%s is missing a json tag (required for signal name mapping)", f.GoName))
		}
		if f.HasExplicitTag && f.JSONName == "" {
			errs = append(errs, fmt.Sprintf("Data.%s has invalid json tag (must include a non-empty field name)", f.GoName))
		}
		if strings.HasPrefix(f.JSONName, "_") {
			errs = append(errs, fmt.Sprintf("Data.%s json tag %q must not start with '_' (use the UI section)", f.GoName, f.JSONName))
		}
	}

	for _, f := range def.UI {
		if f.HasExplicitTag {
			errs = append(errs, fmt.Sprintf("UI.%s must not have a json tag (auto-derived as %q; remove it)", f.GoName, f.JSONName))
		}
	}

	for _, f := range def.Computed {
		if f.HasExplicitTag {
			errs = append(errs, fmt.Sprintf("Computed.%s must not have a json tag (auto-derived as %q; remove it)", f.GoName, f.JSONName))
		}
	}

	for _, f := range def.Ids {
		if f.HasExplicitTag {
			errs = append(errs, fmt.Sprintf("Ids.%s must not have a json tag (Ids are not signals)", f.GoName))
		}
	}

	for _, section := range []struct {
		name   string
		fields []FieldDef
	}{
		{"Data", def.Data},
		{"UI", def.UI},
		{"Computed", def.Computed},
	} {
		for _, f := range section.fields {
			if _, ok := supportedSignalTypes[f.GoType]; !ok {
				errs = append(errs, fmt.Sprintf("%s.%s has unsupported type %q", section.name, f.GoName, f.GoType))
			}
		}
	}

	for _, f := range def.Ids {
		if f.GoType != "string" {
			errs = append(errs, fmt.Sprintf("Ids.%s must be of type string, got %q", f.GoName, f.GoType))
		}
	}

	seen := make(map[string]string, len(def.Data)+len(def.UI)+len(def.Computed))
	for _, f := range def.Data {
		path := def.RouteID + "." + f.JSONName
		if prev, ok := seen[path]; ok {
			errs = append(errs, fmt.Sprintf("duplicate signal path %q: Data.%s conflicts with %s", path, f.GoName, prev))
		}
		seen[path] = "Data." + f.GoName
	}
	for _, f := range def.UI {
		path := "_" + def.RouteID + "." + f.JSONName
		if prev, ok := seen[path]; ok {
			errs = append(errs, fmt.Sprintf("duplicate signal path %q: UI.%s conflicts with %s", path, f.GoName, prev))
		}
		seen[path] = "UI." + f.GoName
	}
	for _, f := range def.Computed {
		path := "_" + def.RouteID + f.GoName
		if prev, ok := seen[path]; ok {
			errs = append(errs, fmt.Sprintf("duplicate signal path %q: Computed.%s conflicts with %s", path, f.GoName, prev))
		}
		seen[path] = "Computed." + f.GoName
	}

	return errs
}

var supportedSignalTypes = map[string]struct{}{
	"string": {}, "bool": {},
	"int": {}, "int8": {}, "int16": {}, "int32": {}, "int64": {},
	"float32": {}, "float64": {},
	"[]string": {}, "[]int": {}, "[]bool": {},
}

func toLowerFirst(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func toKebab(s string) string {
	var result strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				if unicode.IsLower(prev) {
					result.WriteRune('-')
				} else if i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
					result.WriteRune('-')
				}
			}
			result.WriteRune(unicode.ToLower(r))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

var funcMap = template.FuncMap{
	"toLowerFirst": toLowerFirst,
	"toKebab":      toKebab,
	"hasData":      func(d SignalDef) bool { return len(d.Data) > 0 },
	"hasUI":        func(d SignalDef) bool { return len(d.UI) > 0 },
	"hasComputed":  func(d SignalDef) bool { return len(d.Computed) > 0 },
	"hasIds":       func(d SignalDef) bool { return len(d.Ids) > 0 },
	"bt":           func() string { return "`" },
}

var tmplStr = `// Code generated by signalgen; DO NOT EDIT.
//
// Source: github.com/imperial-hex/data-star-sig-gen/cmd/signalgen

package {{ .Package }}

import (
	"net/http"
	"strings"

	"github.com/starfederation/datastar-go/datastar"
)

// GetSigName strips the "$" prefix from a signal path for use in
// DataStar attributes such as data-bind and data-indicator.
func GetSigName(signalRef string) string {
	return strings.TrimPrefix(signalRef, "$")
}
{{ range .Signals }}{{ $s := . }}
// ============================================================
// {{ .Name }}Signals — runtime helpers & typed DSL
// ============================================================

// ReadDataSignals reads request payload and returns page signals with Data populated.
func Read{{ .Name }}DataSignals(r *http.Request) ({{ .Name }}ServerSignals, error) {
	var payload struct {
{{ if hasData . }}		Data struct {
{{- range .Data }}
			{{ .GoName }} {{ .GoType }} {{ bt }}json:"{{ .JSONName }}"{{ bt }}
{{- end }}
		} {{ bt }}json:"{{ .RouteID }}"{{ bt }}
{{ end }}	}
	if err := datastar.ReadSignals(r, &payload); err != nil {
		return {{ .Name }}ServerSignals{}, err
	}

	return {{ .Name }}ServerSignals{
		Data: {{ .Name }}DataValues{
{{- range .Data }}
			{{ .GoName }}: payload.Data.{{ .GoName }},
{{- end }}
		},
	}, nil
}

// PatchDataSignals patches only the Data namespace back to the client.
func Patch{{ .Name }}DataSignals(sse *datastar.ServerSentEventGenerator, data {{ .Name }}DataValues) ({{ .Name }}ServerSignals, error) {
	payload := map[string]any{
{{ if hasData . }}
		"{{ .RouteID }}": data,
{{ end }}
	}
	if err := sse.MarshalAndPatchSignals(payload); err != nil {
		return {{ .Name }}ServerSignals{}, err
	}

	return {{ .Name }}ServerSignals{
		Data: data,
	}, nil
}
{{ if hasData . }}

// {{ .Name }}DataValues holds typed Data defaults passed from server code.
type {{ .Name }}DataValues struct {
{{- range .Data }}
	{{ .GoName }} {{ .GoType }} {{ bt }}json:"{{ .JSONName }}"{{ bt }}
{{- end }}
}
{{ end }}{{ if hasUI . }}

// {{ .Name }}UIValues holds typed UI defaults used for initialization payload.
type {{ .Name }}UIValues struct {
{{- range .UI }}
	{{ .GoName }} {{ .GoType }} {{ bt }}json:"{{ .JSONName }}"{{ bt }}
{{- end }}
}
{{ end }}
{{ if or (hasData .) (hasUI .) }}

// {{ .Name }}Defaults groups typed Data and UI defaults for initialization.
type {{ .Name }}Defaults struct {
{{ if hasData . }}	Data {{ .Name }}DataValues
{{ end }}{{ if hasUI . }}	UI   {{ .Name }}UIValues
{{ end }}
}
{{ end }}
{{ if hasData . }}

// {{ .Name }}DataPaths holds typed signal path strings for Data fields.
type {{ .Name }}DataPaths struct {
{{- range .Data }}
	{{ .GoName }} string
{{- end }}
}
{{ end }}{{ if hasUI . }}

// {{ .Name }}UIPaths holds typed signal path strings for UI fields.
type {{ .Name }}UIPaths struct {
{{- range .UI }}
	{{ .GoName }} string
{{- end }}
}
{{ end }}{{ if hasComputed . }}

// {{ .Name }}ComputedPaths holds typed signal path strings for Computed fields.
type {{ .Name }}ComputedPaths struct {
{{- range .Computed }}
	{{ .GoName }} string
{{- end }}
}
{{ end }}{{ if hasIds . }}

// {{ .Name }}IdValues holds deterministic DOM element identifiers.
type {{ .Name }}IdValues struct {
{{- range .Ids }}
	{{ .GoName }} string
{{- end }}
}

{{ end }}

// {{ .Name }}ServerSignals is the server-side container passed to root components.
// It carries typed defaults and deterministic IDs; DSL and payload are derived from it.
type {{ .Name }}ServerSignals struct {
{{ if hasData . }}	Data {{ .Name }}DataValues
{{ end }}{{ if hasUI . }}	UI   {{ .Name }}UIValues
{{ end }}
}

{{ if hasIds . }}// IDs returns deterministic DOM IDs for {{ .Name }} page signals.
func (s {{ .Name }}ServerSignals) IDs() {{ .Name }}IdValues {
	return {{ .Name }}IdValues{
{{- range .Ids }}
		{{ .GoName }}: "{{ $s.RouteID }}-{{ .GoName | toKebab }}",
{{- end }}
	}
}
{{ end }}

// {{ .Name }}SignalsDSL is the typed template-side DSL object.
type {{ .Name }}SignalsDSL struct {
{{ if hasData . }}	Data {{ .Name }}DataPaths
{{ end }}{{ if hasUI . }}	UI   {{ .Name }}UIPaths
{{ end }}{{ if hasComputed . }}	Computed {{ .Name }}ComputedPaths
{{ end }}{{ if hasIds . }}	Ids  {{ .Name }}IdValues
{{ end }}
}

// Initialize{{ .Name }}Signals builds a server-side container for rendering.
// It accepts typed Data and UI defaults.
func Initialize{{ .Name }}Signals(defaults {{ .Name }}Defaults) {{ .Name }}ServerSignals {
	return {{ .Name }}ServerSignals{
{{ if hasData . }}		Data: defaults.Data,
{{ end }}{{ if hasUI . }}		UI: defaults.UI,
{{ end }}
	}
}

// ToInitSignalsPayload converts server signals into a data-signals payload.
func (s {{ .Name }}ServerSignals) ToInitSignalsPayload() map[string]any {
	payload := map[string]any{
{{ if hasData . }}		"{{ .RouteID }}": s.Data,
{{ end }}{{ if hasUI . }}		"_{{ .RouteID }}": s.UI,
{{ end }}	}
	return payload
}

// ToSignalsDSL converts server signal container into templ DSL paths.
func (s {{ .Name }}ServerSignals) ToSignalsDSL() {{ .Name }}SignalsDSL {
	return {{ .Name }}SignalsDSL{
{{ if hasData . }}		Data: {{ .Name }}DataPaths{
{{- range .Data }}
			{{ .GoName }}: "${{ $s.RouteID }}.{{ .JSONName }}",
{{- end }}
		},
{{ end }}{{ if hasUI . }}		UI: {{ .Name }}UIPaths{
{{- range .UI }}
			{{ .GoName }}: "$_{{ $s.RouteID }}.{{ .JSONName }}",
{{- end }}
		},
{{ end }}{{ if hasComputed . }}		Computed: {{ .Name }}ComputedPaths{
{{- range .Computed }}
			{{ .GoName }}: "$_{{ $s.RouteID }}{{ .GoName }}",
{{- end }}
		},
{{ end }}{{ if hasIds . }}		Ids: s.IDs(),
{{ end }}
	}
}
{{ end }}
`

var genTmpl = template.Must(template.New("gen").Funcs(funcMap).Parse(tmplStr))
