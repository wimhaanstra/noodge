package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// File is a loaded config together with everything needed to report problems
// against the source it came from.
type File struct {
	// Path is the absolute path to the config file.
	Path string
	// Dir is the directory holding the config file. Commands run here unless
	// they declare their own cwd.
	Dir string
	// Source is the raw bytes, kept so errors can quote the offending line.
	Source []byte
	// Config is the decoded config.
	Config *Config

	ast *ast.File
}

// VersionError reports a config written for a newer noodge than this one.
// Detecting this before decoding matters: without the check, a file using a
// future field would fail with a misleading "unknown field" error instead of
// telling the user to upgrade.
type VersionError struct {
	Path      string
	Found     int
	Supported int
}

func (e *VersionError) Error() string {
	return fmt.Sprintf("%s declares version %d, but this noodge understands up to version %d — run 'noodge upgrade'",
		e.Path, e.Found, e.Supported)
}

// ParseError wraps a YAML syntax or type error, formatted with the source
// excerpt and position that goccy provides.
type ParseError struct {
	Path string
	Err  error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s:\n%s", e.Path, yaml.FormatError(e.Err, false, true))
}

func (e *ParseError) Unwrap() error { return e.Err }

// Load reads and decodes the config at path. Unknown fields are rejected, so
// a typo in a field name is reported rather than silently ignored.
//
// Load does not validate semantics; call Validate for that.
func Load(path string) (*File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}

	if v, ok := peekVersion(src); ok && v > FormatVersion {
		return nil, &VersionError{Path: abs, Found: v, Supported: FormatVersion}
	}

	astFile, err := parser.ParseBytes(src, parser.ParseComments)
	if err != nil {
		return nil, &ParseError{Path: abs, Err: err}
	}

	var cfg Config
	if err := yaml.UnmarshalWithOptions(src, &cfg, yaml.Strict()); err != nil {
		return nil, &ParseError{Path: abs, Err: err}
	}

	return &File{
		Path:   abs,
		Dir:    filepath.Dir(abs),
		Source: src,
		Config: &cfg,
		ast:    astFile,
	}, nil
}

// LoadFrom discovers a config starting at dir and loads it.
func LoadFrom(dir string) (*File, error) {
	path, err := Discover(dir)
	if err != nil {
		return nil, err
	}
	return Load(path)
}

// peekVersion reads just the version field, tolerating everything else being
// unrecognisable to this binary.
func peekVersion(src []byte) (int, bool) {
	var probe struct {
		Version int `json:"version"`
	}
	if err := yaml.Unmarshal(src, &probe); err != nil {
		return 0, false
	}
	if probe.Version == 0 {
		return 0, false
	}
	return probe.Version, true
}

// diag builds a diagnostic pointing at the value stored at path.
func (f *File) diag(sev Severity, msg, hint string, path ...step) Diagnostic {
	line, col := positionOf(findValue(f.ast, path...))
	if line == 0 {
		line, col = positionOf(findKey(f.ast, path...))
	}
	return Diagnostic{
		Severity: sev,
		Message:  msg,
		Hint:     hint,
		File:     f.Path,
		Line:     line,
		Col:      col,
	}
}

// diagKey builds a diagnostic pointing at the key at path, which is where a
// problem about a field's absence belongs.
func (f *File) diagKey(sev Severity, msg, hint string, path ...step) Diagnostic {
	line, col := positionOf(findKey(f.ast, path...))
	return Diagnostic{
		Severity: sev,
		Message:  msg,
		Hint:     hint,
		File:     f.Path,
		Line:     line,
		Col:      col,
	}
}
