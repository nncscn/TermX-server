package web

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed all:dist
var distFS embed.FS

var zeroTime time.Time

func Register(engine *gin.Engine) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		//dist 未随构建打入（开发直跑 go build）：仅保留 API 404 语义
		engine.NoRoute(func(c *gin.Context) { apiNotFound(c) })
		return
	}
	hfs := http.FS(sub)

	engine.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api") {
			apiNotFound(c)
			return
		}
		name := strings.TrimPrefix(path, "/")
		if name == "" || !exists(hfs, name) {
			serve(hfs, c, "index.html", false)
			return
		}
		serve(hfs, c, name, true)
	})
}

func apiNotFound(c *gin.Context) {
	c.JSON(http.StatusNotFound, gin.H{"code": 40401, "message": "接口不存在", "data": nil})
}

func exists(hfs http.FileSystem, name string) bool {
	f, err := hfs.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	return err == nil && !st.IsDir()
}

func serve(hfs http.FileSystem, c *gin.Context, name string, cache bool) {
	f, err := hfs.Open(name)
	if err != nil {
		c.String(http.StatusNotFound, "资源不存在")
		return
	}
	defer f.Close()
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		c.String(http.StatusInternalServerError, "资源读取失败")
		return
	}
	if cache {
		c.Header("Cache-Control", "public, max-age=86400")
	} else {
		c.Header("Cache-Control", "no-cache")
	}
	http.ServeContent(c.Writer, c.Request, name, zeroTime, rs)
}
