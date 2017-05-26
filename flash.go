package egret

import (
	"fmt"
	"net/http"
	"net/url"
)

// Flash represents a cookie that is overwritten on each request.
// It allows data to be stored across one page at a time.
// This is commonly used to implement success or error messages.
// E.g. the Post/Redirect/Get pattern:
// http://en.wikipedia.org/wiki/Post/Redirect/Get
type Flash struct {
	// `Data` is the input which is read in `restoreFlash`, `Out` is the output which is set in a FLASH cookie at the end of the `FlashHandler()`
	Data, Out map[string]string
}

// Error serializes the given msg and args to an "error" key within
// the Flash cookie.
func (f Flash) Error(msg string, args ...interface{}) {
	if len(args) == 0 {
		f.Out["error"] = msg
	} else {
		f.Out["error"] = fmt.Sprintf(msg, args...)
	}
}

// Success serializes the given msg and args to a "success" key within
// the Flash cookie.
func (f Flash) Success(msg string, args ...interface{}) {
	if len(args) == 0 {
		f.Out["success"] = msg
	} else {
		f.Out["success"] = fmt.Sprintf(msg, args...)
	}
}

// FlashHandler is a Egret Handler that retrieves and sets the flash cookie.
// Within Egret, it is available as a Flash attribute on Context instances.
// The name of the Flash cookie is set as CookiePrefix + "_FLASH".
func FlashHandler(ctx *Context) {
	ctx.Flash = restoreFlash(ctx.Request.Request)
	ctx.RenderArgs["flash"] = ctx.Flash.Data

	// Store the flash.
	var flashValue string
	for key, value := range ctx.Flash.Out {
		flashValue += "\x00" + key + ":" + value + "\x00"
	}
	ctx.SetCookie(&http.Cookie{
		Name:     CookiePrefix + "_FLASH",
		Value:    url.QueryEscape(flashValue),
		HttpOnly: true,
		Secure:   CookieSecure,
		Path:     "/",
	})
	ctx.Next()
}

// restoreFlash deserializes a Flash cookie struct from a request.
func restoreFlash(req *http.Request) Flash {
	flash := Flash{
		Data: make(map[string]string),
		Out:  make(map[string]string),
	}
	if cookie, err := req.Cookie(CookiePrefix + "_FLASH"); err == nil {
		ParseKeyValueCookie(cookie.Value, func(key, val string) {
			flash.Data[key] = val
		})
	}
	return flash
}
