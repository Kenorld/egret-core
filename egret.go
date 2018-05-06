package egret

import (
	"go/build"
	"html"
	htmpl "html/template"
	"log"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kenorld/egret-conf"
	"github.com/kenorld/egret-core/serializer"
	"github.com/kenorld/egret-core/template"
	"github.com/kenorld/egret-core/template/native"

	"github.com/sirupsen/logrus"
)

const (
	// EgretCoreImportPath egret core improt path
	EgretCoreImportPath   = "github.com/kenorld/egret-core"
	DefaultDateFormat     = "2006-01-02"
	DefaultDateTimeFormat = "2006-01-02 15:04"
)

var invalidSlugPattern = regexp.MustCompile(`[^a-z0-9 _-]`)
var whiteSpacePattern = regexp.MustCompile(`\s+`)
var Logger = logrus.StandardLogger()

var (
	AppName     string // e.g. "sample"
	AppRoot     string // e.g. "/app1"
	BasePath    string // e.g. "/Users/user/gocode/src/corp/sample"
	AppCorePath string // e.g. "/Users/user/gocode/src/corp/sample/app"
	ImportPath  string // e.g. "corp/sample"
	SourcePath  string // e.g. "/Users/user/gocode/src"

	Config  *conf.Context
	RunMode string // Application-defined (by default, "dev" or "prod")
	DevMode bool   // if true, RunMode is a development mode.

	// Egret installation details
	EgretPath string // e.g. "/Users/user/gocode/src/egret-core"

	// Where to look for templates
	// Ordered by priority. (Earlier paths take precedence over later paths.)
	CodePaths     []string
	TemplatePaths []string

	// ConfPaths where to look for configurations
	// Config load order
	// 1. framework (egret/conf/*)
	// 2. application (conf/*)
	// 3. user supplied configs (...) - User configs can override/add any from above
	ConfPaths []string

	Modules []Module

	// Server config.
	//
	// Alert: This is how the app is configured, which may be different from
	// the current process reality.  For example, if the app is configured for
	// port 9000, HttpPort will always be 9000, even though in dev mode it is
	// run on a random port and proxied.
	HttpNetwork           string
	HttpPort              int    // e.g. 9000
	HttpAddr              string // e.g. "", "127.0.0.1"
	HttpTLSEnabled        bool   // e.g. true if using ssl
	HttpTLSCert           string // e.g. "/path/to/cert.pem"
	HttpTLSKey            string // e.g. "/path/to/key.pem"
	HttpTLSLetsEncrypt    bool
	HttpTLSLetsEncryptDir string
	UnixFileMode          os.FileMode

	// All cookies dropped by the framework begin with this prefix.
	CookiePrefix string
	// Cookie domain
	CookieDomain string
	// Cookie flags
	CookieSecure bool

	// Delimiters to use when rendering templates
	TemplateDelims string

	Initialized bool

	// Private
	SecretKey      []byte // Key used to sign cookies. An empty key disables signing.
	packaged       bool   // If true, this is running from a pre-built package.
	DateTimeFormat string
	DateFormat     string

	// MainWatcher for the whole project
	MainWatcher *Watcher
	// MainTemplateManager for the whole project
	MainTemplateManager *template.Manager
	// MainSerializerManager for the whole project
	MainSerializerManager *serializer.Manager

	SharedTemplateFunc = map[string]interface{}{
		"url": ReverseURL,
		// Format a date according to the application's default date(time) format.
		"date": func(date time.Time) string {
			return date.Format(DateFormat)
		},
		"datetime": func(date time.Time) string {
			return date.Format(DateTimeFormat)
		},

		"set": func(renderArgs map[string]interface{}, key string, value interface{}) htmpl.JS {
			renderArgs[key] = value
			return htmpl.JS("")
		},
		"append": func(renderArgs map[string]interface{}, key string, value interface{}) htmpl.JS {
			if renderArgs[key] == nil {
				renderArgs[key] = []interface{}{value}
			} else {
				renderArgs[key] = append(renderArgs[key].([]interface{}), value)
			}
			return htmpl.JS("")
		},
		"firstof": func(args ...interface{}) interface{} {
			for _, val := range args {
				switch val.(type) {
				case nil:
					continue
				case string:
					if val == "" {
						continue
					}
					return val
				default:
					return val
				}
			}
			return nil
		},
		// Pads the given string with &nbsp;'s up to the given width.
		"pad": func(str string, width int) htmpl.HTML {
			if len(str) >= width {
				return htmpl.HTML(html.EscapeString(str))
			}
			return htmpl.HTML(html.EscapeString(str) + strings.Repeat("&nbsp;", width-len(str)))
		},

		// "msg": func(renderArgs map[string]interface{}, message string, args ...interface{}) htmpl.HTML {
		// 	str, ok := renderArgs[CurrentLocaleRenderArg].(string)
		// 	if !ok {
		// 		return ""
		// 	}
		// 	return htmpl.HTML(MessageFunc(str, message, args...))
		// },

		// Replaces newlines with <br>
		"nl2br": func(text string) htmpl.HTML {
			return htmpl.HTML(strings.Replace(htmpl.HTMLEscapeString(text), "\n", "<br>", -1))
		},

		// Skips sanitation on the parameter.  Do not use with dynamic data.
		"raw": func(text string) htmpl.HTML {
			return htmpl.HTML(text)
		},

		// Pluralize, a helper for pluralizing words to correspond to data of dynamic length.
		// items - a slice of items, or an integer indicating how many items there are.
		// pluralOverrides - optional arguments specifying the output in the
		//     singular and plural cases.  by default "" and "s"
		"pluralize": func(items interface{}, pluralOverrides ...string) string {
			singular, plural := "", "s"
			if len(pluralOverrides) >= 1 {
				singular = pluralOverrides[0]
				if len(pluralOverrides) == 2 {
					plural = pluralOverrides[1]
				}
			}

			switch v := reflect.ValueOf(items); v.Kind() {
			case reflect.Int:
				if items.(int) != 1 {
					return plural
				}
			case reflect.Slice:
				if v.Len() != 1 {
					return plural
				}
			default:
				logrus.Error("pluralize: unexpected type: ", v)
			}
			return singular
		},

		"slug": func(text string) string {
			separator := "-"
			text = strings.ToLower(text)
			text = invalidSlugPattern.ReplaceAllString(text, "")
			text = whiteSpacePattern.ReplaceAllString(text, separator)
			text = strings.Trim(text, separator)
			return text
		},
		"even": func(a int) bool { return (a % 2) == 0 },
	}
)

