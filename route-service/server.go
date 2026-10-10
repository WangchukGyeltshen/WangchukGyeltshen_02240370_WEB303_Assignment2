// server.go
// gRPC handlers for the Route Service. Every request is validated here
// before it reaches the database, and repository errors are converted
// into proper gRPC status codes (NOT_FOUND, INVALID_ARGUMENT, etc.).
package main

import (
	"context"
	"errors"
	"log"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "trekking/proto/route"
)

var validDifficulties = map[string]bool{"EASY": true, "MODERATE": true, "HARD": true}

// routeServer implements pb.RouteServiceServer.
type routeServer struct {
	pb.UnimplementedRouteServiceServer
	repo *RouteRepository
}

// validateRoute checks all route fields and returns INVALID_ARGUMENT
// listing every problem found, so the client can fix them in one go.
func validateRoute(name, dzongkhag, difficulty string, days, altitude, group int32) error {
	var problems []string

	if name == "" {
		problems = append(problems, "name is required")
	} else if len(name) > 100 {
		problems = append(problems, "name must be at most 100 characters")
	}
	if dzongkhag == "" {
		problems = append(problems, "dzongkhag is required")
	}
	if !validDifficulties[difficulty] {
		problems = append(problems, "difficulty must be EASY, MODERATE or HARD")
	}
	if days < 1 || days > 60 {
		problems = append(problems, "duration_days must be between 1 and 60")
	}
	if altitude < 500 || altitude > 8000 {
		problems = append(problems, "max_altitude_m must be between 500 and 8000")
	}
	if group < 1 || group > 50 {
		problems = append(problems, "max_group_size must be between 1 and 50")
	}

	if len(problems) > 0 {
		return status.Errorf(codes.InvalidArgument, "invalid route: %s", strings.Join(problems, "; "))
	}
	return nil
}

func validateID(id int64) error {
	if id <= 0 {
		return status.Error(codes.InvalidArgument, "id must be a positive number")
	}
	return nil
}

// toStatus maps repository errors to gRPC status errors.
func toStatus(op string, err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrDuplicate):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		log.Printf("%s failed: %v", op, err)
		return status.Error(codes.Internal, "internal database error")
	}
}

func (s *routeServer) CreateRoute(ctx context.Context, in *pb.CreateRouteRequest) (*pb.Route, error) {
	// Normalise input before validating.
	in.Name = strings.TrimSpace(in.Name)
	in.Dzongkhag = strings.TrimSpace(in.Dzongkhag)
	in.Difficulty = strings.ToUpper(strings.TrimSpace(in.Difficulty))

	if err := validateRoute(in.Name, in.Dzongkhag, in.Difficulty, in.DurationDays, in.MaxAltitudeM, in.MaxGroupSize); err != nil {
		return nil, err
	}
	rt, err := s.repo.Create(ctx, in)
	if err != nil {
		return nil, toStatus("CreateRoute", err)
	}
	log.Printf("CreateRoute: created route %d (%s)", rt.Id, rt.Name)
	return rt, nil
}

func (s *routeServer) GetRoute(ctx context.Context, in *pb.GetRouteRequest) (*pb.Route, error) {
	if err := validateID(in.Id); err != nil {
		return nil, err
	}
	rt, err := s.repo.Get(ctx, in.Id)
	if err != nil {
		return nil, toStatus("GetRoute", err)
	}
	log.Printf("GetRoute: returned route %d", rt.Id)
	return rt, nil
}

func (s *routeServer) ListRoutes(ctx context.Context, _ *pb.ListRoutesRequest) (*pb.ListRoutesResponse, error) {
	routes, err := s.repo.List(ctx)
	if err != nil {
		return nil, toStatus("ListRoutes", err)
	}
	return &pb.ListRoutesResponse{Routes: routes}, nil
}

func (s *routeServer) UpdateRoute(ctx context.Context, in *pb.UpdateRouteRequest) (*pb.Route, error) {
	if err := validateID(in.Id); err != nil {
		return nil, err
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Dzongkhag = strings.TrimSpace(in.Dzongkhag)
	in.Difficulty = strings.ToUpper(strings.TrimSpace(in.Difficulty))

	if err := validateRoute(in.Name, in.Dzongkhag, in.Difficulty, in.DurationDays, in.MaxAltitudeM, in.MaxGroupSize); err != nil {
		return nil, err
	}
	rt, err := s.repo.Update(ctx, in)
	if err != nil {
		return nil, toStatus("UpdateRoute", err)
	}
	log.Printf("UpdateRoute: updated route %d", rt.Id)
	return rt, nil
}

func (s *routeServer) DeleteRoute(ctx context.Context, in *pb.DeleteRouteRequest) (*pb.DeleteRouteResponse, error) {
	if err := validateID(in.Id); err != nil {
		return nil, err
	}
	if err := s.repo.Delete(ctx, in.Id); err != nil {
		return nil, toStatus("DeleteRoute", err)
	}
	log.Printf("DeleteRoute: deleted route %d", in.Id)
	return &pb.DeleteRouteResponse{Message: "route deleted"}, nil
}