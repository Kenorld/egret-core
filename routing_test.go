package egret

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

var handler0 = func(c *Context) {}
var handler1 = func(c *Context) {}
var handler2 = func(c *Context) {}
var handler3 = func(c *Context) {}
var handler4 = func(c *Context) {}
var handler5 = func(c *Context) {}
var handler6 = func(c *Context) {}
var handler7 = func(c *Context) {}
var handler8 = func(c *Context) {}
var handler9 = func(c *Context) {}

func isLastHandler(handlers []HandlerFunc, handler HandlerFunc) bool {
	if handlers == nil || len(handlers) == 0 {
		return false
	}
	p0, p1 := reflect.ValueOf(handlers[len(handlers)-1]), reflect.ValueOf(handler)
	return p0.Pointer() == p1.Pointer()
}

func testSimpleRoute(t *testing.T, tpath string, turl string, strictSlash bool, exceptedParams map[string]string) {
	router := NewRouter()
	path := router.Path(tpath).Get(handler0)
	path.SetStrictSlash(strictSlash)
	// fmt.Print(router.tree.pathNode.print(0))
	testURL, _ := url.Parse(turl)
	handlers, params := router.Match("GET", testURL)
	if !isLastHandler(handlers, handler0) {
		t.Errorf("Not found same handler")
	}
	assert.Equal(t, exceptedParams, params)
}
func TestSimpleRoutes(t *testing.T) {
	testSimpleRoute(t, "/abc/<p1>/<p2>/<p3>", "http://test.com/abc/111/222/33", false, map[string]string{
		"p1": "111",
		"p2": "222",
		"p3": "33",
	})

	testSimpleRoute(t, "/users/<name>/<id>.json", "http://test.com/users/chris/123.json", false, map[string]string{
		"name": "chris",
		"id":   "123",
	})

	testSimpleRoute(t, "/users/<name>-<id:\\d+>.json", "http://test.com/users/chris-123.json", false, map[string]string{
		"name": "chris",
		"id":   "123",
	})

	testSimpleRoute(t, "/users/<*path>", "http://test.com/users/chris/123.json", false, map[string]string{
		"*path": "chris/123.json",
	})

	testSimpleRoute(t, "/users/<id>", "http://test.com/users/123?action=delete", false, map[string]string{
		"id": "123",
	})
	testSimpleRoute(t, "/users/<id>", "http://test.com/users/123/?action=delete", false, map[string]string{
		"id": "123",
	})
	// testSimpleRoute(t, "/users/<id>/", "http://test.com/users/123?action=delete", false, map[string]string{
	// 	"id": "123",
	// })
}

func TestMultiRoutes(t *testing.T) {
	router := NewRouter()
	router.Path("/<*path>").Options(handler0)
	router.Path("/test/<id:[^.]+>").Get(handler1)
	router.Path("/test/<id:[^.]+>.mp4").Get(handler2)
	fmt.Print(router.tree.pathNode.print(0))

	testURL, _ := url.Parse("http://test.com/test/abc.mp4")
	handlers, _ := router.Match("OPTIONS", testURL)
	if !isLastHandler(handlers, handler0) {
		t.Errorf("OPTIONS: Not found same handler")
	}
	handlers, _ = router.Match("GET", testURL)
	if !isLastHandler(handlers, handler2) {
		t.Errorf("GET: Not found same handler")
	}
}
