// Package srpc implements a simple RPC-over-HTTP library.
package srpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// QueryKey is the key for the query parameter that sRPC will use to issue state-preserving requests.
const QueryKey = "srpc"

// Procedure is a function that can be called remotely.
type Procedure[Response, Request any] = func(ctx context.Context, req Request) (Response, error)

// Endpoint represents a single procedure.
//
// Both the client side and the server side can be constructed from this type.
type Endpoint[Response, Request any] struct {
	method        string
	path          string
	stateChanging bool
	resc          Codec[Response]
	reqc          Codec[Request]
}

// NewEndpointJSON constructs an endpoint with the JSON codec.
func NewEndpointJSON[Response, Request any](method, path string) Endpoint[Response, Request] {
	return NewEndpoint(method, path, NewCodecJSON[Response](), NewCodecJSON[Request]())
}

// NewEndpoint constructs a new endpoint with the given codecs.
func NewEndpoint[Response, Request any](method, path string, resc Codec[Response], reqc Codec[Request]) Endpoint[Response, Request] {
	if !strings.HasPrefix(path, "/") {
		panic(fmt.Sprintf("path must start with '/', %q provided", path))
	}
	return Endpoint[Response, Request]{
		method:        method,
		path:          path,
		stateChanging: method != http.MethodGet && method != http.MethodOptions && method != http.MethodHead,
		resc:          resc,
		reqc:          reqc,
	}
}

////////////
// Server //
////////////

// Mux is the subset of [*http.ServeMux] that srpc requires to work.
type Mux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// Validable represents all requests that can be validated.
//
// All requests that implement this and that are not valid will be automatically rejected.
type Validable interface {
	Validate() error
}

// ErrorResponse can be returned by handlers to control the error code when returning an error.
type ErrorResponse interface {
	Status() int
	Message() string
}

// Register registers the endpoint on the mux, implemented by the procedure.
func (e *Endpoint[Response, Request]) Register(m Mux, p Procedure[Response, Request]) {
	m.HandleFunc(e.method+" "+e.path, func(hResp http.ResponseWriter, hReq *http.Request) {
		ctx := hReq.Context()

		// Parse Request

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

		// Create Response

		resp, err := p(ctx, req)
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

		// Send Response

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

////////////
// Client //
////////////

// ErrBadOrigin is returned when a bad origin is given to construct a Connector.
var ErrBadOrigin = errors.New("bad connector origin")

// Transport can be used to connect to a remote Endpoint.
type Transport struct {
	origin  string
	client  *http.Client
	cookies []*http.Cookie
}

// NewTransport creates a new Connector.
//
// The only mandatory parameter is origin, which must have a "http" or "https" scheme,
// a valid domain, and must not contain any path or query.
func NewTransport(origin string, client *http.Client, cookies []*http.Cookie) (*Transport, error) {
	c := &Transport{
		origin:  origin,
		client:  client,
		cookies: cookies,
	}

	u, err := url.Parse(c.origin)
	switch {
	case err != nil:
		return nil, fmt.Errorf("%w: invalid URL: %w", ErrBadOrigin, err)
	case !strings.EqualFold(u.Scheme, "http") &&
		!strings.EqualFold(u.Scheme, "https"):
		return nil, fmt.Errorf(`%w: scheme must be "http" or "https": %q`, ErrBadOrigin, c.origin)
	case u.Path != "":
		return nil, fmt.Errorf("%w: path must be empty: %q", ErrBadOrigin, u.Path)
	case u.RawQuery != "":
		return nil, fmt.Errorf("%w: query must be empty: %q", ErrBadOrigin, u.RawQuery)
	}

	if client == nil {
		c.client = http.DefaultClient
	}
	return c, nil
}

// RemoteWithOrigin is like Remote, but it creates a transport for the given origin.
//
// If the origin is invalid, RemoteWithOrigin panics.
func (e *Endpoint[Response, Request]) RemoteWithOrigin(origin string) Procedure[Response, Request] {
	conn, err := NewTransport(origin, nil, nil)
	if err != nil {
		panic(err)
	}
	return e.Remote(conn)
}

// Remote returns the remote procedure, ready to be called.
//
// The endpoint needs to be registered and served on the remote server.
func (e *Endpoint[Response, Request]) Remote(conn *Transport) Procedure[Response, Request] {
	rawURL := conn.origin + e.path
	reqCtor := func(ctx context.Context, streamUp io.Reader) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, e.method, rawURL, streamUp)
	}
	if !e.stateChanging {
		reqCtor = func(ctx context.Context, streamUp io.Reader) (*http.Request, error) {
			buf, err := io.ReadAll(streamUp)
			if err != nil {
				return nil, err
			}
			q := "?" + QueryKey + "=" + url.QueryEscape(string(buf))
			return http.NewRequestWithContext(ctx, e.method, rawURL+q, nil)
		}
	}

	return func(ctx context.Context, req Request) (resp Response, err error) {
		var zero Response

		// Create Request

		streamUp, err := e.reqc.Co(ctx, req)
		if err != nil {
			return zero, fmt.Errorf("encoding request: %w", err)
		}
		hReq, err := reqCtor(ctx, streamUp)
		if err != nil {
			return zero, fmt.Errorf("converting request to HTTP: %w", err)
		}
		hReq.Header.Set("Content-Type", e.reqc.ContentType)
		for _, cookie := range conn.cookies {
			hReq.AddCookie(cookie)
		}

		// TODO MW

		// Roundtrip

		hResp, err := conn.client.Do(hReq) //nolint: gosec // these are hardcoded in sources.
		if err != nil {
			return zero, fmt.Errorf("issuing request: %w", err)
		}

		// Cleanups

		if !e.resc.KeepOpen {
			defer func() {
				if cerr := hResp.Body.Close(); cerr != nil {
					err = errors.Join(err, cerr)
				}
			}()
		}
		if !e.reqc.KeepOpen {
			if c, ok := streamUp.(io.Closer); ok {
				defer func() {
					if cerr := c.Close(); cerr != nil {
						err = errors.Join(err, fmt.Errorf("closing Request: %w", cerr))
					}
				}()
			}
		}

		// Decoding

		if hResp.StatusCode != http.StatusOK {
			return zero, readErr(hResp)
		}
		if ct := hResp.Header.Get("Content-Type"); ct != e.resc.ContentType {
			return zero, fmt.Errorf("Content-Type: want %q got %q", e.resc.ContentType, ct)
		}
		resp, err = e.resc.Dec(ctx, hResp.Body)
		if err != nil {
			return zero, fmt.Errorf("decoding response: %w", err)
		}
		return resp, nil
	}
}

var (
	_ ErrorResponse = &WireError{}
	_ error         = &WireError{}
)

// WireError is an error that can be sent over the wire.
//
// It allows to set HTTP status and message.
type WireError struct {
	Msg  string
	Code int
}

// Error implements [error].
func (w *WireError) Error() string {
	return fmt.Sprintf("%v %v: %v", w.Code, http.StatusText(w.Code), w.Msg)
}

// Message implements [ErrorResponse].
func (w *WireError) Message() string { return w.Msg }

// Status implements [ErrorResponse].
func (w *WireError) Status() int { return w.Code }

func readErr(resp *http.Response) error {
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return &WireError{Msg: string(buf), Code: resp.StatusCode}
}
