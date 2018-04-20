package web

import (
	"net/http"
)

// StatusError is a sentinel error sent to RenderWithError to indicate that
// a specific HTTP return code should be returned.
type StatusError struct {
	Err  error
	Code int
}

func (se *StatusError) Error() string {
	if se.Err != nil {
		return se.Err.Error()
	}
	return http.StatusText(se.Code)
}

// RenderWithError calls fn. If fn returns an error, RenderWithError will write
// it to the ResponseWriter, setting the appropriate status if possible.
//
// If a StatusError is returned, it is rendered specially, with its code being
// used. Otherwise, http.StatusInternalServerError will be used.
func (s *Site) RenderWithError(rw http.ResponseWriter, ct string, fn func() error) {
	rw.Header().Set("Content-Type", ct)

	err := fn()
	if err == nil {
		return
	}

	// Convert our error to a StatusError.
	st, ok := err.(*StatusError)
	if !ok {
		st = &StatusError{
			Code: http.StatusInternalServerError,
			Err:  err,
		}
	}

	// Log the error, if configured.
	if s.Logger != nil {
		if st.Code >= http.StatusInternalServerError {
			s.Logger.Sugar().Errorf("Internal error while rendering: %s", err)
		} else {
			s.Logger.Sugar().Warnf("Error while rendering: %s", err)
		}
	}

	rw.WriteHeader(st.Code)
	_, _ = rw.Write([]byte(st.Error()))
}
