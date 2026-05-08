package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"strings"
)

const header = `---
title: Operator flags
description: Generated reference for frp-operator binary flags and env vars.
---

# Operator flags

Generated from ` + "`pkg/operator/flags.go`" + `. Do not edit by hand — run ` + "`make site-gen`" + `.

| Flag | Type | Default | Env var | Description |
|------|------|---------|---------|-------------|
`

// flagEntry holds parsed info for one CLI flag.
type flagEntry struct {
	name        string // e.g. "leader-elect"
	typeName    string // e.g. "bool"
	defaultVal  string // e.g. `false` or `"info"`
	description string // raw description string
	fieldName   string // e.g. "LeaderElection" (from &cfg.FieldName)
	envVar      string // e.g. "LEADER_ELECT" (from applyEnv analysis)
}

// generate parses srcPath and writes MDX to out.
func generate(srcPath string, out io.Writer) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, srcPath, nil, 0)
	if err != nil {
		return fmt.Errorf("parse %s: %w", srcPath, err)
	}

	// Step 1: Walk applyEnv to build field → env-var map.
	fieldToEnv := extractEnvMappings(f)

	// Step 2: Walk LoadConfigFromArgs to collect flag entries.
	entries := extractFlags(f, fieldToEnv)

	// Step 3: Render output.
	if _, err := io.WriteString(out, header); err != nil {
		return err
	}
	for _, e := range entries {
		envCol := ""
		if e.envVar != "" {
			envCol = "`" + e.envVar + "`"
		}
		desc := strings.ReplaceAll(e.description, "|", `\|`)
		row := fmt.Sprintf("| `--%-s` | `%s` | `%s` | %s | %s |\n",
			e.name, e.typeName, e.defaultVal, envCol, desc)
		if _, err := io.WriteString(out, row); err != nil {
			return err
		}
	}
	return nil
}

// extractEnvMappings finds the applyEnv function and returns field→envVar.
// It handles two patterns:
//
//	if v := os.Getenv("KEY"); v != "" { cfg.Field = v }
//	if v, ok := someHelper("KEY"); ok { cfg.Field = v }
func extractEnvMappings(f *ast.File) map[string]string {
	m := map[string]string{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "applyEnv" || fn.Body == nil {
			continue
		}
		for _, stmt := range fn.Body.List {
			ifStmt, ok := stmt.(*ast.IfStmt)
			if !ok {
				continue
			}
			// Extract env key from the init statement of the if.
			envKey := envKeyFromInit(ifStmt.Init)
			if envKey == "" {
				continue
			}
			// Find cfg.Field assignments inside the if body.
			for _, bodyStmt := range ifStmt.Body.List {
				assign, ok := bodyStmt.(*ast.AssignStmt)
				if !ok {
					continue
				}
				for _, lhs := range assign.Lhs {
					field := cfgField(lhs)
					if field != "" {
						m[field] = envKey
					}
				}
			}
		}
	}
	return m
}

// envKeyFromInit extracts the env-var key string from an if-init statement.
// Handles: `v := os.Getenv("KEY")` and `v, ok := someHelper("KEY")`.
func envKeyFromInit(init ast.Stmt) string {
	if init == nil {
		return ""
	}
	assign, ok := init.(*ast.AssignStmt)
	if !ok {
		return ""
	}
	if len(assign.Rhs) != 1 {
		return ""
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok {
		return ""
	}
	if len(call.Args) != 1 {
		return ""
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	// Strip surrounding quotes.
	return strings.Trim(lit.Value, `"`)
}

// cfgField returns the field name if expr is cfg.FieldName, else "".
func cfgField(expr ast.Expr) string {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || ident.Name != "cfg" {
		return ""
	}
	return sel.Sel.Name
}

// typeForMethod maps fs.*Var method names to type strings.
var typeForMethod = map[string]string{
	"BoolVar":     "bool",
	"StringVar":   "string",
	"IntVar":      "int",
	"Float64Var":  "float64",
	"DurationVar": "duration",
}

// extractFlags finds LoadConfigFromArgs and collects all fs.*Var calls.
func extractFlags(f *ast.File, fieldToEnv map[string]string) []flagEntry {
	var entries []flagEntry
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "LoadConfigFromArgs" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok || ident.Name != "fs" {
				return true
			}
			typeName, ok := typeForMethod[sel.Sel.Name]
			if !ok {
				return true
			}
			// Expect at least 4 args: &cfg.Field, "flag-name", default, "description"
			if len(call.Args) < 4 {
				return true
			}
			// Arg 0: &cfg.Field → extract field name.
			fieldName := fieldFromRef(call.Args[0])
			// Arg 1: flag name string literal.
			flagName := stringLit(call.Args[1])
			if flagName == "" {
				return true
			}
			// Arg 2: default value.
			defVal := defaultVal(call.Args[2])
			// Arg 3: description string literal.
			desc := stringLit(call.Args[3])

			envVar := ""
			if fieldName != "" {
				envVar = fieldToEnv[fieldName]
			}

			entries = append(entries, flagEntry{
				name:        flagName,
				typeName:    typeName,
				defaultVal:  defVal,
				description: desc,
				fieldName:   fieldName,
				envVar:      envVar,
			})
			return true
		})
		break
	}
	return entries
}

// fieldFromRef extracts field name from &cfg.FieldName expression.
func fieldFromRef(expr ast.Expr) string {
	unary, ok := expr.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return ""
	}
	return cfgField(unary.X)
}

// stringLit returns the unquoted string value of a string literal node.
func stringLit(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	return strings.Trim(lit.Value, `"`)
}

// defaultVal renders a default value node as a string for the table.
// String literals keep their quotes; other literals and identifiers are plain.
func defaultVal(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.BasicLit:
		// Return the literal as-is (keeps surrounding quotes for strings).
		return v.Value
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		// e.g. cfg.SomeField — just emit the selector part.
		return cfgField(expr)
	case *ast.CallExpr:
		// e.g. cfg.BatchIdleDuration (already resolved) — not common for defaults.
		return ""
	}
	return ""
}

func main() {
	srcPath := "pkg/operator/flags.go"
	if len(os.Args) > 1 {
		srcPath = os.Args[1]
	}
	if err := generate(srcPath, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "gen-flags-doc: %v\n", err)
		os.Exit(1)
	}
}
