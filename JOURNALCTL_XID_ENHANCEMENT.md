# Journalctl XID Error Collection Enhancement

This enhancement adds the ability to capture XID errors from system logs (journalctl) to the dcgm-exporter, providing comprehensive GPU error monitoring beyond what DCGM alone can detect.

## Overview

The existing `DCGM_FI_DEV_XID_ERRORS` metric only captures XID errors that are reported through DCGM. However, some XID errors may appear in system logs (via `journalctl`) but not be captured by DCGM. This enhancement adds a new collector that monitors journalctl for XID errors and exposes them as Prometheus metrics.

## New Components

### 1. Journalctl XID Collector (`journalctl_xid_collector.go`)

A new collector that:

- Executes `journalctl --dmesg -b -g 'NVRM' --no-pager` to scan for NVIDIA-related kernel messages
- Parses XID error messages using regex patterns
- Extracts GPU index, XID code, and error message
- Maintains a cache of detected XID errors
- Exposes metrics with detailed labels including XID code and description

### 2. New Counter (`DCGM_EXP_XID_ERRORS_LOG`)

A new exporter counter that enables the journalctl XID collector:

- Counter name: `DCGM_EXP_XID_ERRORS_LOG`
- Metric type: `counter`
- Description: "XID errors detected from journalctl logs"

### 3. Enhanced Configuration

The new collector is automatically enabled when `DCGM_EXP_XID_ERRORS_LOG` is included in the counter configuration.

## Usage

### Enabling the Collector

Add the following line to your counters configuration file (e.g., `default-counters.csv`):

```csv
DCGM_EXP_XID_ERRORS_LOG, counter, XID errors detected from journalctl logs.
```

### Example journalctl Command

The collector executes commands similar to:

```bash
journalctl --dmesg -b -g 'NVRM' --no-pager --output=short-iso --since <last_scan_time>
```

### Example XID Error Format

The collector looks for messages like:

```
2024-01-15T10:30:45+0000 hostname kernel: NVRM: Xid (62): GPU has fallen off the bus
2024-01-15T10:31:00+0000 hostname kernel: NVRM: Xid (48): Double Bit ECC Error on GPU 0
```

## Metrics Exposed

The collector exposes metrics with the following labels:

- `xid`: The XID error code (e.g., "62", "48")
- `source`: Always "journalctl" to distinguish from DCGM-based metrics
- `gpu`: GPU index (e.g., "0", "1")
- `description`: Human-readable description of the XID error (if available)

### Example Metric

```
DCGM_EXP_XID_ERRORS_LOG{gpu="0",xid="62",source="journalctl",description="GPU has fallen off the bus"} 1
DCGM_EXP_XID_ERRORS_LOG{gpu="1",xid="48",source="journalctl",description="Double Bit ECC Error"} 1
```

## Implementation Details

### Thread Safety

The collector uses read-write mutexes to ensure thread-safe access to the XID error cache.

### Performance Considerations

- Scans journalctl every 30 seconds by default
- Uses incremental scanning (only scans logs since last scan time)
- Caches parsed XID errors to avoid reprocessing
- Minimal overhead on system resources

### Error Handling

- Gracefully handles journalctl command failures
- Continues operation even if individual XID parsing fails
- Logs errors for debugging purposes

## Testing

The implementation includes comprehensive unit tests covering:

- Counter enablement detection
- Collector initialization
- Journalctl output parsing
- GPU index extraction
- Timestamp parsing
- Metric creation

## Configuration Options

The collector supports the following configuration (future enhancement):

- `scan_interval`: Time between journalctl scans (default: 30s)
- `journalctl_args`: Custom arguments for journalctl command
- `xid_pattern`: Custom regex pattern for XID error detection

## Benefits

1. **Comprehensive Monitoring**: Captures XID errors that may not be reported by DCGM
2. **Historical Data**: Can detect XID errors from previous boots
3. **Detailed Information**: Provides XID error descriptions and timestamps
4. **Unified Monitoring**: Integrates with existing dcgm-exporter infrastructure
5. **Prometheus Integration**: Exposes metrics in standard Prometheus format

## Limitations

1. **System Dependency**: Requires `journalctl` command to be available
2. **Permission Requirements**: May require elevated privileges to access system logs
3. **Platform Specific**: Designed for Linux systems with systemd/journald
4. **Log Rotation**: May miss XID errors if logs are rotated between scans

## Future Enhancements

1. **Configuration Options**: Add configurable scan intervals and journalctl arguments
2. **Multiple Log Sources**: Support for other log sources (e.g., `/var/log/messages`)
3. **Alerting Integration**: Built-in alerting for critical XID errors
4. **Historical Analysis**: Trend analysis and XID error pattern detection
5. **Performance Optimization**: More efficient log parsing and caching strategies

## Troubleshooting

### Common Issues

1. **Permission Denied**: Ensure the dcgm-exporter has access to system logs
2. **journalctl Not Found**: Verify journalctl is installed and available in PATH
3. **No XID Errors Detected**: Check if XID errors are actually present in logs
4. **High CPU Usage**: Consider increasing scan interval if system is under load

### Debugging

Enable debug logging to see detailed information about XID error detection:

```bash
dcgm-exporter --log-level DEBUG
```

### Manual Testing

Test journalctl command manually:

```bash
journalctl --dmesg -b -g 'NVRM' --no-pager --output=short-iso
```

## References

- [NVIDIA XID Error Documentation](https://docs.nvidia.com/deploy/xid-errors/)
- [DCGM Exporter GitHub Repository](https://github.com/NVIDIA/dcgm-exporter)
- [Prometheus Metrics Documentation](https://prometheus.io/docs/concepts/metric_types/)
