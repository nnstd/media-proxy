package prometheus

//
// Copyright (c) 2021-present Ankur Srivastava and Contributors
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/gofiber/fiber/v2/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"go.opentelemetry.io/otel/trace"
)

// FiberPrometheus ...
type FiberPrometheus struct {
	gatherer prometheus.Gatherer
	registry prometheus.Registerer

	constLabels prometheus.Labels

	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	requestInFlight *prometheus.GaugeVec

	defaultURL        string
	skipPaths         map[string]bool
	ignoreStatusCodes map[int]bool
	registeredRoutes  map[string]struct{}
	routesOnce        sync.Once
}

func create(registry prometheus.Registerer, serviceName, namespace, subsystem string, labels map[string]string) *FiberPrometheus {
	if registry == nil {
		registry = prometheus.NewRegistry()
	}

	constLabels := make(prometheus.Labels)
	if serviceName != "" {
		constLabels["service"] = serviceName
	}
	for label, value := range labels {
		constLabels[label] = value
	}

	counter := promauto.With(registry).NewCounterVec(
		prometheus.CounterOpts{
			Name:        prometheus.BuildFQName(namespace, subsystem, "requests_total"),
			Help:        "Count all http requests by status code, method and path.",
			ConstLabels: constLabels,
		},
		[]string{"status_code", "method", "path"},
	)

	histogram := promauto.With(registry).NewHistogramVec(prometheus.HistogramOpts{
		Name:        prometheus.BuildFQName(namespace, subsystem, "request_duration_seconds"),
		Help:        "Duration of all HTTP requests by status code, method and path.",
		ConstLabels: constLabels,
		Buckets: []float64{
			0.000000001, // 1ns
			0.000000002,
			0.000000005,
			0.00000001, // 10ns
			0.00000002,
			0.00000005,
			0.0000001, // 100ns
			0.0000002,
			0.0000005,
			0.000001, // 1µs
			0.000002,
			0.000005,
			0.00001, // 10µs
			0.00002,
			0.00005,
			0.0001, // 100µs
			0.0002,
			0.0005,
			0.001, // 1ms
			0.002,
			0.005,
			0.01, // 10ms
			0.02,
			0.05,
			0.1, // 100 ms
			0.2,
			0.5,
			1.0, // 1s
			2.0,
			5.0,
			10.0, // 10s
			15.0,
			20.0,
			30.0,
			60.0, // 1m
		},
	},
		[]string{"status_code", "method", "path"},
	)

	gauge := promauto.With(registry).NewGaugeVec(prometheus.GaugeOpts{
		Name:        prometheus.BuildFQName(namespace, subsystem, "requests_in_progress_total"),
		Help:        "All the requests in progress",
		ConstLabels: constLabels,
	}, []string{"method"})

	// If the registerer is also a gatherer, use it, falling back to the
	// DefaultGatherer.
	gatherer, ok := registry.(prometheus.Gatherer)
	if !ok {
		gatherer = prometheus.DefaultGatherer
	}

	return &FiberPrometheus{
		gatherer:    gatherer,
		registry:    registry,
		constLabels: constLabels,

		requestsTotal:   counter,
		requestDuration: histogram,
		requestInFlight: gauge,

		defaultURL: "/metrics",
	}
}

// New creates a new instance of FiberPrometheus middleware
// serviceName is available as a const label
func New(serviceName string) *FiberPrometheus {
	return create(nil, serviceName, "http", "", nil)
}

// NewWith creates a new instance of FiberPrometheus middleware but with an ability
// to pass namespace and a custom subsystem
// Here serviceName is created as a constant-label for the metrics
// Namespace, subsystem get prefixed to the metrics.
//
// For e.g. namespace = "my_app", subsystem = "http" then metrics would be
// `my_app_http_requests_total{...,service= "serviceName"}`
func NewWith(serviceName, namespace, subsystem string) *FiberPrometheus {
	return create(nil, serviceName, namespace, subsystem, nil)
}

// NewWithLabels creates a new instance of FiberPrometheus middleware but with an ability
// to pass namespace and a custom subsystem
// Here labels are created as a constant-labels for the metrics
// Namespace, subsystem get prefixed to the metrics.
//
// For e.g. namespace = "my_app", subsystem = "http" and labels = map[string]string{"key1": "value1", "key2":"value2"}
// then then metrics would become
// `my_app_http_requests_total{...,key1= "value1", key2= "value2" }`
func NewWithLabels(labels map[string]string, namespace, subsystem string) *FiberPrometheus {
	return create(nil, "", namespace, subsystem, labels)
}

