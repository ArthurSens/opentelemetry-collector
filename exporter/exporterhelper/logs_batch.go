// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package exporterhelper // import "go.opentelemetry.io/collector/exporter/exporterhelper"

import (
	"context"
	"errors"

	"go.opentelemetry.io/collector/exporter/exporterbatcher"
	"go.opentelemetry.io/collector/pdata/plog"
)

// MergeSplit splits and/or merges the provided logs request and the current request into one or more requests
// conforming with the MaxSizeConfig.
func (req *logsRequest) MergeSplit(_ context.Context, cfg exporterbatcher.MaxSizeConfig, r2 Request) ([]Request, error) {
	var req2 *logsRequest
	if r2 != nil {
		var ok bool
		req2, ok = r2.(*logsRequest)
		if !ok {
			return nil, errors.New("invalid input type")
		}
	}

	if cfg.MaxSizeItems == 0 {
		req2.ld.ResourceLogs().MoveAndAppendTo(req.ld.ResourceLogs())
		return []Request{req}, nil
	}

	var (
		res          []Request
		destReq      *logsRequest
		capacityLeft = cfg.MaxSizeItems
	)
	for _, srcReq := range []*logsRequest{req, req2} {
		if srcReq == nil {
			continue
		}

		srcCount := srcReq.ld.LogRecordCount()
		if srcCount <= capacityLeft {
			if destReq == nil {
				destReq = srcReq
			} else {
				srcReq.ld.ResourceLogs().MoveAndAppendTo(destReq.ld.ResourceLogs())
			}
			capacityLeft -= srcCount
			continue
		}

		for {
			extractedLogs := extractLogs(srcReq.ld, capacityLeft)
			if extractedLogs.LogRecordCount() == 0 {
				break
			}
			capacityLeft -= extractedLogs.LogRecordCount()
			if destReq == nil {
				destReq = newLogsRequest(extractedLogs, srcReq.pusher).(*logsRequest)
			} else {
				extractedLogs.ResourceLogs().MoveAndAppendTo(destReq.ld.ResourceLogs())
			}
			// Create new batch once capacity is reached.
			if capacityLeft == 0 {
				res = append(res, destReq)
				destReq = nil
				capacityLeft = cfg.MaxSizeItems
			}
		}
	}

	if destReq != nil {
		res = append(res, destReq)
	}
	return res, nil
}

// extractLogs extracts logs from the input logs and returns a new logs with the specified number of log records.
func extractLogs(srcLogs plog.Logs, count int) plog.Logs {
	destLogs := plog.NewLogs()
	srcLogs.ResourceLogs().RemoveIf(func(srcRL plog.ResourceLogs) bool {
		if count == 0 {
			return false
		}
		needToExtract := resourceLogsCount(srcRL) > count
		if needToExtract {
			srcRL = extractResourceLogs(srcRL, count)
		}
		count -= resourceLogsCount(srcRL)
		srcRL.MoveTo(destLogs.ResourceLogs().AppendEmpty())
		return !needToExtract
	})
	return destLogs
}

// extractResourceLogs extracts resource logs and returns a new resource logs with the specified number of log records.
func extractResourceLogs(srcRL plog.ResourceLogs, count int) plog.ResourceLogs {
	destRL := plog.NewResourceLogs()
	destRL.SetSchemaUrl(srcRL.SchemaUrl())
	srcRL.Resource().CopyTo(destRL.Resource())
	srcRL.ScopeLogs().RemoveIf(func(srcSL plog.ScopeLogs) bool {
		if count == 0 {
			return false
		}
		needToExtract := srcSL.LogRecords().Len() > count
		if needToExtract {
			srcSL = extractScopeLogs(srcSL, count)
		}
		count -= srcSL.LogRecords().Len()
		srcSL.MoveTo(destRL.ScopeLogs().AppendEmpty())
		return !needToExtract
	})
	return destRL
}

// extractScopeLogs extracts scope logs and returns a new scope logs with the specified number of log records.
func extractScopeLogs(srcSL plog.ScopeLogs, count int) plog.ScopeLogs {
	destSL := plog.NewScopeLogs()
	destSL.SetSchemaUrl(srcSL.SchemaUrl())
	srcSL.Scope().CopyTo(destSL.Scope())
	srcSL.LogRecords().RemoveIf(func(srcLR plog.LogRecord) bool {
		if count == 0 {
			return false
		}
		srcLR.MoveTo(destSL.LogRecords().AppendEmpty())
		count--
		return true
	})
	return destSL
}

// resourceLogsCount calculates the total number of log records in the plog.ResourceLogs.
func resourceLogsCount(rl plog.ResourceLogs) int {
	count := 0
	for k := 0; k < rl.ScopeLogs().Len(); k++ {
		count += rl.ScopeLogs().At(k).LogRecords().Len()
	}
	return count
}
