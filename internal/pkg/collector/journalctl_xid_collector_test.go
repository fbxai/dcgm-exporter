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
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/NVIDIA/go-dcgm/pkg/dcgm"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	mockdevicewatcher "github.com/NVIDIA/dcgm-exporter/internal/mocks/pkg/devicewatcher"
	mockexec "github.com/NVIDIA/dcgm-exporter/internal/mocks/pkg/exec"
	"github.com/NVIDIA/dcgm-exporter/internal/pkg/appconfig"
	"github.com/NVIDIA/dcgm-exporter/internal/pkg/counters"
	"github.com/NVIDIA/dcgm-exporter/internal/pkg/deviceinfo"
	"github.com/NVIDIA/dcgm-exporter/internal/pkg/devicewatchlistmanager"
	"github.com/NVIDIA/dcgm-exporter/internal/pkg/exec"
	"github.com/NVIDIA/dcgm-exporter/internal/pkg/testutils"
)

func TestIsDCGMExpXIDErrorsLogEnabled(t *testing.T) {
	tests := []struct {
		name string
		arg  counters.CounterList
		want bool
	}{
		{
			name: "empty",
			arg:  counters.CounterList{},
			want: false,
		},
		{
			name: "counter disabled",
			arg: counters.CounterList{
				counters.Counter{
					FieldID:   1,
					FieldName: "random1",
				},
				counters.Counter{
					FieldID:   2,
					FieldName: "random2",
				},
			},
			want: false,
		},
		{
			name: "counter enabled",
			arg: counters.CounterList{
				counters.Counter{
					FieldID:   1,
					FieldName: counters.DCGMExpXIDErrorsLog,
				},
				counters.Counter{
					FieldID:   2,
					FieldName: "random2",
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, IsDCGMExpXIDErrorsLogEnabled(tt.arg), "unexpected response")
		})
	}
}

