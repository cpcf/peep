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

// generateUniqueVars creates unique variable names to avoid conflicts
func generateUniqueVars() (string, string) {
	var randBytes [4]byte
	rand.Read(randBytes[:])
	suffix := hex.EncodeToString(randBytes[:])
	return "f_" + suffix, "err_" + suffix
}

// hasMainFunction checks if the AST contains a main function
func hasMainFunction(node *ast.File) bool {
	var found bool
	ast.Inspect(node, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" && fn.Recv == nil {
			found = true
			return false
		}
		return true
	})
	return found
}

// addImportIfMissing adds an import to the AST if it's not already present
func addImportIfMissing(fset *token.FileSet, node *ast.File, pkg string) {
	for _, imp := range node.Imports {
		if imp.Path.Value == fmt.Sprintf("\"%s\"", pkg) {
			return
		}
	}
	astutil.AddImport(fset, node, pkg)
}

// instrumentMainFunction injects profiling code into the main function
func instrumentMainFunction(node *ast.File, profileFile, fileVar, errVar string) {
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
}

// processGoFile instruments a Go file with profiling code
func processGoFile(sourceFile, profileFile string) (*ast.File, *token.FileSet, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, sourceFile, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse %s: %w", sourceFile, err)
	}

	if !hasMainFunction(node) {
		return nil, nil, fmt.Errorf("no main function found in %s", sourceFile)
	}

	// Add required imports
	addImportIfMissing(fset, node, "os")
	addImportIfMissing(fset, node, "log")
	addImportIfMissing(fset, node, "runtime/pprof")

	// Generate unique variable names and instrument
	fileVar, errVar := generateUniqueVars()
	instrumentMainFunction(node, profileFile, fileVar, errVar)

	return node, fset, nil
}

// writeAndExecute writes the instrumented AST to a temp file and executes it
func writeAndExecute(node *ast.File, fset *token.FileSet, profileFile string, web bool) error {
	// Write modified file to temp
	tempFile := filepath.Join(os.TempDir(), "main_prof.go")
	out, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()
	defer os.Remove(tempFile)

	if err := printer.Fprint(out, fset, node); err != nil {
		return fmt.Errorf("failed to write modified code: %w", err)
	}

	// Run the instrumented file
	cmd := exec.Command("go", "run", tempFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()

	fmt.Println("[prof] Running instrumented program...")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("execution failed: %w", err)
	}

	fmt.Printf("[prof] CPU profile saved to %s\n", profileFile)

	// Launch web UI if requested
	if web {
		fmt.Println("[prof] Launching pprof web UI at http://localhost:8080...")
		webCmd := exec.Command("go", "tool", "pprof", "-http=:8080", profileFile)
		webCmd.Stdout = os.Stdout
		webCmd.Stderr = os.Stderr
		if err := webCmd.Run(); err != nil {
			return fmt.Errorf("failed to launch pprof web UI: %w", err)
		}
	}
	return nil
}

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

	// Process the Go file
	node, fset, err := processGoFile(sourceFile, profileFile)
	if err != nil {
		log.Fatal(err)
	}

	// Write and execute the instrumented file
	if err := writeAndExecute(node, fset, profileFile, web); err != nil {
		log.Fatal(err)
	}
}
