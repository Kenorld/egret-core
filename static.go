package egret

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/spf13/cast"
)

// This method handles requests for files. The supplied prefix may be absolute
// or relative. If the prefix is relative it is assumed to be relative to the
// application directory. The filepath may either be just a file or an
// additional filepath to search for the given file. This response may return
// the following responses in the event of an error or invalid request;
//   403(Forbidden): If the prefix filepath combination results in a directory.
//   404(Not found): If the prefix and filepath combination results in a non-existent file.
//   500(Internal Server Error): There are a few edge cases that would likely indicate some configuration error outside of egret.
//
//
// Examples:
// Serving a directory
// egret.Static(router.Path("/**"), []string{"/public"}, egret.StaticOptions{"listing": true})

// Static Options
// map[string]string{
//	indexes: "index.html,index.htm"
//	listing: false
//	stripPath: true
//}
type StaticOptions map[string]interface{}
type staticFileInfo struct {
	Name    string
	ModTime time.Time
	IsDir   bool
	Mode    os.FileMode
	Size    int64
}

func Static(zone *Zone, rootPaths []string, options ...map[string]interface{}) {
	var indexes []string
	opts := map[string]interface{}{}
	if len(options) > 0 {
		opts = options[0]
	}
	listing := cast.ToBool(opts["listing"])
	oi := opts["indexes"]
	if oi != nil {
		if ois, ok := oi.(string); ok {
			indexes = strings.Split(ois, ",")
			for i, idx := range indexes {
				indexes[i] = strings.TrimSpace(idx)
			}
		} else if ois, ok := oi.([]string); ok {
			indexes = ois
		} else {
			indexes = []string{}
			Logger.Warn("Error satic option indexes value and ignored")
		}
	}

	for i, rpath := range rootPaths {
		if !filepath.IsAbs(rpath) {
			rootPaths[i] = filepath.Join(BasePath, rpath)
		}
	}
	file, _ := opts["file"].(string)
	zone.Get(getHandler(rootPaths, indexes, listing, file))
}

func getHandler(rootPaths []string, indexes []string, listing bool, file string) HandlerFunc {
	return func(ctx *Context) {
		vpath := ""
		if file != "" {
			vpath = file
		} else {
			for key, path := range ctx.Params {
				if key[0] == '*' {
					vpath = path
					break
				}
			}
		}
		for _, rpath := range rootPaths {
			fname := filepath.Join(rpath, vpath)
			finfo, err := os.Stat(fname)
			if err != nil {
				if os.IsNotExist(err) || err.(*os.PathError).Err == syscall.ENOTDIR {
					if RunMode == "dev" {
						Logger.Warn("File not found", zap.String("path", vpath), zap.Error(err))
					}
					ctx.NotFound("File not found")
					return
				}
				Logger.Error("Error trying to get file info", zap.String("path", vpath), zap.Error(err))
				ctx.RenderError(err)
				return
			}

			if finfo.Mode().IsDir() {
				if ctx.Request.URL.Path[len(ctx.Request.URL.Path)-1] != '/' {
					ctx.Request.URL.Path += "/"
					ctx.Redirect(ctx.Request.URL.String())
					return
				}
				if !indexDir(ctx, vpath, rootPaths, indexes) {
					if listing {
						listDir(ctx, vpath, rootPaths)
						return
					}
					// Disallow directory listing
					Logger.Warn("Attempted directory listing", zap.String("path", vpath))
					ctx.Forbidden("Directory listing not allowed.")
					return
				}
				return
			}
			renderFile(ctx, fname)
			ctx.Next()
			return
		}
	}
}
func renderFile(ctx *Context, fname string) {
	// Open request file path
	file, err := os.Open(fname)
	if err != nil {
		if os.IsNotExist(err) {
			Logger.Warn("File not found", zap.String("path", fname), zap.Error(err))
			ctx.NotFound("File not found")
			return
		}
		Logger.Error("Error opening", zap.String("path", fname), zap.Error(err))
		ctx.RenderError(err)
		return
	}
	ctx.RenderFile(file, "inline")
}
func listDir(ctx *Context, webpath string, rootPaths []string) {
	if webpath == "" || webpath[0] != '/' {
		webpath = "/" + webpath
	}
	if webpath[len(webpath)-1] != '/' {
		webpath = webpath + "/"
	}
	ctx.RenderTemplate("directory-list.html", map[string]interface{}{
		"Base":  webpath,
		"Files": getDirList(webpath, rootPaths),
	}, nil)
}
func indexDir(ctx *Context, webpath string, rootPaths []string, indexes []string) bool {
	for _, rpath := range rootPaths {
		for _, idx := range indexes {
			fname := filepath.Join(rpath, webpath, idx)
			if _, err := os.Stat(fname); err == nil {
				renderFile(ctx, fname)
				return true
			}
		}
	}
	return false
}
func getDirList(webpath string, rootPaths []string) []*staticFileInfo {
	files := []*staticFileInfo{}
	for _, rpath := range rootPaths {
		fname := filepath.Join(rpath, webpath)
		infos, _ := ioutil.ReadDir(fname)
		for _, info := range infos {
			finfo := &staticFileInfo{
				Name:    info.Name(),
				ModTime: info.ModTime(),
				IsDir:   info.IsDir(),
				Mode:    info.Mode(),
				Size:    info.Size(),
			}
			files = append(files, finfo)
		}
	}
	return files
}
