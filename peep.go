package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/tools/go/ast/astutil"
)

func main() {
	var web bool
	var profileFile string
	flag.BoolVar(&web, "web", false, "Open pprof web UI after execution")
	flag.StringVar(&profileFile, "o", "cpu.prof", "Output profile file name")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Println("Usage: prof [--web] <main.go>")
		os.Exit(1)
	}

	sourceFile := flag.Arg(0)
	fset := token.NewFileSet()

	node, err := parser.ParseFile(fset, sourceFile, nil, parser.ParseComments)
	if err != nil {
		log.Fatalf("Failed to parse %s: %v", sourceFile, err)
	}

	// Inject imports if not present
	addImport := func(pkg string) {
		found := false
		for _, imp := range node.Imports {
			if imp.Path.Value == fmt.Sprintf("\"%s\"", pkg) {
				found = true
				break
			}
		}
		if !found {
			astutil.AddImport(fset, node, pkg)
		}
	}

	addImport("os")
	addImport("log")
	addImport("runtime/pprof")

	// Check if main function exists
	var hasMain bool
	ast.Inspect(node, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" && fn.Recv == nil {
			hasMain = true
		}
		return true
	})

	if !hasMain {
		log.Fatalf("No main function found in %s", sourceFile)
	}

	// Generate unique variable names
	var randBytes [4]byte
	rand.Read(randBytes[:])
	suffix := hex.EncodeToString(randBytes[:])
	fileVar := "f_" + suffix
	errVar := "err_" + suffix

	ast.Inspect(node, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" && fn.Recv == nil {
			stmts := []ast.Stmt{
				// f_<id>, err_<id> := os.Create("profile.prof")
				&ast.AssignStmt{
					Lhs: []ast.Expr{
						ast.NewIdent(fileVar),
						ast.NewIdent(errVar),
					},
					Tok: token.DEFINE,
					Rhs: []ast.Expr{
						&ast.CallExpr{
							Fun: &ast.SelectorExpr{
								X:   ast.NewIdent("os"),
								Sel: ast.NewIdent("Create"),
							},
							Args: []ast.Expr{
								&ast.BasicLit{
									Kind:  token.STRING,
									Value: fmt.Sprintf("\"%s\"", profileFile),
								},
							},
						},
					},
				},
				// if err_<id> != nil { log.Fatal(err_<id>) }
				&ast.IfStmt{
					Cond: &ast.BinaryExpr{
						X:  ast.NewIdent(errVar),
						Op: token.NEQ,
						Y:  ast.NewIdent("nil"),
					},
					Body: &ast.BlockStmt{
						List: []ast.Stmt{
							&ast.ExprStmt{
								X: &ast.CallExpr{
									Fun: &ast.SelectorExpr{
										X:   ast.NewIdent("log"),
										Sel: ast.NewIdent("Fatal"),
									},
									Args: []ast.Expr{ast.NewIdent(errVar)},
								},
							},
						},
					},
				},
				// pprof.StartCPUProfile(f_<id>)
				&ast.ExprStmt{
					X: &ast.CallExpr{
						Fun: &ast.SelectorExpr{
							X:   ast.NewIdent("pprof"),
							Sel: ast.NewIdent("StartCPUProfile"),
						},
						Args: []ast.Expr{ast.NewIdent(fileVar)},
					},
				},
				// defer pprof.StopCPUProfile()
				&ast.DeferStmt{
					Call: &ast.CallExpr{
						Fun: &ast.SelectorExpr{
							X:   ast.NewIdent("pprof"),
							Sel: ast.NewIdent("StopCPUProfile"),
						},
					},
				},
			}

			// Inject at beginning of main
			fn.Body.List = append(stmts, fn.Body.List...)
			return false
		}
		return true
	})

	// Write modified file to temp
	tempFile := filepath.Join(os.TempDir(), "main_prof.go")
	out, err := os.Create(tempFile)
	if err != nil {
		log.Fatalf("Failed to create temp file: %v", err)
	}
	defer out.Close()
	defer os.Remove(tempFile)

	if err := printer.Fprint(out, fset, node); err != nil {
		log.Fatalf("Failed to write modified code: %v", err)
	}

	// Run the instrumented file
	cmd := exec.Command("go", "run", tempFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()

	fmt.Println("[prof] Running instrumented program...")
	if err := cmd.Run(); err != nil {
		log.Fatalf("Execution failed: %v", err)
	}

	fmt.Printf("[prof] CPU profile saved to %s\n", profileFile)

	// Launch web UI if requested
	if web {
		fmt.Println("[prof] Launching pprof web UI at http://localhost:8080...")
		webCmd := exec.Command("go", "tool", "pprof", "-http=:8080", profileFile)
		webCmd.Stdout = os.Stdout
		webCmd.Stderr = os.Stderr
		if err := webCmd.Run(); err != nil {
			log.Fatalf("Failed to launch pprof web UI: %v", err)
		}
	}
}
