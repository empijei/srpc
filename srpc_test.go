package srpc_test

import (
	"context"
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
}
