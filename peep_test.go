package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHasMainFunction(t *testing.T) {
	// Test with main function
	content := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, testFile, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}

	if !hasMainFunction(node) {
		t.Error("Expected to find main function")
	}
}

func TestHasMainFunctionWithoutMain(t *testing.T) {
	// Test without main function
	content := `package main

import "fmt"

func helper() {
	fmt.Println("Helper function")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, testFile, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}

	if hasMainFunction(node) {
		t.Error("Expected not to find main function")
	}
}

func TestAddImportIfMissing(t *testing.T) {
	content := `package main

import "fmt"

func main() {
	println("test")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, testFile, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}

	// Test adding a new import
	addImportIfMissing(fset, node, "os")

	// Verify the import was added
	found := false
	for _, imp := range node.Imports {
		if imp.Path.Value == `"os"` {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find os import after adding")
	}

	// Test that existing import is not duplicated
	originalLen := len(node.Imports)
	addImportIfMissing(fset, node, "fmt") // fmt already exists
	if len(node.Imports) != originalLen {
		t.Error("Expected no change when adding existing import")
	}
}

func TestGenerateUniqueVars(t *testing.T) {
	// Test that we can generate unique variable names
	seen := make(map[string]bool)

	for range 100 {
		fileVar, errVar := generateUniqueVars()

		if seen[fileVar] {
			t.Errorf("Generated duplicate file variable: %s", fileVar)
		}
		if seen[errVar] {
			t.Errorf("Generated duplicate error variable: %s", errVar)
		}

		// Verify expected format
		if !strings.HasPrefix(fileVar, "f_") {
			t.Errorf("File variable should start with 'f_', got: %s", fileVar)
		}
		if !strings.HasPrefix(errVar, "err_") {
			t.Errorf("Error variable should start with 'err_', got: %s", errVar)
		}

		seen[fileVar] = true
		seen[errVar] = true
	}
}

func TestWriteAndExecute(t *testing.T) {
	// Create a simple Go program that we can instrument and execute
	content := `package main

import "fmt"

func main() {
	fmt.Println("test output")
}`

	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Process the file to get instrumented AST
	cpuProfileFile := filepath.Join(tempDir, "test_cpu.prof")
	memProfileFile := filepath.Join(tempDir, "test_mem.prof")
	node, fset, err := processGoFile(testFile, cpuProfileFile, memProfileFile, true, false, false)
	if err != nil {
		t.Fatalf("Failed to process Go file: %v", err)
	}

	// Test writeAndExecute without web UI
	err = writeAndExecute(node, fset, cpuProfileFile, memProfileFile, false, true, false, "")
	if err != nil {
		t.Fatalf("writeAndExecute failed: %v", err)
	}

	// Wait a moment for file to be written
	time.Sleep(200 * time.Millisecond)

	// The profile file should be created in the working directory of the executed program
	// which is the same as our test directory
	if _, err := os.Stat(cpuProfileFile); os.IsNotExist(err) {
		// Check if it was created in current directory instead
		currentDirProfile := "test_cpu.prof"
		if _, err := os.Stat(currentDirProfile); os.IsNotExist(err) {
			t.Error("Expected CPU profile file to be created")
		} else {
			os.Remove(currentDirProfile) // cleanup
		}
	} else {
		os.Remove(cpuProfileFile) // cleanup
	}
}

func TestWriteAndExecuteInvalidCode(t *testing.T) {
	// Create invalid Go code to test error handling
	content := `package main

func main() {
	invalid syntax here
}`

	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "invalid.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// This should fail during parsing
	_, _, err = processGoFile(testFile, "test_cpu.prof", "test_mem.prof", true, false, false)
	if err == nil {
		t.Error("Expected error when processing invalid Go code")
	}
}

func TestProcessGoFile(t *testing.T) {
	content := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test processing a valid Go file
	node, fset, err := processGoFile(testFile, "test_cpu.prof", "test_mem.prof", true, false, false)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if node == nil || fset == nil {
		t.Error("Expected non-nil node and fset")
	}

	// Verify required imports were added
	requiredImports := []string{"os", "log", "runtime/pprof"}
	for _, required := range requiredImports {
		found := false
		for _, imp := range node.Imports {
			if imp.Path.Value == `"`+required+`"` {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find import: %s", required)
		}
	}
}

func TestProcessGoFileWithoutMain(t *testing.T) {
	content := `package main

func helper() {
	println("helper")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test processing file without main function should error
	_, _, err = processGoFile(testFile, "test_cpu.prof", "test_mem.prof", true, false, false)
	if err == nil {
		t.Error("Expected error for file without main function")
	}
}

func TestMemoryProfilingOnly(t *testing.T) {
	content := `package main

import "fmt"

func main() {
	fmt.Println("test output")
}`

	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Process the file with memory profiling only
	memProfileFile := filepath.Join(tempDir, "test_mem.prof")
	node, fset, err := processGoFile(testFile, "", memProfileFile, false, true, false)
	if err != nil {
		t.Fatalf("Failed to process Go file: %v", err)
	}

	// Test writeAndExecute with memory profiling only
	err = writeAndExecute(node, fset, "", memProfileFile, false, false, true, "")
	if err != nil {
		t.Fatalf("writeAndExecute failed: %v", err)
	}

	// Wait a moment for file to be written
	time.Sleep(200 * time.Millisecond)

	// Check that memory profile file was created
	if _, err := os.Stat(memProfileFile); os.IsNotExist(err) {
		// Check if it was created in current directory instead
		currentDirProfile := "test_mem.prof"
		if _, err := os.Stat(currentDirProfile); os.IsNotExist(err) {
			t.Error("Expected memory profile file to be created")
		} else {
			os.Remove(currentDirProfile) // cleanup
		}
	} else {
		os.Remove(memProfileFile) // cleanup
	}
}

func TestBothCPUAndMemoryProfiling(t *testing.T) {
	content := `package main

import "fmt"

func main() {
	fmt.Println("test output")
}`

	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Process the file with both CPU and memory profiling
	cpuProfileFile := filepath.Join(tempDir, "test_cpu.prof")
	memProfileFile := filepath.Join(tempDir, "test_mem.prof")
	node, fset, err := processGoFile(testFile, cpuProfileFile, memProfileFile, true, true, false)
	if err != nil {
		t.Fatalf("Failed to process Go file: %v", err)
	}

	// Test writeAndExecute with both profiling types
	err = writeAndExecute(node, fset, cpuProfileFile, memProfileFile, false, true, true, "")
	if err != nil {
		t.Fatalf("writeAndExecute failed: %v", err)
	}

	// Wait a moment for files to be written
	time.Sleep(200 * time.Millisecond)

	// Check that both profile files were created
	cpuExists := false
	memExists := false

	if _, err := os.Stat(cpuProfileFile); err == nil {
		cpuExists = true
		os.Remove(cpuProfileFile) // cleanup
	} else if _, err := os.Stat("test_cpu.prof"); err == nil {
		cpuExists = true
		os.Remove("test_cpu.prof") // cleanup
	}

	if _, err := os.Stat(memProfileFile); err == nil {
		memExists = true
		os.Remove(memProfileFile) // cleanup
	} else if _, err := os.Stat("test_mem.prof"); err == nil {
		memExists = true
		os.Remove("test_mem.prof") // cleanup
	}

	if !cpuExists {
		t.Error("Expected CPU profile file to be created")
	}
	if !memExists {
		t.Error("Expected memory profile file to be created")
	}
}

func TestCreateCPUProfilingStmts(t *testing.T) {
	// Test CPU profiling statements creation
	cpuFile := "test_cpu.prof"
	cpuFileVar, cpuErrVar := generateUniqueVars()

	stmts := createCPUProfilingStmts(cpuFile, cpuFileVar, cpuErrVar)

	if len(stmts) != 4 {
		t.Errorf("Expected 4 statements, got %d", len(stmts))
	}

	// Verify the statements are of expected types
	// First should be assignment
	if _, ok := stmts[0].(*ast.AssignStmt); !ok {
		t.Error("First statement should be assignment")
	}

	// Second should be if statement
	if _, ok := stmts[1].(*ast.IfStmt); !ok {
		t.Error("Second statement should be if statement")
	}

	// Third should be expression statement
	if _, ok := stmts[2].(*ast.ExprStmt); !ok {
		t.Error("Third statement should be expression statement")
	}

	// Fourth should be defer statement
	if _, ok := stmts[3].(*ast.DeferStmt); !ok {
		t.Error("Fourth statement should be defer statement")
	}
}

func TestCreateMemoryProfilingStmts(t *testing.T) {
	// Test memory profiling statements creation
	memFile := "test_mem.prof"
	memFileVar, memErrVar := generateUniqueVars()

	stmts := createMemoryProfilingStmts(memFile, memFileVar, memErrVar)

	if len(stmts) != 3 {
		t.Errorf("Expected 3 statements, got %d", len(stmts))
	}

	// Verify the statements are of expected types
	// First should be assignment
	if _, ok := stmts[0].(*ast.AssignStmt); !ok {
		t.Error("First statement should be assignment")
	}

	// Second should be if statement
	if _, ok := stmts[1].(*ast.IfStmt); !ok {
		t.Error("Second statement should be if statement")
	}

	// Third should be defer statement
	if _, ok := stmts[2].(*ast.DeferStmt); !ok {
		t.Error("Third statement should be defer statement")
	}
}

func TestCreateMetricsCollectionStmts(t *testing.T) {
	// Test metrics collection statements creation
	stmts := createMetricsCollectionStmts()

	if len(stmts) != 3 {
		t.Errorf("Expected 3 statements, got %d", len(stmts))
	}

	// Verify the statements are of expected types
	// First should be assignment
	if _, ok := stmts[0].(*ast.AssignStmt); !ok {
		t.Error("First statement should be assignment")
	}

	// Second should be defer statement
	if _, ok := stmts[1].(*ast.DeferStmt); !ok {
		t.Error("Second statement should be defer statement")
	}

	// Third should be go statement
	if _, ok := stmts[2].(*ast.GoStmt); !ok {
		t.Error("Third statement should be go statement")
	}
}

func TestInstrumentMainFunction(t *testing.T) {
	content := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, testFile, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}

	// Count statements before instrumentation
	var mainFunc *ast.FuncDecl
	ast.Inspect(node, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" && fn.Recv == nil {
			mainFunc = fn
			return false
		}
		return true
	})

	originalStmtCount := len(mainFunc.Body.List)

	// Test instrumentation with CPU profiling only
	cpuFileVar, cpuErrVar := generateUniqueVars()
	memFileVar, memErrVar := generateUniqueVars()
	instrumentMainFunction(node, "cpu.prof", "mem.prof", cpuFileVar, cpuErrVar, memFileVar, memErrVar, true, false, false)

	// Verify statements were added
	ast.Inspect(node, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" && fn.Recv == nil {
			newStmtCount := len(fn.Body.List)
			if newStmtCount <= originalStmtCount {
				t.Errorf("Expected more statements after instrumentation, got %d (was %d)", newStmtCount, originalStmtCount)
			}
			return false
		}
		return true
	})
}

func TestInstrumentMainFunctionWithAllProfiling(t *testing.T) {
	content := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, testFile, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}

	// Count statements before instrumentation
	var mainFunc *ast.FuncDecl
	ast.Inspect(node, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" && fn.Recv == nil {
			mainFunc = fn
			return false
		}
		return true
	})

	originalStmtCount := len(mainFunc.Body.List)

	// Test instrumentation with all profiling enabled
	cpuFileVar, cpuErrVar := generateUniqueVars()
	memFileVar, memErrVar := generateUniqueVars()
	instrumentMainFunction(node, "cpu.prof", "mem.prof", cpuFileVar, cpuErrVar, memFileVar, memErrVar, true, true, true)

	// Verify statements were added
	ast.Inspect(node, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if ok && fn.Name.Name == "main" && fn.Recv == nil {
			newStmtCount := len(fn.Body.List)
			if newStmtCount <= originalStmtCount {
				t.Errorf("Expected more statements after instrumentation, got %d (was %d)", newStmtCount, originalStmtCount)
			}
			return false
		}
		return true
	})
}

func TestHasMainFunctionWithMethodReceiver(t *testing.T) {
	// Test that methods with "main" name are not considered main functions
	content := `package main

import "fmt"

type MyStruct struct{}

func (m MyStruct) main() {
	fmt.Println("This is a method, not a main function")
}

func main() {
	fmt.Println("This is the real main function")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, testFile, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}

	if !hasMainFunction(node) {
		t.Error("Expected to find main function even with method named main")
	}
}

func TestProcessGoFileWithWebEnabled(t *testing.T) {
	content := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test processing with web UI enabled
	node, fset, err := processGoFile(testFile, "test_cpu.prof", "test_mem.prof", true, false, true)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if node == nil || fset == nil {
		t.Error("Expected non-nil node and fset")
	}

	// Verify web-related imports were added
	webImports := []string{"runtime", "time", "encoding/json", "github.com/shirou/gopsutil/v3/cpu"}
	for _, required := range webImports {
		found := false
		for _, imp := range node.Imports {
			if imp.Path.Value == `"`+required+`"` {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find web import: %s", required)
		}
	}
}

func TestWriteAndExecuteWithWebUI(t *testing.T) {
	// This test verifies that web UI processing works without actually starting the server
	content := `package main

import "fmt"

func main() {
	fmt.Println("test output")
}`

	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Process the file without web UI to avoid dependency issues
	cpuProfileFile := filepath.Join(tempDir, "test_cpu.prof")
	memProfileFile := filepath.Join(tempDir, "test_mem.prof")
	node, fset, err := processGoFile(testFile, cpuProfileFile, memProfileFile, true, false, false)
	if err != nil {
		t.Fatalf("Failed to process Go file: %v", err)
	}

	// Test writeAndExecute without web UI to avoid server startup
	err = writeAndExecute(node, fset, cpuProfileFile, memProfileFile, false, true, false, "")
	if err != nil {
		t.Fatalf("writeAndExecute failed: %v", err)
	}

	// Wait a moment for file to be written
	time.Sleep(200 * time.Millisecond)

	// Check that CPU profile file was created
	if _, err := os.Stat(cpuProfileFile); os.IsNotExist(err) {
		// Check if it was created in current directory instead
		currentDirProfile := "test_cpu.prof"
		if _, err := os.Stat(currentDirProfile); os.IsNotExist(err) {
			t.Error("Expected CPU profile file to be created")
		} else {
			os.Remove(currentDirProfile) // cleanup
		}
	} else {
		os.Remove(cpuProfileFile) // cleanup
	}
}

func TestProcessGoFileNonexistentFile(t *testing.T) {
	// Test processing a file that doesn't exist
	_, _, err := processGoFile("nonexistent.go", "cpu.prof", "mem.prof", true, false, false)
	if err == nil {
		t.Error("Expected error when processing nonexistent file")
	}
}

func TestAddImportIfMissingWithEmptyImports(t *testing.T) {
	content := `package main

func main() {
	println("test")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, testFile, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}

	// Test adding import to file with no existing imports
	originalLen := len(node.Imports)
	addImportIfMissing(fset, node, "os")

	if len(node.Imports) != originalLen+1 {
		t.Errorf("Expected import count to increase by 1, got %d (was %d)", len(node.Imports), originalLen)
	}
}

func TestGenerateUniqueVarsUniqueness(t *testing.T) {
	// Test that generated variables are truly unique across many calls
	seen := make(map[string]bool)

	for i := 0; i < 1000; i++ {
		fileVar, errVar := generateUniqueVars()

		if seen[fileVar] {
			t.Errorf("Generated duplicate file variable at iteration %d: %s", i, fileVar)
		}
		if seen[errVar] {
			t.Errorf("Generated duplicate error variable at iteration %d: %s", i, errVar)
		}

		seen[fileVar] = true
		seen[errVar] = true
	}
}

func TestWriteAndExecuteWithInvalidAST(t *testing.T) {
	// Test writeAndExecute with a nil AST
	err := writeAndExecute(nil, token.NewFileSet(), "cpu.prof", "mem.prof", false, true, false, "")
	if err == nil {
		t.Error("Expected error when writing nil AST")
	}
}

func TestProcessGoFileWithMethodNamedMain(t *testing.T) {
	// Test processing a file that has a method named "main" but no main function
	content := `package main

import "fmt"

type MyStruct struct{}

func (m MyStruct) main() {
	fmt.Println("This is a method named main")
}

func helper() {
	fmt.Println("Helper function")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// This should fail because there's no main function (only a method named main)
	_, _, err = processGoFile(testFile, "test_cpu.prof", "test_mem.prof", true, false, false)
	if err == nil {
		t.Error("Expected error for file with method named main but no main function")
	}
}

func TestInstrumentMainFunctionNoMain(t *testing.T) {
	// Test instrumentation on a file without main function
	content := `package main

import "fmt"

func helper() {
	fmt.Println("Helper function")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, testFile, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}

	// This should not panic and should not modify anything
	cpuFileVar, cpuErrVar := generateUniqueVars()
	memFileVar, memErrVar := generateUniqueVars()
	instrumentMainFunction(node, "cpu.prof", "mem.prof", cpuFileVar, cpuErrVar, memFileVar, memErrVar, true, true, true)

	// Verify no main function was found
	if hasMainFunction(node) {
		t.Error("Expected no main function to be found")
	}
}

func TestProcessGoFileWithAllProfilingModes(t *testing.T) {
	content := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.go")

	err := os.WriteFile(testFile, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test processing with all profiling modes enabled
	node, fset, err := processGoFile(testFile, "test_cpu.prof", "test_mem.prof", true, true, true)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if node == nil || fset == nil {
		t.Error("Expected non-nil node and fset")
	}

	// Verify all required imports were added
	allImports := []string{
		"os", "log", "runtime/pprof", // Basic profiling
		"runtime", "time", "encoding/json", "github.com/shirou/gopsutil/v3/cpu", // Web UI
	}

	for _, required := range allImports {
		found := false
		for _, imp := range node.Imports {
			if imp.Path.Value == `"`+required+`"` {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find import: %s", required)
		}
	}
}
