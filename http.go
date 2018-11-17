package egret

import (
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/net/websocket"
)

const (
	// ContentType represents the header["Content-Type"]
	ContentType = "Content-Type"
	// ContentLength represents the header["Content-Length"]
	ContentLength = "Content-Length"
	// ContentEncodingHeader represents the header["Content-Encoding"]
	ContentEncodingHeader = "Content-Encoding"
	// VaryHeader represents the header "Vary"
	VaryHeader = "Vary"
	// AcceptEncodingHeader represents the header key & value "Accept-Encoding"
	AcceptEncodingHeader = "Accept-Encoding"
	// ContentHTML is the  string of text/html response headers
	ContentHTML = "text/html"
	// ContentBinary header value for binary data.
	ContentBinary = "application/octet-stream"
	// ContentJSON header value for JSON data.
	ContentJSON = "application/json"
	// ContentJavascript header value for Javascript/JSONP
	// conversional
	ContentJavascript = "application/javascript"
	// ContentText header value for Text data.
	ContentText = "text/plain"
	// ContentXML header value for XML data.
	ContentXML = "text/xml"

	// contentMarkdown custom key/content type, the real is the text/html
	ContentMarkdown = "text/markdown"
)

type Request struct {
	*http.Request
	ContentType     string
	Format          string // "html", "xml", "json", or "txt"
	AcceptLanguages AcceptLanguages
	Locale          string
	Websocket       *websocket.Conn
}

type Response struct {
	Status      int
	ContentType string

	Writer http.ResponseWriter

	headerWrited bool
}

func NewResponse(w http.ResponseWriter) *Response {
	return &Response{Status: http.StatusOK, Writer: w, headerWrited: false}
}

func NewRequest(r *http.Request) *Request {
	return &Request{
		Request:         r,
		ContentType:     ResolveContentType(r),
		Format:          ResolveFormat(r),
		AcceptLanguages: ResolveAcceptLanguage(r),
	}
}

// Write the header (for now, just the status code).
// The status may be set directly by the application (c.Response.Status = 501).
// if it isn't, then fall back to the provided status code.
// func (resp *Response) WriteHeader(defaultStatusCode int, defaultFormat string) {
// 	if resp.Status == 0 {
// 		resp.Status = defaultStatusCode
// 	}
// 	if resp.ContentType == "" {
// 		resp.SetFormat(defaultFormat)
// 	} else {
// 		resp.SetContentType(resp.ContentType)
// 	}
// 	resp.SetStatusCode(resp.Status)
// }

// func (resp *Response) SetStatusCode(code int) {
// 	fmt.Println("=======code:::", code)
// 	resp.Writer.WriteHeader(resp.Status)
// }

// SetHeader write to the response writer's header to a given key the given value(s)
//
// Note: If you want to send a multi-line string as header's value use: strings.TrimSpace first.
func (resp *Response) SetHeader(k string, v string) {
	resp.Writer.Header().Set(k, v)
}

func (resp *Response) Header() http.Header {
	return resp.Writer.Header()
}

// SetContentType sets the response writer's header key 'Content-Type' to a given value(s)
// func (resp *Response) SetContentType(s string) {
// 	resp.Writer.Header().Set("Content-Type", s)
// }
func (resp *Response) SetFormat(s string) {
	if s == "" {
		s = "html"
	}
	cs, cc := strings.Contains(s, "/"), strings.Contains(s, ";")
	if cs && cc {
		resp.ContentType = s
	}
	if !cs {
		s = strings.TrimSpace(strings.Split(s, ";")[0])
		switch s {
		case "html":
			s = "text/html"
		case "javascript":
			s = "application/javascript"
		case "json":
			s = "application/json"
		case "text":
			s = "text/plain"
		case "txt":
			s = "text/plain"
		}
	}
	if !cc {
		s += "; charset=utf-8"
	}
	resp.ContentType = s
}

