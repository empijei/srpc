package srpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
)

type empty struct{}

func (empty) Read(_ []byte) (n int, err error) { return 0, io.EOF }

type Codec[T any] struct {
	ContentType string
	KeepOpen    bool
	Co          func(ctx context.Context, t T) (io.Reader, error)
	Dec         func(ctx context.Context, r io.Reader) (T, error)
}

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
