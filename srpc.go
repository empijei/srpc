// Package srpc implements a simple RPC-over-HTTP library.
package srpc

import (
	"context"
	"net/http"
)

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
	Handle(pattern string, handler http.Handler)
}

type Handler[Response, Request any] func(ctx context.Context, resp Response, req Request) error

func (e *Endpoint[Response, Request]) Serve(m Mux, h Handler[Response, Request]) {
	// TODO
}

type Connector struct {
	Origin  string
	HClient *http.Client
}

type Client[Response, Request any] func(ctx context.Context, req Request) (Response, error)

func (e *Endpoint[Response, Request]) Connect(ctx context.Context, conn *Connector) Client[Response, Request] {
	return func(ctx context.Context, req Request) (Response, error) {
		// TODO
	}
}
