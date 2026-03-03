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
	tst.Go(t)
	t.Run("POST", func(t *testing.T) {
		ctx := tst.Go(t)
		mux := http.NewServeMux()
		Ep.Register(mux, func(ctx context.Context, req Req) (rsp Resp, _ error) {
			rsp.A = "resp" + req.B
			return
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		c := Ep.RemoteWithOrigin(srv.URL)
		got := tst.Do(c(ctx, Req{"req"}))(t)
		tst.Is(Resp{"respreq"}, got, t)
	})
	t.Run("GET", func(t *testing.T) {
		ctx := tst.Go(t)
		ep := srpc.NewEndpointJSON[Resp, Req](http.MethodGet, "/get")
		mux := http.NewServeMux()
		ep.Register(mux, func(ctx context.Context, req Req) (rsp Resp, _ error) {
			rsp.A = "get" + req.B
			return
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()
		c := ep.RemoteWithOrigin(srv.URL)
		got := tst.Do(c(ctx, Req{"req"}))(t)
		tst.Is(Resp{"getreq"}, got, t)
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
	ctx := tst.Go(t)
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
		got := tst.Do(c(ctx, ValReq{"ok"}))(t)
		tst.Is(Resp{"ok"}, got, t)
	})

	t.Run("Invalid", func(t *testing.T) {
		_, err := c(ctx, ValReq{""})
		tst.Err("cannot be empty", err, t)
		we := tst.DoB(errors.AsType[*srpc.WireError](err))(t)
		tst.Is(http.StatusBadRequest, we.Code, t)
		tst.Is("Invalid request: invalid: B cannot be empty", we.Msg, t)
	})
}

func TestErrors(t *testing.T) {
	ctx := tst.Go(t)
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
		_, err := c(ctx, Req{"fail"})
		tst.Err("custom error", err, t)
		werr := tst.DoB(errors.AsType[*srpc.WireError](err))(t)
		tst.Is(http.StatusTeapot, werr.Code, t)
		tst.Is("custom error", werr.Msg, t)
		tst.Err("", werr, t)
	})

	t.Run("GenericError", func(t *testing.T) {
		_, err := c(ctx, Req{"other err"})
		tst.Err("Bad Request", err, t)
		werr := tst.DoB(errors.AsType[*srpc.WireError](err))(t)
		tst.Is(http.StatusBadRequest, werr.Code, t)
		tst.Is("Bad Request", werr.Msg, t)
		tst.Err("", werr, t)
	})
}

func TestTransport(t *testing.T) {
	tst.Go(t)
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
			_, err := srpc.NewTransport(tt.origin, nil, nil)
			ok := err == nil
			tst.Is(tt.ok, ok, t)
		})
	}
}

func TestSugar(t *testing.T) {
	ctx := tst.Go(t)
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("WriteOnly", func(t *testing.T) {
		ep := srpc.NewEndpointJSON[struct{}, Req](http.MethodPost, "/write")
		epw := (*srpc.EndpointW[Req])(&ep)
		var lastReq string
		epw.Register(mux, func(ctx context.Context, req Req) error {
			lastReq = req.B
			return nil
		})
		c := epw.RemoteWithOrigin(srv.URL)
		tst.No(c(ctx, Req{"write"}), t)
		tst.Is("write", lastReq, t)
	})

	t.Run("ReadOnly", func(t *testing.T) {
		ep := srpc.NewEndpointJSON[Resp, struct{}](http.MethodGet, "/read")
		epr := (*srpc.EndpointR[Resp])(&ep)
		epr.Register(mux, func(ctx context.Context) (Resp, error) {
			return Resp{"read"}, nil
		})
		c := epr.RemoteWithOrigin(srv.URL)
		got := tst.Do(c(ctx))(t)
		tst.Is(Resp{"read"}, got, t)
	})
}
