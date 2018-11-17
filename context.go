package egret

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"go.uber.org/zap"
	"github.com/spf13/cast"
)

type (
	Context struct {
		Request  *Request
		Response *Response

		Binary *Binary
		Error  error

		Flash   Flash   // User cookie, cleared after 1 request.
		Session Session // Session, stored in cookie, signed.

		Params   map[string]string // list of route parameter names
		Handlers []HandlerFunc     // the handlers associated with the current route

		Store      map[string]interface{}
		RenderArgs map[string]interface{}
		index      int // the index of the currently executing handler in handlers
	}
	RenderOptions map[string]interface{}
	Binary        struct {
		Reader   io.Reader
		Name     string
		Length   int64
		Delivery string
		ModTime  time.Time
	}
)

var (
	// StaticCacheDuration expiration duration for INACTIVE file handlers, it's the only one global configuration
	// which can be changed.
	StaticCacheDuration = 20 * time.Second

	lastModifiedHeaderKey       = "Last-Modified"
	ifModifiedSinceHeaderKey    = "If-Modified-Since"
	contentDispositionHeaderKey = "Content-Disposition"
	cacheControlHeaderKey       = "Cache-Control"
	contentEncodingHeaderKey    = "Content-Encoding"
	acceptEncodingHeaderKey     = "Accept-Encoding"
	varyHeaderKey               = "Vary"
)

func NewContext(req *Request, resp *Response) *Context {
	return &Context{
		Request:    req,
		Response:   resp,
		RenderArgs: map[string]interface{}{},
		Store:      map[string]interface{}{},
	}
}

func (c *Context) Param(name string) string {
	return c.Params[name]
}

// Get returns the named store item previously registered with the context by calling Set.
// If the named store item cannot be found, nil will be returned.
func (c *Context) Get(name string, defaultValue ...interface{}) interface{} {
	v, ok := c.Store[name]
	if !ok && len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return v
}

// Set stores the named store item in the context so that it can be retrieved later.
func (c *Context) Set(name string, value interface{}) {
	c.Store[name] = value
}

// GetHeader returns the request header's value based on its name.
func (c *Context) GetHeader(name string) string {
	return c.Request.Header.Get(name)
}

