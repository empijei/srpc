# srpc

Simple RPC library for Go.

## Usage

Declare an endpoint:

```go
package mathapi

type SqResponse struct {
	Square float64
}

type SqRequest struct {
	Base float64
}

var Square = srpc.
    NewEndpointJSON[
    SqResponse,
    SqRequest](
        "GET", "/api/square")
```

Serve the endpoint:

```go
// square is the Procedure we want to expose
func square(ctx context.Context, req mathapi.SqRequest) (mathapi.SqResponse, error) {
    res := math.Pow(req.Base, 2)
    if math.IsNaN(res) {
        return mathapi.SqResponse{}, errNaN
    }
    return mathapi.SqResponse{Square: res}, nil
}


m := http.NewServeMux()
mathapi.Square.Register(m, square)
// … register more endpoints
http.ListenAndServe(address, m)
```

Use the endpoint:

```go
square := mathapi.Square.RemoteWithOrigin(serverOrigin)
res, err := square(ctx, mathapi.SqRequest{42})
if err != nil {
	// Handle potential connection or codec error
}
fmt.Println(res.Square)

```

## Codecs

Codecs allow to specify how to represent data on the wire.

This package already provides JSON, Raw and Seq (SSE-based) codecs.

New ones can easily be implemented, see `codecs.go` for inspiration.
