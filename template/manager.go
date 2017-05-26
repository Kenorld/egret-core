package template

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/valyala/bytebufferpool"
)

type (
	// Entries the template Templates with their loader
	Entries []*Entry

	// Entry contains a template Template and its Loader
	Entry struct {
		Loader   *Loader
		Template Template
	}

	// Manager is an optional feature, used when you want to use multiple template engines
	// It stores the loaders with each of the template engine,
	// the identifier of each template engine is the (loader's) Extension
	// the registry finds the correct template engine and executes the template
	// so you can use and render a template file by it's file extension
	Manager struct {
		// Reload reloads the template engine on each execute, used when the project is under development status
		// if true the template will reflect the runtime template files changes
		// defaults to false
		// Reload bool

		// Entries the template Templates with their loader
		Entries Entries
		// SharedFuncs funcs that will be shared all over the supported template engines
		SharedFuncs map[string]interface{}

		buffer *bytebufferpool.Pool
	}
)

// LoadTemplate loads the Template using its registered loader
// Internal Note:
// Loader can be used without a mux because of this we have this type of function here which just pass itself's field into other itself's field
// which, normally, is not a smart choice.
func (entry *Entry) LoadTemplate() error {
	return entry.Loader.LoadTemplate(entry.Template)
}

// LoadAll loads all template engines entries, returns the first error
func (entries Entries) LoadAll() error {
	for i, n := 0, len(entries); i < n; i++ {
		if err := entries[i].LoadTemplate(); err != nil {
			return err
		}
	}
	return nil
}

// Find receives a filename, gets its extension and returns the template engine responsible for that file extension
func (entries Entries) Find(filename string) *Entry {
	extension := filepath.Ext(filename)
	// Read-Only no locks needed, at serve/runtime-time the library is not supposed to add new template engines
	for i, n := 0, len(entries); i < n; i++ {
		e := entries[i]
		if e.Loader.Extension == extension {
			if _, err := os.Stat(filepath.Join(e.Loader.Directory, filename)); err == nil {
				return e
			}
		}
	}
	return nil
}

// NewManager returns a new Manager
// Manager is an optional feature, used when you want to use multiple template engines
// It stores the loaders with each of the template engine,
// the identifier of each template engine is the (loader's) Extension
// the registry finds the correct template engine and executes the template
// so you can use and render a template file by it's file extension
func NewManager(sharedFuncs map[string]interface{}) *Manager {
	m := &Manager{ /*Reload: false, */ Entries: Entries{}, buffer: &bytebufferpool.Pool{}}
	m.SharedFuncs = sharedFuncs
	return m
}

// AddTemplate adds but not loads a template engine, returns the entry's Loader
func (m *Manager) AddTemplate(e Template) *Loader {
	// add the shared  funcs
	if funcer, ok := e.(TemplateFuncs); ok {
		if funcer.Funcs() != nil && m.SharedFuncs != nil {
			for k, v := range m.SharedFuncs {
				funcer.Funcs()[k] = v
			}
		}
	}
	entry := &Entry{Template: e, Loader: NewLoader()}
	m.Entries = append(m.Entries, entry)
	// returns the entry's Loader(pointer)
	return entry.Loader
}

// Load loads all template engines entries, returns the first error
// it just calls and returns the Entries.LoadALl
func (m *Manager) Load() error {
	return m.Entries.LoadAll()
}

func (m *Manager) Refresh() error {
	for _, entry := range m.Entries {
		entry.LoadTemplate()
	}
	return nil
}

// ExecuteWriter calls the correct template Template's ExecuteWriter func
func (m *Manager) ExecuteWriter(out io.Writer, name string, binding interface{}, options map[string]interface{}) (err error) {
	if m == nil {
		//file extension, but no template engine registered
		return fmt.Errorf("No template engine found for '%s'", filepath.Ext(name))
	}

	entry := m.Entries.Find(name)
	if entry == nil {
		return fmt.Errorf("Template %s was not found", name)
	}

	// if m.Reload {
	// 	if err = entry.LoadTemplate(); err != nil {
	// 		return
	// 	}
	// }

	return entry.Template.ExecuteWriter(out, name, binding, options)
}

// ExecuteString executes a template from a specific template engine and returns its contents result as string, it doesn't renders
func (m *Manager) ExecuteString(name string, binding interface{}, options map[string]interface{}) (result string, err error) {
	out := m.buffer.Get()
	defer m.buffer.Put(out)
	err = m.ExecuteWriter(out, name, binding, options)
	if err == nil {
		result = out.String()
	}
	return
}

var errNoTemplateTemplateSupportsRawParsing = errors.New("Not found a valid template engine found which supports raw parser")

// ExecuteRaw read moreon template.go:TemplateRawParser
// parse with the first valid TemplateRawParser
func (m *Manager) ExecuteRaw(src string, wr io.Writer, binding interface{}) error {
	if m == nil {
		//file extension, but no template engine registered
		return fmt.Errorf("No template engine found for '%s'", src)
	}

	for _, e := range m.Entries {
		if p, is := e.Template.(TemplateRawExecutor); is {
			return p.ExecuteRaw(src, wr, binding)
		}
	}
	return errNoTemplateTemplateSupportsRawParsing
}

// ExecuteRawString receives raw template source contents and returns it's result as string
func (m *Manager) ExecuteRawString(src string, binding interface{}) (result string, err error) {
	out := m.buffer.Get()
	defer m.buffer.Put(out)
	err = m.ExecuteRaw(src, out, binding)
	if err == nil {
		result = out.String()
	}
	return
}
