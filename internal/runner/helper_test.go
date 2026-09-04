package runner

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

// The tests in this package need a program that reports exactly what it
// received. Using the test binary itself means the assertions go through a
// real shell, with real quoting, on whatever platform the tests run on —
// which is the only way to know the quoting rules are right.
//
// Invoked as: <test binary> -noodge-helper <mode> [args...]
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "-noodge-helper" {
		os.Exit(helper(os.Args[2], os.Args[3:]))
	}
	os.Exit(m.Run())
}

func helper(mode string, args []string) int {
	switch mode {
	case "echo":
		// One argument per line, so a value that was wrongly split into two
		// arguments is immediately visible.
		for _, a := range args {
			fmt.Println(a)
		}

	case "exit":
		if len(args) == 0 {
			return 1
		}
		code, err := strconv.Atoi(args[0])
		if err != nil {
			return 1
		}
		return code

	case "touch":
		if len(args) == 0 {
			return 1
		}
		if err := os.WriteFile(args[0], []byte("ran"), 0o644); err != nil {
			return 1
		}

	case "env":
		for _, a := range args {
			fmt.Printf("%s=%s\n", a, os.Getenv(a))
		}

	case "sleep":
		// Used to show that a group's entries really do overlap in time.
		if len(args) < 1 {
			return 1
		}
		ms, err := strconv.Atoi(args[0])
		if err != nil {
			return 1
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
		if len(args) > 1 {
			fmt.Println(args[1])
		}

	case "cwd":
		dir, err := os.Getwd()
		if err != nil {
			return 1
		}
		fmt.Println(dir)

	default:
		fmt.Fprintln(os.Stderr, "unknown helper mode:", mode)
		return 2
	}

	return 0
}

// helperPath is the test binary, for use as a step's program.
func helperPath(t *testing.T) string {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}
