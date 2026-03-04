package srpc

import (
	"context"
	"io"
	"iter"
)

var ParseSSE = parseSSE

func SeqWriterTo[T any](ctx context.Context, seq iter.Seq2[T, error]) io.WriterTo {
	return &wireSeq[T]{seq: seq}
}
