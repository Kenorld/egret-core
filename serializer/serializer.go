// Package serializer helps GoLang Developers to serialize any custom type to []byte or string.
// Your custom serializers are finally, organised.
//
// Built'n supported serializers: JSON, JSONP, XML, Markdown, Text, Binary Data.
//
// This package is already used by Iris & Q Web Frameworks.
package serializer

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/kenorld/eject-core/serializer/data"
	"github.com/kenorld/eject-core/serializer/json"
	"github.com/kenorld/eject-core/serializer/jsonp"
	"github.com/kenorld/eject-core/serializer/markdown"
	"github.com/kenorld/eject-core/serializer/text"
	"github.com/kenorld/eject-core/serializer/xml"
)

type (
	// Serializer is the interface which all serializers should implement
	Serializer interface {
		ContentType() string
		// Serialize accepts an object with serialization options and returns its bytes representation
		Serialize(interface{}, ...map[string]interface{}) ([]byte, error)
	}
	// SerializeFunc is the alternative way to implement a Serializer using a simple function
	SerializeFunc func(interface{}, ...map[string]interface{}) ([]byte, error)
)

// Serialize accepts an object with serialization options and returns its bytes representation
func (s SerializeFunc) Serialize(obj interface{}, options ...map[string]interface{}) ([]byte, error) {
	return s(obj, options...)
}

// NotAllowedKeyChar the rune which is not allowed to be inside a serializer key string
// this exists because almost all package's users will use kataras/go-template with kataras/go-serializer
// in one method, so we need something to tell if the 'renderer' wants to render a
// serializer's result or the template's result, you don't have to worry about these things.
const NotAllowedKeyChar = '.'

// Manager is optionally, used when your app needs to manage more than one serializer
// keeps a map with a key of the ContentType(or any string) and a collection of Manager
//
// if a ContentType(key) has more than one serializer registered to it then the final result will be all of the serializer's results combined
type Manager map[string][]Serializer

//NewManager create new Manager
func NewManager() *Manager {
	return &Manager{}
}

// For puts a serializer(s) to the map
func (s Manager) For(key string, serializer ...Serializer) {
	if s == nil {
		s = make(map[string][]Serializer)
	}

	if strings.IndexByte(key, NotAllowedKeyChar) != -1 {
		return
	}

	if s[key] == nil {
		s[key] = make([]Serializer, 0)
	}

	s[key] = append(s[key], serializer...)
}

var (
	errKeyMissing   = errors.New("Please specify a key")
	errManagerEmpty = errors.New("Manager map is empty")
)

var (
	once               sync.Once
	defaultManagerKeys = [...]string{json.ContentType, jsonp.ContentType, xml.ContentType, markdown.ContentType, text.ContentType, data.ContentType}
)

// RegisterDefaults register defaults serializer for each of the default serializer keys (data,json,jsonp,markdown,text,xml)
func RegisterDefaults(serializers *Manager) {
	for _, ctype := range defaultManagerKeys {

		if sers := (*serializers)[ctype]; sers == nil || len(sers) == 0 {
			// if not exists
			switch ctype {
			case json.ContentType:
				serializers.For(ctype, json.New())
			case jsonp.ContentType:
				serializers.For(ctype, jsonp.New())
			case xml.ContentType:
				serializers.For(ctype, xml.New())
			case markdown.ContentType:
				serializers.For(ctype, markdown.New())
			case text.ContentType:
				serializers.For(ctype, text.New())
			case data.ContentType:
				serializers.For(ctype, data.New())
			}
		}
	}
}

// Serialize returns the result as bytes representation of the serializer(s)
func (s Manager) Serialize(key string, obj interface{}, options map[string]interface{}) ([]byte, error) {
	if key == "" {
		return nil, errKeyMissing
	}
	if s == nil {
		return nil, errManagerEmpty
	}

	serializers := s[key]
	if serializers == nil {
		return nil, fmt.Errorf("Serializer with key %s couldn't be found", key)
	}
	var finalResult []byte

	for i, n := 0, len(serializers); i < n; i++ {
		result, err := serializers[i].Serialize(obj, options)
		if err != nil {
			return nil, err
		}
		finalResult = append(finalResult, result...)
	}
	return finalResult, nil
}

// SerializeToString returns the string representation of the serializer(s)
// same as Serialize but returns string
func (s Manager) SerializeToString(key string, obj interface{}, options map[string]interface{}) (string, error) {
	result, err := s.Serialize(key, obj, options)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

// Len returns the length of the serializers map
func (s Manager) Len() int {
	if s == nil {
		return 0
	}

	return len(s)
}

// Options is just a shortcut of a map[string]interface{}, which can be passed to the Serialize/SerializeToString funcs
type Options map[string]interface{}
