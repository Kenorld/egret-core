package egret

import (
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/cast"
)

/*
/users/<id:\d+>
/users/<*path>
router.Path("/users/<id>/", func(){})
*/
type (
	// // Handler the main Iris Handler interface.
	// Handler interface {
	// 	// Serve handle route
	// 	Serve(ctx *Context)
	// }
	// HandlerFunc type is an adapter to allow the use of
	// ordinary functions as HTTP handlers.  If f is a function
	// with the appropriate signature, HandlerFunc(f) is a
	// Handler that calls f.
	HandlerFunc func(c *Context)

	ConstraintFunc func(url string, params map[string]string) bool
	Zone           struct {
		path           string
		node           *pathNode
		zones          []*Zone
		parent         *Zone
		host           *Host
		router         *Router
		beforeHandlers map[string][]HandlerFunc
		afterHandlers  map[string][]HandlerFunc
	}
	Host struct {
		zones          []*Zone
		path           string
		regex          *regexp.Regexp
		constraint     ConstraintFunc
		router         *Router
		namedZones     map[string]*Zone
		tree           *pathTree
		beforeHandlers map[string][]HandlerFunc
		afterHandlers  map[string][]HandlerFunc
	}

	//Router is struct for save router data.
	Router struct {
		tree           *pathTree
		zones          []*Zone
		namedZones     map[string]*Zone
		hosts          []*Host
		beforeHandlers map[string][]HandlerFunc
		afterHandlers  map[string][]HandlerFunc
	}
)

const (
	regexEscapeChars = ".$^{[(|)*+?\\"
)

var allMethods = strings.Split("GET,POST,DELETE,PUT,PATCH,CONNECT,HEAD,OPTIONS,TRACE", ",")

//Host set router host name.
func (r *Router) Host(path string) *Host {
	host := &Host{
		path:           path,
		regex:          regexp.MustCompile(strings.Replace(strings.Replace(path, ".", "\\.", -1), "*", ".*", -1)),
		zones:          []*Zone{},
		router:         r,
		beforeHandlers: make(map[string][]HandlerFunc),
		afterHandlers:  make(map[string][]HandlerFunc),
	}
	r.hosts = append(r.hosts, host)
	return host
}

func setPresetHandlers(allHandlers map[string][]HandlerFunc, method string, handlers []HandlerFunc) {
	ms := getMethods(method)
	for _, m := range ms {
		if allHandlers[m] == nil {
			allHandlers[m] = handlers
		} else {
			allHandlers[m] = append(allHandlers[m], handlers...)
		}
	}
}

func (z *Zone) Route(method string, handlers ...HandlerFunc) *Zone {
	ms := getMethods(method)
	for _, m := range ms {
		allHandlers := handlers
		for pz := z; pz != nil; {
			if pz.beforeHandlers[m] != nil {
				allHandlers = append(pz.beforeHandlers[m], allHandlers...)
			}
			if pz.afterHandlers[m] != nil {
				allHandlers = append(allHandlers, pz.afterHandlers[m]...)
			}
			pz = pz.parent
		}
		if z.router.beforeHandlers[m] != nil {
			allHandlers = append(z.router.beforeHandlers[m], allHandlers...)
		}
		if z.router.afterHandlers[m] != nil {
			allHandlers = append(allHandlers, z.router.afterHandlers[m]...)
		}
		z.node.handlers[method] = allHandlers
	}

	return z
}

func getMethods(l string) []string {
	ms := []string{}
	if l == "*" {
		ms = allMethods
	} else {
		ms = strings.Split(l, ",")
	}
	return ms
}
func (z *Zone) SetConstraint(c ConstraintFunc) *Zone {
	z.node.setConstraint(c)
	return z
}
func (z *Zone) SetStrictSlash(value bool) *Zone {
	z.node.setStrictSlash(value)
	return z
}

func (r *Router) Path(path string, constraint ...ConstraintFunc) *Zone {
	// path = normalizePath(path)
	zone := &Zone{
		path:           path,
		zones:          []*Zone{},
		beforeHandlers: make(map[string][]HandlerFunc),
		afterHandlers:  make(map[string][]HandlerFunc),
	}
	zone.node, _ = r.tree.add(path)
	zone.router = r
	r.zones = append(r.zones, zone)
	if len(constraint) > 0 {
		zone.SetConstraint(constraint[0])
	}
	return zone
}

func (z *Zone) Path(path string) *Zone {
	path = joinPath(z.path, path)
	Logger.Info(path)
	if z.host != nil {
		z.host.tree.add(path)
	}
	zone := &Zone{
		path:           path,
		router:         z.router,
		parent:         z,
		zones:          []*Zone{},
		beforeHandlers: make(map[string][]HandlerFunc),
		afterHandlers:  make(map[string][]HandlerFunc),
	}
	zone.node, _ = z.router.tree.add(path)
	z.zones = append(z.zones, zone)
	return zone
}

