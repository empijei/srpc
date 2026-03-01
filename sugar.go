package srpc

import "context"

////////////////
// Write Only //
////////////////

type (
	// EndpointW is like [Endpoint] but for functions that return no content.
	EndpointW[Request any] Endpoint[struct{}, Request]
	// ProcedureW is like [Procedure], but for functions that return no value.
	ProcedureW[Request any] func(ctx context.Context, req Request) error
)

// Register is like [Endpoint.Register] for EndpointW.
func (e *EndpointW[Request]) Register(m Mux, h ProcedureW[Request]) {
	(*Endpoint[struct{}, Request])(e).Register(m, func(ctx context.Context, req Request) (struct{}, error) {
		return struct{}{}, h(ctx, req)
	})
}

// Remote is like [Endpoint.Remote] for EndpointW.
func (e *EndpointW[Request]) Remote(conn *Transport) ProcedureW[Request] {
	cl := (*Endpoint[struct{}, Request])(e).Remote(conn)
	return func(ctx context.Context, req Request) error {
		_, err := cl(ctx, req)
		return err
	}
}

///////////////
// Read Only //
///////////////

type (
	// EndpointR is like [Endpoint] for functions that take no inputs.
	EndpointR[Response any] Endpoint[Response, struct{}]
	// ProcedureR is like [Procedure] for functions that take no parameters.
	ProcedureR[Response any] func(ctx context.Context) (Response, error)
)

// Register is like [Endpoint.Register] for [EndpointR].
func (e *EndpointR[Response]) Register(m Mux, h ProcedureR[Response]) {
	(*Endpoint[Response, struct{}])(e).Register(m, func(ctx context.Context, _ struct{}) (Response, error) {
		return h(ctx)
	})
}

// Remote is like [Endpoint.Remote] for [EndpointR].
func (e *EndpointR[Response]) Remote(conn *Transport) ProcedureR[Response] {
	cl := (*Endpoint[Response, struct{}])(e).Remote(conn)
	return func(ctx context.Context) (Response, error) {
		return cl(ctx, struct{}{})
	}
}
