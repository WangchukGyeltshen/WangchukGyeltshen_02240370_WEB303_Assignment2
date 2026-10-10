// server.go
// gRPC handlers for the Permit Service (Service A).
//
// - Create/Update: validate input, then call Route Service to confirm the
//   route exists and the group size fits. If Route Service is down the
//   request is rejected with UNAVAILABLE (nothing is stored).
// - Get/List: read permits from permits_db, then enrich them with route
//   details from Route Service. If Route Service is down, a FALLBACK
//   response is returned (permit data without route details).
package main

import (
	"context"
	"errors"
	"log"
	"net/mail"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "trekking/proto/permit"
	routepb "trekking/proto/route"
)

var validStatuses = map[string]bool{"PENDING": true, "APPROVED": true, "CANCELLED": true}

type permitServer struct {
	pb.UnimplementedPermitServiceServer
	repo   *PermitRepository
	routes *RouteClient
}

// ---------- validation ----------

func validatePermit(routeID int64, name, email string, group int32, startDate string, mustBeFuture bool) []string {
	var problems []string

	if routeID <= 0 {
		problems = append(problems, "route_id must be a positive number")
	}
	if len(name) < 2 || len(name) > 100 {
		problems = append(problems, "trekker_name must be 2 to 100 characters")
	}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		problems = append(problems, "email must be a valid email address")
	}
	if group < 1 || group > 50 {
		problems = append(problems, "group_size must be between 1 and 50")
	}
	start, err := time.Parse(dateLayout, startDate)
	if err != nil {
		problems = append(problems, "start_date must be in YYYY-MM-DD format")
	} else if mustBeFuture && start.Before(time.Now().Truncate(24*time.Hour)) {
		problems = append(problems, "start_date cannot be in the past")
	}
	return problems
}

func invalid(problems []string) error {
	return status.Errorf(codes.InvalidArgument, "invalid permit: %s", strings.Join(problems, "; "))
}

func validateID(id int64) error {
	if id <= 0 {
		return status.Error(codes.InvalidArgument, "id must be a positive number")
	}
	return nil
}

func toStatus(op string, err error) error {
	if errors.Is(err, ErrNotFound) {
		return status.Error(codes.NotFound, err.Error())
	}
	log.Printf("%s failed: %v", op, err)
	return status.Error(codes.Internal, "internal database error")
}

// ---------- calls to Route Service ----------

// checkRoute confirms (via gRPC) that the route exists and the group fits.
func (s *permitServer) checkRoute(ctx context.Context, routeID int64, group int32) (*routepb.Route, error) {
	rt, err := s.routes.GetRoute(ctx, routeID)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, status.Errorf(codes.InvalidArgument, "route_id %d does not exist", routeID)
		}
		return nil, err // UNAVAILABLE from the resilient client
	}
	if group > rt.MaxGroupSize {
		return nil, status.Errorf(codes.InvalidArgument,
			"group_size %d exceeds the maximum of %d for route %q", group, rt.MaxGroupSize, rt.Name)
	}
	return rt, nil
}

// enrich adds route details to a permit, with a fallback when
// Route Service cannot be reached.
func (s *permitServer) enrich(ctx context.Context, p *pb.Permit) {
	rt, err := s.routes.GetRoute(ctx, p.RouteId)
	switch {
	case err == nil:
		p.RouteName, p.RouteDzongkhag, p.RouteLookup = rt.Name, rt.Dzongkhag, "OK"
	case status.Code(err) == codes.NotFound:
		p.RouteLookup = "ROUTE_NOT_FOUND: the route for this permit no longer exists"
	default:
		p.RouteLookup = "FALLBACK: Route Service is unavailable, route details omitted"
	}
}

// ---------- handlers ----------

func (s *permitServer) CreatePermit(ctx context.Context, in *pb.CreatePermitRequest) (*pb.Permit, error) {
	in.TrekkerName = strings.TrimSpace(in.TrekkerName)
	in.Email = strings.TrimSpace(in.Email)
	in.StartDate = strings.TrimSpace(in.StartDate)

	if p := validatePermit(in.RouteId, in.TrekkerName, in.Email, in.GroupSize, in.StartDate, true); len(p) > 0 {
		return nil, invalid(p)
	}
	rt, err := s.checkRoute(ctx, in.RouteId, in.GroupSize)
	if err != nil {
		return nil, err
	}
	p, err := s.repo.Create(ctx, in)
	if err != nil {
		return nil, toStatus("CreatePermit", err)
	}
	p.RouteName, p.RouteDzongkhag, p.RouteLookup = rt.Name, rt.Dzongkhag, "OK"
	log.Printf("CreatePermit: created permit %d for route %d", p.Id, p.RouteId)
	return p, nil
}

func (s *permitServer) GetPermit(ctx context.Context, in *pb.GetPermitRequest) (*pb.Permit, error) {
	if err := validateID(in.Id); err != nil {
		return nil, err
	}
	p, err := s.repo.Get(ctx, in.Id)
	if err != nil {
		return nil, toStatus("GetPermit", err)
	}
	s.enrich(ctx, p)
	return p, nil
}

func (s *permitServer) ListPermits(ctx context.Context, _ *pb.ListPermitsRequest) (*pb.ListPermitsResponse, error) {
	permits, err := s.repo.List(ctx)
	if err != nil {
		return nil, toStatus("ListPermits", err)
	}
	for _, p := range permits {
		s.enrich(ctx, p)
	}
	return &pb.ListPermitsResponse{Permits: permits}, nil
}

func (s *permitServer) UpdatePermit(ctx context.Context, in *pb.UpdatePermitRequest) (*pb.Permit, error) {
	if err := validateID(in.Id); err != nil {
		return nil, err
	}
	in.TrekkerName = strings.TrimSpace(in.TrekkerName)
	in.Email = strings.TrimSpace(in.Email)
	in.StartDate = strings.TrimSpace(in.StartDate)
	in.Status = strings.ToUpper(strings.TrimSpace(in.Status))

	problems := validatePermit(in.RouteId, in.TrekkerName, in.Email, in.GroupSize, in.StartDate, false)
	if !validStatuses[in.Status] {
		problems = append(problems, "status must be PENDING, APPROVED or CANCELLED")
	}
	if len(problems) > 0 {
		return nil, invalid(problems)
	}
	// Make sure the permit exists before calling Route Service.
	if _, err := s.repo.Get(ctx, in.Id); err != nil {
		return nil, toStatus("UpdatePermit", err)
	}
	rt, err := s.checkRoute(ctx, in.RouteId, in.GroupSize)
	if err != nil {
		return nil, err
	}
	p, err := s.repo.Update(ctx, in)
	if err != nil {
		return nil, toStatus("UpdatePermit", err)
	}
	p.RouteName, p.RouteDzongkhag, p.RouteLookup = rt.Name, rt.Dzongkhag, "OK"
	log.Printf("UpdatePermit: updated permit %d", p.Id)
	return p, nil
}

func (s *permitServer) DeletePermit(ctx context.Context, in *pb.DeletePermitRequest) (*pb.DeletePermitResponse, error) {
	if err := validateID(in.Id); err != nil {
		return nil, err
	}
	if err := s.repo.Delete(ctx, in.Id); err != nil {
		return nil, toStatus("DeletePermit", err)
	}
	log.Printf("DeletePermit: deleted permit %d", in.Id)
	return &pb.DeletePermitResponse{Message: "permit deleted"}, nil
}