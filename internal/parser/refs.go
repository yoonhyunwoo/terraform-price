package parser

import "github.com/hashicorp/hcl/v2"

// scopeRoots name language scopes, never resource types.
var scopeRoots = map[string]bool{
	"var": true, "local": true, "data": true, "module": true,
	"terraform": true, "path": true, "cwd": true, "each": true,
	"count": true, "self": true,
}

// IsScopeRoot reports whether a traversal root names a scope, not a resource.
func IsScopeRoot(name string) bool { return scopeRoots[name] }

// RootName returns the root keyword of a traversal ("var" in var.x.y).
func RootName(t hcl.Traversal) (string, bool) {
	if len(t) == 0 {
		return "", false
	}
	if root, ok := t[0].(hcl.TraverseRoot); ok {
		return root.Name, true
	}
	return "", false
}

// SplitRef splits aws_instance.a.attr into ("aws_instance.a", rest);
// ok=false for scope roots and malformed traversals.
func SplitRef(t hcl.Traversal) (addr string, rest []hcl.Traverser, ok bool) {
	if len(t) < 2 {
		return "", nil, false
	}
	root, rok := t[0].(hcl.TraverseRoot)
	if !rok || IsScopeRoot(root.Name) {
		return "", nil, false
	}
	name, nok := t[1].(hcl.TraverseAttr)
	if !nok {
		return "", nil, false
	}
	return root.Name + "." + name.Name, t[2:], true
}
