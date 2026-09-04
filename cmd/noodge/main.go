// Command noodge runs the documented commands a project declares in its
// noodge.yaml.
package main

import (
	"os"

	"github.com/wimhaanstra/noodge/internal/cli"
)

func main() {
	env := &cli.Env{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
	}
	os.Exit(cli.Execute(env, os.Args[1:]))
}
