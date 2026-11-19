package grpcapi

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type outageErr struct{}

func (outageErr) Error() string         { return "authentication quorum unavailable" }
func (outageErr) AuthUnavailable() bool { return true }

// A quorum outage must reach a gRPC client as a retryable Unavailable, never
// as Unauthenticated, which tells it to drop a token that is still valid.
func TestAuthErrorSeparatesOutageFromInvalidToken(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"outage", outageErr{}, codes.Unavailable},
		{"wrapped outage", errors.Join(errors.New("ctx"), outageErr{}), codes.Unavailable},
		{"bad token", errors.New("invalid token"), codes.Unauthenticated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := status.Code(authError(tt.err, "invalid token")); got != tt.want {
				t.Fatalf("code = %v, want %v", got, tt.want)
			}
		})
	}
}
