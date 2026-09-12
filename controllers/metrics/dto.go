package metrics

import dto "github.com/prometheus/client_model/go"

// dtoMetric wraps the generated protobuf metric, keeping the client_model import in one file.
type dtoMetric struct {
	Metric dto.Metric
}
