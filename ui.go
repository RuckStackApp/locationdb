package locationdb

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed ui/dist/* ui/dist/assets/*
var uiAssets embed.FS

func (app *App) uiHandler() http.Handler {
	sub, err := fs.Sub(uiAssets, "ui/dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	indexHTML, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") || r.URL.Path == "/healthz" {
			http.NotFound(w, r)
			return
		}
		cleanPath := path.Clean(r.URL.Path)
		if cleanPath == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(indexHTML)
			return
		}
		if _, err := fs.Stat(sub, strings.TrimPrefix(cleanPath, "/")); err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(indexHTML)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
