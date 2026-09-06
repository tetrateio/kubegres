package metrics

import dto "github.com/prometheus/client_model/go"

// dtoMetric wraps the generated protobuf metric so that the import of the client_model package
// stays confined to one file.
type dtoMetric struct {
	Metric dto.Metric
}
