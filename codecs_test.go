package srpc_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/empijei/srpc"
	"github.com/empijei/tst"
)

func TestParseSSE(t *testing.T) {
	a := tst.Go(t)
	r := strings.NewReader(`event: val
data: "foo"

event: val
data: "bar"

event: err
data: "this is an error\nwith newlines"

`)
	type evt struct {
		Buf, Err string
	}
	var got []evt
	for buf, err := range srpc.ParseSSE(r) {
		var serr string
		if err != nil {
			serr = err.Error()
		}
		got = append(got, evt{Buf: string(buf), Err: serr})
	}
	want := []evt{
		{Buf: `"foo"`},
		{Buf: `"bar"`},
		{Err: `this is an error
with newlines`},
	}
	a.Is(want, got)
}

type stubWriter struct {
	buf bytes.Buffer
}

func (sw *stubWriter) Flush()              {}
func (sw *stubWriter) Header() http.Header { return http.Header{} }
func (sw *stubWriter) Write(buf []byte) (int, error) {
	return sw.buf.Write(buf)
}
func (sw *stubWriter) WriteHeader(statusCode int) {}

func TestWriteSSE(t *testing.T) {
	a := tst.Go(t)
	seq := iter.Seq2[int, error](func(yield func(i int, err error) bool) {
		if !yield(1, nil) {
			return
		}
		if !yield(2, nil) {
			return
		}
		if !yield(-1, errors.New("error")) {
			return
		}
	})
	var sb stubWriter
	w := srpc.SeqWriterTo(t.Context(), seq)
	_ = a.Do(w.WriteTo(&sb))
	want := `event: val
data: 1

event: val
data: 2

event: err
data: "error"

`
	a.Is(want, sb.buf.String())
}

type SeqResp struct {
	Data int
}

func TestRoundtrip(t *testing.T) {
	a := tst.Go(t)
	ctx := t.Context()
	cd := srpc.NewCodecSeq[SeqResp]()
	var seq iter.Seq2[SeqResp, error] = func(yield func(SeqResp, error) bool) {
		for i := range 10 {
			if !yield(SeqResp{i}, nil) {
				return
			}
		}
	}
	r := a.Do(cd.Co(ctx, seq))
	var sb stubWriter
	_ = a.Do(io.Copy(&sb, r))
	gotSeq := a.Do(cd.Dec(ctx, &sb.buf))
	c := 0
	for v, err := range gotSeq {
		a.No(err)
		a.Is(c, v.Data)
		c++
	}
	a.Is(10, c)
}

func TestStream(t *testing.T) {
	a := tst.Go(t)
	ctx := t.Context()
	ep := srpc.NewEndpoint(http.MethodPost, "/api/reverse", srpc.CodecStream, srpc.CodecStream)
	m := http.NewServeMux()
	reverse := func(ctx context.Context, req io.ReadCloser) (io.ReadCloser, error) {
		buf, err := io.ReadAll(req)
		if err != nil {
			return nil, fmt.Errorf("read request: %w", err)
		}
		slices.Reverse(buf)
		return io.NopCloser(bytes.NewReader(buf)), nil
	}
	ep.Register(m, reverse)
	srv := httptest.NewServer(m)
	defer srv.Close()

	rev := ep.RemoteWithOrigin(srv.URL)
	req := io.NopCloser(strings.NewReader("Hello World!"))
	gotR := a.Do(rev(ctx, req))
	defer func() { a.No(gotR.Close()) }()
	got := a.Do(io.ReadAll(gotR))
	a.Is("!dlroW olleH", string(got))
}
