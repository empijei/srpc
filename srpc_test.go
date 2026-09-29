package srpc_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/empijei/srpc"
	"github.com/empijei/tst"
)

type Resp struct {
	A string
}

type Req struct {
	B string
}

var Ep = srpc.NewEndpointJSON[Resp, Req](http.MethodPost, "/foo")

func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()
	t.Run("POST", func(t *testing.T) {
		a := tst.Sync(t)
		ctx := t.Context()
		mux := http.NewServeMux()
		Ep.Register(mux, func(ctx context.Context, req Req) (rsp Resp, _ error) {
			rsp.A = "resp" + req.B
			return
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		c := Ep.RemoteWithOrigin(srv.URL)
		got := a.Do(c(ctx, Req{"req"}))
		a.Is(Resp{"respreq"}, got)
	})
	t.Run("GET", func(t *testing.T) {
		a := tst.Sync(t)
		ctx := t.Context()
		ep := srpc.NewEndpointJSON[Resp, Req](http.MethodGet, "/get")
		mux := http.NewServeMux()
		ep.Register(mux, func(ctx context.Context, req Req) (rsp Resp, _ error) {
			rsp.A = "get" + req.B
			return
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		c := ep.RemoteWithOrigin(srv.URL)
		got := a.Do(c(ctx, Req{"req"}))
		a.Is(Resp{"getreq"}, got)
	})
}

type ValReq struct {
	B string
}

func (v ValReq) Validate() error {
	if v.B == "" {
		return errors.New("invalid: B cannot be empty")
	}
	return nil
}

func TestValidation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	ep := srpc.NewEndpointJSON[Resp, ValReq](http.MethodPost, "/val")
	mux := http.NewServeMux()
	ep.Register(mux, func(ctx context.Context, req ValReq) (rsp Resp, _ error) {
		rsp.A = "ok"
		return
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := ep.RemoteWithOrigin(srv.URL)

	t.Run("Valid", func(t *testing.T) {
		a := tst.Sync(t)
		got := a.Do(c(ctx, ValReq{"ok"}))
		a.Is(Resp{"ok"}, got)
	})

	t.Run("Invalid", func(t *testing.T) {
		a := tst.Sync(t)
		_, err := c(ctx, ValReq{""})
		a.Err("cannot be empty", err)
		we := a.DoB(errors.AsType[*srpc.WireError](err))
		a.Is(http.StatusBadRequest, we.Code)
		a.Is("Invalid request: invalid: B cannot be empty", we.Msg)
	})
}

func TestErrors(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	ep := srpc.NewEndpointJSON[Resp, Req](http.MethodPost, "/err")
	mux := http.NewServeMux()
	ep.Register(mux, func(ctx context.Context, req Req) (rsp Resp, _ error) {
		if req.B == "fail" {
			return Resp{}, &srpc.WireError{Msg: "custom error", Code: http.StatusTeapot}
		}
		return Resp{}, context.DeadlineExceeded
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := ep.RemoteWithOrigin(srv.URL)

	t.Run("CustomError", func(t *testing.T) {
		a := tst.Sync(t)
		_, err := c(ctx, Req{"fail"})
		a.Err("custom error", err)
		werr := a.DoB(errors.AsType[*srpc.WireError](err))
		a.Is(http.StatusTeapot, werr.Code)
		a.Is("custom error", werr.Msg)
		a.Err("", werr)
	})

	t.Run("GenericError", func(t *testing.T) {
		a := tst.Sync(t)
		_, err := c(ctx, Req{"other err"})
		a.Err("Bad Request", err)
		werr := a.DoB(errors.AsType[*srpc.WireError](err))
		a.Is(http.StatusBadRequest, werr.Code)
		a.Is("Bad Request", werr.Msg)
		a.Err("", werr)
	})
}

func TestTransport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		origin string
		ok     bool
	}{
		{"ftp://example.com", false},
		{"http://example.com/path", false},
		{"http://example.com?query", false},
		{":invalid", false},
		{"web.dev", false},
		{"https://web.dev", true},
	}
	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			a := tst.Go(t)
			_, err := srpc.NewTransport(tt.origin, nil, nil)
			ok := err == nil
			a.Is(tt.ok, ok)
		})
	}
}

func TestSugar(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("WriteOnly", func(t *testing.T) {
		a := tst.Sync(t)
		ctx := t.Context()
		ep := srpc.NewEndpointJSON[struct{}, Req](http.MethodPost, "/write")
		epw := (*srpc.EndpointW[Req])(&ep)
		var lastReq string
		epw.Register(mux, func(ctx context.Context, req Req) error {
			lastReq = req.B
			return nil
		})
		c := epw.RemoteWithOrigin(srv.URL)
		a.No(c(ctx, Req{"write"}))
		a.Is("write", lastReq)
	})

	t.Run("ReadOnly", func(t *testing.T) {
		a := tst.Sync(t)
		ep := srpc.NewEndpointJSON[Resp, struct{}](http.MethodGet, "/read")
		epr := (*srpc.EndpointR[Resp])(&ep)
		epr.Register(mux, func(ctx context.Context) (Resp, error) {
			return Resp{"read"}, nil
		})
		c := epr.RemoteWithOrigin(srv.URL)
		got := a.Do(c(t.Context()))
		a.Is(Resp{"read"}, got)
	})
}
