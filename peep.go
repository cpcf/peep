package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"time"

	_ "net/http/pprof"

	"golang.org/x/tools/go/ast/astutil"
)

// Metrics holds both CPU and memory usage
type Metrics struct {
	Alloc       uint64  `json:"alloc"`
	TotalAlloc  uint64  `json:"totalAlloc"`
	Sys         uint64  `json:"sys"`
	NumGC       uint32  `json:"numGC"`
	PauseTotal  uint64  `json:"pauseTotal"`
	CPUPercent  float64 `json:"cpuPercent"` // total system CPU percent (0-100 * cores)
	TimestampMS int64   `json:"timestampMs"`
}

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

// createCPUProfilingStmts creates AST statements for CPU profiling setup
func createCPUProfilingStmts(cpuFile, cpuFileVar, cpuErrVar string) []ast.Stmt {
	return []ast.Stmt{
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
	}
}

// createMemoryProfilingStmts creates AST statements for memory profiling setup
func createMemoryProfilingStmts(memFile, memFileVar, memErrVar string) []ast.Stmt {
	return []ast.Stmt{
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
	}
}

// createMetricsCollectionStmts creates AST statements for metrics collection
func createMetricsCollectionStmts() []ast.Stmt {
	return []ast.Stmt{
		// metricsFile := "peep_metrics.json"
		&ast.AssignStmt{
			Lhs: []ast.Expr{ast.NewIdent("metricsFile")},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{
				&ast.BasicLit{
					Kind:  token.STRING,
					Value: `"peep_metrics.json"`,
				},
			},
		},
		// defer os.Remove(metricsFile)
		&ast.DeferStmt{
			Call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   ast.NewIdent("os"),
					Sel: ast.NewIdent("Remove"),
				},
				Args: []ast.Expr{ast.NewIdent("metricsFile")},
			},
		},
		// go func() { ... }()
		&ast.GoStmt{
			Call: &ast.CallExpr{
				Fun: &ast.FuncLit{
					Type: &ast.FuncType{},
					Body: &ast.BlockStmt{
						List: []ast.Stmt{
							// ticker := time.NewTicker(500 * time.Millisecond)
							&ast.AssignStmt{
								Lhs: []ast.Expr{ast.NewIdent("ticker")},
								Tok: token.DEFINE,
								Rhs: []ast.Expr{
									&ast.CallExpr{
										Fun: &ast.SelectorExpr{
											X:   ast.NewIdent("time"),
											Sel: ast.NewIdent("NewTicker"),
										},
										Args: []ast.Expr{
											&ast.BinaryExpr{
												X: &ast.BasicLit{
													Kind:  token.INT,
													Value: "500",
												},
												Op: token.MUL,
												Y: &ast.SelectorExpr{
													X:   ast.NewIdent("time"),
													Sel: ast.NewIdent("Millisecond"),
												},
											},
										},
									},
								},
							},
							// defer ticker.Stop()
							&ast.DeferStmt{
								Call: &ast.CallExpr{
									Fun: &ast.SelectorExpr{
										X:   ast.NewIdent("ticker"),
										Sel: ast.NewIdent("Stop"),
									},
								},
							},
							// for range ticker.C { ... }
							&ast.RangeStmt{
								Key:   ast.NewIdent("_"),
								Value: nil,
								Tok:   token.ASSIGN,
								X: &ast.SelectorExpr{
									X:   ast.NewIdent("ticker"),
									Sel: ast.NewIdent("C"),
								},
								Body: &ast.BlockStmt{
									List: []ast.Stmt{
										// var m runtime.MemStats
										&ast.DeclStmt{
											Decl: &ast.GenDecl{
												Tok: token.VAR,
												Specs: []ast.Spec{
													&ast.ValueSpec{
														Names: []*ast.Ident{ast.NewIdent("m")},
														Type: &ast.SelectorExpr{
															X:   ast.NewIdent("runtime"),
															Sel: ast.NewIdent("MemStats"),
														},
													},
												},
											},
										},
										// runtime.ReadMemStats(&m)
										&ast.ExprStmt{
											X: &ast.CallExpr{
												Fun: &ast.SelectorExpr{
													X:   ast.NewIdent("runtime"),
													Sel: ast.NewIdent("ReadMemStats"),
												},
												Args: []ast.Expr{
													&ast.UnaryExpr{
														Op: token.AND,
														X:  ast.NewIdent("m"),
													},
												},
											},
										},
										// cpuPct, _ := cpu.Percent(0, false)
										&ast.AssignStmt{
											Lhs: []ast.Expr{ast.NewIdent("cpuPct"), ast.NewIdent("_")},
											Tok: token.DEFINE,
											Rhs: []ast.Expr{
												&ast.CallExpr{
													Fun: &ast.SelectorExpr{
														X:   ast.NewIdent("cpu"),
														Sel: ast.NewIdent("Percent"),
													},
													Args: []ast.Expr{
														&ast.BasicLit{Kind: token.INT, Value: "0"},
														ast.NewIdent("false"),
													},
												},
											},
										},
										// var cpuVal float64
										&ast.DeclStmt{
											Decl: &ast.GenDecl{
												Tok: token.VAR,
												Specs: []ast.Spec{
													&ast.ValueSpec{
														Names: []*ast.Ident{ast.NewIdent("cpuVal")},
														Type:  ast.NewIdent("float64"),
													},
												},
											},
										},
										// if len(cpuPct) > 0 { cpuVal = cpuPct[0] }
										&ast.IfStmt{
											Cond: &ast.BinaryExpr{
												X: &ast.CallExpr{
													Fun:  ast.NewIdent("len"),
													Args: []ast.Expr{ast.NewIdent("cpuPct")},
												},
												Op: token.GTR,
												Y:  &ast.BasicLit{Kind: token.INT, Value: "0"},
											},
											Body: &ast.BlockStmt{
												List: []ast.Stmt{
													&ast.AssignStmt{
														Lhs: []ast.Expr{ast.NewIdent("cpuVal")},
														Tok: token.ASSIGN,
														Rhs: []ast.Expr{
															&ast.IndexExpr{
																X:     ast.NewIdent("cpuPct"),
																Index: &ast.BasicLit{Kind: token.INT, Value: "0"},
															},
														},
													},
												},
											},
										},
										// metrics := map[string]interface{}{ ... }
										&ast.AssignStmt{
											Lhs: []ast.Expr{ast.NewIdent("metrics")},
											Tok: token.DEFINE,
											Rhs: []ast.Expr{
												&ast.CompositeLit{
													Type: &ast.MapType{
														Key: ast.NewIdent("string"),
														Value: &ast.InterfaceType{
															Methods: &ast.FieldList{},
														},
													},
													Elts: []ast.Expr{
														&ast.KeyValueExpr{
															Key:   &ast.BasicLit{Kind: token.STRING, Value: `"alloc"`},
															Value: &ast.SelectorExpr{X: ast.NewIdent("m"), Sel: ast.NewIdent("Alloc")},
														},
														&ast.KeyValueExpr{
															Key:   &ast.BasicLit{Kind: token.STRING, Value: `"totalAlloc"`},
															Value: &ast.SelectorExpr{X: ast.NewIdent("m"), Sel: ast.NewIdent("TotalAlloc")},
														},
														&ast.KeyValueExpr{
															Key:   &ast.BasicLit{Kind: token.STRING, Value: `"sys"`},
															Value: &ast.SelectorExpr{X: ast.NewIdent("m"), Sel: ast.NewIdent("Sys")},
														},
														&ast.KeyValueExpr{
															Key:   &ast.BasicLit{Kind: token.STRING, Value: `"numGC"`},
															Value: &ast.SelectorExpr{X: ast.NewIdent("m"), Sel: ast.NewIdent("NumGC")},
														},
														&ast.KeyValueExpr{
															Key:   &ast.BasicLit{Kind: token.STRING, Value: `"pauseTotal"`},
															Value: &ast.SelectorExpr{X: ast.NewIdent("m"), Sel: ast.NewIdent("PauseTotalNs")},
														},
														&ast.KeyValueExpr{
															Key:   &ast.BasicLit{Kind: token.STRING, Value: `"cpuPercent"`},
															Value: ast.NewIdent("cpuVal"),
														},
														&ast.KeyValueExpr{
															Key: &ast.BasicLit{Kind: token.STRING, Value: `"timestampMs"`},
															Value: &ast.CallExpr{
																Fun: &ast.SelectorExpr{
																	X: &ast.CallExpr{
																		Fun: &ast.SelectorExpr{
																			X:   ast.NewIdent("time"),
																			Sel: ast.NewIdent("Now"),
																		},
																	},
																	Sel: ast.NewIdent("UnixMilli"),
																},
															},
														},
													},
												},
											},
										},
										// data, _ := json.Marshal(metrics)
										&ast.AssignStmt{
											Lhs: []ast.Expr{ast.NewIdent("data"), ast.NewIdent("_")},
											Tok: token.DEFINE,
											Rhs: []ast.Expr{
												&ast.CallExpr{
													Fun: &ast.SelectorExpr{
														X:   ast.NewIdent("json"),
														Sel: ast.NewIdent("Marshal"),
													},
													Args: []ast.Expr{ast.NewIdent("metrics")},
												},
											},
										},
										// os.WriteFile(metricsFile, data, 0644)
										&ast.ExprStmt{
											X: &ast.CallExpr{
												Fun: &ast.SelectorExpr{
													X:   ast.NewIdent("os"),
													Sel: ast.NewIdent("WriteFile"),
												},
												Args: []ast.Expr{
													ast.NewIdent("metricsFile"),
													ast.NewIdent("data"),
													&ast.BasicLit{Kind: token.INT, Value: "0644"},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// instrumentMainFunction injects profiling code into the main function
func instrumentMainFunction(node *ast.File, cpuFile, memFile, cpuFileVar, cpuErrVar, memFileVar, memErrVar string, enableCPU, enableMem, enableWeb bool) {
	ast.Inspect(node, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" && fn.Recv == nil {
			var stmts []ast.Stmt

			if enableCPU {
				// CPU profiling setup
				stmts = append(stmts, createCPUProfilingStmts(cpuFile, cpuFileVar, cpuErrVar)...)
			}

			if enableMem {
				// Memory profiling setup
				stmts = append(stmts, createMemoryProfilingStmts(memFile, memFileVar, memErrVar)...)
			}

			if enableWeb {
				// Metrics collection for dashboard
				stmts = append(stmts, createMetricsCollectionStmts()...)
			}

			// Inject at beginning of main
			fn.Body.List = append(stmts, fn.Body.List...)
			return false
		}
		return true
	})
}

// processGoFile instruments a Go file with profiling code
func processGoFile(sourceFile, cpuFile, memFile string, enableCPU, enableMem, enableWeb bool) (*ast.File, *token.FileSet, error) {
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

	if enableWeb {
		addImportIfMissing(fset, node, "runtime")
		addImportIfMissing(fset, node, "time")
		addImportIfMissing(fset, node, "encoding/json")
		addImportIfMissing(fset, node, "github.com/shirou/gopsutil/v3/cpu")
	}

	// Generate unique variable names and instrument
	cpuFileVar, cpuErrVar := generateUniqueVars()
	memFileVar, memErrVar := generateUniqueVars()
	instrumentMainFunction(node, cpuFile, memFile, cpuFileVar, cpuErrVar, memFileVar, memErrVar, enableCPU, enableMem, enableWeb)

	return node, fset, nil
}

// startDashboardServer starts the live dashboard server
func startDashboardServer(ctx context.Context) {
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		// Read metrics from the file written by target process
		data, err := os.ReadFile("peep_metrics.json")
		if err != nil {
			// If file doesn't exist yet, return empty metrics
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{}"))
			return
		}

		// Parse the JSON to check timestamp
		var metrics map[string]any
		if err := json.Unmarshal(data, &metrics); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{}"))
			return
		}

		// Check if data is stale (older than 2 seconds)
		if timestampMs, ok := metrics["timestampMs"]; ok {
			if ts, ok := timestampMs.(float64); ok {
				now := time.Now().UnixMilli()
				if now-int64(ts) > 2000 {
					// Data is stale, return empty metrics
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte("{}"))
					return
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})

	// Serve static dashboard from ./static
	http.Handle("/", http.FileServer(http.Dir("./static")))

	addr := ":6060"
	server := &http.Server{Addr: addr}

	go func() {
		log.Printf("[prof] Live dashboard server listening on %s (pprof at %s/debug/pprof/)\n", addr, addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("[prof] Shutting down dashboard server")
	ctxShutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctxShutdown)
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

	// Start live dashboard if requested (before running the program)
	var dashboardCtx context.Context
	var dashboardStop context.CancelFunc
	if web {
		fmt.Println("[prof] Starting live dashboard server...")
		dashboardCtx, dashboardStop = signal.NotifyContext(context.Background(), os.Interrupt)
		defer dashboardStop()

		go func() {
			startDashboardServer(dashboardCtx)
		}()

		// Give the dashboard time to start
		time.Sleep(1 * time.Second)
		fmt.Println("[prof] Dashboard available at http://localhost:6060")
		fmt.Println("[prof] pprof available at http://localhost:6060/debug/pprof/")
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

	// Keep dashboard running after program completion if requested
	if web {
		fmt.Println("[prof] Program completed. Dashboard still running at http://localhost:6060")
		fmt.Println("[prof] Press Ctrl+C to stop the dashboard server")
		<-dashboardCtx.Done()
		fmt.Println("[prof] Dashboard server stopped")
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
	node, fset, err := processGoFile(sourceFile, cpuOutFile, memOutFile, enableCPU, enableMem, web)
	if err != nil {
		log.Fatal(err)
	}

	// Write and execute the instrumented file
	if err := writeAndExecute(node, fset, cpuOutFile, memOutFile, web, enableCPU, enableMem); err != nil {
		log.Fatal(err)
	}
}
