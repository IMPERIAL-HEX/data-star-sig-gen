package main

import (
	"bytes"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignalGen_GeneratesExpectedAPIsAndPaths(t *testing.T) {
	tempDir := t.TempDir()
	signalsPath := filepath.Join(tempDir, "signals.go")

	src := `package pages

type LoginSignals struct {
	Data struct {
		Email string ` + "`json:\"email\"`" + `
	}
	UI struct {
		Loading bool
	}
	Computed struct {
		IsValid bool
	}
	Ids struct {
		Form string
	}
}
`

	if err := os.WriteFile(signalsPath, []byte(src), 0644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	files, err := listSourceFiles(tempDir)
	if err != nil {
		t.Fatalf("list source files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 source file, got %d", len(files))
	}

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, files[0], nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}

	defs := extractSignalDefs(parsed)
	if len(defs) != 1 {
		t.Fatalf("expected 1 signal definition, got %d", len(defs))
	}

	if errs := validate(defs[0]); len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}

	ctx := GenContext{Package: parsed.Name.Name, Signals: defs}

	var buf bytes.Buffer
	if err := genTmpl.Execute(&buf, ctx); err != nil {
		t.Fatalf("execute template: %v", err)
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		t.Fatalf("gofmt output: %v", err)
	}

	out := string(formatted)
	mustContain(t, out, "func ReadLoginDataSignals(r *http.Request) (LoginServerSignals, error)")
	mustContain(t, out, "func PatchLoginDataSignals(sse *datastar.ServerSentEventGenerator, data LoginDataValues) (LoginServerSignals, error)")
	mustContain(t, out, "func InitializeLoginSignals(defaults LoginDefaults) LoginServerSignals")
	mustContain(t, out, "Email: \"$login.email\",")
	mustContain(t, out, "Loading: \"$_login.loading\",")
	mustContain(t, out, "IsValid: \"$_loginIsValid\",")
	mustContain(t, out, "Form: \"login-form\",")
}

func TestSignalGen_ValidationRejectsMissingDataJSONTag(t *testing.T) {
	def := SignalDef{
		Name:    "Profile",
		RouteID: "profile",
		Data: []FieldDef{
			{GoName: "Email", GoType: "string", JSONName: "", HasExplicitTag: false},
		},
	}

	errs := validate(def)
	if len(errs) == 0 {
		t.Fatal("expected validation errors, got none")
	}

	joined := strings.Join(errs, "\n")
	if !strings.Contains(joined, "Data.Email is missing a json tag") {
		t.Fatalf("expected missing json tag error, got: %s", joined)
	}
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected generated output to contain %q", want)
	}
}
