package srpc_test

import (
	"bytes"
	"errors"
	"io"
	"iter"
	"net/http"
	"strings"
	"testing"

	"github.com/empijei/srpc"
	"github.com/empijei/tst"
)

func TestParseSSE(t *testing.T) {
	tst.Go(t)
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
	tst.Is(want, got, t)
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
	ctx := tst.Go(t)
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
	w := srpc.SeqWriterTo(ctx, seq)
	_ = tst.Do(w.WriteTo(&sb))(t)
	want := `event: val
data: 1

event: val
data: 2

event: err
data: "error"

`
	tst.Is(want, sb.buf.String(), t)
}

type SeqResp struct {
	Data int
}

func TestRoundtrip(t *testing.T) {
	ctx := tst.Go(t)
	cd := srpc.NewCodecSeq[SeqResp]()
	var seq iter.Seq2[SeqResp, error] = func(yield func(SeqResp, error) bool) {
		for i := range 10 {
			if !yield(SeqResp{i}, nil) {
				return
			}
		}
	}
	r := tst.Do(cd.Co(ctx, seq))(t)
	var sb stubWriter
	_ = tst.Do(io.Copy(&sb, r))(t)
	gotSeq := tst.Do(cd.Dec(ctx, &sb.buf))(t)
	c := 0
	for v, err := range gotSeq {
		tst.No(err, t)
		tst.Is(c, v.Data, t)
		c++
	}
	tst.Is(10, c, t)
}
