package middleware

import "net/http"

// Middleware wraps an HTTP handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware to a handler in declaration order.
func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	for _, middleware := range middlewares {
		handler = middleware(handler)
	}
	return handler
}
