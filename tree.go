package egret

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	pathNodeTypeStatic = iota
	pathNodeTypeRegex
	pathNodeTypeRoot
)

type (
	pathNodeType int
	pathNode     struct {
		kind        pathNodeType
		path        string
		regex       *regexp.Regexp
		pnames      []string
		constraint  ConstraintFunc
		strictSlash bool
		handlers    map[string][]HandlerFunc
		children    []*pathNode
	}
	pathTree struct {
		*pathNode
	}
)

func newPathTree() *pathTree {
	return &pathTree{
		&pathNode{
			kind:     pathNodeTypeRoot,
			handlers: make(map[string][]HandlerFunc, 0),
			children: make([]*pathNode, 0),
		},
	}
}
func newPathNode() *pathNode {
	return &pathNode{
		handlers: make(map[string][]HandlerFunc, 0),
		children: make([]*pathNode, 0),
	}
}

func (n *pathNode) setConstraint(constraint ConstraintFunc) *pathNode {
	n.constraint = constraint
	return n
}
func (n *pathNode) setStrictSlash(strictSlash bool) *pathNode {
	n.strictSlash = strictSlash
	return n
}

func normalizePath(path string) string {
	npath := ""
	for i, pn := 0, len(path); i < pn; i++ {
		ch := path[i]
		if ch == '<' {
			pname, pattern, pnameParsed := "", "", false
			for i++; i < pn; i++ {
				ch = path[i]
				if ch == '>' {
					break
				} else if ch == ':' {
					pnameParsed = true
					continue
				}
				if pnameParsed {
					pattern += string(ch)
				} else {
					pname += string(ch)
				}
			}
			if pattern == "" || pattern == ".*" || pattern == "[^/]*" || pattern == "[^/]+" {
				pattern = ".+"
			}
			if pattern == "" {
				npath += "<" + pname + ">"
			} else {
				npath += "<" + pname + ":" + pattern + ">"
			}
		} else {
			npath += string(ch)
		}
	}
	if npath[0] != '/' {
		return "/" + npath
	}
	return npath
}
func (t *pathTree) add(path string) (*pathNode, bool) {
	path = normalizePath(path)
	for _, child := range t.children {
		node, ok := child.add(path)
		if ok {
			return node, ok
		}
	}
	return t.pathNode.addChild(path), true
}
func (n *pathNode) add(path string) (*pathNode, bool) {
	matched := 0

	//find the common prefix
	for ; matched < len(path) && matched < len(n.path); matched++ {
		if path[matched] != n.path[matched] {
			break
		}
	}
	if matched == len(n.path) {
		if matched == len(path) {
			// the pathNode key is the same as the key: make the current pathNode as data pathNode
			// if the pathNode is already a data pathNode, ignore the new data since we only care the first matched pathNode
			return n, true
		}

		// the pathNode key is a prefix of the path: create a child pathNode
		newPath := path[matched:]

		for _, child := range n.children {
			n, ok := child.add(newPath)
			if ok {
				return n, true
			}
		}

		//this new path can not be added to child pathNode, need to create new child pathNode in current pathNode
		return n.addChild(newPath), true
	}

	// no common prefix, or partial common prefix with a non-static pathNode: should skip this pathNode
	if matched == 0 || n.regex != nil {
		return nil, false
	}

	// the pathNode key shares a partial prefix with the key: split the pathNode key
	n1 := &pathNode{
		kind:     pathNodeTypeStatic,
		path:     n.path[matched:],
		regex:    n.regex,
		handlers: n.handlers,
		children: n.children,
	}

	n.path = path[0:matched]
	n.handlers = make(map[string][]HandlerFunc, 0)
	n.regex = nil //n must be static pathNode, regex is already nil, no need to set it again.
	n.children = []*pathNode{n1}

	return n.add(path)
}