// Query returns the first value for the named component of the URL query parameters.
// If key is not present, it returns the specified default value or an empty string.
func (c *Context) Query(name string, defaultValue ...string) string {
	if vs, _ := c.Request.URL.Query()[name]; len(vs) > 0 {
		return vs[0]
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

// Form returns the first value for the named component of the query.
// Form reads the value from POST and PUT body parameters as well as URL query parameters.
// The form takes precedence over the latter.
// If key is not present, it returns the specified default value or an empty string.
func (c *Context) Form(key string, defaultValue ...string) string {
	r := c.Request
	r.ParseMultipartForm(32 << 20)
	if vs := r.Form[key]; len(vs) > 0 {
		return vs[0]
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

// PostForm returns the first value for the named component from POST and PUT body parameters.
// If key is not present, it returns the specified default value or an empty string.
func (c *Context) Post(key string, defaultValue ...string) string {
	r := c.Request
	r.ParseMultipartForm(32 << 20)
	if vs := r.PostForm[key]; len(vs) > 0 {
		return vs[0]
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func (c *Context) SetCookie(cookie *http.Cookie) {
	http.SetCookie(c.Response.Writer, cookie)
}

// GetCookie returns cookie's value by it's name
// returns empty string if nothing was found.
func (c *Context) GetCookie(name string) string {
	cookie, err := c.Request.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// RemoveCookie deletes a cookie by it's name.
func (c *Context) RemoveCookie(name string) {
	cookie := &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Expires:  time.Now().Add(-time.Duration(1) * time.Minute),
		MaxAge:   -1,
	}
	c.SetCookie(cookie)
	// delete request's cookie also, which is temporary available
	c.Request.Header.Set("Cookie", "")
}

// VisitAllCookies takes a visitor which loops
// on each (request's) cookies' name and value.
func (c *Context) VisitAllCookies(visitor func(name string, value string)) {
	for _, cookie := range c.Request.Cookies() {
		visitor(cookie.Name, cookie.Value)
	}
}

var maxAgeExp = regexp.MustCompile(`maxage=(\d+)`)

// MaxAge returns the "cache-control" request header's value
// seconds as int64
// if header not found or parse failed then it returns -1.
func (c *Context) MaxAge() int64 {
	header := c.GetHeader(cacheControlHeaderKey)
	if header == "" {
		return -1
	}
	m := maxAgeExp.FindStringSubmatch(header)
	if len(m) == 2 {
		if v, err := strconv.Atoi(m[1]); err == nil {
			return int64(v)
		}
	}
	return -1
}

func (c *Context) RenderError(err error) {
	c.Response.Status = http.StatusInternalServerError
	c.Error = err
}
func (c *Context) SetStatusCode(status int) *Context {
	c.Response.Status = status
	return c
}
func (c *Context) SetStatusCodeIfNil(status int) *Context {
	if c.Response.Status == 0 {
		c.Response.Status = status
	}
	return c
}

func (c *Context) Next() {
	if c.index < len(c.Handlers) {
		handler := c.Handlers[c.index]
		c.index++
		handler(c)
	}
}

// Abort skips the rest of the handlers associated with the current route.
// Abort is normally used when a handler handles the request normally and wants to skip the rest of the handlers.
// If a handler wants to indicate an error condition, it should simply return the error without calling Abort.
func (c *Context) Abort() {
	c.index = len(c.Handlers)
}

// ReverseURL creates a URL using the named route and the parameter values.
// The parameters should be given in the sequence of name1, value1, name2, value2, and so on.
// If a parameter in the route is not provided a value, the parameter token will remain in the resulting URL.
// Parameter values will be properly URL encoded.
// The method returns an empty string if the URL creation fails.
func (c *Context) ReverseURL(name string, pairs ...map[string]interface{}) (string, error) {
	return ReverseURL(name, pairs...)
}

// Read populates the given struct variable with the store from the current request.
// If the request is NOT a GET request, it will check the "Content-Type" header
// and find a matching reader from DataReaders to read the request store.
// If there is no match or if the request is a GET request, it will use DefaultFormDataReader
// to read the request store.
func (c *Context) Read(store interface{}) error {
	if c.Request.Method != "GET" {
		t := c.Request.ContentType
		if reader, ok := DataReaders[t]; ok {
			return reader.Read(c.Request, store)
		}
	}

	return DefaultFormDataReader.Read(c.Request, store)
}

/* Response */
func (c *Context) RenderTemplate(path string, o interface{}, options map[string]interface{}) *Context {
	c.RenderArgs["Entity"] = o
	c.RenderArgs["template.path"] = path
	c.RenderArgs["template.options"] = options
	return c
}

func (c *Context) ExecuteRender() error {
	if c.Error != nil {
		viewPath := "errors/500." + c.Request.Format
		if _, ok := c.Error.(*Error); !ok {
			c.Error = &Error{
				Status:  500,
				Title:   "Error",
				Path:    "Unknown",
				Summary: c.Error.Error(),
			}
		}
		e, _ := c.Error.(*Error)
		c.Response.Status = e.Status
		c.SetStatusCodeIfNil(500)
		c.Response.SetFormat(c.Request.Format)
		c.Response.EnsureHeaderWrited()

		if c.Response.Status != 0 {
			viewPath = "errors/" + cast.ToString(c.Response.Status) + "." + c.Request.Format
		}
		//if DevMode {
		//fmt.Println("Server Error: ", c.Error)
		//}
		return MainTemplateManager.ExecuteWriter(c.Response.Writer, viewPath, map[string]interface{}{
			"DevMode": DevMode,
			"RunMode": RunMode,
			"Error":   c.Error,
		}, nil)
	} else if url := cast.ToString(c.RenderArgs["redirectURL"]); url != "" {
		status := cast.ToInt(c.RenderArgs["httpStatus"])
		if status == 0 {
			status = http.StatusFound
		}
		http.Redirect(c.Response.Writer, c.Request.Request, url, status)
		return nil
	} else if format := cast.ToString(c.RenderArgs["serialize.format"]); format != "" {
		c.SetStatusCodeIfNil(http.StatusOK)
		if format == ContentHTML {
			c.Response.ContentType = format + "; charset=utf-8"
			html, _ := c.RenderArgs["Entity"].(string)
			c.Response.Write([]byte(html))
			return nil
		}
		bytes, err := MainSerializerManager.Serialize(format, c.RenderArgs["Entity"], cast.ToStringMap(c.RenderArgs["serializer.options"]))
		if format == ContentMarkdown {
			format = ContentHTML
		}
		c.Response.ContentType = format + "; charset=utf-8"
		c.Response.Write(bytes)
		return err
	} else if c.Binary != nil {
		c.SetStatusCodeIfNil(http.StatusOK)
		return sendBinary(c.Request, c.Response, c.Binary)
	}
	viewPath, options := cast.ToString(c.RenderArgs["template.path"]), cast.ToStringMap(c.RenderArgs["template.options"])
	format := cast.ToString(options["format"])
	if format == "" {
		format = "text/html"
	}
	c.Response.ContentType = format + "; charset=utf-8"
	c.Response.EnsureHeaderWrited()
	if viewPath != "" {
		return MainTemplateManager.ExecuteWriter(c.Response.Writer, viewPath, c.RenderArgs, options)
	}
	return nil
	// if err != nil {
	// 	return err
	// }
	// return MainTemplateManager.ExecuteRaw(viewPath, c.Response.Writer, c.Entity)
}

func sendBinary(req *Request, resp *Response, r *Binary) (err error) {
	disposition := string(r.Delivery)
	if r.Name != "" {
		disposition += fmt.Sprintf(`; filename="%s"`, r.Name)
	}
	resp.SetHeader(contentDispositionHeaderKey, disposition)

	// If we have a ReadSeeker, delegate to http.ServeContent
	if rs, ok := r.Reader.(io.ReadSeeker); ok {
		// http.ServeContent doesn't know about response.ContentType, so we set the respective header.
		if resp.ContentType == "" {
			contentType := ContentTypeByFilename(r.Name)
			resp.ContentType = contentType
		}
		http.ServeContent(resp.Writer, req.Request, r.Name, r.ModTime, rs)
	} else {
		// Else, do a simple io.Copy.
		if r.Length != -1 {
			resp.SetHeader("Content-Length", strconv.FormatInt(r.Length, 10))
		}
		resp.ContentType = ContentTypeByFilename(r.Name)
		io.Copy(resp.Writer, r.Reader)
	}

	// Close the Reader if we can
	if v, ok := r.Reader.(io.Closer); ok {
		v.Close()
	}
	return nil
}

// Uses encoding/json.Marshal to return JSON to the client.
func (c *Context) RenderJSON(o interface{}) *Context {
	c.RenderArgs["serialize.format"] = ContentJSON
	c.RenderArgs["Entity"] = o
	return c
}

func (c *Context) RenderMarkdown(content string) *Context {
	c.RenderArgs["serialize.format"] = "text/markdown"
	c.RenderArgs["Entity"] = content
	return c
}

func (c *Context) RenderMarkdownFile(path string) *Context {
	content, err := ioutil.ReadFile(GetAbsPath(path))
	if err != nil {
		return c.NotFound("Markdown file is not exist!")
	}
	return c.RenderMarkdown(string(content))
}

// Renders a JSONP result using encoding/json.Marshal
func (c *Context) RenderJSONP(o interface{}, callback string) *Context {
	c.RenderArgs["serialize.format"] = ContentJavascript
	c.RenderArgs["Entity"] = o
	return c
}

// Uses encoding/xml.Marshal to return XML to the client.
func (c *Context) RenderXML(o interface{}) *Context {
	c.RenderArgs["serialize.format"] = ContentXML
	c.RenderArgs["Entity"] = o
	return c
}

// Render plaintext in response, printf style.
func (c *Context) RenderText(text string, objs ...interface{}) *Context {
	finalText := text
	if len(objs) > 0 {
		finalText = fmt.Sprintf(text, objs...)
	}
	c.RenderArgs["serialize.format"] = ContentText
	c.RenderArgs["Entity"] = finalText
	return c
}

// Render html in response
func (c *Context) RenderHTML(html string) *Context {
	c.RenderArgs["Entity"] = html
	c.RenderArgs["serialize.format"] = ContentHTML
	return c
}

// RenderFile returns a file, either displayed inline or downloaded
// as an attachment. The name and size are taken from the file info.
func (c *Context) RenderFile(file *os.File, delivery string) *Context {
	var (
		modtime       = time.Now()
		fileInfo, err = file.Stat()
	)
	if err != nil {
		Logger.Warn("Render file error", zap.Error(err))
	}
	if fileInfo != nil {
		modtime = fileInfo.ModTime()
	}
	return c.RenderBinary(file, filepath.Base(file.Name()), delivery, modtime)
}

func (c *Context) RenderFileFromPath(fname string, delivery string) *Context {
	file, err := os.Open(fname)
	if err != nil {
		return c.NotFound(fname)
	}
	return c.RenderFile(file, delivery)
}

// SendFile sends file for force-download to the client
// Use this instead of ServeFile to 'force-download' bigger files to the client.
func (c *Context) SendFile(file *os.File, destNameArgs ...string) *Context {
	var (
		modtime       = time.Now()
		fileInfo, err = file.Stat()
		destName      = filepath.Base(file.Name())
	)
	if err != nil {
		Logger.Warn("Send file error", zap.Error(err))
	}
	if fileInfo != nil {
		modtime = fileInfo.ModTime()
	}
	if len(destNameArgs) > 0 {
		destName = destNameArgs[0]
	}
	return c.RenderBinary(file, destName, "attachment", modtime)
}

func (c *Context) SendFileFromPath(fname string, destNameArgs ...string) *Context {
	file, err := os.Open(fname)
	if err != nil {
		return c.NotFound(fname)
	}
	return c.SendFile(file, destNameArgs...)
}

// RenderBinary is like RenderFile() except that it instead of a file on disk,
// it renders store from memory (which could be a file that has not been written,
// the output from some function, or bytes streamed from somewhere else, as long
// it implements io.Reader).  When called directly on something generated or
// streamed, modtime should mostly likely be time.Now().
func (c *Context) RenderBinary(memfile io.Reader, filename string, delivery string, modtime time.Time) *Context {
	c.Binary = &Binary{Reader: memfile, Name: filename, Delivery: delivery, Length: -1, ModTime: modtime}
	return c
}

// Redirect to an action or to a URL.
//   c.Redirect("/path")
func (c *Context) Redirect(url string, status ...int) *Context {
	c.RenderArgs["redirectURL"] = url
	if len(status) > 0 {
		c.RenderArgs["httpStatus"] = status[0]
	} else {
		c.RenderArgs["httpStatus"] = http.StatusFound
	}
	return c
}

// Todo returns an HTTP 501 Not Implemented "todo" indicating that the
// action isn't done yet.
func (c *Context) Todo() *Context {
	c.Response.Status = http.StatusNotImplemented
	c.Error = &Error{
		Status:  500,
		Title:   "TODO",
		Summary: "this action is not implemented",
	}
	return c
}

// NotFound returns an HTTP 404 Not Found response whose body is the
// formatted string of msg and objs.
func (c *Context) NotFound(msg string, objs ...interface{}) *Context {
	finalText := msg
	if len(objs) > 0 {
		finalText = fmt.Sprintf(msg, objs...)
	}
	c.Response.Status = http.StatusNotFound
	c.Error = &Error{
		Status:  404,
		Name:    "not_found",
		Title:   "Not Found",
		Summary: finalText,
	}
	return c
}

// Forbidden returns an HTTP 403 Forbidden response whose body is the
// formatted string of msg and objs.
func (c *Context) Forbidden(msg string, objs ...interface{}) *Context {
	finalText := msg
	if len(objs) > 0 {
		finalText = fmt.Sprintf(msg, objs...)
	}
	c.Response.Status = http.StatusForbidden
	c.Error = &Error{
		Status:  403,
		Name:    "forbidden",
		Title:   "Forbidden",
		Summary: finalText,
	}
	return c
}
