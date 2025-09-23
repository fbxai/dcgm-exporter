/*
 * Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package collector

import (
	"bufio"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NVIDIA/dcgm-exporter/internal/pkg/appconfig"
	"github.com/NVIDIA/dcgm-exporter/internal/pkg/counters"
	"github.com/NVIDIA/dcgm-exporter/internal/pkg/devicewatchlistmanager"
	"github.com/NVIDIA/dcgm-exporter/internal/pkg/exec"
)

// XIDError represents a parsed XID error from journalctl
type XIDError struct {
	Timestamp time.Time
	XIDCode   int
	Message   string
	GPUIndex  int
}

// journalctlXIDCollector collects XID errors from journalctl logs
type journalctlXIDCollector struct {
	expCollector
	executor        exec.Exec
	xidErrors       map[string]XIDError // Key: "gpu_index:xid_code", Value: XIDError
	xidErrorsMutex  sync.RWMutex
	lastScanTime    time.Time
	scanInterval    time.Duration
	xidPattern      *regexp.Regexp
	gpuIndexPattern *regexp.Regexp
}

// NewJournalctlXIDCollector creates a new journalctl XID collector
func NewJournalctlXIDCollector(
	counterList counters.CounterList,
	hostname string,
	config *appconfig.Config,
	deviceWatchList devicewatchlistmanager.WatchList,
) (Collector, error) {
	if !IsDCGMExpXIDErrorsLogEnabled(counterList) {
		slog.Error(counters.DCGMExpXIDErrorsLog + " collector is disabled")
		return nil, fmt.Errorf(counters.DCGMExpXIDErrorsLog + " collector is disabled")
	}

	collector := journalctlXIDCollector{
		executor:     &exec.RealExec{},
		xidErrors:    make(map[string]XIDError),
		scanInterval: 30 * time.Second, // Default scan interval
		// Regex pattern to match XID errors in journalctl output
		// Supports both formats:
		// - "NVRM: Xid (62): GPU has fallen off the bus"
		// - "NVRM: Xid (PCI:0018:01:00): 149, NETIR_INT Fatal"
		xidPattern: regexp.MustCompile(`NVRM:\s+Xid\s+\([^)]*\):\s*(\d+),\s*(.+)|NVRM:\s+Xid\s+\((\d+)\):\s*(.+)`),
		// Regex pattern to extract GPU index from PCI bus ID or other identifiers
		gpuIndexPattern: regexp.MustCompile(`GPU\s+(\d+)|nvidia(\d+)|PCI:(\d+):(\d+):(\d+)`),
	}

	var err error
	collector.expCollector, err = newExpCollector(
		counterList.LabelCounters(),
		hostname,
		config,
		deviceWatchList,
	)
	if err != nil {
		return nil, err
	}

	collector.counter = counterList[slices.IndexFunc(counterList, func(c counters.Counter) bool {
		return c.FieldName == counters.DCGMExpXIDErrorsLog
	})]

	collector.labelFiller = func(metricValueLabels map[string]string, entityValue int64) {
		metricValueLabels["xid"] = fmt.Sprint(entityValue)
		metricValueLabels["source"] = "journalctl"
	}

	return &collector, nil
}

// IsDCGMExpXIDErrorsLogEnabled checks if the journalctl XID errors collector is enabled
func IsDCGMExpXIDErrorsLogEnabled(counterList counters.CounterList) bool {
	return slices.ContainsFunc(counterList, func(c counters.Counter) bool {
		return c.FieldName == counters.DCGMExpXIDErrorsLog
	})
}

// GetMetrics collects XID errors from journalctl and returns metrics
func (c *journalctlXIDCollector) GetMetrics() (MetricsByCounter, error) {
	// Scan journalctl for new XID errors
	if err := c.scanJournalctl(); err != nil {
		slog.Error("Failed to scan journalctl for XID errors", "error", err)
		return nil, err
	}

	// Convert collected XID errors to metrics
	metrics := make(MetricsByCounter)
	xidCounts := make(map[string]int) // Key: "gpu_index:xid_code", Value: count

	c.xidErrorsMutex.RLock()
	for key := range c.xidErrors {
		xidCounts[key]++
	}
	c.xidErrorsMutex.RUnlock()

	// Create metrics for each XID error type
	for key, count := range xidCounts {
		parts := strings.Split(key, ":")
		if len(parts) != 2 {
			continue
		}

		gpuIndex, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}

		xidCode, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		// Create metric for this XID error
		metric := c.createXIDMetric(gpuIndex, xidCode, count)
		metrics[c.counter] = append(metrics[c.counter], metric)
	}

	return metrics, nil
}

// scanJournalctl scans journalctl for XID errors since the last scan
func (c *journalctlXIDCollector) scanJournalctl() error {
	now := time.Now()

	// Build journalctl command
	args := []string{
		"--dmesg",    // Show kernel messages
		"-b",         // Current boot
		"-g", "NVRM", // Filter for NVIDIA messages
		"--no-pager",         // Don't use pager
		"--output=short-iso", // ISO timestamp format
	}

	// Add time filter if we have a previous scan time
	if !c.lastScanTime.IsZero() {
		args = append(args, "--since", c.lastScanTime.Format(time.RFC3339))
	}

	cmd := c.executor.Command("journalctl", args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to execute journalctl: %w", err)
	}

	// Parse the output for XID errors
	c.parseJournalctlOutput(string(output))

	c.lastScanTime = now
	return nil
}

// parseJournalctlOutput parses journalctl output and extracts XID errors
func (c *journalctlXIDCollector) parseJournalctlOutput(output string) {
	scanner := bufio.NewScanner(strings.NewReader(output))

	c.xidErrorsMutex.Lock()
	defer c.xidErrorsMutex.Unlock()

	for scanner.Scan() {
		line := scanner.Text()

		// Look for XID error pattern
		matches := c.xidPattern.FindStringSubmatch(line)
		if len(matches) < 3 {
			continue
		}

		var xidCode int
		var message string
		var err error

		// Handle PCI format: "NVRM: Xid (PCI:0018:01:00): 149, message"
		if matches[1] != "" && matches[2] != "" {
			xidCode, err = strconv.Atoi(matches[1])
			if err != nil {
				continue
			}
			message = matches[2]
		} else if matches[3] != "" && matches[4] != "" {
			// Handle simple format: "NVRM: Xid (62): message"
			xidCode, err = strconv.Atoi(matches[3])
			if err != nil {
				continue
			}
			message = matches[4]
		} else {
			continue
		}

		// Try to extract GPU index from the line
		gpuIndex := c.extractGPUIndex(line)

		// Create unique key for this XID error
		key := fmt.Sprintf("%d:%d", gpuIndex, xidCode)

		// Parse timestamp from the line (journalctl short-iso format)
		timestamp := c.parseTimestamp(line)

		// Store the XID error
		c.xidErrors[key] = XIDError{
			Timestamp: timestamp,
			XIDCode:   xidCode,
			Message:   message,
			GPUIndex:  gpuIndex,
		}

		slog.Debug("Found XID error in journalctl",
			"gpu_index", gpuIndex,
			"xid_code", xidCode,
			"message", message,
			"timestamp", timestamp)
	}
}

// extractGPUIndex extracts GPU index from journalctl line
func (c *journalctlXIDCollector) extractGPUIndex(line string) int {
	// Try to find GPU index in various formats
	matches := c.gpuIndexPattern.FindStringSubmatch(line)
	if len(matches) > 1 {
		// Handle PCI format: PCI:0018:01:00
		if matches[3] != "" && matches[4] != "" && matches[5] != "" {
			// Use the device number (third component) as GPU index
			if index, err := strconv.Atoi(matches[4]); err == nil {
				return index
			}
		}

		// Handle other formats: GPU 1, nvidia1, PCI 1:
		for i := 1; i < len(matches); i++ {
			if matches[i] != "" {
				if index, err := strconv.Atoi(matches[i]); err == nil {
					return index
				}
			}
		}
	}

	// Default to GPU 0 if we can't determine the index
	return 0
}

// parseTimestamp parses timestamp from journalctl line
func (c *journalctlXIDCollector) parseTimestamp(line string) time.Time {
	// journalctl short-iso format: "2024-01-15T10:30:45+0000 hostname kernel: message"
	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 1 {
		return time.Now()
	}

	timestamp, err := time.Parse("2006-01-02T15:04:05-0700", parts[0])
	if err != nil {
		// Fallback to current time if parsing fails
		return time.Now()
	}

	return timestamp
}

// createXIDMetric creates a metric for a specific XID error
func (c *journalctlXIDCollector) createXIDMetric(gpuIndex, xidCode, count int) Metric {
	labels := map[string]string{
		"xid":    fmt.Sprintf("%d", xidCode),
		"source": "journalctl",
		"gpu":    fmt.Sprintf("%d", gpuIndex),
	}

	// Add XID error description if available
	if xidCode < len(xidErrCodeToText) {
		labels["description"] = xidErrCodeToText[xidCode]
	}

	return Metric{
		Counter:       c.counter,
		Value:         fmt.Sprintf("%d", count),
		GPU:           fmt.Sprintf("%d", gpuIndex),
		GPUUUID:       "",
		GPUDevice:     fmt.Sprintf("nvidia%d", gpuIndex),
		GPUModelName:  "",
		UUID:          "",
		MigProfile:    "",
		GPUInstanceID: "",
		Hostname:      c.hostname,
		Labels:        labels,
		Attributes:    map[string]string{},
	}
}
