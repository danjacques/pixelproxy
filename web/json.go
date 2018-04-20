package web

import (
	"encoding/json"
	"net/http"

	"github.com/danjacques/pixelproxy/util/logging"
)

// HandleJSON returns an http.HandlerFunc that accepts a JSON request and
// returns a JSON response object.
func HandleJSON(fn func(rw http.ResponseWriter, req *http.Request) interface{}) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		c := req.Context()
		result := fn(rw, req)

		// If our result is an error, return an error JSON type.
		if err, ok := result.(error); ok {
			type responseError struct {
				Error string `json:"_error"`
			}

			logging.S(c).Warnf("Error processing request %s: %s", req.URL, err)
			result = responseError{err.Error()}
		}

		rw.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(rw)
		if err := enc.Encode(result); err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
		}
	}
}