func (n *pathNode) addChild(path string) *pathNode {
	// find the first occurrence of a param token
	p0, p1, level := -1, -1, 0
	for i := 0; i < len(path); i++ {
		if p0 < 0 && path[i] == '<' {
			p0 = i
			level++
		}
		if p0 >= 0 && path[i] == '>' {
			p1 = i
			level--
			if level == 0 {
				break
			}
		}
	}

	if p1 < 0 {
		//no param token: create a static pathNode
		child := &pathNode{
			kind:     pathNodeTypeStatic,
			path:     path,
			handlers: make(map[string][]HandlerFunc, 0),
			children: make([]*pathNode, 0),
		}
		n.children = append(n.children, child)
		return child
	}
	if p0 > 0 && p1 > 0 {
		// param token occurs after a static string
		child := &pathNode{
			kind:     pathNodeTypeStatic,
			path:     path[0:p0],
			handlers: make(map[string][]HandlerFunc, 0),
			children: make([]*pathNode, 0),
		}
		n.children = append(n.children, child)
		n = child
		path = path[p0:]
		p1 -= p0
		p0 = 0
	}

	child := &pathNode{
		kind:     pathNodeTypeRegex,
		path:     path[p0 : p1+1],
		pnames:   make([]string, 1),
		handlers: make(map[string][]HandlerFunc, 0),
		children: make([]*pathNode, 0),
	}
	n.children = append(n.children, child)

	pnames, pattern, end := getPnames(path, 0)
	child.pnames = pnames
	if pattern != "" && pattern != "(.+)" {
		child.regex = regexp.MustCompile("^" + pattern)
	}
	if end == len(path)-1 {
		return child
	}
	return child.addChild(path[end+1:])
}
func (n *pathNode) get(method string, url *url.URL) ([]HandlerFunc, map[string]string) {
	params := make(map[string]string, 0)
	handlers := n.innerGet(method, url.Path, url.Path, url.RawQuery, params)
	return handlers, params
}

func (n *pathNode) innerGet(method, fullPath, reset, rawQuery string, params map[string]string) []HandlerFunc {
	if n.kind == pathNodeTypeRoot { //only set root pathNode's path to nil{
		if len(n.children) > 0 {
			for _, child := range n.children {
				handlers := child.innerGet(method, fullPath, reset, rawQuery, params)
				if handlers != nil {
					return handlers
				}
			}
		}
		return nil
	}
	if n.kind == pathNodeTypeStatic {
		npl, rtl := len(n.path), len(reset)
		if npl > rtl {
			if npl == rtl+1 {
				if n.path == reset+"/" {
					reset = ""
				} else {
					return nil
				}
			} else {
				return nil
			}
		} else {
			if reset[0:npl] == n.path {
				reset = reset[npl:]
			} else {
				return nil
			}
		}
	} else if n.kind == pathNodeTypeRegex {
		if n.regex != nil {
			ln := strings.IndexByte(reset, '/')
			if ln == -1 {
				ln = len(reset)
			}
			subp := reset[0:ln]
			results := n.regex.FindStringSubmatch(subp)
			if len(results) == len(n.pnames)+1 {
				for i, il := 0, len(n.pnames); i < il; i++ {
					params[n.pnames[i]] = results[i+1]
				}
			} else {
				return nil
			}
			reset = reset[len(results[0]):]
		} else {
			if n.pnames[0][0] == '*' {
				params[n.pnames[0]] = reset[0:len(reset)]
				return n.handlers[method]
			}
			ln := strings.IndexByte(reset, '/')
			if ln == -1 {
				ln = len(reset)
			}
			params[n.pnames[0]] = reset[0:ln]
			reset = reset[ln:]
		}
	}

	if reset != "" {
		if len(n.children) > 0 {
			for _, child := range n.children {
				handlers := child.innerGet(method, fullPath, reset, rawQuery, params)
				if handlers != nil {
					return handlers
				}
			}
			return nil
		} else if reset == "/" {
			if n.strictSlash {
				rurl := fullPath[0 : len(fullPath)-1]
				if rawQuery != "" {
					rurl += "?" + rawQuery
				}
				return []HandlerFunc{
					func(ctx *Context) {
						ctx.Redirect(rurl, http.StatusMovedPermanently)
					},
				}
			}
			return n.handlers[method]
		}
		return nil
	}
	return n.handlers[method]
}

func (n *pathNode) print(level int) string {
	r := fmt.Sprintf("%v{kind: %v, path: %v, regex: %v, handlers: %v, children: %v, pnames: %v}\n", strings.Repeat(" ", level<<2), n.kind, n.path, n.regex, len(n.handlers), len(n.children), n.pnames)
	for _, child := range n.children {
		if child != nil {
			r += child.print(level + 1)
		}
	}
	return r
}

var regchs = "{}[]()^$.|*+?\\"

func getPnames(path string, startIndex int) (pnames []string, parttern string, endIndex int) {
	pnames, pattern := []string{}, ""

	pn := len(path)
	for i := startIndex; i < pn; i++ {
		ch := path[i]
		if ch == '/' {
			return pnames, pattern, i - 1
		} else if ch == '<' {
			pname, pnameParsed := "", false
			for i++; i < pn; i++ {
				ch := path[i]
				if ch == '>' {
					pnames = append(pnames, pname)
					pattern += ")"
					break
				}
				if ch == ':' {
					pnameParsed = true
					pattern += "("
					continue
				}
				if pnameParsed {
					pattern += string(ch)
				} else {
					pname += string(ch)
				}
			}
		} else {
			if strings.Contains(regchs, string(ch)) {
				pattern += "\\" + string(ch)
			} else {
				pattern += string(ch)
			}
		}
	}
	return pnames, pattern, pn - 1
}
