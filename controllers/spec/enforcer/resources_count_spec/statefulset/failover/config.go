/*
Copyright 2021 Reactive Tech Limited.
"Reactive Tech Limited" is a company located in England, United Kingdom.
https://www.reactive-tech.io

Lead Developer: Alex Arica

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

package failover

import (
	"time"

	v1 "reactive-tech.io/kubegres/api/v1"
	"reactive-tech.io/kubegres/controllers/ctx"
	"reactive-tech.io/kubegres/internal/postgres"
)

// Config is spec.failover with every optional field filled in, so defaulting lives in one place
// instead of at each read site.
type Config struct {
	// IntelligentFailoverEnabled turns on selection by replication state.
	IntelligentFailoverEnabled bool

	// MaxReplicationLagBytes is how far behind the promoted Replica may be. Zero turns it off.
	MaxReplicationLagBytes int64

	// HealthCheckTimeout limits each replication-state query.
	HealthCheckTimeout time.Duration

	// FallbackToLegacy allows readiness-based selection when no Replica can be reached.
	FallbackToLegacy bool

	// RequireStreaming rejects candidates whose WAL stream has broken.
	RequireStreaming bool

	// AllowUnsafeManualPromotion lets spec.failover.promotePod skip the health checks.
	AllowUnsafeManualPromotion bool

	// PrimaryStabilityWindow is how long the Primary must stay unhealthy before an automatic
	// failover starts. Zero starts immediately.
	PrimaryStabilityWindow time.Duration

	// MinHealthyReplicas is how many Replicas must be ready before a failover is complete. Zero
	// means the new Primary being ready is enough.
	MinHealthyReplicas int32
}

// ResolveConfig applies the defaults to spec.failover.
//
// Every gate is off by default, so a Kubegres resource written before these fields existed
// behaves exactly as it did.
func ResolveConfig(spec v1.KubegresFailover) Config {
	config := Config{
		FallbackToLegacy:   ctx.DefaultFailOverFallbackToLegacy,
		HealthCheckTimeout: ctx.DefaultFailOverHealthCheckTimeout.Duration,
	}

	if spec.PrimaryStabilityWindow != nil && spec.PrimaryStabilityWindow.Duration > 0 {
		config.PrimaryStabilityWindow = spec.PrimaryStabilityWindow.Duration
	}

	if spec.MinHealthyReplicas != nil && *spec.MinHealthyReplicas > 0 {
		config.MinHealthyReplicas = *spec.MinHealthyReplicas
	}

	intelligent := spec.IntelligentFailover
	if intelligent == nil {
		return config
	}

	config.IntelligentFailoverEnabled = intelligent.Enabled
	config.RequireStreaming = intelligent.RequireStreamingReplica
	config.AllowUnsafeManualPromotion = intelligent.AllowUnsafeManualPromotion

	if intelligent.HealthCheckTimeout != nil && intelligent.HealthCheckTimeout.Duration > 0 {
		config.HealthCheckTimeout = intelligent.HealthCheckTimeout.Duration
	}

	if intelligent.FallbackToLegacy != nil {
		config.FallbackToLegacy = *intelligent.FallbackToLegacy
	}

	// Nil means unset and takes the default. An explicit zero means no lag limit at all.
	maxLag := ctx.DefaultFailOverMaxReplicationLag
	if intelligent.MaxReplicationLag != nil {
		maxLag = *intelligent.MaxReplicationLag
	}
	if lagBytes, ok := maxLag.AsInt64(); ok && lagBytes > 0 {
		config.MaxReplicationLagBytes = lagBytes
	}

	return config
}

// SelectionConstraints turns the config into the limits the election applies, measured against
// the last WAL position seen on the healthy Primary.
func (c Config) SelectionConstraints(referenceLSN postgres.LSN) SelectionConstraints {
	return SelectionConstraints{
		MaxReplicationLagBytes: c.MaxReplicationLagBytes,
		ReferenceLSN:           referenceLSN,
		RequireStreaming:       c.RequireStreaming,
	}
}
