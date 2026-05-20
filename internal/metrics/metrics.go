// Package metrics defines all Prometheus metrics for the Shop service
// and exposes a single Registry that can be used by middleware and
// instrumented repositories.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// HTTP metrics (§4.1 — web traffic)
var (
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shop_http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "shop_http_request_duration_seconds",
			Help:    "HTTP request latency.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	HTTPResponseBytesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "shop_http_response_bytes_total",
			Help: "Total bytes sent in HTTP responses.",
		},
	)

	// Approximate unique visitors per day: key = IP + UA + date (truncated to day).
	// We use a simple counter; the visitor key set is maintained in the middleware.
	HTTPUniqueVisitorsTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "shop_http_unique_visitors_total",
			Help: "Approximate number of unique visitors today (IP + User-Agent + date).",
		},
	)
)

// Business metrics (§4.1)
var (
	ArticlesTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "shop_articles_total",
			Help: "Current number of articles in the catalogue.",
		},
	)

	OrdersTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shop_orders_total",
			Help: "Total number of orders by status.",
		},
		[]string{"status"},
	)

	ActiveCarts = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "shop_active_carts",
			Help: "Number of non-empty in-memory carts.",
		},
	)
)
