package srpc

import "context"

////////////////
// Write Only //
////////////////

type (
	EndpointW[Request any] Endpoint[struct{}, Request]
	HandlerW[Request any]  func(ctx context.Context, req Request) error
	ClientW[Request any]   func(ctx context.Context, req Request) error
)

func (e *EndpointW[Request]) Serve(m Mux, h HandlerW[Request]) {
	(*Endpoint[struct{}, Request])(e).Serve(m, func(ctx context.Context, _ struct{}, req Request) error {
		return h(ctx, req)
	})
}

func (e *EndpointW[Request]) Connect(ctx context.Context, conn *Connector) ClientW[Request] {
	cl := (*Endpoint[struct{}, Request])(e).Connect(ctx, conn)
	return func(ctx context.Context, req Request) error {
		_, err := cl(ctx, req)
		return err
	}
}

///////////////
// Read Only //
///////////////

type (
	EndpointR[Response any] Endpoint[Response, struct{}]
	HandlerR[Response any]  func(ctx context.Context, resp Response) error
	ClientR[Response any]   func(ctx context.Context) (Response, error)
)

func (e *EndpointR[Response]) Serve(m Mux, h HandlerW[Response]) {
	(*Endpoint[Response, struct{}])(e).Serve(m, func(ctx context.Context, resp Response, _ struct{}) error {
		return h(ctx, resp)
	})
}

func (e *EndpointR[Response]) Connect(ctx context.Context, conn *Connector) ClientR[Response] {
	cl := (*Endpoint[Response, struct{}])(e).Connect(ctx, conn)
	return func(ctx context.Context) (Response, error) {
		return cl(ctx, struct{}{})
	}
}
