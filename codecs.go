package srpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"sync/atomic"
)

type empty struct{}

func (empty) Read(_ []byte) (n int, err error) { return 0, io.EOF }

// Codec implements functions to enCode and Decode requests and responses.
//
// Ideally
//
//	var value T
//	wire, _ := myCodec.Co(ctx, value)
//	got := myCodec.Dec(ctx, wire)
//	got == value
//
// Should be true for every possible value of T.
type Codec[T any] struct {
	// ContentType is the HTTP Content-Type header to use for responses.
	ContentType string
	// KeepOpen tells this library to not close streams after client calls return.
	KeepOpen bool
	// Co encodes the given value to the returned io.Reader.
	//
	// Implementers have the guarantee that the returned reader will be copied with
	// io.Copy to the response writer, or used as request body/query on the client side.
	Co func(ctx context.Context, t T) (io.Reader, error)
	// Dec decodes data from a stream.
	Dec func(ctx context.Context, r io.Reader) (T, error)
}

// JSON

// NewCodecJSON creates a new Codec that uses JSON as wire format.
func NewCodecJSON[T any]() Codec[T] {
	var zero T
	_, isEmpty := any(zero).(struct{})
	return Codec[T]{
		ContentType: "application/json",
		Co: func(_ context.Context, t T) (io.Reader, error) {
			if isEmpty {
				return empty{}, nil
			}

			buf, err := json.Marshal(t)
			if err != nil {
				return nil, err
			}
			return bytes.NewReader(buf), nil
		},
		Dec: func(_ context.Context, r io.Reader) (t T, err error) {
			if isEmpty {
				return zero, nil
			}
			return t, json.NewDecoder(r).Decode(&t)
		},
	}
}

// Seq

const (
	evtVal = "val"
	evtErr = "err"
)

type wireSeq[T any] struct {
	seq    iter.Seq2[T, error]
	closed atomic.Bool
}

func (i *wireSeq[T]) Read(_ []byte) (n int, err error) {
	return 0, errors.New("sequence codec should only be used in io.Copy")
}

func (i *wireSeq[T]) Close() error {
	i.closed.Store(true)
	return nil
}

type writeFlusher interface {
	http.ResponseWriter
	http.Flusher
}

func (i *wireSeq[T]) WriteTo(baseWriter io.Writer) (int64, error) {
	w, ok := baseWriter.(writeFlusher)
	if !ok {
		return 0, errors.New("sequence codec can only be used with writers that implement http.Flusher and http.ResponseWriter")
	}

	w.WriteHeader(http.StatusOK)
	w.Flush()

	if i.seq == nil {
		return 0, nil
	}
	var (
		n   int64
		err error
		buf bytes.Buffer
		enc = json.NewEncoder(&buf)
	)

	for v, seqErr := range i.seq {
		if i.closed.Load() {
			return n, io.EOF
		}
		n, err = i.writeKind(w, n, seqErr != nil)
		if err != nil {
			return n, err
		}
		n, err = i.writeMessage(w, n, v, seqErr, &buf, enc)
		if err != nil {
			return n, err
		}
		w.Flush()
	}
	return n, nil
}

func (i *wireSeq[T]) writeKind(w writeFlusher, n int64, isErr bool) (int64, error) {
	c, err := io.WriteString(w, "event: ")
	if err != nil {
		return n, err
	}
	n += int64(c)

	evt := evtVal
	if isErr {
		evt = evtErr
	}
	c, err = io.WriteString(w, evt)
	if err != nil {
		return n, err
	}
	n += int64(c)

	c, err = io.WriteString(w, "\ndata: ")
	if err != nil {
		return n, err
	}
	return n + int64(c), nil
}

func (i *wireSeq[T]) writeMessage(w writeFlusher, n int64, v T, seqErr error, buf *bytes.Buffer, enc *json.Encoder) (int64, error) {
	buf.Reset()
	var msg any = v
	if seqErr != nil {
		msg = seqErr.Error()
	}
	if err := enc.Encode(msg); err != nil {
		return n, err
	}
	buf.WriteRune('\n')
	c, err := io.Copy(w, buf)
	if err != nil {
		return n, err
	}
	return n + c, nil
}

var (
	evtValB = []byte(evtVal)
	evtErrB = []byte(evtErr)
)

func parseSSE(r io.Reader) iter.Seq2[[]byte, error] {
	split := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
			return i + 2, data[0:i], nil
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	}
	parse := func(data []byte) (msg []byte, err error) {
		act, data, ok := bytes.Cut(data, []byte(": "))
		if !ok {
			return nil, errors.New("invalid sse message: missing ': '")
		}
		if !bytes.Equal(act, []byte("event")) {
			return nil, fmt.Errorf("unknown prefix: %q", act)
		}

		kind, data, ok := bytes.Cut(data, []byte("\n"))
		if !ok {
			return nil, errors.New(`invalid sse message: missing '\n'`)
		}
		const prefix = "data: "
		if !bytes.HasPrefix(data, []byte(prefix)) {
			return nil, fmt.Errorf(`unexpected prefix: %q, want %q`, data, evtVal)
		}
		data = data[len(prefix):]

		switch {
		case bytes.Equal(kind, evtValB):
			return data, nil
		case bytes.Equal(kind, evtErrB):
			var msg string
			if err := json.Unmarshal(data, &msg); err != nil {
				return nil, err
			}
			return nil, errors.New(msg)
		default:
			return nil, fmt.Errorf("unknown event kind: %q", kind)
		}
	}
	s := bufio.NewScanner(r)
	const maxTokenSize = 1024 * 1024
	s.Split(split)
	s.Buffer(nil, maxTokenSize)
	return func(yield func([]byte, error) bool) {
		for s.Scan() {
			msg, err := parse(s.Bytes())
			if !yield(msg, err) {
				return
			}
		}
		if err := s.Err(); err != nil {
			yield(nil, err)
		}
	}
}

// NewCodecSeq constructs an iter.Seq codec that supports Server-Sent-Events.
//
// The events are "val" and "err".
//
// Yielding a value and an error at the same time is not supported.
func NewCodecSeq[T any]() Codec[iter.Seq2[T, error]] {
	return Codec[iter.Seq2[T, error]]{
		ContentType: "text/event-stream",
		KeepOpen:    true,
		Co: func(_ context.Context, seq iter.Seq2[T, error]) (io.Reader, error) {
			return &wireSeq[T]{seq: seq}, nil
		},
		Dec: func(_ context.Context, wf io.Reader) (iter.Seq2[T, error], error) {
			return func(yield func(T, error) bool) {
				defer func() {
					if c, ok := wf.(io.Closer); ok {
						_ = c.Close()
					}
				}()
				var zero T
				for buf, err := range parseSSE(wf) {
					if err != nil {
						if !yield(zero, err) {
							return
						}
						continue
					}
					var t T
					if err := json.Unmarshal(buf, &t); err != nil {
						if !yield(zero, err) {
							return
						}
					}
					if !yield(t, nil) {
						return
					}
				}
			}, nil
		},
	}
}
