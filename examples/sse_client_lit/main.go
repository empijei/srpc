package main

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"iter"
	"net/http"
	"time"

	"github.com/empijei/srpc"
)

type Request struct {
	From int
}

type Response struct {
	Value int
}

var ep = srpc.NewEndpointSeq[Response, Request]("/api/counter")

//go:embed sse-client.lit.html
var html string

func main() {
	m := http.NewServeMux()
	m.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, html)
	})
	ep.Register(m, func(ctx context.Context, req Request) (iter.Seq2[Response, error], error) {
		if req.From >= 1000 {
			return func(yield func(Response, error) bool) {}, &srpc.WireError{
				Msg:  "Value too high",
				Code: http.StatusBadRequest,
			}
		}
		return func(yield func(Response, error) bool) {
			for i := req.From; i < 1000; i++ {
				select {
				case <-ctx.Done():
					return
				case <-time.After(1 * time.Second):
					if !yield(Response{i}, nil) {
						return
					}
				}
			}
		}, nil
	})
	addr := "localhost:8080"
	fmt.Println("Visit http://" + addr)
	http.ListenAndServe(addr, m)
}
