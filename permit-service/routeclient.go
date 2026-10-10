// routeclient.go
// Resilient gRPC client used by Permit Service to call Route Service.
//
// Three resilience patterns are layered on every call:
//   1. Timeout:          each attempt gets its own deadline, so a slow
//                        Route Service can never make us hang.
//   2. Retry + backoff:  transient failures (UNAVAILABLE, DEADLINE_EXCEEDED)
//                        are retried with exponential backoff (e.g. 200ms, 400ms).
//   3. Circuit breaker:  after several consecutive failures the breaker
//                        OPENS and calls fail fast for a cool-down period,
//                        then it goes HALF-OPEN to test if Route Service is back.
package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	routepb "trekking/proto/route"
)

type RouteClient struct {
	conn        *grpc.ClientConn
	client      routepb.RouteServiceClient
	cb          *gobreaker.CircuitBreaker[*routepb.Route]
	timeout     time.Duration // per-attempt deadline
	maxAttempts int           // total attempts including the first
	baseBackoff time.Duration // wait before the 1st retry, doubled each time
}

// isTransient reports whether an error is temporary and worth retrying.
// NOT_FOUND or INVALID_ARGUMENT are real answers, so they are not retried.
func isTransient(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted:
		return true
	}
	return false
}

func NewRouteClient(addr string, timeout time.Duration, maxAttempts int,
	baseBackoff, breakerOpenFor time.Duration, breakerFailures uint32) (*RouteClient, error) {

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	settings := gobreaker.Settings{
		Name:        "route-service",
		MaxRequests: 1,                // trial requests allowed in HALF-OPEN
		Interval:    30 * time.Second, // reset failure counts while CLOSED
		Timeout:     breakerOpenFor,   // how long the breaker stays OPEN
		ReadyToTrip: func(c gobreaker.Counts) bool {
			return c.ConsecutiveFailures >= breakerFailures
		},
		// Only transient errors count as failures. A NOT_FOUND answer means
		// Route Service is healthy, so it must not trip the breaker.
		IsSuccessful: func(err error) bool {
			return err == nil || !isTransient(err)
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			log.Printf("circuit breaker %q changed: %s -> %s", name, from, to)
		},
	}

	return &RouteClient{
		conn:        conn,
		client:      routepb.NewRouteServiceClient(conn),
		cb:          gobreaker.NewCircuitBreaker[*routepb.Route](settings),
		timeout:     timeout,
		maxAttempts: maxAttempts,
		baseBackoff: baseBackoff,
	}, nil
}

func (c *RouteClient) Close() error {
	return c.conn.Close()
}

// GetRoute fetches a route through circuit breaker -> retry -> timeout.
// It always returns a gRPC status error that the caller can pass on.
func (c *RouteClient) GetRoute(ctx context.Context, id int64) (*routepb.Route, error) {
	rt, err := c.cb.Execute(func() (*routepb.Route, error) {
		return c.getWithRetry(ctx, id)
	})
	if err == nil {
		return rt, nil
	}

	// Breaker is OPEN (or HALF-OPEN and busy): fail fast.
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		log.Printf("GetRoute(%d): circuit breaker open, failing fast", id)
		return nil, status.Error(codes.Unavailable,
			"route service is unavailable (circuit breaker open), please try again later")
	}
	// All retries used up on a transient error.
	if isTransient(err) {
		return nil, status.Errorf(codes.Unavailable,
			"route service is unavailable after %d attempts: %s", c.maxAttempts, status.Convert(err).Message())
	}
	// Non-transient answers (e.g. NOT_FOUND) pass straight through.
	return nil, err
}

// getWithRetry performs up to maxAttempts calls, each with its own timeout,
// sleeping baseBackoff, 2*baseBackoff, 4*baseBackoff... between attempts.
func (c *RouteClient) getWithRetry(ctx context.Context, id int64) (*routepb.Route, error) {
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		rt, err := c.client.GetRoute(callCtx, &routepb.GetRouteRequest{Id: id})
		cancel()

		if err == nil {
			return rt, nil
		}
		if !isTransient(err) {
			return nil, err // real answer, do not retry
		}

		lastErr = err
		if attempt == c.maxAttempts {
			break
		}
		backoff := c.baseBackoff * time.Duration(1<<(attempt-1))
		log.Printf("GetRoute(%d) attempt %d/%d failed: %s; retrying in %v",
			id, attempt, c.maxAttempts, status.Code(err), backoff)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		}
	}
	log.Printf("GetRoute(%d): giving up after %d attempts", id, c.maxAttempts)
	return nil, lastErr
}