func TestNewJournalctlXIDCollector(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDeviceWatcher := mockdevicewatcher.NewMockWatcher(ctrl)
	mockExec := mockexec.NewMockExec(ctrl)

	sampleDeviceInfo := &deviceinfo.Info{}
	sampleDeviceFields := []dcgm.Short{42}
	sampleCollectorInterval := int64(1)
	sampleConfig := appconfig.Config{}
	sampleHostname := "localhost"
	var sampleCleanups []func()

	sampleDCGMExpXIDLogCounter := counters.Counter{
		FieldID:   1,
		FieldName: counters.DCGMExpXIDErrorsLog,
	}

	sampleOtherCounter := counters.Counter{
		FieldID:   2,
		FieldName: "random2",
	}

	sampleLabelCounter := counters.Counter{
		FieldID:   3,
		FieldName: "random2",
		PromType:  "label",
	}

	type args struct {
		counterList     counters.CounterList
		hostname        string
		config          *appconfig.Config
		deviceWatchList *devicewatchlistmanager.WatchList
	}
	tests := []struct {
		name       string
		args       args
		conditions func(watcher *mockdevicewatcher.MockWatcher, exec *mockexec.MockExec)
		want       func(string, *appconfig.Config, devicewatchlistmanager.WatchList) Collector
		wantErr    bool
	}{
		{
			name: "counter is disabled",
			args: args{
				counterList:     counters.CounterList{},
				hostname:        sampleHostname,
				config:          nil,
				deviceWatchList: &devicewatchlistmanager.WatchList{},
			},
			conditions: func(watcher *mockdevicewatcher.MockWatcher, exec *mockexec.MockExec) {},
			want: func(
				_ string, _ *appconfig.Config,
				_ devicewatchlistmanager.WatchList,
			) Collector {
				return nil
			},
			wantErr: true,
		},
		{
			name: "new journalctl XID collector watcher fails",
			args: args{
				counterList: counters.CounterList{
					sampleDCGMExpXIDLogCounter,
					sampleOtherCounter,
					sampleLabelCounter,
				},
				hostname: sampleHostname,
				config:   &sampleConfig,
				deviceWatchList: devicewatchlistmanager.NewWatchList(sampleDeviceInfo, sampleDeviceFields, nil,
					mockDeviceWatcher, sampleCollectorInterval),
			},
			conditions: func(watcher *mockdevicewatcher.MockWatcher, exec *mockexec.MockExec) {
				watcher.EXPECT().WatchDeviceFields(gomock.Any(), gomock.Any(),
					gomock.Any()).Return(nil,
					dcgm.FieldHandle{},
					sampleCleanups, fmt.Errorf("some error"))
			},
			want: func(
				_ string, _ *appconfig.Config,
				_ devicewatchlistmanager.WatchList,
			) Collector {
				return nil
			},
			wantErr: true,
		},
		{
			name: "new journalctl XID collector success",
			args: args{
				counterList: counters.CounterList{
					sampleDCGMExpXIDLogCounter,
					sampleOtherCounter,
					sampleLabelCounter,
				},
				hostname: sampleHostname,
				config:   &sampleConfig,
				deviceWatchList: devicewatchlistmanager.NewWatchList(sampleDeviceInfo, sampleDeviceFields, nil,
					mockDeviceWatcher, sampleCollectorInterval),
			},
			conditions: func(watcher *mockdevicewatcher.MockWatcher, exec *mockexec.MockExec) {
				watcher.EXPECT().WatchDeviceFields(gomock.Any(), gomock.Any(),
					gomock.Any()).Return(nil,
					dcgm.FieldHandle{},
					sampleCleanups, nil)
			},
			want: func(
				hostname string, config *appconfig.Config,
				deviceWatchList devicewatchlistmanager.WatchList,
			) Collector {
				return &journalctlXIDCollector{
					expCollector: expCollector{
						baseExpCollector: baseExpCollector{
							deviceWatchList: deviceWatchList,
							counter:         sampleDCGMExpXIDLogCounter,
							labelsCounters:  []counters.Counter{sampleLabelCounter},
							hostname:        hostname,
							config:          config,
							cleanups:        sampleCleanups,
						},
					},
					executor:     &exec.RealExec{},
					xidErrors:    make(map[string]XIDError),
					scanInterval: 30 * time.Second,
				}
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.conditions(mockDeviceWatcher, mockExec)

			got, err := NewJournalctlXIDCollector(tt.args.counterList, tt.args.hostname, tt.args.config,
				*tt.args.deviceWatchList)
			want := tt.want(tt.args.hostname, tt.args.config, *tt.args.deviceWatchList)

			if !tt.wantErr {
				assert.NoError(t, err, "unexpected error")

				if got != nil {
					wantAttrs := testutils.GetFields(&want.(*journalctlXIDCollector).expCollector, testutils.Fields)
					gotAttrs := testutils.GetFields(&got.(*journalctlXIDCollector).expCollector, testutils.Fields)
					assert.Equal(t, wantAttrs, gotAttrs, "unexpected result")

					gotFuncAttrs := testutils.GetFields(&got.(*journalctlXIDCollector).expCollector, testutils.Functions)
					for functionName, value := range gotFuncAttrs {
						assert.NotNilf(t, value, "unexpected %s to be not nil", functionName)
					}
				}
			} else {
				assert.Error(t, err, "expected error")
				assert.Equal(t, want, got, "unexpected result")
			}
		})
	}
}