// Init initializes Egret -- it provides paths for getting around the app.
//
// Params:
//   mode - the run mode, which determines which app.yaml settings are used.
//   importPath - the Go import path of the application.
//   srcPath - the path to the source directory, containing Egret and the app.
//     If not specified (""), then a functioning Go installation is required.
func Init(mode, importPath, srcPath string) {
	// Ignore trailing slashes.
	ImportPath = strings.TrimRight(importPath, "/")
	SourcePath = srcPath
	RunMode = mode

	// If the SourcePath is not specified, find it using build.Import.
	var egretSourcePath string // may be different from the app source path
	if SourcePath == "" {
		egretSourcePath, SourcePath = findSrcPaths(importPath)
	} else {
		// If the SourcePath was specified, assume both Egret and the app are within it.
		SourcePath = path.Clean(SourcePath)
		egretSourcePath = SourcePath
		packaged = true
	}

	EgretPath = filepath.Join(egretSourcePath, filepath.FromSlash(EgretCoreImportPath))
	BasePath = filepath.Join(SourcePath, filepath.FromSlash(importPath))
	AppCorePath = filepath.Join(BasePath, "core")

	CodePaths = []string{AppCorePath}

	if ConfPaths == nil {
		ConfPaths = []string{}
	}

	// Config load order
	// 1. framework (egret/conf/*)
	// 2. application (conf/*)
	// 3. user supplied configs (...) - User configs can override/add any from above
	ConfPaths = append(
		[]string{
			filepath.Join(BasePath, "conf"),
			filepath.Join(EgretPath, "conf"),
		},
		ConfPaths...)

	TemplatePaths = []string{
		filepath.Join(AppCorePath, "views"),
		path.Join(EgretPath, "views"),
	} // Load app.yaml
	var err error

	Config, err = conf.LoadContext("app", ConfPaths)
	if err != nil || Config == nil {
		log.Fatalln("Failed to load app.yaml:", err)
	}

	// if !Config.IsSet(mode) {
	// 	log.Fatalln("app.yaml: No mode found:", mode)
	// }
	Config.SetSection(mode)

	initLog()

	// Configure properties from app.yaml
	DevMode = Config.GetBoolDefault("dev_mode", false)
	UnixFileMode = os.FileMode(Config.GetIntDefault("unix_file_mode", 0666))
	HttpNetwork = Config.GetStringDefault("serve.network", "tcp")
	HttpPort = Config.GetIntDefault("serve.port", 9000)
	HttpAddr = Config.GetStringDefault("serve.addr", "")
	HttpTLSEnabled = Config.GetBoolDefault("serve.tls.enabled", false)
	HttpTLSCert = Config.GetStringDefault("serve.tls.cert", "")
	HttpTLSKey = Config.GetStringDefault("serve.tls.key", "")
	HttpTLSLetsEncrypt = Config.GetBoolDefault("serve.letsencrypt.enabled", false)
	HttpTLSLetsEncryptDir = Config.GetStringDefault("serve.letsencrypt.cache_dir", "")
	if HttpTLSEnabled && !HttpTLSLetsEncrypt {
		if HttpTLSCert == "" {
			log.Fatalln("No serve.tls.cert provided.")
		}
		if HttpTLSKey == "" {
			log.Fatalln("No serve.tls.key provided.")
		}
	}

	AppName = Config.GetStringDefault("name", "(not set)")
	AppRoot = Config.GetStringDefault("root", "")
	CookiePrefix = Config.GetStringDefault("cookie.prefix", "EGRET")
	CookieDomain = Config.GetStringDefault("cookie.domain", "")
	CookieSecure = Config.GetBoolDefault("cookie.secure", !DevMode)
	TemplateDelims = Config.GetStringDefault("template.delimiters", "")
	if secretStr := Config.GetStringDefault("secret", ""); secretStr != "" {
		SecretKey = []byte(secretStr)
	}

	DateTimeFormat = Config.GetStringDefault("format.datetime", DefaultDateTimeFormat)
	DateFormat = Config.GetStringDefault("format.date", DefaultDateFormat)

	// Initialized = true
	logrus.WithFields(logrus.Fields{
		"Version":          Version,
		"BuildDate":        BuildDate,
		"MinimumGoVersion": MinimumGoVersion,
	}).Info("Initialized Egret.")

	initTemplate()
	initSerializer()

	Initialized = true
	runStartupHooks()
}
func initTemplate() {
	MainTemplateManager = template.NewManager(SharedTemplateFunc)

	if Config.GetBoolDefault("template.native.enabled", false) {
		bpath := Config.GetStringDefault(filepath.Join(BasePath, "template.native.root"), filepath.Join(AppCorePath, "views"))
		cfg := native.Config{Layout: Config.GetStringDefault("template.native.layout", template.NoLayout)}
		tmpl := native.New(cfg)
		UseTemplate(tmpl).Register(bpath, ".html")
		tmpl = native.New(cfg)
		UseTemplate(tmpl).Register(bpath, ".json")
		tmpl = native.New(cfg)
		UseTemplate(tmpl).Register(bpath, ".xml")
		tmpl = native.New(cfg)
		UseTemplate(tmpl).Register(bpath, ".txt")

		bpath = filepath.Join(EgretPath, "views")
		cfg = native.Config{Layout: template.NoLayout}
		tmpl = native.New(cfg)
		UseTemplate(tmpl).Register(bpath, ".html")
		tmpl = native.New(cfg)
		UseTemplate(tmpl).Register(bpath, ".json")
		tmpl = native.New(cfg)
		UseTemplate(tmpl).Register(bpath, ".xml")
		tmpl = native.New(cfg)
		UseTemplate(tmpl).Register(bpath, ".txt")
	}
	MainTemplateManager.Refresh()
}

