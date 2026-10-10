// main.go
// API Gateway: the single REST entry point for clients (Postman).
// It forwards each request to Route Service or Permit Service over gRPC
// and maps gRPC errors to HTTP status codes (see errors.go).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	permitpb "trekking/proto/permit"
	routepb "trekking/proto/route"
)

// Gateway holds the gRPC clients used by the HTTP handlers.
type Gateway struct {
	routes  routepb.RouteServiceClient
	permits permitpb.PermitServiceClient
	timeout time.Duration // deadline for each downstream gRPC call
}

// ctx returns a request context with the gateway's deadline applied.
func (g *Gateway) ctx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), g.timeout)
}

// decodeJSON reads a JSON body strictly: unknown fields, wrong types or
// malformed JSON are rejected with 400 before anything reaches a service.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

// parseID reads {id} from the URL and checks it is a positive integer.
func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id in URL must be a positive integer")
		return 0, false
	}
	return id, true
}

// logRequests logs method, path, status and duration of every request.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%v)", r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required environment variable %s", key)
	}
	return v
}

func dial(addr string) *grpc.ClientConn {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to create gRPC client for %s: %v", addr, err)
	}
	return conn
}

func main() {
	httpPort := mustEnv("HTTP_PORT")
	routeAddr := mustEnv("ROUTE_SERVICE_ADDR")
	permitAddr := mustEnv("PERMIT_SERVICE_ADDR")
	timeoutMs, err := strconv.Atoi(mustEnv("REQUEST_TIMEOUT_MS"))
	if err != nil || timeoutMs <= 0 {
		log.Fatal("REQUEST_TIMEOUT_MS must be a positive integer")
	}

	routeConn := dial(routeAddr)
	defer routeConn.Close()
	permitConn := dial(permitAddr)
	defer permitConn.Close()

	g := &Gateway{
		routes:  routepb.NewRouteServiceClient(routeConn),
		permits: permitpb.NewPermitServiceClient(permitConn),
		timeout: time.Duration(timeoutMs) * time.Millisecond,
	}

	// Resource-based URLs (Go 1.22+ method and path patterns).
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /routes", g.createRoute)
	mux.HandleFunc("GET /routes", g.listRoutes)
	mux.HandleFunc("GET /routes/{id}", g.getRoute)
	mux.HandleFunc("PUT /routes/{id}", g.updateRoute)
	mux.HandleFunc("DELETE /routes/{id}", g.deleteRoute)

	mux.HandleFunc("POST /permits", g.createPermit)
	mux.HandleFunc("GET /permits", g.listPermits)
	mux.HandleFunc("GET /permits/{id}", g.getPermit)
	mux.HandleFunc("PUT /permits/{id}", g.updatePermit)
	mux.HandleFunc("DELETE /permits/{id}", g.deletePermit)

	srv := &http.Server{
		Addr:              ":" + httpPort,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		log.Println("shutting down API Gateway...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	log.Printf("API Gateway listening on :%s (routes -> %s, permits -> %s, timeout %dms)",
		httpPort, routeAddr, permitAddr, timeoutMs)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("HTTP server error: %v", err)
	}
}