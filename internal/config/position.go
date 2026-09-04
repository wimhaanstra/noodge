package config

import "github.com/goccy/go-yaml/ast"

// Positions in a YAML document are resolved by walking the parsed AST rather
// than with a YAML path expression, because command names legitimately contain
// colons ("start:local") and quoting those into a path expression correctly is
// more fragile than walking the tree.

// keyText returns the text of a mapping key node.
func keyText(n ast.Node) string {
	if n == nil {
		return ""
	}
	if s, ok := n.(*ast.StringNode); ok {
		return s.Value
	}
	if tok := n.GetToken(); tok != nil {
		return tok.Value
	}
	return ""
}

// docBody returns the root node of the first document in the file.
func docBody(file *ast.File) ast.Node {
	if file == nil || len(file.Docs) == 0 {
		return nil
	}
	return file.Docs[0].Body
}

// mappingValues normalises the two shapes a mapping can take: a MappingNode
// holding several pairs, or a bare MappingValueNode when there is only one.
func mappingValues(n ast.Node) []*ast.MappingValueNode {
	switch v := n.(type) {
	case *ast.MappingNode:
		return v.Values
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{v}
	}
	return nil
}

// pair returns the key/value pair stored under key in a mapping node.
func pair(n ast.Node, key string) *ast.MappingValueNode {
	for _, mv := range mappingValues(n) {
		if keyText(mv.Key) == key {
			return mv
		}
	}
	return nil
}

// step is one hop in a lookup: a mapping key, or a sequence index.
type step struct {
	key   string
	index int
	isIdx bool
}

func key(k string) step { return step{key: k} }
func index(i int) step  { return step{index: i, isIdx: true} }

// findValue walks the path and returns the value node at the end of it.
func findValue(file *ast.File, path ...step) ast.Node {
	cur := docBody(file)
	for _, s := range path {
		if cur == nil {
			return nil
		}
		if s.isIdx {
			seq, ok := cur.(*ast.SequenceNode)
			if !ok || s.index >= len(seq.Values) {
				return nil
			}
			cur = seq.Values[s.index]
			continue
		}
		mv := pair(cur, s.key)
		if mv == nil {
			return nil
		}
		cur = mv.Value
	}
	return cur
}

// findKey walks the path and returns the key node of the final mapping entry,
// which is where a diagnostic about a missing or misnamed field belongs.
func findKey(file *ast.File, path ...step) ast.Node {
	if len(path) == 0 {
		return nil
	}
	parent := findValue(file, path[:len(path)-1]...)
	last := path[len(path)-1]
	if parent == nil || last.isIdx {
		return nil
	}
	mv := pair(parent, last.key)
	if mv == nil {
		return nil
	}
	return mv.Key
}

// positionOf reports the 1-based line and column of a node, or 0,0 when the
// node is absent or carries no token.
func positionOf(n ast.Node) (line, col int) {
	if n == nil {
		return 0, 0
	}
	tok := n.GetToken()
	if tok == nil || tok.Position == nil {
		return 0, 0
	}
	return tok.Position.Line, tok.Position.Column
}
