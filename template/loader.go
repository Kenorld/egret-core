package template

import (
	"errors"
	"os"
	"path/filepath"
)

var (
	// DefaultExtension the default file extension if empty setted
	DefaultExtension = ".html"
	// DefaultDirectory the default directory if empty setted
	DefaultDirectory = "." + string(os.PathSeparator) + "views"
)

type (
	// Loader contains the funcs to set the location for the templates by directory or by binary
	Loader struct {
		Directory string
		Extension string
		// AssetFunc and NamesFunc used when files are distrubuted inside the app executable
		AssetFunc func(name string) ([]byte, error)
		NamesFunc func() []string
	}
	// BinaryLoader optionally, called after TemplateLocation's Directory, used when files are distrubuted inside the app executable
	// sets the AssetFunc and NamesFunc
	BinaryLoader struct {
		*Loader
	}
)

// NewLoader returns a default Loader which is used to load template engine(s)
func NewLoader() *Loader {
	return &Loader{Directory: DefaultDirectory, Extension: DefaultExtension}
}

// Register sets the directory to load from
// returns the Binary location which is optional
func (t *Loader) Register(dir string, fileExtension string) *BinaryLoader {
	if dir == "" {
		dir = DefaultDirectory // the default templates dir
	}
	if fileExtension == "" {
		fileExtension = DefaultExtension
	} else if fileExtension[0] != '.' { // if missing the start dot
		fileExtension = "." + fileExtension
	}

	t.Directory = dir
	t.Extension = fileExtension

	return &BinaryLoader{Loader: t}
}

// Binary optionally, called after Loader.Directory, used when files are distrubuted inside the app executable
// sets the AssetFunc and NamesFunc
func (t *BinaryLoader) Binary(assetFunc func(name string) ([]byte, error), namesFunc func() []string) {
	if assetFunc == nil || namesFunc == nil {
		return
	}

	t.AssetFunc = assetFunc
	t.NamesFunc = namesFunc
	// if extension is not static(setted by .Directory)
	if t.Extension == "" {
		if names := namesFunc(); len(names) > 0 {
			t.Extension = filepath.Ext(names[0]) // we need the extension to get the correct template engine on the Render method
		}
	}
}

// IsBinary returns true if .Binary is called and AssetFunc and NamesFunc are setted
func (t *Loader) IsBinary() bool {
	return t.AssetFunc != nil && t.NamesFunc != nil
}

var errMissingDirectoryOrAssets = errors.New("Missing Directory or Assets by binary for the template engine!")

// LoadTemplate receives a template Template and calls its LoadAssets or the LoadDirectory with the loader's locations
func (t *Loader) LoadTemplate(e Template) error {
	if t.IsBinary() {
		return e.LoadAssets(t.Directory, t.Extension, t.AssetFunc, t.NamesFunc)
	} else if t.Directory != "" {
		return e.LoadDirectory(t.Directory, t.Extension)
	}
	return errMissingDirectoryOrAssets
}
