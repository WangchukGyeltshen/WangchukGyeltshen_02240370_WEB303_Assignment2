// routes.go
// REST handlers for /routes. Each handler converts the HTTP request
// into a gRPC call to Route Service and converts the reply back to JSON.
//
//   POST   /routes        create a route
//   GET    /routes        list routes
//   GET    /routes/{id}   get one route
//   PUT    /routes/{id}   update a route
//   DELETE /routes/{id}   delete a route
package main

import (
	"fmt"
	"net/http"

	routepb "trekking/proto/route"
)

// routeInput is the JSON body accepted for POST and PUT.
type routeInput struct {
	Name         string `json:"name"`
	Dzongkhag    string `json:"dzongkhag"`
	Difficulty   string `json:"difficulty"`
	DurationDays int32  `json:"duration_days"`
	MaxAltitudeM int32  `json:"max_altitude_m"`
	MaxGroupSize int32  `json:"max_group_size"`
}

func (g *Gateway) createRoute(w http.ResponseWriter, r *http.Request) {
	var in routeInput
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := g.ctx(r)
	defer cancel()

	rt, err := g.routes.CreateRoute(ctx, &routepb.CreateRouteRequest{
		Name: in.Name, Dzongkhag: in.Dzongkhag, Difficulty: in.Difficulty,
		DurationDays: in.DurationDays, MaxAltitudeM: in.MaxAltitudeM, MaxGroupSize: in.MaxGroupSize,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/routes/%d", rt.Id))
	writeJSON(w, http.StatusCreated, rt)
}

func (g *Gateway) listRoutes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := g.ctx(r)
	defer cancel()

	resp, err := g.routes.ListRoutes(ctx, &routepb.ListRoutesRequest{})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp.Routes)
}

func (g *Gateway) getRoute(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	ctx, cancel := g.ctx(r)
	defer cancel()

	rt, err := g.routes.GetRoute(ctx, &routepb.GetRouteRequest{Id: id})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rt)
}

func (g *Gateway) updateRoute(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var in routeInput
	if !decodeJSON(w, r, &in) {
		return
	}
	ctx, cancel := g.ctx(r)
	defer cancel()

	rt, err := g.routes.UpdateRoute(ctx, &routepb.UpdateRouteRequest{
		Id: id, Name: in.Name, Dzongkhag: in.Dzongkhag, Difficulty: in.Difficulty,
		DurationDays: in.DurationDays, MaxAltitudeM: in.MaxAltitudeM, MaxGroupSize: in.MaxGroupSize,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rt)
}

func (g *Gateway) deleteRoute(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	ctx, cancel := g.ctx(r)
	defer cancel()

	resp, err := g.routes.DeleteRoute(ctx, &routepb.DeleteRouteRequest{Id: id})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}