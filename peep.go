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
func instrumentMainFunction(node *ast.File, cpuFile, memFile, cpuFileVar, cpuErrVar, memFileVar, memErrVar string, enableCPU, enableMem bool) {
	ast.Inspect(node, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" && fn.Recv == nil {
			var stmts []ast.Stmt

			if enableCPU {
				// CPU profiling setup
				stmts = append(stmts,
					// cpuFile, cpuErr := os.Create("cpu.prof")
					&ast.AssignStmt{
						Lhs: []ast.Expr{
							ast.NewIdent(cpuFileVar),
							ast.NewIdent(cpuErrVar),
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
										Value: fmt.Sprintf("\"%s\"", cpuFile),
									},
								},
							},
						},
					},
					// if cpuErr != nil { log.Fatal(cpuErr) }
					&ast.IfStmt{
						Cond: &ast.BinaryExpr{
							X:  ast.NewIdent(cpuErrVar),
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
										Args: []ast.Expr{ast.NewIdent(cpuErrVar)},
									},
								},
							},
						},
					},
					// pprof.StartCPUProfile(cpuFile)
					&ast.ExprStmt{
						X: &ast.CallExpr{
							Fun: &ast.SelectorExpr{
								X:   ast.NewIdent("pprof"),
								Sel: ast.NewIdent("StartCPUProfile"),
							},
							Args: []ast.Expr{ast.NewIdent(cpuFileVar)},
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
				)
			}

			if enableMem {
				// Memory profiling setup
				stmts = append(stmts,
					// memFile, memErr := os.Create("mem.prof")
					&ast.AssignStmt{
						Lhs: []ast.Expr{
							ast.NewIdent(memFileVar),
							ast.NewIdent(memErrVar),
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
										Value: fmt.Sprintf("\"%s\"", memFile),
									},
								},
							},
						},
					},
					// if memErr != nil { log.Fatal(memErr) }
					&ast.IfStmt{
						Cond: &ast.BinaryExpr{
							X:  ast.NewIdent(memErrVar),
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
										Args: []ast.Expr{ast.NewIdent(memErrVar)},
									},
								},
							},
						},
					},
					// defer func() { pprof.WriteHeapProfile(memFile); memFile.Close() }()
					&ast.DeferStmt{
						Call: &ast.CallExpr{
							Fun: &ast.FuncLit{
								Type: &ast.FuncType{},
								Body: &ast.BlockStmt{
									List: []ast.Stmt{
										&ast.ExprStmt{
											X: &ast.CallExpr{
												Fun: &ast.SelectorExpr{
													X:   ast.NewIdent("pprof"),
													Sel: ast.NewIdent("WriteHeapProfile"),
												},
												Args: []ast.Expr{ast.NewIdent(memFileVar)},
											},
										},
										&ast.ExprStmt{
											X: &ast.CallExpr{
												Fun: &ast.SelectorExpr{
													X:   ast.NewIdent(memFileVar),
													Sel: ast.NewIdent("Close"),
												},
											},
										},
									},
								},
							},
						},
					},
				)
			}

			// Inject at beginning of main
			fn.Body.List = append(stmts, fn.Body.List...)
			return false
		}
		return true
	})
}

// processGoFile instruments a Go file with profiling code
func processGoFile(sourceFile, cpuFile, memFile string, enableCPU, enableMem bool) (*ast.File, *token.FileSet, error) {
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
	cpuFileVar, cpuErrVar := generateUniqueVars()
	memFileVar, memErrVar := generateUniqueVars()
	instrumentMainFunction(node, cpuFile, memFile, cpuFileVar, cpuErrVar, memFileVar, memErrVar, enableCPU, enableMem)

	return node, fset, nil
}

// writeAndExecute writes the instrumented AST to a temp file and executes it
func writeAndExecute(node *ast.File, fset *token.FileSet, cpuFile, memFile string, web bool, enableCPU, enableMem bool) error {
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

	if enableCPU && enableMem {
		fmt.Println("[prof] Running instrumented program with CPU and memory profiling...")
	} else if enableMem {
		fmt.Println("[prof] Running instrumented program with memory profiling...")
	} else {
		fmt.Println("[prof] Running instrumented program with CPU profiling...")
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("execution failed: %w", err)
	}

	if enableCPU && enableMem {
		fmt.Printf("[prof] CPU profile saved to %s\n", cpuFile)
		fmt.Printf("[prof] Memory profile saved to %s\n", memFile)
	} else if enableMem {
		fmt.Printf("[prof] Memory profile saved to %s\n", memFile)
	} else {
		fmt.Printf("[prof] CPU profile saved to %s\n", cpuFile)
	}

	// Launch web UI if requested
	if web {
		var profileFileForWeb string
		if enableCPU {
			profileFileForWeb = cpuFile
		} else {
			profileFileForWeb = memFile
		}
		fmt.Printf("[prof] Launching pprof web UI at http://localhost:8080 for %s...\n", profileFileForWeb)
		webCmd := exec.Command("go", "tool", "pprof", "-http=:8080", profileFileForWeb)
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
	var cpuOutFile string
	var memOutFile string
	var memOnly bool
	var cpuOnly bool
	flag.BoolVar(&web, "web", false, "Open pprof web UI after execution")
	flag.StringVar(&cpuOutFile, "cpu-out", "", "Output file for CPU profile")
	flag.StringVar(&memOutFile, "mem-out", "", "Output file for memory profile")
	flag.BoolVar(&memOnly, "mem", false, "Enable memory profiling (use alone for memory-only)")
	flag.BoolVar(&cpuOnly, "cpu", false, "Enable CPU profiling (use alone for CPU-only)")
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Println("Usage: peep [--web] [--mem] [--cpu] [--cpu-out file] [--mem-out file] <main.go>")
		os.Exit(1)
	}

	// Determine profiling modes
	enableCPU := cpuOnly || (!memOnly && !cpuOnly)
	enableMem := memOnly || (!memOnly && !cpuOnly)

	sourceFile := flag.Arg(0)

	// Set default profile names if not specified
	if cpuOutFile == "" && (enableCPU || (!memOnly && !cpuOnly)) {
		cpuOutFile = "cpu.prof"
	}
	if memOutFile == "" && (enableMem || (!memOnly && !cpuOnly)) {
		memOutFile = "mem.prof"
	}

	// Process the Go file
	node, fset, err := processGoFile(sourceFile, cpuOutFile, memOutFile, enableCPU, enableMem)
	if err != nil {
		log.Fatal(err)
	}

	// Write and execute the instrumented file
	if err := writeAndExecute(node, fset, cpuOutFile, memOutFile, web, enableCPU, enableMem); err != nil {
		log.Fatal(err)
	}
}