func (resp *Response) EnsureHeaderWrited() {
	if !resp.headerWrited {
		resp.headerWrited = true
		resp.SetHeader(ContentType, resp.ContentType)
		resp.Writer.WriteHeader(resp.Status)
	}
}
func (resp *Response) Write(data []byte) (int, error) {
	resp.EnsureHeaderWrited()
	return resp.Writer.Write(data)
}

// Get the content type.
// e.g. From "multipart/form-data; boundary=--" to "multipart/form-data"
// If none is specified, returns "text/html" by default.
func ResolveContentType(req *http.Request) string {
	contentType := req.Header.Get(ContentType)
	if contentType == "" {
		return "text/html"
	}
	return strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
}

// ResolveFormat maps the request's Accept MIME type declaration to
// a Request.Format attribute, specifically "html", "xml", "json", or "txt",
// returning a default of "html" when Accept header cannot be mapped to a
// value above.
func ResolveFormat(req *http.Request) string {
	accept := req.Header.Get("accept")

	switch {
	case accept == "",
		strings.HasPrefix(accept, "*/*"), // */
		strings.Contains(accept, "application/xhtml"),
		strings.Contains(accept, "text/html"):
		return "html"
	case strings.Contains(accept, "application/json"),
		strings.Contains(accept, "text/javascript"),
		strings.Contains(accept, "application/javascript"):
		return "json"
	case strings.Contains(accept, "application/xml"),
		strings.Contains(accept, "text/xml"):
		return "xml"
	case strings.Contains(accept, "text/plain"):
		return "txt"
	}

	return "html"
}

// AcceptLanguage is a single language from the Accept-Language HTTP header.
type AcceptLanguage struct {
	Language string
	Quality  float32
}

// AcceptLanguages is collection of sortable AcceptLanguage instances.
type AcceptLanguages []AcceptLanguage

func (al AcceptLanguages) Len() int           { return len(al) }
func (al AcceptLanguages) Swap(i, j int)      { al[i], al[j] = al[j], al[i] }
func (al AcceptLanguages) Less(i, j int) bool { return al[i].Quality > al[j].Quality }
func (al AcceptLanguages) String() string {
	output := bytes.NewBufferString("")
	for i, language := range al {
		output.WriteString(fmt.Sprintf("%s (%1.1f)", language.Language, language.Quality))
		if i != len(al)-1 {
			output.WriteString(", ")
		}
	}
	return output.String()
}

// ResolveAcceptLanguage returns a sorted list of Accept-Language
// header values.
//
// The results are sorted using the quality defined in the header for each
// language range with the most qualified language range as the first
// element in the slice.
//
// See the HTTP header fields specification
// (http://www.w3.org/Protocols/rfc2616/rfc2616-sec14.html#sec14.4) for more details.
func ResolveAcceptLanguage(req *http.Request) AcceptLanguages {
	header := req.Header.Get("Accept-Language")
	if header == "" {
		return nil
	}

	acceptLanguageHeaderValues := strings.Split(header, ",")
	acceptLanguages := make(AcceptLanguages, len(acceptLanguageHeaderValues))

	for i, languageRange := range acceptLanguageHeaderValues {
		if qualifiedRange := strings.Split(languageRange, ";q="); len(qualifiedRange) == 2 {
			quality, error := strconv.ParseFloat(qualifiedRange[1], 32)
			if error != nil {
				Logger.Warn("Detected malformed Accept-Language header quality, assuming quality is 1", zap.String("language_range", languageRange))
				acceptLanguages[i] = AcceptLanguage{qualifiedRange[0], 1}
			} else {
				acceptLanguages[i] = AcceptLanguage{qualifiedRange[0], float32(quality)}
			}
		} else {
			acceptLanguages[i] = AcceptLanguage{languageRange, 1}
		}
	}

	sort.Sort(acceptLanguages)
	return acceptLanguages
}
