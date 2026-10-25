package web

import (
	"fmt"
	"net/url"
	"strings"
)

// nodeKind distinguishes the three kinds of routing tree nodes.
type nodeKind uint8

const (
	kindStatic   nodeKind = iota // literal segment, e.g. "users"
	kindParam                    // ":name", matches one non-empty segment
	kindWildcard                 // "*name", must be last; captures the rest
)

// segment is one parsed segment of a registered route pattern.
type segment struct {
	kind  nodeKind
	value string // static text, or the parameter/wildcard name
}

// node is a single vertex of a per-method routing tree. A node holds at
// most one handler plus three classes of children: static children keyed
// by their literal text, a single parameter child, and a single wildcard
// child. Lookup always prefers static children, then the parameter child,
// then the wildcard child, backtracking when a preferred branch dead-ends.
type node struct {
	kind     nodeKind
	name     string
	static   map[string]*node
	param    *node
	wildcard *node
	handler  HandlerFunc
}

// parsePattern validates a raw route pattern and splits it into segments.
// A trailing slash is ignored ("/a/" is equivalent to "/a"), the root
// pattern "/" yields zero segments, and every pattern must begin with '/'.
// Percent-escapes in static segments are decoded so that patterns and
// request paths are compared on their decoded form.
func parsePattern(raw string) ([]segment, error) {
	if raw == "" {
		return nil, fmt.Errorf("web: invalid route %q: path must not be empty", raw)
	}
	if raw[0] != '/' {
		return nil, fmt.Errorf("web: invalid route %q: path must start with '/'", raw)
	}
	trimmed := raw
	if len(trimmed) > 1 && trimmed[len(trimmed)-1] == '/' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if trimmed == "/" {
		return []segment{}, nil
	}
	parts := strings.Split(trimmed, "/")[1:]
	segs := make([]segment, 0, len(parts))
	for i, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("web: invalid route %q: empty path segment at position %d", raw, i)
		}
		switch {
		case strings.HasPrefix(part, "*"):
			name := part[1:]
			if name == "" {
				return nil, fmt.Errorf("web: invalid route %q: wildcard segment must have a name", raw)
			}
			if i != len(parts)-1 {
				return nil, fmt.Errorf("web: invalid route %q: wildcard %q must be the last segment", raw, name)
			}
			segs = append(segs, segment{kind: kindWildcard, value: name})
		case strings.HasPrefix(part, ":"):
			name := part[1:]
			if name == "" {
				return nil, fmt.Errorf("web: invalid route %q: parameter segment must have a name", raw)
			}
			segs = append(segs, segment{kind: kindParam, value: name})
		default:
			value, err := url.PathUnescape(part)
			if err != nil {
				return nil, fmt.Errorf("web: invalid route %q: malformed percent-encoding in segment %d", raw, i)
			}
			segs = append(segs, segment{kind: kindStatic, value: value})
		}
	}
	return segs, nil
}

// insert registers h at the node reached by following segs, creating
// intermediate nodes as needed. label is used in error messages. It
// rejects duplicate handlers for the same full pattern and conflicting
// parameter or wildcard names at the same position.
func (n *node) insert(label string, segs []segment, h HandlerFunc) error {
	if len(segs) == 0 {
		if n.handler != nil {
			return fmt.Errorf("web: route %s is already registered", label)
		}
		n.handler = h
		return nil
	}
	head := segs[0]
	var child *node
	switch head.kind {
	case kindStatic:
		if n.static == nil {
			n.static = make(map[string]*node)
		}
		child = n.static[head.value]
		if child == nil {
			child = &node{kind: kindStatic, name: head.value}
			n.static[head.value] = child
		}
	case kindParam:
		if n.param == nil {
			n.param = &node{kind: kindParam, name: head.value}
		}
		if n.param.name != head.value {
			return fmt.Errorf("web: route %s: parameter name %q conflicts with existing parameter %q at the same position",
				label, head.value, n.param.name)
		}
		child = n.param
	case kindWildcard:
		if n.wildcard == nil {
			n.wildcard = &node{kind: kindWildcard, name: head.value}
		}
		if n.wildcard.name != head.value {
			return fmt.Errorf("web: route %s: wildcard name %q conflicts with existing wildcard %q at the same position",
				label, head.value, n.wildcard.name)
		}
		child = n.wildcard
	}
	return child.insert(label, segs[1:], h)
}

// search walks segs through the tree and returns the matching handler,
// filling params with captured values. Matching prefers static children,
// then the parameter child, then the wildcard child at every level,
// backtracking out of dead-end branches. A wildcard matches even when no
// segments remain, capturing the empty string. It returns nil when no
// handler matches. Params are only written along the successful path.
func (n *node) search(segs []string, params map[string]string) HandlerFunc {
	if len(segs) == 0 {
		if n.handler != nil {
			return n.handler
		}
		if n.wildcard != nil && n.wildcard.handler != nil {
			params[n.wildcard.name] = ""
			return n.wildcard.handler
		}
		return nil
	}
	if n.static != nil {
		if child := n.static[segs[0]]; child != nil {
			if h := child.search(segs[1:], params); h != nil {
				return h
			}
		}
	}
	// A parameter matches exactly one non-empty segment. splitRequestPath
	// never yields empty segments, but search also enforces the rule itself.
	if n.param != nil && segs[0] != "" {
		if h := n.param.search(segs[1:], params); h != nil {
			params[n.param.name] = segs[0]
			return h
		}
	}
	if n.wildcard != nil && n.wildcard.handler != nil {
		params[n.wildcard.name] = strings.Join(segs, "/")
		return n.wildcard.handler
	}
	return nil
}

// splitRequestPath normalizes the escaped request path and splits it into
// percent-decoded segments. A single trailing slash is dropped ("/a/"
// becomes ["a"]) and the root path "/" yields zero segments. It reports
// false when the path contains an empty interior segment (a double "/"),
// which must never match any route.
func splitRequestPath(escaped string) ([]string, bool) {
	p := escaped
	if p == "" {
		p = "/"
	}
	if p[0] != '/' {
		return nil, false
	}
	if len(p) > 1 && p[len(p)-1] == '/' {
		p = p[:len(p)-1]
	}
	if p == "/" {
		return []string{}, true
	}
	parts := strings.Split(p, "/")[1:]
	for i, part := range parts {
		if part == "" {
			return nil, false
		}
		decoded, err := url.PathUnescape(part)
		if err != nil {
			// Malformed escapes cannot be produced by a parsed *http.Request
			// URL; keep the raw text so behavior stays well defined.
			decoded = part
		}
		parts[i] = decoded
	}
	return parts, true
}
