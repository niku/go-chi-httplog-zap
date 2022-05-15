package httplog

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func LogEntry(ctx context.Context) zap.SugaredLogger {
	raw := RawLogEntry(ctx)
	return *raw.Sugar()
}

func RawLogEntry(ctx context.Context) zap.Logger {
	entry, ok := ctx.Value(middleware.LogEntryCtxKey).(*zapLogEntry)
	if !ok || entry == nil {
		return *zap.NewNop()
	} else {
		return *entry.Logger
	}
}

func LogEntrySetField(ctx context.Context, key string, value interface{}) {
	if entry, ok := ctx.Value(middleware.LogEntryCtxKey).(*zapLogEntry); ok {
		entry.Logger = entry.Logger.With(zap.Reflect(key, value))
	}
}

func LogEntrySetFields(ctx context.Context, fields map[string]interface{}) {
	if entry, ok := ctx.Value(middleware.LogEntryCtxKey).(*zapLogEntry); ok {
		for k, v := range fields {
			entry.Logger = entry.Logger.With(zap.Reflect(k, v))
		}
	}
}

func ZapRequestLogger(logger *zap.Logger) func(next http.Handler) http.Handler {
	f := &zapdLogFormatter{Logger: logger}
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			entry := f.NewLogEntry(r)
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			buf := bytes.NewBuffer(make([]byte, 0))
			ww.Tee(buf)

			t1 := time.Now()
			defer func() {
				var respBody []byte
				respBody, _ = ioutil.ReadAll(buf)
				extra := extraLogEntry{Body: respBody}

				entry.Write(ww.Status(), ww.BytesWritten(), ww.Header(), time.Since(t1), extra)
			}()

			next.ServeHTTP(ww, middleware.WithLogEntry(r, entry))
		}
		return http.HandlerFunc(fn)
	}
}

type extraLogEntry struct {
	Body []byte
}

type zapdLogFormatter struct {
	*zap.Logger
}

// implement interface of middleware.LogFormatter https://github.com/go-chi/chi/blob/v5.0.7/middleware/logger.go#L66
func (l *zapdLogFormatter) NewLogEntry(r *http.Request) middleware.LogEntry {
	logger := l.Logger.With(
		zap.Object("httpRequest", &httpRequestLog{Request: r}),
	)
	logger.Info("Request started")
	entry := &zapLogEntry{Logger: logger}
	return entry
}

type zapLogEntry struct {
	*zap.Logger
}

// implement interface of middleware.LogEntry https://github.com/go-chi/chi/blob/v5.0.7/middleware/logger.go#L72
func (l *zapLogEntry) Write(status, bytes int, header http.Header, elapsed time.Duration, extra interface{}) {
	httpResponseLog := &httpResponseLog{
		Status:  &status,
		Bytes:   &bytes,
		Header:  &header,
		Elapsed: &elapsed,
		Extra:   &extra,
	}
	l.Logger.Info(
		"Request complete",
		zap.Object("httpResponse", httpResponseLog),
	)
}

// implement interface of middleware.LogEntry https://github.com/go-chi/chi/blob/v5.0.7/middleware/logger.go#L73
func (l *zapLogEntry) Panic(v interface{}, stack []byte) {
	// Prevent showing duplicate stacktrace.
	// One is from zap embedded function, the other is from argument of stack.
	l.Logger.WithOptions(zap.AddStacktrace(zap.FatalLevel+1)).Error(
		"Panic",
		zap.String("panic", fmt.Sprintf("%+v", v)),
		zap.String("stack", string(stack)),
	)
}

type httpRequestLog struct {
	*http.Request
}

// implement interface of zapcore.ObjectMarshaler https://github.com/uber-go/zap/blob/v1.21.0/zapcore/marshaler.go#L31
func (r *httpRequestLog) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	enc.AddString("method", r.Method)
	enc.AddString("scheme", scheme)
	enc.AddString("host", r.Host)
	enc.AddString("requestURI", r.RequestURI)
	enc.AddString("proto", r.Proto)
	enc.AddString("remoteAddr", r.RemoteAddr)
	if len(r.Header) > 0 {
		enc.AddObject("header", &httpHeaderLog{Header: &r.Header})
	}
	reqID := middleware.GetReqID(r.Context())
	if reqID != "" {
		enc.AddString("reqId", reqID)
	}

	//
	// log request Body
	//
	b := bytes.NewBuffer(make([]byte, 0))
	reader := io.TeeReader(r.Body, b)
	bytes, _ := io.ReadAll(reader)
	defer r.Body.Close()
	if len(bytes) != 0 {
		enc.AddString("body", string(bytes))
	}
	r.Body = io.NopCloser(b)

	return nil
}

type httpResponseLog struct {
	Status  *int
	Bytes   *int
	Header  *http.Header
	Elapsed *time.Duration
	Extra   *interface{}
}

// implement interface of zapcore.ObjectMarshaler https://github.com/uber-go/zap/blob/v1.21.0/zapcore/marshaler.go#L31
func (r *httpResponseLog) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddInt("status", *r.Status)
	enc.AddInt("bytes", *r.Bytes)
	enc.AddDuration("elapsed", *r.Elapsed)
	if len(*r.Header) > 0 {
		enc.AddObject("header", &httpHeaderLog{Header: r.Header})
	}

	if extra, ok := (*r.Extra).(extraLogEntry); ok {
		if len(extra.Body) != 0 {
			enc.AddString("body", string(extra.Body))
		}
	}
	return nil
}

type httpHeaderLog struct {
	*http.Header
}

// implement interface of zapcore.ObjectMarshaler https://github.com/uber-go/zap/blob/v1.21.0/zapcore/marshaler.go#L31
func (h *httpHeaderLog) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	maskedString := "***"
	for k, v := range *h.Header {
		k = strings.ToLower(k)
		// values should be masked
		if (k == "authorization" || k == "cookie" || k == "set-cookie") && len(v) != 0 {
			enc.AddString(k, maskedString)
			continue
		}
		switch {
		case len(v) == 0:
			continue
		case len(v) == 1:
			enc.AddString(k, v[0])
		default:
			enc.AddString(k, fmt.Sprintf("[%s]", strings.Join(v, "], [")))
		}
	}
	return nil
}