func (d *Host) Match(method string, url *url.URL) (handlers []HandlerFunc, params map[string]string) {
	if d.regex.MatchString(url.Host) && (d.constraint == nil || d.constraint(url.String(), params)) {
		return d.tree.get(method, url)
	}
	return nil, nil
}
func (r *Router) Match(method string, url *url.URL) (handlers []HandlerFunc, params map[string]string) {
	for _, host := range r.hosts {
		handlers, params := host.Match(method, url)
		if handlers != nil {
			return handlers, params
		}
	}
	return r.tree.get(method, url)
}

//Reverse build url by route name and params.
func (r *Router) Reverse(routeName string, pairsArgs ...map[string]interface{}) (string, error) {
	zone, pairs := r.namedZones[routeName], pairsArgs[0]
	if zone != nil {
		path := ""
		for i := 0; i < len(zone.path); i++ {
			if zone.path[i] == '<' {
				pname, skipReset := "", false
				for ; i < len(zone.path); i++ {
					if zone.path[i] == '>' {
						break
					}
					if skipReset {
						continue
					}
					if zone.path[i] == ':' {
						skipReset = true
					}
					if !skipReset {
						pname += zone.path
					}
				}
				if pairs[pname] != nil {
					path += cast.ToString(pairs[pname])
				} else {
					return "", errors.New("Missing argument: " + pname)
				}
			} else {
				path += string(zone.path[i])
			}
		}
		return path, nil
	}
	return "", errors.New("Not found!")
}
func (z *Zone) Name(name string) *Zone {
	namedZones := z.router.namedZones
	if z.host != nil {
		namedZones = z.host.namedZones
	}

	if namedZones[name] != nil {
		Logger.Error("Named path already setted: " + name)
	}
	namedZones[name] = z
	return z
}
func (r *Router) Before(method string, handlers ...HandlerFunc) *Router {
	setPresetHandlers(r.beforeHandlers, method, handlers)
	return r
}
func (r *Router) After(method string, handlers ...HandlerFunc) *Router {
	setPresetHandlers(r.afterHandlers, method, handlers)
	return r
}
func (z *Zone) Before(method string, handlers ...HandlerFunc) *Zone {
	setPresetHandlers(z.beforeHandlers, method, handlers)
	return z
}
func (z *Zone) After(method string, handlers ...HandlerFunc) *Zone {
	setPresetHandlers(z.afterHandlers, method, handlers)
	return z
}

func (z *Zone) Any(handlers ...HandlerFunc) *Zone {
	z.Route("*", handlers...)
	return z
}
func (z *Zone) Get(handlers ...HandlerFunc) *Zone {
	z.Route("GET", handlers...)
	return z
}
func (z *Zone) Post(handlers ...HandlerFunc) *Zone {
	z.Route("POST", handlers...)
	return z
}
func (z *Zone) Delete(handlers ...HandlerFunc) *Zone {
	z.Route("DELETE", handlers...)
	return z
}
func (z *Zone) Put(handlers ...HandlerFunc) *Zone {
	z.Route("PUT", handlers...)
	return z
}
func (z *Zone) Patch(handlers ...HandlerFunc) *Zone {
	z.Route("PATCH", handlers...)
	return z
}
func (z *Zone) Connect(handlers ...HandlerFunc) *Zone {
	z.Route("CONNECT", handlers...)
	return z
}
func (z *Zone) Head(handlers ...HandlerFunc) *Zone {
	z.Route("HEAD", handlers...)
	return z
}
func (z *Zone) Options(handlers ...HandlerFunc) *Zone {
	z.Route("OPTIONS", handlers...)
	return z
}
func (z *Zone) Trace(handlers ...HandlerFunc) *Zone {
	z.Route("TRACE", handlers...)
	return z
}

func joinPath(args ...string) string {
	path := ""
	for _, arg := range args {
		if path != "" && path[len(path)-1] == '/' {
			if arg[0] == '/' {
				path += arg[1:]
			} else {
				path += arg
			}
		} else {
			if arg[0] == '/' {
				path += arg
			} else {
				path += "/" + arg
			}
		}
	}
	return path
}

var routers = []*Router{}

func NewRouter() *Router {
	router := &Router{
		tree:           newPathTree(),
		namedZones:     make(map[string]*Zone),
		beforeHandlers: make(map[string][]HandlerFunc),
		afterHandlers:  make(map[string][]HandlerFunc),
	}
	routers = append(routers, router)
	return router
}

func ReverseURL(name string, pairs ...map[string]interface{}) (string, error) {
	for _, router := range routers {
		if url, err := router.Reverse(name, pairs...); err == nil {
			return url, nil
		}
	}
	return "", errors.New("Not found valid named route")
}
