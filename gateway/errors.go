// errors.go
// Helpers for writing JSON responses and translating gRPC status codes
// into HTTP status codes (e.g. NOT_FOUND -> 404, UNAVAILABLE -> 503).
package main

import (
	"encoding/json"
	"log"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// grpcToHTTP maps a gRPC status code to the matching HTTP status code.
func grpcToHTTP(c codes.Code) int {
	switch c {
	case codes.OK:
		return http.StatusOK
	case codes.InvalidArgument, codes.OutOfRange, codes.FailedPrecondition:
		return http.StatusBadRequest // 400
	case codes.NotFound:
		return http.StatusNotFound // 404
	case codes.AlreadyExists:
		return http.StatusConflict // 409
	case codes.Unauthenticated:
		return http.StatusUnauthorized // 401
	case codes.PermissionDenied:
		return http.StatusForbidden // 403
	case codes.Unimplemented:
		return http.StatusNotImplemented // 501
	case codes.Unavailable:
		return http.StatusServiceUnavailable // 503
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout // 504
	default:
		return http.StatusInternalServerError // 500
	}
}

// errorResponse is the JSON body returned for every error.
type errorResponse struct {
	Status   int    `json:"status"`
	GRPCCode string `json:"grpc_code,omitempty"`
	Error    string `json:"error"`
}

func writeJSON(w http.ResponseWriter, httpStatus int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("failed to write response: %v", err)
	}
}

// writeError sends an error that was detected in the gateway itself.
func writeError(w http.ResponseWriter, httpStatus int, msg string) {
	writeJSON(w, httpStatus, errorResponse{Status: httpStatus, Error: msg})
}

// writeGRPCError converts an error returned by a service into HTTP.
func writeGRPCError(w http.ResponseWriter, err error) {
	st := status.Convert(err)
	httpStatus := grpcToHTTP(st.Code())
	log.Printf("gRPC error %s -> HTTP %d: %s", st.Code(), httpStatus, st.Message())
	writeJSON(w, httpStatus, errorResponse{
		Status:   httpStatus,
		GRPCCode: st.Code().String(),
		Error:    st.Message(),
	})
}