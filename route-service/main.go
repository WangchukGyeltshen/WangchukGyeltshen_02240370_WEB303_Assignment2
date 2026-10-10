// main.go
// Entry point for the Route Service (Service B).
// All configuration comes from environment variables (no hard-coded
// ports or database details).
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "trekking/proto/route"
)

// mustEnv reads a required environment variable or stops the service.
func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required environment variable %s", key)
	}
	return v
}

// connectDB opens the database and retries while Postgres is starting up.
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

	db, err := connectDB(dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	repo := NewRouteRepository(db)
	if err := repo.InitSchema(context.Background()); err != nil {
		log.Fatalf("failed to create schema: %v", err)
	}

	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		log.Fatalf("failed to listen on port %s: %v", grpcPort, err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterRouteServiceServer(grpcServer, &routeServer{repo: repo})
	reflection.Register(grpcServer) // lets tools like grpcurl/Postman discover the API

	// Stop gracefully when Docker sends SIGTERM (e.g. docker compose stop).
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		log.Println("shutting down Route Service...")
		grpcServer.GracefulStop()
	}()

	log.Printf("Route Service listening on :%s", grpcPort)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC server error: %v", err)
	}
}