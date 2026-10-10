// permits.go
// REST handlers for /permits, forwarded to Permit Service over gRPC.
//
//   POST   /permits        create a permit
//   GET    /permits        list permits
//   GET    /permits/{id}   get one permit (with route details)
//   PUT    /permits/{id}   update a permit
//   DELETE /permits/{id}   delete a permit
package main

import (
	"fmt"
	"net/http"

	permitpb "trekking/proto/permit"
)

// permitInput is the JSON body for POST. Status is ignored on create
// (new permits always start as PENDING).
type permitInput struct {
	RouteID     int64  `json:"route_id"`
	TrekkerName string `json:"trekker_name"`
	Email       string `json:"email"`
	GroupSize   int32  `json:"group_size"`
	StartDate   string `json:"start_date"`
	Status      string `json:"status"`
}

func (g *Gateway) createPermit(w http.ResponseWriter, r *http.Request) {
	var in permitInput
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := g.ctx(r)
	defer cancel()

	p, err := g.permits.CreatePermit(ctx, &permitpb.CreatePermitRequest{
		RouteId: in.RouteID, TrekkerName: in.TrekkerName, Email: in.Email,
		GroupSize: in.GroupSize, StartDate: in.StartDate,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/permits/%d", p.Id))
	writeJSON(w, http.StatusCreated, p)
}

func (g *Gateway) listPermits(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := g.ctx(r)
	defer cancel()

	resp, err := g.permits.ListPermits(ctx, &permitpb.ListPermitsRequest{})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp.Permits)
}

func (g *Gateway) getPermit(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	ctx, cancel := g.ctx(r)
	defer cancel()

	p, err := g.permits.GetPermit(ctx, &permitpb.GetPermitRequest{Id: id})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (g *Gateway) updatePermit(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var in permitInput
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := g.ctx(r)
	defer cancel()

	p, err := g.permits.UpdatePermit(ctx, &permitpb.UpdatePermitRequest{
		Id: id, RouteId: in.RouteID, TrekkerName: in.TrekkerName, Email: in.Email,
		GroupSize: in.GroupSize, StartDate: in.StartDate, Status: in.Status,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (g *Gateway) deletePermit(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	ctx, cancel := g.ctx(r)
	defer cancel()

	resp, err := g.permits.DeletePermit(ctx, &permitpb.DeletePermitRequest{Id: id})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}