func initSerializer() {
	MainSerializerManager = serializer.NewManager()
	serializer.RegisterDefaults(MainSerializerManager)
}

func initLog() {
	level := Config.GetStringDefault("log.level", "warning")
	output := Config.GetStringDefault("log.output", "stderr")
	format := Config.GetStringDefault("log.format", "json")

	lvlnum, err := logrus.ParseLevel(level)
	if err != nil {
		lvlnum = logrus.WarnLevel
	}
	logrus.SetLevel(lvlnum)

	if format == "json" {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	} else if format == "text" || format == "txt" {
		logrus.SetFormatter(&logrus.TextFormatter{ForceColors: false})
	}
	if output == "stdout" || output == "stderr" {
		logrus.SetOutput(os.Stdout)
	} else {
		logFile, err := CreateFile(output)
		if err != nil {
			logrus.Warnf("Couldn't open log-file, falling back to stdout: %s", err)
		} else {
			logrus.SetOutput(logFile)
		}
	}
}

// findSrcPaths uses the "go/build" package to find the source root for Egret
// and the app.
func findSrcPaths(importPath string) (egretSourcePath, appSourcePath string) {
	var (
		gopaths = filepath.SplitList(build.Default.GOPATH)
		goroot  = build.Default.GOROOT
	)

	if len(gopaths) == 0 {
		logrus.Fatal("GOPATH environment variable is not set. Please refer to http://golang.org/doc/code.html to configure your Go environment.")
	}

	if ContainsString(gopaths, goroot) {
		logrus.WithFields(logrus.Fields{
			"GOPATH": gopaths,
			"GOROOT": goroot,
		}).Fatal("GOPATH must not include your GOROOT. Please refer to http://golang.org/doc/code.html to configure your Go environment.")
	}

	appPkg, err := build.Import(importPath, "", build.FindOnly)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"importPath": importPath,
			"error":      err,
		}).Fatal("Failed to import.")
	}

	egretPkg, err := build.Import(EgretCoreImportPath, "", build.FindOnly)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"error": err,
		}).Fatal("Failed to find Egret.")
	}

	return egretPkg.SrcRoot, appPkg.SrcRoot
}

