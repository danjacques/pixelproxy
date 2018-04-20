package web

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
)

// WithContextMiddleware is a gorilla/mux middleware Handle that adds
// the supplied Context to each http.Request that gets handled.
func WithContextMiddleware(c context.Context) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// Add a Context to each request.
		return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			// Pull out the vars from the current request Context.
			vars := mux.Vars(req)

			// Create a new Request with our base Context.
			req = req.WithContext(c)
			req = mux.SetURLVars(req, vars)
			next.ServeHTTP(rw, req)
		})
	}
}
