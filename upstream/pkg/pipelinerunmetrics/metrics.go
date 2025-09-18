/*
Copyright 2024 The Tekton Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pipelinerunmetrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tektoncd/chains/pkg/chains"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
)

var (
	sgCount = stats.Float64(chains.PipelineRunSignedName,
		chains.PipelineRunSignedDesc,
		stats.UnitDimensionless)

	sgCountView *view.View

	plCount = stats.Float64(chains.PipelineRunUploadedName,
		chains.PipelineRunUploadedDesc,
		stats.UnitDimensionless)

	plCountView *view.View

	stCount = stats.Float64(chains.PipelineRunStoredName,
		chains.PipelineRunStoredDesc,
		stats.UnitDimensionless)

	stCountView *view.View

	mrCount = stats.Float64(chains.PipelineRunMarkedName,
		chains.PipelineRunMarkedDesc,
		stats.UnitDimensionless)

	mrCountView *view.View

	// New error metrics for better observability
	errorCount = stats.Float64("pipelinerun_error_count",
		"Number of errors encountered during pipeline run processing",
		stats.UnitDimensionless)

	errorCountView *view.View

	// Duration metrics for performance monitoring
	processTimeDuration = stats.Float64("pipelinerun_process_duration_seconds",
		"Time taken to process pipeline runs in seconds",
		stats.UnitSeconds)

	processTimeDurationView *view.View
)

// Recorder holds keys for Tekton metrics
type Recorder struct {
	initialized bool
}

// We cannot register the view multiple times, so NewRecorder lazily
// initializes this singleton and returns the same recorder across any
// subsequent invocations.
var (
	once sync.Once
	r    *Recorder
)

// NewRecorder creates a new metrics recorder instance
// to log the PipelineRun related metrics
func NewRecorder(ctx context.Context) (*Recorder, error) {
	var errRegistering error
	logger := logging.FromContext(ctx)
	once.Do(func() {
		r = &Recorder{
			initialized: true,
		}
		errRegistering = viewRegister()
		if errRegistering != nil {
			r.initialized = false
			logger.Errorf("View Register Failed ", r.initialized)
			return
		}
	})

	return r, errRegistering
}

func viewRegister() error {
	sgCountView = &view.View{
		Description: sgCount.Description(),
		Measure:     sgCount,
		Aggregation: view.Count(),
	}

	plCountView = &view.View{
		Description: plCount.Description(),
		Measure:     plCount,
		Aggregation: view.Count(),
	}

	stCountView = &view.View{
		Description: stCount.Description(),
		Measure:     stCount,
		Aggregation: view.Count(),
	}

	mrCountView = &view.View{
		Description: mrCount.Description(),
		Measure:     mrCount,
		Aggregation: view.Count(),
	}

	errorCountView = &view.View{
		Description: errorCount.Description(),
		Measure:     errorCount,
		Aggregation: view.Count(),
	}

	processTimeDurationView = &view.View{
		Description: processTimeDuration.Description(),
		Measure:     processTimeDuration,
		Aggregation: view.Distribution(0.1, 0.5, 1.0, 5.0, 10.0, 30.0, 60.0),
	}

	return view.Register(
		sgCountView,
		plCountView,
		stCountView,
		mrCountView,
		errorCountView,
		processTimeDurationView,
	)
}

func (r *Recorder) RecordCountMetrics(ctx context.Context, metricType string) {
	logger := logging.FromContext(ctx)
	if !r.initialized {
		logger.Errorf("Ignoring the metrics recording as recorder not initialized ")
		return
	}
	switch mt := metricType; mt {
	case chains.SignedMessagesCount:
		r.countMetrics(ctx, sgCount)
	case chains.PayloadUploadeCount:
		r.countMetrics(ctx, plCount)
	case chains.SignsStoredCount:
		r.countMetrics(ctx, stCount)
	case chains.MarkedAsSignedCount:
		r.countMetrics(ctx, mrCount)
	default:
		logger.Errorf("Ignoring the metrics recording as valid Metric type matching %v was not found", mt)
	}

}

func (r *Recorder) countMetrics(ctx context.Context, measure *stats.Float64Measure) {
	metrics.Record(ctx, measure.M(1))
}

// RecordErrorMetrics records error occurrences with optional error type classification
func (r *Recorder) RecordErrorMetrics(ctx context.Context, errorType string) {
	logger := logging.FromContext(ctx)
	if !r.initialized {
		logger.Errorf("Ignoring the error metrics recording as recorder not initialized")
		return
	}
	// Record error count with optional tags for error classification
	metrics.Record(ctx, errorCount.M(1))
	logger.Debugf("Recorded error metric for type: %s", errorType)
}

// RecordDurationMetrics records the time taken for pipeline processing
func (r *Recorder) RecordDurationMetrics(ctx context.Context, durationSeconds float64) {
	logger := logging.FromContext(ctx)
	if !r.initialized {
		logger.Errorf("Ignoring the duration metrics recording as recorder not initialized")
		return
	}
	if durationSeconds < 0 {
		logger.Errorf("Invalid duration provided: %f seconds, must be non-negative", durationSeconds)
		return
	}
	metrics.Record(ctx, processTimeDuration.M(durationSeconds))
	logger.Debugf("Recorded processing duration: %f seconds", durationSeconds)
}

// GetMetricsStatus returns a summary of the current metrics configuration
// This is useful for debugging and monitoring the health of metrics collection
func (r *Recorder) GetMetricsStatus(ctx context.Context) map[string]interface{} {
	logger := logging.FromContext(ctx)
	
	status := map[string]interface{}{
		"initialized": r.initialized,
		"timestamp":   time.Now().Format(time.RFC3339),
	}
	
	if !r.initialized {
		logger.Warn("Metrics recorder not properly initialized")
		status["error"] = "recorder not initialized"
		return status
	}
	
	// List all available metrics views
	availableViews := []string{
		sgCountView.Name,
		plCountView.Name,
		stCountView.Name,
		mrCountView.Name,
		errorCountView.Name,
		processTimeDurationView.Name,
	}
	
	status["available_views"] = availableViews
	status["total_views"] = len(availableViews)
	
	logger.Debugf("Metrics status: %d views available, initialized: %v", len(availableViews), r.initialized)
	
	return status
}

// RecordBatchMetrics allows recording multiple metrics in a single operation
// This is more efficient when multiple metrics need to be updated simultaneously
func (r *Recorder) RecordBatchMetrics(ctx context.Context, operations []MetricOperation) error {
	logger := logging.FromContext(ctx)
	
	if !r.initialized {
		return fmt.Errorf("metrics recorder not initialized")
	}
	
	if len(operations) == 0 {
		logger.Warn("No metric operations provided for batch recording")
		return nil
	}
	
	for i, op := range operations {
		switch op.Type {
		case "count":
			r.RecordCountMetrics(ctx, op.MetricType)
		case "error":
			r.RecordErrorMetrics(ctx, op.MetricType)
		case "duration":
			if op.Value <= 0 {
				logger.Errorf("Invalid duration value for operation %d: %f", i, op.Value)
				continue
			}
			r.RecordDurationMetrics(ctx, op.Value)
		default:
			logger.Errorf("Unknown metric operation type: %s", op.Type)
		}
	}
	
	logger.Debugf("Successfully processed %d metric operations", len(operations))
	return nil
}

// MetricOperation represents a single metrics operation for batch processing
type MetricOperation struct {
	Type       string  // "count", "error", or "duration"
	MetricType string  // specific metric type identifier
	Value      float64 // value for duration metrics (ignored for count/error)
}
