package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist recebe o build da página (web/build, copiado pelo `make web`). No
// repositório só tem o .gitkeep: os testes de Go não precisam do Node.
//
//go:embed all:dist
var dist embed.FS

// spaFallback é a página que o adapter-static gera para as rotas da SPA.
const spaFallback = "200.html"

// notBuilt é mostrado quando o binário foi compilado sem a página.
const notBuilt = `<!doctype html><meta charset="utf-8"><title>dotboot</title>
<body style="font-family:system-ui;background:#0b0d12;color:#e6e8ee;display:grid;place-items:center;height:100vh;margin:0">
<div><h1>dotboot</h1><p>A página não foi compilada neste binário: corre <code>make web</code> e volta a compilar
(ou usa <code>make build</code>, que faz as duas coisas). A API está em <code>/api</code>.</p></div>`

// staticHandler serve a página. Os caminhos que não são ficheiros caem no
// 200.html, e o router do SvelteKit trata deles no browser.
func staticHandler() http.Handler {
	app, err := fs.Sub(dist, "dist/app")
	if err != nil {
		panic(err) // "dist/app" é um caminho válido; o Sub só falha com um inválido
	}
	files := http.FileServerFS(app)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if info, err := fs.Stat(app, name); name != "" && err == nil && !info.IsDir() {
			// Os ficheiros de _app/immutable têm um hash no nome: nunca mudam.
			if strings.HasPrefix(name, "_app/immutable/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		page, err := fs.ReadFile(app, spaFallback)
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(notBuilt)) // o cliente pode ter desligado
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(page) // idem
	})
}
