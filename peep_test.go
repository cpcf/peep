package main

import (
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
	err = writeAndExecute(node, fset, cpuProfileFile, memProfileFile, false, true, false)
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
	err = writeAndExecute(node, fset, "", memProfileFile, false, false, true)
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
	err = writeAndExecute(node, fset, cpuProfileFile, memProfileFile, false, true, true)
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
