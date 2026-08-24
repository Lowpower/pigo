package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

const defaultAddr = ":8080"

func main() {
	addr := defaultAddr
	if v := os.Getenv("PIGO_ADDR"); v != "" {
		addr = v
	}

	log.Printf("pigo listening on %s", addr)
	if err := http.ListenAndServe(addr, newRouter()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// newRouter wires up the HTTP handlers so it can be reused in tests.
func newRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", indexHandler)
	mux.HandleFunc("/health", healthHandler)
	return mux
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"service": "pigo",
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>pigo</title>
  <style>
    :root { color-scheme: light dark; }
    body {
      margin: 0; min-height: 100vh; display: grid; place-items: center;
      font-family: system-ui, -apple-system, Segoe UI, Roboto, sans-serif;
      background: linear-gradient(135deg, #6366f1 0%, #8b5cf6 50%, #ec4899 100%);
      color: #fff;
    }
    .card {
      background: rgba(255,255,255,0.12); backdrop-filter: blur(8px);
      border: 1px solid rgba(255,255,255,0.25); border-radius: 18px;
      padding: 3rem 3.5rem; text-align: center; box-shadow: 0 12px 40px rgba(0,0,0,0.25);
    }
    h1 { margin: 0 0 .4rem; font-size: 3rem; letter-spacing: -1px; }
    p { margin: .25rem 0; opacity: .92; }
    code {
      background: rgba(0,0,0,0.25); padding: .15rem .45rem; border-radius: 6px;
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    }
  </style>
</head>
<body>
  <div class="card">
    <h1>pigo</h1>
    <p>A minimal Go web service.</p>
    <p>Health check: <code>GET /health</code></p>
  </div>
</body>
</html>
`
