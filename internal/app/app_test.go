package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pelfox/gophprofile/internal/healthcheck"
)

type healthyDatabase struct{}

func (healthyDatabase) Ping(context.Context) error {
	return nil
}

type openRabbitMQ struct{}

func (openRabbitMQ) IsClosed() bool {
	return false
}

func TestHealthcheckRoutes(t *testing.T) {
	router := newRouter(
		nil,
		http.NotFoundHandler(),
		healthcheck.NewHandler(healthyDatabase{}, openRabbitMQ{}),
	)

	for _, path := range []string{"/health/live", "/health/ready"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)

			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
			}
		})
	}
}
