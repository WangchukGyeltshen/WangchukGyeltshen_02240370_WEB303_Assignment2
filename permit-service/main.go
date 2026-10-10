// main.go
// Entry point for the Permit Service (Service A).
// Database details, the Route Service address and all resilience
// settings come from environment variables.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "trekking/proto/permit"
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required environment variable %s", key)
	}
	return v
}

func mustEnvInt(key string) int {
	n, err := strconv.Atoi(mustEnv(key))
	if err != nil || n <= 0 {
		log.Fatalf("environment variable %s must be a positive integer", key)
	}
	return n
}

func connectDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	for attempt := 1; attempt <= 10; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = db.PingContext(ctx)
		cancel()
		if err == nil {
			return db, nil
		}
		log.Printf("database not ready (attempt %d/10): %v", attempt, err)
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("could not connect to database: %w", err)
}

func main() {
	grpcPort := mustEnv("GRPC_PORT")
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		mustEnv("DB_HOST"), mustEnv("DB_PORT"), mustEnv("DB_USER"),
		mustEnv("DB_PASSWORD"), mustEnv("DB_NAME"))

	// Resilience settings for calls to Route Service.
	routeAddr := mustEnv("ROUTE_SERVICE_ADDR")
	timeout := time.Duration(mustEnvInt("ROUTE_TIMEOUT_MS")) * time.Millisecond
	maxAttempts := mustEnvInt("ROUTE_MAX_ATTEMPTS")
	backoff := time.Duration(mustEnvInt("ROUTE_BACKOFF_MS")) * time.Millisecond
	cbOpenFor := time.Duration(mustEnvInt("CB_OPEN_SECONDS")) * time.Second
	cbFailures := uint32(mustEnvInt("CB_FAILURE_THRESHOLD"))

	db, err := connectDB(dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	repo := NewPermitRepository(db)
	if err := repo.InitSchema(context.Background()); err != nil {
		log.Fatalf("failed to create schema: %v", err)
	}

	routes, err := NewRouteClient(routeAddr, timeout, maxAttempts, backoff, cbOpenFor, cbFailures)
	if err != nil {
		log.Fatalf("failed to create Route Service client: %v", err)
	}
	defer routes.Close()

	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		log.Fatalf("failed to listen on port %s: %v", grpcPort, err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterPermitServiceServer(grpcServer, &permitServer{repo: repo, routes: routes})
	reflection.Register(grpcServer)

	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		log.Println("shutting down Permit Service...")
		grpcServer.GracefulStop()
	}()

	log.Printf("Permit Service listening on :%s (Route Service at %s, timeout=%v, attempts=%d, backoff=%v, breaker opens after %d failures for %v)",
		grpcPort, routeAddr, timeout, maxAttempts, backoff, cbFailures, cbOpenFor)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC server error: %v", err)
	}
}