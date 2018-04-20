package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var (
	httpRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "web_http_requests",
		Help: "Number of HTTP requests made to the proxy.",
	})

	httpResponses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "web_http_responses",
		Help: "Number of HTTP responses, by code.",
	}, []string{"code"})

	httpLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "web_response_latency",
		Help:    "Latency of HTTP operations.",
		Buckets: prometheus.DefBuckets,
	}, []string{"code"})

	httpResponseSizes = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "web_response_sizes",
		Help:    "Size of web responses.",
		Buckets: prometheus.ExponentialBuckets(100, 10, 5),
	}, []string{"code"})
)

func init() {
	prometheus.MustRegister(
		httpRequests,
		httpResponses,
		httpLatency,
		httpResponseSizes,
	)
}

// MonitoringMiddleware exposes a chainable http.Handler middleware method that
// offers HTTP server monitoring.
type MonitoringMiddleware struct {
	// Logger, is not nil, is the logger to use.
	Logger *zap.Logger
}

// Middleware wraps next in before and after monitoring middleware.
func (lh *MonitoringMiddleware) Middleware(next http.Handler) http.Handler {
	// Identify our logger.
	baseLogger := lh.Logger
	if baseLogger == nil {
		baseLogger = zap.NewNop()
	}
	logger := baseLogger.Sugar()

	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		crw := capturingResponseWriter{
			base:      rw,
			status:    http.StatusOK,
			hasStatus: false,
		}

		// Handle monitoring in defer.
		httpRequests.Inc()
		startTime := time.Now()
		defer func() {
			duration := time.Now().Sub(startTime)

			logger.Debugf("Received HTTP request for %q from %s (%d / %v), response=(%d bytes)",
				req.RequestURI, req.RemoteAddr, crw.status, http.StatusText(crw.status), crw.bytes)

			labels := prometheus.Labels{
				"code": strconv.Itoa(crw.status),
			}
			httpResponses.With(labels).Inc()
			httpLatency.With(labels).Observe(duration.Seconds())
			httpResponseSizes.With(labels).Observe(float64(crw.bytes))
		}()

		// If we panic during request, return an internal server error and log.
		defer func() {
			if r := recover(); r != nil {
				rw.WriteHeader(http.StatusInternalServerError)
				logger.Errorf("Panic caught during HTTP handling of %q from %s: %s", req.RequestURI, req.RemoteAddr, r)
			}
		}()

		// Call the next middleware.
		next.ServeHTTP(&crw, req)
	})
}

type capturingResponseWriter struct {
	base http.ResponseWriter

	hasStatus bool
	status    int
	bytes     int64
}

func (crw *capturingResponseWriter) Header() http.Header { return crw.base.Header() }

func (crw *capturingResponseWriter) WriteHeader(status int) {
	if crw.hasStatus {
		return
	}

	crw.status = status
	crw.hasStatus = true
	crw.base.WriteHeader(status)
}

func (crw *capturingResponseWriter) Write(b []byte) (int, error) {
	crw.hasStatus = true
	crw.bytes += int64(len(b))
	return crw.base.Write(b)
}
