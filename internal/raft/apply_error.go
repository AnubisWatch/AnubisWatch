package raft

import "errors"

// The apply path has to tell two kinds of failure apart.
//
// A transient failure -- a storage or IO error -- may still succeed on a
// later attempt, so the node retries the committed entry. A deterministic
// failure -- an undecodable payload, a rejected command, an unknown op --
// can never succeed, so retrying it forever would wedge the node at that
// index and stall snapshotting and log compaction with it.
//
// retryableError marks the first kind. Every other error is deterministic by
// default, so a new failure mode is skipped rather than retried forever.

// retryableError marks an apply failure that a later attempt may still
// succeed at.
type retryableError struct {
	err error
}

func (e *retryableError) Error() string { return e.err.Error() }

func (e *retryableError) Unwrap() error { return e.err }

// retryable marks err as a transient failure. It returns nil for a nil error.
func retryable(err error) error {
	if err == nil {
		return nil
	}
	return &retryableError{err: err}
}

// isRetryableApplyError reports whether err is, or wraps, a transient
// failure that may be retried.
func isRetryableApplyError(err error) bool {
	var transient *retryableError
	return errors.As(err, &transient)
}

// Retryable marks err as a transient failure, so the node retries the
// committed entry instead of skipping it.
//
// Command handlers own the meaning of their commands and therefore the
// classification of their failures. A handler that rejects a command
// deterministically (a payload it can never decode, a state change it
// refuses) must return the plain error: it is skipped and the node keeps
// applying the rest of the log. A handler that fails for a transient reason
// (a storage error, a temporarily unavailable peer) must wrap the error with
// Retryable, otherwise the command is lost.
func Retryable(err error) error {
	return retryable(err)
}
