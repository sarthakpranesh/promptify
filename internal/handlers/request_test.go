package handlers

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIsHTTPS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{
			name: "plain http",
			req:  httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil),
			want: false,
		},
		{
			name: "direct tls",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "https://localhost:8080/", nil)
				r.TLS = &tls.ConnectionState{}
				return r
			}(),
			want: true,
		},
		{
			name: "forwarded https",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
				r.Header.Set("X-Forwarded-Proto", "https")
				return r
			}(),
			want: true,
		},
		{
			name: "forwarded https comma chain",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
				r.Header.Set("X-Forwarded-Proto", "https, http")
				return r
			}(),
			want: true,
		},
		{
			name: "forwarded http",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
				r.Header.Set("X-Forwarded-Proto", "http")
				return r
			}(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := requestIsHTTPS(tt.req); got != tt.want {
				t.Fatalf("requestIsHTTPS() = %v, want %v", got, tt.want)
			}
		})
	}
}
