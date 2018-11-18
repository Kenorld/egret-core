package markdown

import (
	"github.com/kenorld/blackfriday"
	"github.com/microcosm-cc/bluemonday"
)

const (
	// ContentType the custom key for the serializer, when used inside egret, Q web frameworks or simply net/http
	ContentType = "text/markdown"
)

// Serializer the serializer which renders a markdown contents as html
type Serializer struct {
	config Config
}

// New returns a new markdown serializer
func New(cfg ...Config) *Serializer {
	c := DefaultConfig().Merge(cfg)
	return &Serializer{config: c}
}

// ContentType the custom key for the serializer, when used inside egret, Q web frameworks or simply net/http
func (e *Serializer) ContentType() string {
	return ContentType
}

// Serialize accepts the 'object' value and converts it to bytes in order to be 'renderable'
// implements the go-serializer.Serializer interface
func (e *Serializer) Serialize(val interface{}, options ...map[string]interface{}) ([]byte, error) {
	var b []byte
	if s, isString := val.(string); isString {
		b = []byte(s)
	} else {
		b = val.([]byte)
	}
	buf := blackfriday.Run(b)
	if e.config.MarkdownSanitize {
		buf = bluemonday.UGCPolicy().SanitizeBytes(buf)
	}

	return buf, nil
}
