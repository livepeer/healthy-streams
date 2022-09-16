package api

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/golang/glog"
	"github.com/julienschmidt/httprouter"
	"github.com/livepeer/livepeer-data/health"
	"github.com/livepeer/livepeer-data/pkg/data"
	"github.com/livepeer/livepeer-data/pkg/jsse"
	"github.com/livepeer/livepeer-data/views"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	sseRetryBackoff = 10 * time.Second
	ssePingDelay    = 20 * time.Second
	sseBufferSize   = 128

	streamIDParam = "streamId"
	assetIDParam  = "assetId"
)

type APIHandlerOptions struct {
	ServerName, APIRoot, AuthURL  string
	RegionalHostFormat, OwnRegion string
	Prometheus                    bool
}

type apiHandler struct {
	opts      APIHandlerOptions
	serverCtx context.Context
	core      *health.Core
	views     *views.Client
}

func NewHandler(serverCtx context.Context, opts APIHandlerOptions, healthcore *health.Core, views *views.Client) http.Handler {
	handler := &apiHandler{opts, serverCtx, healthcore, views}

	router := httprouter.New()
	router.HandlerFunc("GET", "/_healthz", handler.healthcheck)
	if opts.Prometheus {
		router.Handler("GET", "/metrics", promhttp.Handler())
	}
	addStreamHealthHandlers(router, handler)
	addViewershipHandlers(router, handler)

	globalMiddlewares := []middleware{handler.cors()}
	return prepareHandler("", false, router, globalMiddlewares...)
}

func addStreamHealthHandlers(router *httprouter.Router, handler *apiHandler) {
	healthcore, opts := handler.core, handler.opts
	middlewares := []middleware{
		streamStatus(healthcore),
		regionProxy(opts.RegionalHostFormat, opts.OwnRegion),
	}
	if opts.AuthURL != "" {
		middlewares = append(middlewares, authorization(opts.AuthURL))
	}
	addApiHandler := func(apiPath, name string, handler http.HandlerFunc) {
		fullPath := path.Join(opts.APIRoot, "/stream/:"+streamIDParam, apiPath)
		fullHandler := prepareHandlerFunc(name, opts.Prometheus, handler, middlewares...)
		router.Handler("GET", fullPath, fullHandler)
	}
	addApiHandler("/health", "get_stream_health", handler.getStreamHealth)
	addApiHandler("/events", "stream_health_events", handler.subscribeEvents)
}

func addViewershipHandlers(router *httprouter.Router, handler *apiHandler) {
	opts := handler.opts
	middlewares := []middleware{}
	if opts.AuthURL != "" {
		middlewares = append(middlewares, authorization(opts.AuthURL))
	}
	addApiHandler := func(apiPath, name string, handler http.HandlerFunc) {
		fullPath := path.Join(opts.APIRoot, "/views/:"+assetIDParam, apiPath)
		fullHandler := prepareHandlerFunc(name, opts.Prometheus, handler, middlewares...)
		router.Handler("GET", fullPath, fullHandler)
	}
	addApiHandler("/total", "get_total_views", handler.getTotalViews)
}

func (h *apiHandler) cors() middleware {
	return inlineMiddleware(func(rw http.ResponseWriter, r *http.Request, next http.Handler) {
		if h.opts.ServerName != "" {
			rw.Header().Set("Server", h.opts.ServerName)
		}
		rw.Header().Set("Access-Control-Allow-Origin", "*")
		rw.Header().Set("Access-Control-Allow-Headers", "*")
		if origin := r.Header.Get("Origin"); origin != "" {
			rw.Header().Set("Access-Control-Allow-Origin", origin)
			rw.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		next.ServeHTTP(rw, r)
	})
}

func (h *apiHandler) healthcheck(rw http.ResponseWriter, r *http.Request) {
	status := http.StatusOK
	if !h.core.IsHealthy() {
		status = http.StatusServiceUnavailable
	}
	rw.WriteHeader(status)
}

func (h *apiHandler) getTotalViews(rw http.ResponseWriter, r *http.Request) {
	views, err := h.views.GetTotalViews(r.Context(), apiParam(r, assetIDParam))
	if err != nil {
		respondError(rw, http.StatusInternalServerError, err)
		return
	}
	respondJson(rw, http.StatusOK, views)
}

func (h *apiHandler) getStreamHealth(rw http.ResponseWriter, r *http.Request) {
	respondJson(rw, http.StatusOK, getStreamStatus(r))
}

func (h *apiHandler) subscribeEvents(rw http.ResponseWriter, r *http.Request) {
	var (
		streamStatus = getStreamStatus(r)
		streamID     = streamStatus.ID
		sseOpts      = jsse.InitOptions(r).
				WithClientRetryBackoff(sseRetryBackoff).
				WithPing(ssePingDelay)

		lastEventID, err = parseInputUUID(sseOpts.LastEventID)
		from, err1       = parseInputTimestamp(r.URL.Query().Get("from"))
		to, err2         = parseInputTimestamp(r.URL.Query().Get("to"))
		mustFindLast, _  = strconv.ParseBool(r.URL.Query().Get("mustFindLast"))
	)
	if errs := nonNilErrs(err, err1, err2); len(errs) > 0 {
		respondError(rw, http.StatusBadRequest, errs...)
		return
	}

	var (
		pastEvents   []data.Event
		subscription <-chan data.Event
	)
	ctx, cancel := unionCtx(r.Context(), h.serverCtx)
	defer cancel()
	if to != nil {
		if from == nil {
			respondError(rw, http.StatusBadRequest, errors.New("query 'from' is required when using 'to'"))
			return
		}
		pastEvents, err = h.core.GetPastEvents(streamID, from, to)
	} else {
		pastEvents, subscription, err = h.core.SubscribeEvents(ctx, streamID, lastEventID, from)
		if err == health.ErrEventNotFound && !mustFindLast {
			pastEvents, subscription, err = h.core.SubscribeEvents(ctx, streamID, nil, nil)
		}
	}
	if err != nil {
		respondError(rw, http.StatusInternalServerError, err)
		return
	}

	sseEvents := makeSSEEventChan(ctx, pastEvents, subscription)
	err = jsse.ServeEvents(ctx, sseOpts, rw, sseEvents)
	if err != nil {
		status := http.StatusInternalServerError
		if httpErr, ok := err.(jsse.HTTPError); ok {
			status, err = httpErr.StatusCode, httpErr.Cause
		}
		glog.Errorf("Error serving events. err=%q", err)
		respondError(rw, status, err)
	}
}

func makeSSEEventChan(ctx context.Context, pastEvents []data.Event, subscription <-chan data.Event) <-chan jsse.Event {
	if subscription == nil {
		events := make(chan jsse.Event, len(pastEvents))
		for _, evt := range pastEvents {
			sendEvent(ctx, events, evt)
		}
		close(events)
		return events
	}
	events := make(chan jsse.Event, sseBufferSize)
	go func() {
		defer close(events)
		for _, evt := range pastEvents {
			if !sendEvent(ctx, events, evt) {
				return
			}
		}
		for evt := range subscription {
			if !sendEvent(ctx, events, evt) {
				return
			}
		}
	}()
	return events
}

func sendEvent(ctx context.Context, dest chan<- jsse.Event, evt data.Event) bool {
	sseEvt, err := toSSEEvent(evt)
	if err != nil {
		glog.Errorf("Skipping bad event due to error converting to SSE. evtID=%q, streamID=%q, err=%q", evt.ID(), evt.StreamID(), err)
		return true
	}
	select {
	case dest <- sseEvt:
		return true
	case <-ctx.Done():
		return false
	}
}
