// Package srpc implements a simple RPC-over-HTTP library.
package srpc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

const QueryKey = "srpc"

type keyCtx string

const (
	headerKey keyCtx = "request headers"
)

type Validable interface {
	Validate() error
}

type ErrorResponse interface {
	Status() int
	Message() string
}

type Endpoint[Response, Request any] struct {
	method        string
	path          string
	stateChanging bool
	resc          Codec[Response]
	reqc          Codec[Request]
}

func NewEndpointJSON[Response, Request any](method, path string) Endpoint[Response, Request] {
	return NewEndpoint(method, path, NewCodecJSON[Response](), NewCodecJSON[Request]())
}

func NewEndpoint[Response, Request any](method, path string, resc Codec[Response], reqc Codec[Request]) Endpoint[Response, Request] {
	stateChanging := true
	if method == http.MethodGet ||
		method == http.MethodOptions ||
		method == http.MethodHead {
		stateChanging = false
	}
	return Endpoint[Response, Request]{
		method:        method,
		path:          path,
		stateChanging: stateChanging,
		resc:          resc,
		reqc:          reqc,
	}
}

type Mux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

type Handler[Response, Request any] func(ctx context.Context, req Request) (Response, error)

func (e *Endpoint[Response, Request]) Serve(m Mux, h Handler[Response, Request]) {
	m.HandleFunc(e.method+" "+e.path, func(hResp http.ResponseWriter, hReq *http.Request) {
		ctx := hReq.Context()
		ctx = context.WithValue(ctx, headerKey, hReq.Header)

		// Parse Request.

		var req Request
		{
			streamUp := hReq.Body
			if !e.stateChanging {
				streamUp = io.NopCloser(strings.NewReader(hReq.URL.Query().Get(QueryKey)))
			}

			var err error
			req, err = e.reqc.Dec(ctx, streamUp)
			if err != nil {
				slog.LogAttrs(ctx, slog.LevelInfo, "Bad request",
					slog.String("error", fmt.Sprintf("decoding: %s", err)))
				http.Error(hResp, "Unable to decode request.", http.StatusBadRequest)
				return
			}

			// TODO middleware

			if val, ok := any(req).(Validable); ok {
				if err := val.Validate(); err != nil {
					slog.LogAttrs(ctx, slog.LevelInfo, "Invalid request",
						slog.String("error", fmt.Sprintf("validating: %s", err)))
					http.Error(hResp, "Invalid request.", http.StatusBadRequest)
					return
				}
			}

		}

		// Create Response.

		resp, err := h(ctx, req)
		if err != nil {
			// TODO find a way to have error codecs or at least to make errors.Is work with these.

			status := http.StatusBadRequest
			var msg string
			if serr, ok := err.(ErrorResponse); ok {
				status = serr.Status()
				msg = serr.Message()
			}
			if msg == "" {
				msg = http.StatusText(status)
			}

			slog.LogAttrs(ctx, slog.LevelInfo, "Handler Error",
				slog.String("error", fmt.Sprintf("processing: %s", err)))
			http.Error(hResp, msg, status)
			return
		}
		streamDown, err := e.resc.Co(ctx, resp)
		if err != nil {
			slog.LogAttrs(ctx, slog.LevelWarn, "Encoder Error",
				slog.String("error", fmt.Sprintf("encoding: %s", err)))
			http.Error(hResp, "Failed to encode response.", http.StatusInternalServerError)
			return
		}

		// Send response.

		hResp.Header().Set("Content-Type", e.resc.ContentType)
		if c, ok := streamDown.(io.Closer); ok {
			defer func() {
				if err := c.Close(); err != nil {
					slog.LogAttrs(ctx, slog.LevelInfo, "streamDown Close",
						slog.String("error", fmt.Sprintf("close: %s", err)))
				}
			}()
		}
		if _, err := io.Copy(hResp, streamDown); err != nil {
			slog.LogAttrs(ctx, slog.LevelInfo, "streamDown Copy",
				slog.String("error", fmt.Sprintf("copy: %s", err)))
			return
		}
	})
}

type Connector struct {
	Origin  string
	Client  *http.Client
	Cookies []*http.Cookie

	errs []error
}

type Client[Response, Request any] func(ctx context.Context, req Request) (Response, error)

func (e *Endpoint[Response, Request]) Connect(ctx context.Context, conn *Connector) Client[Response, Request] {
	return func(ctx context.Context, req Request) (Response, error) {
		// TODO
	}
}
