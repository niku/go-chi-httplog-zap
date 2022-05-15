package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httplog "github.com/niku/go-chi-httplog-zap"
	"go.uber.org/zap"
)

func main() {
	l, _ := zap.NewProduction()

	r := chi.NewRouter()
	r.Use(httplog.ZapRequestLogger(l))
	r.Use(middleware.Recoverer)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world"))
	})

	r.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("oh no")
	})

	r.Get("/info", func(w http.ResponseWriter, r *http.Request) {
		oplog := httplog.LogEntry(r.Context())
		w.Header().Add("Content-Type", "text/plain")
		oplog.Info("info here")
		w.Write([]byte("info here"))
	})

	r.Get("/warn", func(w http.ResponseWriter, r *http.Request) {
		oplog := httplog.LogEntry(r.Context())
		oplog.Warn("warn here")
		w.WriteHeader(400)
		w.Write([]byte("warn here"))
	})

	r.Get("/err", func(w http.ResponseWriter, r *http.Request) {
		oplog := httplog.LogEntry(r.Context())
		oplog.Error("err here")
		w.WriteHeader(500)
		w.Write([]byte("err here"))
	})

	http.ListenAndServe(":5555", r)
}