// NewWithRegistry creates a new instance of FiberPrometheus middleware but with an ability
// to pass a custom registry, serviceName, namespace, subsystem and labels
// Here labels are created as a constant-labels for the metrics
// Namespace, subsystem get prefixed to the metrics.
//
// For e.g. namespace = "my_app", subsystem = "http" and labels = map[string]string{"key1": "value1", "key2":"value2"}
// then then metrics would become
// `my_app_http_requests_total{...,key1= "value1", key2= "value2" }`
func NewWithRegistry(registry prometheus.Registerer, serviceName, namespace, subsystem string, labels map[string]string) *FiberPrometheus {
	return create(registry, serviceName, namespace, subsystem, labels)
}

// NewWithDefaultRegistry creates a new instance of FiberPrometheus middleware using the default prometheus registry
func NewWithDefaultRegistry(serviceName string) *FiberPrometheus {
	return create(prometheus.DefaultRegisterer, serviceName, "http", "", nil)
}

// GetRegistry returns the registry used by the prometheus module
func (ps *FiberPrometheus) GetRegistry() prometheus.Registerer {
	return ps.registry
}

// GetConstLabels returns the const labels used by the prometheus module
func (ps *FiberPrometheus) GetConstLabels() prometheus.Labels {
	return ps.constLabels
}

// RegisterAt will register the prometheus handler at a given URL
func (ps *FiberPrometheus) RegisterAt(app fiber.Router, url string, handlers ...fiber.Handler) {
	ps.defaultURL = url

	h := append(handlers, adaptor.HTTPHandler(promhttp.HandlerFor(ps.gatherer, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})))
	app.Get(ps.defaultURL, h...)
}

// SetSkipPaths allows to set the paths that should be skipped from the metrics
func (ps *FiberPrometheus) SetSkipPaths(paths []string) {
	if ps.skipPaths == nil {
		ps.skipPaths = make(map[string]bool)
	}
	for _, path := range paths {
		ps.skipPaths[path] = true
	}
}

// SetIgnoreStatusCodes allows ignoring specific status codes from being recorded in metrics
func (ps *FiberPrometheus) SetIgnoreStatusCodes(codes []int) {
	if ps.ignoreStatusCodes == nil {
		ps.ignoreStatusCodes = make(map[int]bool)
	}
	for _, code := range codes {
		ps.ignoreStatusCodes[code] = true
	}
}

// Middleware is the actual default middleware implementation
func (ps *FiberPrometheus) Middleware(ctx *fiber.Ctx) error {
	// Retrieve the request method
	method := utils.CopyString(ctx.Method())

	// Increment the in-flight gauge
	ps.requestInFlight.WithLabelValues(method).Inc()
	defer func() {
		ps.requestInFlight.WithLabelValues(method).Dec()
	}()

	// Start metrics timer
	start := time.Now()

	// Continue stack
	err := ctx.Next()

	// Get the route path
	routePath := utils.CopyString(ctx.Route().Path)

	// If the route path is empty, use the current path
	if routePath == "/" {
		routePath = utils.CopyString(ctx.Path())
	}

	// Normalize the path
	if routePath != "" && routePath != "/" {
		routePath = normalizePath(routePath)
	}

	// Build registered routes map once
	ps.routesOnce.Do(func() {
		ps.registeredRoutes = make(map[string]struct{})
		for _, r := range ctx.App().GetRoutes(true) {
			p := r.Path
			if p != "" && p != "/" {
				p = normalizePath(p)
			}
			ps.registeredRoutes[r.Method+" "+p] = struct{}{}
		}
	})

	// Skip metrics for routes that are not registered
	if _, ok := ps.registeredRoutes[method+" "+routePath]; !ok {
		return err
	}

	// Check if the normalized path should be skipped
	if ps.skipPaths[routePath] {
		return nil
	}

	// Determine status code from stack
	status := fiber.StatusInternalServerError
	if err != nil {
		if e, ok := err.(*fiber.Error); ok {
			status = e.Code
		}
	} else {
		status = ctx.Response().StatusCode()
	}

	// Convert status code to string
	statusCode := strconv.Itoa(status)

	// Skip metrics for ignored status codes
	if ps.ignoreStatusCodes[status] {
		return err
	}

	// Update metrics
	ps.requestsTotal.WithLabelValues(statusCode, method, routePath).Inc()

	// Observe the Request Duration
	elapsed := float64(time.Since(start).Nanoseconds()) / 1e9

	traceID := trace.SpanContextFromContext(ctx.UserContext()).TraceID()
	histogram := ps.requestDuration.WithLabelValues(statusCode, method, routePath)

	if traceID.IsValid() {
		if histogramExemplar, ok := histogram.(prometheus.ExemplarObserver); ok {
			histogramExemplar.ObserveWithExemplar(elapsed, prometheus.Labels{"traceID": traceID.String()})
		}

		return err
	}

	histogram.Observe(elapsed)

	return err
}

// normalizePath will remove the trailing slash from the route path
func normalizePath(routePath string) string {
	normalized := strings.TrimRight(routePath, "/")
	if normalized == "" {
		return "/"
	}
	return normalized
}
