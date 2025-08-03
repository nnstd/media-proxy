package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type PerformanceMetrics struct {
	RequestDuration  *prometheus.HistogramVec
	ImageProcessTime *prometheus.HistogramVec
	VideoProcessTime *prometheus.HistogramVec
	CacheHitRatio    *prometheus.GaugeVec
	MemoryUsage      *prometheus.GaugeVec
	HTTPRequestTime  *prometheus.HistogramVec
	ImageSizeBytes   *prometheus.HistogramVec
	VideoSizeBytes   *prometheus.HistogramVec
}

func InitializePerformanceMetrics(registry prometheus.Registerer, constLabels prometheus.Labels) *PerformanceMetrics {
	metrics := &PerformanceMetrics{
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "request_duration_seconds",
			Help:        "Request duration in seconds",
			ConstLabels: constLabels,
			Buckets:     prometheus.DefBuckets,
		}, []string{"type", "status"}),

		ImageProcessTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "image_process_time_seconds",
			Help:        "Image processing time in seconds",
			ConstLabels: constLabels,
			Buckets:     prometheus.DefBuckets,
		}, []string{"operation"}),

		VideoProcessTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "video_process_time_seconds",
			Help:        "Video processing time in seconds",
			ConstLabels: constLabels,
			Buckets:     prometheus.DefBuckets,
		}, []string{"operation"}),

		CacheHitRatio: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        "cache_hit_ratio",
			Help:        "Cache hit ratio (0-1)",
			ConstLabels: constLabels,
		}, []string{"type"}),

		MemoryUsage: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        "memory_usage_bytes",
			Help:        "Memory usage in bytes",
			ConstLabels: constLabels,
		}, []string{"type"}),

		HTTPRequestTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "http_request_time_seconds",
			Help:        "HTTP request time in seconds",
			ConstLabels: constLabels,
			Buckets:     prometheus.DefBuckets,
		}, []string{"hostname"}),

		ImageSizeBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "image_size_bytes",
			Help:        "Image size in bytes",
			ConstLabels: constLabels,
			Buckets:     []float64{1024, 10240, 102400, 1048576, 10485760, 104857600}, // 1KB to 100MB
		}, []string{"format"}),

		VideoSizeBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:        "video_size_bytes",
			Help:        "Video size in bytes",
			ConstLabels: constLabels,
			Buckets:     []float64{1048576, 10485760, 104857600, 1073741824}, // 1MB to 1GB
		}, []string{"format"}),
	}

	// Register all metrics
	registry.MustRegister(
		metrics.RequestDuration,
		metrics.ImageProcessTime,
		metrics.VideoProcessTime,
		metrics.CacheHitRatio,
		metrics.MemoryUsage,
		metrics.HTTPRequestTime,
		metrics.ImageSizeBytes,
		metrics.VideoSizeBytes,
	)

	return metrics
}

// TimeFunction measures the execution time of a function
func TimeFunction[T any](fn func() (T, error), operation string, metrics *PerformanceMetrics) (T, error) {
	start := time.Now()
	result, err := fn()
	duration := time.Since(start).Seconds()

	if metrics != nil {
		metrics.ImageProcessTime.WithLabelValues(operation).Observe(duration)
	}

	return result, err
}

// TimeHTTPRequest measures HTTP request duration
func TimeHTTPRequest(hostname string, metrics *PerformanceMetrics) func() {
	start := time.Now()
	return func() {
		duration := time.Since(start).Seconds()
		if metrics != nil {
			metrics.HTTPRequestTime.WithLabelValues(hostname).Observe(duration)
		}
	}
}
