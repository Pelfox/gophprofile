package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pelfox/gophprofile/internal/controllers"
	"github.com/pelfox/gophprofile/internal/healthcheck"
	"github.com/rs/zerolog"
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
		passthroughMiddleware,
		passthroughMiddleware,
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

func TestUploadRouteRateLimit(t *testing.T) {
	router := newRouter(
		controllers.NewAvatarsController(zerolog.Nop(), nil),
		http.NotFoundHandler(),
		healthcheck.NewHandler(healthyDatabase{}, openRabbitMQ{}),
		newClientIPRateLimit(100, time.Second),
		newClientIPRateLimit(1, time.Hour),
	)

	for attempt, wantStatus := range []int{
		http.StatusUnprocessableEntity,
		http.StatusTooManyRequests,
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", nil)
		request.Header.Set(
			"X-User-ID",
			"11111111-1111-1111-1111-111111111111",
		)

		router.ServeHTTP(recorder, request)

		if recorder.Code != wantStatus {
			t.Fatalf(
				"attempt %d: expected status %d, got %d",
				attempt+1,
				wantStatus,
				recorder.Code,
			)
		}
		if wantStatus == http.StatusTooManyRequests {
			assertRateLimitResponse(t, recorder)
		}
	}
}

func TestRateLimitUsesIngressClientIP(t *testing.T) {
	router := newRouter(
		controllers.NewAvatarsController(zerolog.Nop(), nil),
		http.NotFoundHandler(),
		healthcheck.NewHandler(healthyDatabase{}, openRabbitMQ{}),
		newClientIPRateLimit(1, time.Hour),
		passthroughMiddleware,
	)

	for _, clientIP := range []string{"192.0.2.10", "192.0.2.11"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodGet,
			"/api/v1/avatars/not-a-uuid",
			nil,
		)
		request.Header.Set(trustedClientIPHeader, clientIP)
		router.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf(
				"client %s: expected status %d, got %d",
				clientIP,
				http.StatusBadRequest,
				recorder.Code,
			)
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/avatars/not-a-uuid",
		nil,
	)
	request.Header.Set(trustedClientIPHeader, "192.0.2.10")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf(
			"expected status %d, got %d",
			http.StatusTooManyRequests,
			recorder.Code,
		)
	}
}

func TestAPIRateLimitExcludesHealthEndpoints(t *testing.T) {
	router := newRouter(
		controllers.NewAvatarsController(zerolog.Nop(), nil),
		http.NotFoundHandler(),
		healthcheck.NewHandler(healthyDatabase{}, openRabbitMQ{}),
		newClientIPRateLimit(1, time.Hour),
		newClientIPRateLimit(1, time.Hour),
	)

	for attempt, wantStatus := range []int{
		http.StatusBadRequest,
		http.StatusTooManyRequests,
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodGet,
			"/api/v1/avatars/not-a-uuid",
			nil,
		)
		router.ServeHTTP(recorder, request)

		if recorder.Code != wantStatus {
			t.Fatalf(
				"attempt %d: expected status %d, got %d",
				attempt+1,
				wantStatus,
				recorder.Code,
			)
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"expected health endpoint status %d, got %d",
			http.StatusOK,
			recorder.Code,
		)
	}
}

func passthroughMiddleware(next http.Handler) http.Handler {
	return next
}

func assertRateLimitResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) {
	t.Helper()

	if recorder.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", got)
	}

	var body map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode rate limit response: %v", err)
	}
	if got := body["message"]; got != "Too many requests." {
		t.Fatalf("unexpected rate limit message %q", got)
	}
}
