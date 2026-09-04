// Command gen-schema writes the JSON Schema for noodge.yaml.
//
// It runs at development time rather than inside noodge itself, because
// invopop's AddGoComments reads the Go source to turn doc comments into schema
// descriptions, and a shipped binary has no source to read. The generated file
// is committed and embedded; CI regenerates it and fails if it has drifted.
//
// Usage: go run ./tools/gen-schema
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/wimhaanstra/noodge/internal/config"
)

const (
	modulePath = "github.com/wimhaanstra/noodge"
	schemaID   = "https://wimhaanstra.github.io/noodge/schema/v1/noodge.schema.json"
	outPath    = "internal/schema/noodge.schema.json"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen-schema:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := reflectSchema()
	if err != nil {
		return err
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outPath, out, 0o644)
}

func reflectSchema() (*jsonschema.Schema, error) {
	r, err := newReflector()
	if err != nil {
		return nil, err
	}
	r.ExpandedStruct = true

	root := r.Reflect(&config.Config{})
	root.ID = jsonschema.ID(schemaID)
	root.Title = "noodge.yaml"
	root.Description = "Configuration for noodge, a documented, discoverable task runner."

	// Commands declares a custom schema so the file reads as a plain mapping
	// of name to command, which means the reflector never walks into Command
	// and never emits its definition. Reflect it separately and merge, or the
	// $ref in that custom schema dangles.
	sub, err := newReflector()
	if err != nil {
		return nil, err
	}
	cmd := sub.Reflect(&config.Command{})

	if root.Definitions == nil {
		root.Definitions = jsonschema.Definitions{}
	}
	for name, def := range cmd.Definitions {
		if _, exists := root.Definitions[name]; !exists {
			root.Definitions[name] = def
		}
	}

	return root, nil
}

func newReflector() (*jsonschema.Reflector, error) {
	r := &jsonschema.Reflector{}
	// The doc comments in internal/config are written for a config author, and
	// this is what turns them into editor hover text.
	if err := r.AddGoComments(modulePath, "./internal/config"); err != nil {
		return nil, fmt.Errorf("reading doc comments: %w", err)
	}
	normaliseCommentKeys(r)
	return r, nil
}

// normaliseCommentKeys works around an upstream bug in invopop/jsonschema on
// Windows. AddGoComments builds its map keys by joining directories with the
// OS path separator, producing keys like
//
//	github.com/wimhaanstra/noodge/internal\config.Command.Description
//
// while lookups use reflect's PkgPath, which always uses forward slashes. The
// two never match, so on Windows every doc comment is silently dropped and the
// schema generates with no descriptions at all — which is most of the value.
//
// Rewriting the keys is safe on every platform: a forward slash is already
// what the lookup side expects, so on Unix this is a no-op.
func normaliseCommentKeys(r *jsonschema.Reflector) {
	if len(r.CommentMap) == 0 {
		return
	}

	fixed := make(map[string]string, len(r.CommentMap))
	for k, v := range r.CommentMap {
		fixed[strings.ReplaceAll(k, `\`, "/")] = v
	}
	r.CommentMap = fixed
}