func TestJournalctlXIDCollector_parseJournalctlOutput(t *testing.T) {
	collector := &journalctlXIDCollector{
		xidErrors:       make(map[string]XIDError),
		xidPattern:      regexp.MustCompile(`NVRM:\s+Xid\s+\([^)]*\):\s*(\d+),\s*(.+)|NVRM:\s+Xid\s+\((\d+)\):\s*(.+)`),
		gpuIndexPattern: regexp.MustCompile(`GPU\s+(\d+)|nvidia(\d+)|PCI:(\d+):(\d+):(\d+)`),
	}

	// Test journalctl output with XID errors (both formats)
	testOutput := `2024-01-15T10:30:45+0000 hostname kernel: NVRM: Xid (62): GPU has fallen off the bus
2024-01-15T10:31:00+0000 hostname kernel: NVRM: Xid (48): Double Bit ECC Error on GPU 0
2024-01-15T10:32:15+0000 hostname kernel: NVRM: Xid (79): GPU has fallen off the bus on nvidia1
Sep 19 10:30:06 coastline-turtle-cn02 kernel: NVRM: Xid (PCI:0018:01:00): 149, NETIR_INT  Fatal   XC0 i0 Link -1
Sep 19 10:30:06 coastline-turtle-cn02 kernel: NVRM: Xid (PCI:0018:01:00): 154, GPU recovery action changed from 0x0 (None) to 0x4 (Drain and Reset)`

	collector.parseJournalctlOutput(testOutput)

	// Check that XID errors were parsed correctly
	assert.Len(t, collector.xidErrors, 5, "Expected 5 XID errors to be parsed")

	// Check specific XID error (simple format)
	xid62Key := "0:62"
	xid62, exists := collector.xidErrors[xid62Key]
	assert.True(t, exists, "XID 62 error should exist")
	assert.Equal(t, 62, xid62.XIDCode, "XID code should be 62")
	assert.Equal(t, "GPU has fallen off the bus", xid62.Message, "XID message should match")

	// Check XID 48 error
	xid48Key := "0:48"
	xid48, exists := collector.xidErrors[xid48Key]
	assert.True(t, exists, "XID 48 error should exist")
	assert.Equal(t, 48, xid48.XIDCode, "XID code should be 48")
	assert.Equal(t, "Double Bit ECC Error on GPU 0", xid48.Message, "XID message should match")

	// Check XID 79 error on GPU 1
	xid79Key := "1:79"
	xid79, exists := collector.xidErrors[xid79Key]
	assert.True(t, exists, "XID 79 error should exist")
	assert.Equal(t, 79, xid79.XIDCode, "XID code should be 79")
	assert.Equal(t, "GPU has fallen off the bus on nvidia1", xid79.Message, "XID message should match")

	// Check PCI format XID 149 error
	xid149Key := "1:149"
	xid149, exists := collector.xidErrors[xid149Key]
	assert.True(t, exists, "XID 149 error should exist")
	assert.Equal(t, 149, xid149.XIDCode, "XID code should be 149")
	assert.Equal(t, "NETIR_INT  Fatal   XC0 i0 Link -1", xid149.Message, "XID message should match")

	// Check PCI format XID 154 error
	xid154Key := "1:154"
	xid154, exists := collector.xidErrors[xid154Key]
	assert.True(t, exists, "XID 154 error should exist")
	assert.Equal(t, 154, xid154.XIDCode, "XID code should be 154")
	assert.Equal(t, "GPU recovery action changed from 0x0 (None) to 0x4 (Drain and Reset)", xid154.Message, "XID message should match")
}

func TestJournalctlXIDCollector_extractGPUIndex(t *testing.T) {
	collector := &journalctlXIDCollector{
		gpuIndexPattern: regexp.MustCompile(`GPU\s+(\d+)|nvidia(\d+)|PCI:(\d+):(\d+):(\d+)`),
	}

	tests := []struct {
		name     string
		line     string
		expected int
	}{
		{
			name:     "GPU index in message",
			line:     "NVRM: Xid (62): GPU 1 has fallen off the bus",
			expected: 1,
		},
		{
			name:     "nvidia device in message",
			line:     "NVRM: Xid (48): Double Bit ECC Error on nvidia2",
			expected: 2,
		},
		{
			name:     "PCI format in XID",
			line:     "NVRM: Xid (PCI:0018:01:00): 149, NETIR_INT Fatal",
			expected: 1,
		},
		{
			name:     "PCI format with different device",
			line:     "NVRM: Xid (PCI:0018:02:00): 154, GPU recovery action",
			expected: 2,
		},
		{
			name:     "no GPU index found",
			line:     "NVRM: Xid (62): GPU has fallen off the bus",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.extractGPUIndex(tt.line)
			assert.Equal(t, tt.expected, result, "GPU index should match expected value")
		})
	}
}

func TestJournalctlXIDCollector_parseTimestamp(t *testing.T) {
	collector := &journalctlXIDCollector{}

	tests := []struct {
		name     string
		line     string
		expected time.Time
	}{
		{
			name:     "valid timestamp",
			line:     "2024-01-15T10:30:45+0000 hostname kernel: NVRM: Xid (62): GPU has fallen off the bus",
			expected: time.Date(2024, 1, 15, 10, 30, 45, 0, time.FixedZone("", 0)),
		},
		{
			name:     "invalid timestamp",
			line:     "invalid-timestamp hostname kernel: NVRM: Xid (62): GPU has fallen off the bus",
			expected: time.Now(), // Should return current time as fallback
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.parseTimestamp(tt.line)
			if tt.name == "valid timestamp" {
				assert.Equal(t, tt.expected, result, "Timestamp should match expected value")
			} else {
				// For invalid timestamp, just check it's not zero time
				assert.False(t, result.IsZero(), "Timestamp should not be zero time")
			}
		})
	}
}
