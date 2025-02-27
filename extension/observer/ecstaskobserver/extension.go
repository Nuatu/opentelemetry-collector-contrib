// Copyright  The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ecstaskobserver // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer/ecstaskobserver"

import (
	"context"
	"fmt"
	"strconv"

	"go.opentelemetry.io/collector/component"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/aws/ecsutil"
)

const runningStatus = "RUNNING"

var _ component.Extension = (*ecsTaskObserver)(nil)
var _ observer.EndpointsLister = (*ecsTaskObserver)(nil)
var _ observer.Observable = (*ecsTaskObserver)(nil)

type ecsTaskObserver struct {
	component.Extension
	*observer.EndpointsWatcher
	config           *Config
	metadataProvider ecsutil.MetadataProvider
	telemetry        component.TelemetrySettings
}

func (e *ecsTaskObserver) Shutdown(ctx context.Context) error {
	e.StopListAndWatch()
	return nil
}

// ListEndpoints is invoked by an observer.EndpointsWatcher helper to report task container endpoints.
// It's required to implement observer.EndpointsLister
func (e *ecsTaskObserver) ListEndpoints() []observer.Endpoint {
	taskMetadata, err := e.metadataProvider.FetchTaskMetadata()
	if err != nil {
		e.telemetry.Logger.Warn("error fetching task metadata", zap.Error(err))
	}
	return e.endpointsFromTaskMetadata(taskMetadata)
}

// endpointsFromTaskMetadata walks the tasks ContainerMetadata and returns an observer Endpoint for each running
// container instance. We only need to report running ones since state is maintained by our EndpointsWatcher.
func (e *ecsTaskObserver) endpointsFromTaskMetadata(taskMetadata *ecsutil.TaskMetadata) (endpoints []observer.Endpoint) {
	if taskMetadata == nil {
		return
	}

	for _, container := range taskMetadata.Containers {
		if container.KnownStatus != runningStatus {
			continue
		}

		host := container.Networks[0].IPv4Addresses[0]
		target := host

		port := e.portFromLabels(container.Labels)
		if port != 0 {
			target = fmt.Sprintf("%s:%d", target, port)
		}

		endpoint := observer.Endpoint{
			ID:     observer.EndpointID(fmt.Sprintf("%s-%s", container.ContainerName, container.DockerID)),
			Target: target,
			Details: &observer.Container{
				ContainerID: container.DockerID,
				Host:        host,
				Image:       container.Image,
				Labels:      container.Labels,
				Name:        container.ContainerName,
				Port:        port,
				// no indirection in task containers, so we specify the labeled port again.
				AlternatePort: port,
			},
		}
		endpoints = append(endpoints, endpoint)
	}

	return endpoints
}

// portFromLabels will iterate the PortLabels config option and return the first valid port match
func (e *ecsTaskObserver) portFromLabels(labels map[string]string) uint16 {
	var port uint16
	for _, portLabel := range e.config.PortLabels {
		if p, ok := labels[portLabel]; ok {
			if pint, err := strconv.Atoi(p); err != nil {
				e.telemetry.Logger.Warn("failed parsing port label", zap.String("label", portLabel), zap.Error(err))
				continue
			} else if pint < 0 || pint > 65535 {
				e.telemetry.Logger.Warn("port label value invalid for port usage", zap.String("label", portLabel), zap.Int("value", pint))
				continue
			} else {
				port = uint16(pint)
				break
			}
		}
	}
	return port
}
