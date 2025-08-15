CURRENTLY BROKEN
There's a bug that stops this from working when internal packages are used by the main dir. I'll fix it when I get a chance.

# peep

A Go profiling tool that automatically instruments your code with CPU and memory profiling.

## Installation

```bash
go install github.com/cpcf/peep@latest
```

## Usage

```bash
peep [flags] <main.go>
```

### Flags

- `-cpu`: CPU profiling only
- `-mem`: Memory profiling only  
- `-cpu-out <file>`: CPU profile output file (default: cpu.prof)
- `-mem-out <file>`: Memory profile output file (default: mem.prof)
- `-dash`: Enable live web dashboard
- `-port <port>`: Dashboard port (default: 6060)

### Examples

```bash
# Profile both CPU and memory
peep main.go

# Memory profiling only
peep -mem main.go

# With live dashboard
peep -dash main.go

# Custom output files
peep -cpu-out mycpu.prof -mem-out mymem.prof main.go
```

## How it works

peep parses your Go source code and automatically injects profiling code into the main function, then runs the instrumented program. Profile files are generated during execution.

With `-dash`, a live dashboard runs at `http://localhost:6060` showing real-time metrics.
