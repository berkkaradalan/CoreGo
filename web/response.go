package web

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"strconv"
)

// responseWriter wraps the http.ResponseWriter handed to a request so the
// framework knows whether, and with which status, the response was started
// - no matter if the handler used the Context writers, Response() directly
// or a wrapped net/http handler. The first final status wins; later
// WriteHeader calls are ignored instead of producing "superfluous
// WriteHeader" warnings.
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

// newResponseWriter wraps w.
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}

// WriteHeader forwards the first final status code. Informational 1xx
// codes (except 101 Switching Protocols) are forwarded as-is and do not
// count as starting the response, matching net/http.
func (w *responseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

// Write writes body bytes, implying a 200 status when none was written.
func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Flush sends buffered data to the client when the underlying writer
// supports it, implying a 200 status when none was written.
func (w *responseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Hijack lets the handler take over the connection (e.g. for WebSockets)
// when the underlying writer supports it. A hijacked response counts as
// written with status 101.
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil && !w.wroteHeader {
		w.status = http.StatusSwitchingProtocols
		w.wroteHeader = true
	}
	return conn, rw, err
}

// Unwrap returns the underlying writer, for http.ResponseController.
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// statusCode returns the status that was (or, when nothing was written,
// will implicitly be) sent.
func (w *responseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// headWriter adapts a ResponseWriter for HEAD requests: it buffers the
// body the handler writes, then forwards only the status and headers,
// setting Content-Length to the suppressed body's length. The body itself
// is discarded, as HEAD responses must have none.
type headWriter struct {
	inner       http.ResponseWriter
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func newHeadWriter(inner http.ResponseWriter) *headWriter {
	return &headWriter{inner: inner, status: http.StatusOK}
}

// Header returns the underlying writer's header map so handlers set
// headers normally.
func (w *headWriter) Header() http.Header { return w.inner.Header() }

// WriteHeader records the status code without forwarding it yet.
func (w *headWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.status = code
	w.wroteHeader = true
}

// Write buffers body bytes, which are discarded on finish.
func (w *headWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(b)
}

// Flush is a no-op: the HEAD response is emitted once, on finish.
func (w *headWriter) Flush() {}

// finish emits the buffered status and headers, adding a Content-Length
// for the suppressed body unless the handler set one explicitly.
func (w *headWriter) finish() {
	if w.inner.Header().Get("Content-Length") == "" && w.body.Len() > 0 {
		w.inner.Header().Set("Content-Length", strconv.Itoa(w.body.Len()))
	}
	w.inner.WriteHeader(w.status)
}
