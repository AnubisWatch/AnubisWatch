package grpcapi

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// authUnavailable is implemented by authentication errors that say nothing
// about the caller's credentials: the cluster could not prove quorum, or the
// shared store failed.
type authUnavailable interface{ AuthUnavailable() bool }

// authError maps a failed Authenticate to a gRPC status. An outage is
// Unavailable, a retryable code: Unauthenticated would make a client with a
// still-valid token drop its credentials. Anything else is rejected as
// Unauthenticated with the given message.
func authError(err error, msg string) error {
	var u authUnavailable
	if errors.As(err, &u) && u.AuthUnavailable() {
		return status.Error(codes.Unavailable, "authentication is temporarily unavailable; retry")
	}
	return status.Error(codes.Unauthenticated, msg)
}