type Module struct {
	Name, ImportPath, Path string
}

func loadModules() {
	for _, key := range Config.GetStringMapString("module") {
		moduleImportPath := Config.GetStringDefault(key, "")
		if moduleImportPath == "" {
			continue
		}

		modulePath, err := ResolveImportPath(moduleImportPath)
		if err != nil {
			log.Fatalln("Failed to load module.  Import of", moduleImportPath, "failed:", err)
		}
		addModule(key[len("module."):], moduleImportPath, modulePath)
	}
}

func UseTemplate(tmpl template.Template) *template.Loader {
	return MainTemplateManager.AddTemplate(tmpl)
}

// ResolveImportPath returns the filesystem path for the given import path.
// Returns an error if the import path could not be found.
func ResolveImportPath(importPath string) (string, error) {
	if packaged {
		return path.Join(SourcePath, importPath), nil
	}

	modPkg, err := build.Import(importPath, "", build.FindOnly)
	if err != nil {
		return "", err
	}
	return modPkg.Dir, nil
}

func addModule(name, importPath, modulePath string) {
	Modules = append(Modules, Module{Name: name, ImportPath: importPath, Path: modulePath})
	if codePath := filepath.Join(modulePath, "core"); DirExists(codePath) {
		CodePaths = append(CodePaths, codePath)
		if viewsPath := filepath.Join(modulePath, "core", "views"); DirExists(viewsPath) {
			TemplatePaths = append(TemplatePaths, viewsPath)
		}
	}

	logrus.Info("Loaded module: " + filepath.Base(modulePath))

	// Hack: There is presently no way for the testrunner module to add the
	// "test" subdirectory to the CodePaths.  So this does it instead.
	// if importPath == Config.StringDefault("module.testrunner", "github.com/egret/modules/testrunner") {
	// 	CodePaths = append(CodePaths, filepath.Join(BasePath, "tests"))
	// }
}

// ModuleByName returns the module of the given name, if loaded.
func ModuleByName(name string) (m Module, found bool) {
	for _, module := range Modules {
		if module.Name == name {
			return module, true
		}
	}
	return Module{}, false
}

func CheckInit() {
	if !Initialized {
		panic("Egret has not been initialized!")
	}
}

// Register a function to be run at app startup.
//
// The order you register the functions will be the order they are run.
// You can think of it as a FIFO queue.
// This process will happen after the config file is read
// and before the server is listening for connections.
//
// Ideally, your application should have only one call to init() in the file init.go.
// The reason being that the call order of multiple init() functions in
// the same package is undefined.
// Inside of init() call egret.OnAppStart() for each function you wish to register.
//
// Example:
//
//      // from: yourapp/core/routes/somefile.go
//      func InitDB() {
//          // do DB connection stuff here
//      }
//
//      func FillCache() {
//          // fill a cache from DB
//          // this depends on InitDB having been run
//      }
//
//      // from: yourapp/app/init.go
//      func init() {
//          // set up filters...
//
//          // register startup functions
//          egret.OnAppStart(InitDB)
//          egret.OnAppStart(FillCache)
//      }
//
// This can be useful when you need to establish connections to databases or third-party services,
// setup app components, compile assets, or any thing you need to do between starting Egret and accepting connections.
//
func OnAppStart(f func(), order ...int) {
	o := 1
	if len(order) > 0 {
		o = order[0]
	}
	startupHooks = append(startupHooks, StartupHook{order: o, f: f})
}

func runStartupHooks() {
	sort.Sort(startupHooks)
	for _, hook := range startupHooks {
		hook.f()
	}
}

// StartupHook struct
type StartupHook struct {
	order int
	f     func()
}

type StartupHooks []StartupHook

var startupHooks StartupHooks

func (slice StartupHooks) Len() int {
	return len(slice)
}

func (slice StartupHooks) Less(i, j int) bool {
	return slice[i].order < slice[j].order
}

func (slice StartupHooks) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}
