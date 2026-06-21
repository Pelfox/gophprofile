package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// MetricsResultSuccess is the shared Prometheus label value for successful operations.
	MetricsResultSuccess = "success"
	// MetricsResultError is the shared Prometheus label value for failed operations.
	MetricsResultError = "error"
)

var (
	// HTTP collectors use chi route patterns instead of raw URLs to keep label cardinality bounded.
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gophprofile",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests.",
		},
		[]string{"method", "route", "status"},
	)
	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "gophprofile",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		},
		[]string{"method", "route", "status"},
	)
	avatarUploadsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gophprofile",
			Subsystem: "avatars",
			Name:      "uploads_total",
			Help:      "Total number of avatar upload attempts.",
		},
		[]string{"result"},
	)
	avatarUploadSizeBytes = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "gophprofile",
			Subsystem: "avatars",
			Name:      "upload_size_bytes",
			Help:      "Avatar upload size in bytes.",
			Buckets: []float64{
				1024,
				10 * 1024,
				100 * 1024,
				512 * 1024,
				1024 * 1024,
				2.5 * 1024 * 1024,
				5 * 1024 * 1024,
				10 * 1024 * 1024,
				20 * 1024 * 1024,
			},
		},
	)
	avatarProcessingTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gophprofile",
			Subsystem: "avatars",
			Name:      "processing_total",
			Help:      "Total number of avatar processing state transitions.",
		},
		[]string{"status"},
	)
	avatarDeletionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gophprofile",
			Subsystem: "avatars",
			Name:      "deletions_total",
			Help:      "Total number of avatar deletion attempts.",
		},
		[]string{"result"},
	)
	queuePublishesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gophprofile",
			Subsystem: "queue",
			Name:      "publishes_total",
			Help:      "Total number of queue publish attempts.",
		},
		[]string{"queue", "result"},
	)
	workerJobsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gophprofile",
			Subsystem: "worker",
			Name:      "jobs_total",
			Help:      "Total number of worker jobs.",
		},
		[]string{"job", "result"},
	)
	workerJobDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "gophprofile",
			Subsystem: "worker",
			Name:      "job_duration_seconds",
			Help:      "Worker job processing duration in seconds.",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		},
		[]string{"job", "result"},
	)
)

func init() {
	// Register all custom collectors in the default registry used by promhttp.Handler.
	prometheus.MustRegister(
		httpRequestsTotal,
		httpRequestDuration,
		avatarUploadsTotal,
		avatarUploadSizeBytes,
		avatarProcessingTotal,
		avatarDeletionsTotal,
		queuePublishesTotal,
		workerJobsTotal,
		workerJobDuration,
	)
}

// statusRecorder captures the final response status for HTTP metrics.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}

	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}

	return r.ResponseWriter.Write(body)
}

// MetricsHandler exposes Prometheus metrics for scraping.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// HTTPMetricsMiddleware records request counters and latency with route labels.
func HTTPMetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}

		ObserveHTTPRequest(r.Method, route, status, time.Since(start))
	})
}

// ObserveHTTPRequest records request count and latency for an HTTP route.
func ObserveHTTPRequest(method string, route string, status int, duration time.Duration) {
	statusCode := strconv.Itoa(status)
	httpRequestsTotal.WithLabelValues(method, route, statusCode).Inc()
	httpRequestDuration.WithLabelValues(method, route, statusCode).Observe(duration.Seconds())
}

// RecordAvatarUpload records an avatar upload attempt and, when present, its size.
func RecordAvatarUpload(result string, sizeBytes int64) {
	avatarUploadsTotal.WithLabelValues(result).Inc()
	if sizeBytes >= 0 {
		avatarUploadSizeBytes.Observe(float64(sizeBytes))
	}
}

// RecordAvatarProcessing records an avatar processing state transition.
func RecordAvatarProcessing(status string) {
	avatarProcessingTotal.WithLabelValues(status).Inc()
}

// RecordAvatarDeletion records an avatar deletion attempt.
func RecordAvatarDeletion(result string) {
	avatarDeletionsTotal.WithLabelValues(result).Inc()
}

// RecordQueuePublish records a RabbitMQ publish attempt.
func RecordQueuePublish(queue string, result string) {
	queuePublishesTotal.WithLabelValues(queue, result).Inc()
}

// ObserveWorkerJob records worker job count and duration.
func ObserveWorkerJob(job string, result string, duration time.Duration) {
	workerJobsTotal.WithLabelValues(job, result).Inc()
	workerJobDuration.WithLabelValues(job, result).Observe(duration.Seconds())
}
