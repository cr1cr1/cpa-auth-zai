package main

import (
	"encoding/json"
	"fmt"
	"time"

	_ "time/tzdata" // embed the tz database; containers may lack zoneinfo
)

// UsageDetail is the decoded upstream usage-detail payload. Decimal summary
// values stay strings to preserve exactness.
type UsageDetail struct {
	Granularity string             `json:"granularity"`
	Timezone    string             `json:"timezone"`
	Summary     usageSummaryDetail `json:"summary"`
	ModelUsage  usageModelUsage    `json:"modelUsage"`
}

type usageRatePoint struct {
	Value string `json:"value"`
	Trend string `json:"trend"`
}

type usageSummaryDetail struct {
	CacheHitRate       usageRatePoint `json:"cacheHitRate"`
	OffPeakUsageRate   usageRatePoint `json:"offPeakUsageRate"`
	TotalTokens        usageRatePoint `json:"totalTokens"`
	AverageDailyTokens usageRatePoint `json:"averageDailyTokens"`
}

type usageModelUsage struct {
	TotalUsage struct {
		TotalTokens int64 `json:"totalTokens"`
	} `json:"totalUsage"`
	ModelDataList    []usageModelSeries  `json:"modelDataList"`
	ModelSummaryList []usageModelSummary `json:"modelSummaryList"`
	XTime            []string            `json:"xTime"`
}

type usageModelSeries struct {
	ModelCode                string  `json:"modelCode"`
	ModelName                string  `json:"modelName"`
	SortOrder                int64   `json:"sortOrder"`
	UncachedInputTokensUsage []int64 `json:"uncachedInputTokensUsage"`
	CachedInputTokensUsage   []int64 `json:"cachedInputTokensUsage"`
	InputTokensUsage         []int64 `json:"inputTokensUsage"`
	OutputTokensUsage        []int64 `json:"outputTokensUsage"`
	TotalTokensUsage         []int64 `json:"totalTokensUsage"`
}

type usageModelSummary struct {
	ModelCode   string `json:"modelCode"`
	ModelName   string `json:"modelName"`
	SortOrder   int64  `json:"sortOrder"`
	TotalTokens int64  `json:"totalTokens"`
}

// decodeUsageDetail validates the upstream envelope and pairs every per-model
// array with xTime. It does not sum or transform beyond the decode.
func decodeUsageDetail(raw []byte) (UsageDetail, error) {
	var envelope struct {
		Code    int          `json:"code"`
		Msg     string       `json:"msg"`
		Success *bool        `json:"success"`
		Data    *UsageDetail `json:"data"`
	}
	if errUnmarshal := json.Unmarshal(raw, &envelope); errUnmarshal != nil {
		return UsageDetail{}, fmt.Errorf("zai: decode usage detail: %w", errUnmarshal)
	}
	if envelope.Code != 200 || (envelope.Success != nil && !*envelope.Success) {
		message := sanitizeDetail(envelope.Msg)
		if message == "" {
			message = "usage request failed"
		}
		return UsageDetail{}, fmt.Errorf("zai: usage detail error %d: %s", envelope.Code, message)
	}
	if envelope.Data == nil {
		return UsageDetail{}, fmt.Errorf("zai: usage detail response has no data")
	}
	detail := *envelope.Data
	for _, model := range detail.ModelUsage.ModelDataList {
		for _, series := range [][]int64{
			model.UncachedInputTokensUsage,
			model.CachedInputTokensUsage,
			model.InputTokensUsage,
			model.OutputTokensUsage,
			model.TotalTokensUsage,
		} {
			if len(series) != len(detail.ModelUsage.XTime) {
				return UsageDetail{}, fmt.Errorf(
					"zai: usage arrays for model %s have %d entries, want %d matching xTime",
					model.ModelCode, len(series), len(detail.ModelUsage.XTime))
			}
		}
	}
	return detail, nil
}

// UsageQuery carries the requested history range and the UI-selected zone.
type UsageQuery struct {
	AuthIndex string
	Start     time.Time
	End       time.Time
	Timezone  string
}

type usageViewRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type usageViewMetric struct {
	Value string `json:"value"`
	Trend string `json:"trend"`
}

type usageViewSummary struct {
	CacheHitRate       usageViewMetric `json:"cache_hit_rate"`
	OffPeakUsageRate   usageViewMetric `json:"off_peak_usage_rate"`
	TotalTokens        usageViewMetric `json:"total_tokens"`
	AverageDailyTokens usageViewMetric `json:"average_daily_tokens"`
}

type usageViewTotals struct {
	UncachedInput int64 `json:"uncached_input"`
	CachedInput   int64 `json:"cached_input"`
	Input         int64 `json:"input"`
	Output        int64 `json:"output"`
	Total         int64 `json:"total"`
}

type usageViewDay struct {
	Date          string `json:"date"`
	IntervalStart string `json:"interval_start"`
	IntervalEnd   string `json:"interval_end"`
	usageViewTotals
}

type usageModelView struct {
	Code      string          `json:"code"`
	Name      string          `json:"name"`
	SortOrder int64           `json:"sort_order"`
	Totals    usageViewTotals `json:"totals"`
	Daily     []usageViewDay  `json:"daily"`
}

// UsageView renders the provider-day history in the requested zone.
type UsageView struct {
	RequestedTimezone string           `json:"requested_timezone"`
	SourceTimezone    string           `json:"source_timezone"`
	Granularity       string           `json:"granularity"`
	AggregationBasis  string           `json:"aggregation_basis"`
	Range             usageViewRange   `json:"range"`
	Summary           usageViewSummary `json:"summary"`
	Models            []usageModelView `json:"models"`
}

// buildUsageView renders each Shanghai source day as an explicit interval in
// the requested zone, keeping every source-day total attached to its own day.
// Zero-valued days are still emitted: one daily entry per xTime date.
func buildUsageView(detail UsageDetail, query UsageQuery) (UsageView, error) {
	requestedZone := query.Timezone
	if requestedZone == "" {
		requestedZone = detail.Timezone
	}
	requestedLoc, errLoad := time.LoadLocation(requestedZone)
	if errLoad != nil {
		return UsageView{}, fmt.Errorf("zai: unknown requested timezone %q", requestedZone)
	}
	sourceZone := detail.Timezone
	if sourceZone == "" {
		sourceZone = requestedZone
	}
	sourceLoc, errLoad := time.LoadLocation(sourceZone)
	if errLoad != nil {
		return UsageView{}, fmt.Errorf("zai: unknown source timezone %q", sourceZone)
	}
	basis := "provider-day-boundaries-in-requested-tz"
	if requestedZone == sourceZone {
		basis = "provider-day"
	}
	view := UsageView{
		RequestedTimezone: requestedZone,
		SourceTimezone:    sourceZone,
		Granularity:       detail.Granularity,
		AggregationBasis:  basis,
		Range: usageViewRange{
			Start: query.Start.In(requestedLoc).Format(time.RFC3339),
			End:   query.End.In(requestedLoc).Format(time.RFC3339),
		},
		Summary: usageViewSummary{
			CacheHitRate:       usageViewMetric(detail.Summary.CacheHitRate),
			OffPeakUsageRate:   usageViewMetric(detail.Summary.OffPeakUsageRate),
			TotalTokens:        usageViewMetric(detail.Summary.TotalTokens),
			AverageDailyTokens: usageViewMetric(detail.Summary.AverageDailyTokens),
		},
	}
	for _, model := range detail.ModelUsage.ModelDataList {
		modelView := usageModelView{
			Code:      model.ModelCode,
			Name:      model.ModelName,
			SortOrder: model.SortOrder,
		}
		for index, date := range detail.ModelUsage.XTime {
			parsed, errParse := time.Parse("2006-01-02", date)
			if errParse != nil {
				return UsageView{}, fmt.Errorf("zai: invalid usage date %q", date)
			}
			year, month, day := parsed.Date()
			dayStart := time.Date(year, month, day, 0, 0, 0, 0, sourceLoc)
			dayEnd := time.Date(year, month, day+1, 0, 0, 0, 0, sourceLoc)
			entry := usageViewDay{
				Date:          date,
				IntervalStart: dayStart.In(requestedLoc).Format(time.RFC3339),
				IntervalEnd:   dayEnd.In(requestedLoc).Format(time.RFC3339),
			}
			if index < len(model.UncachedInputTokensUsage) {
				entry.UncachedInput = model.UncachedInputTokensUsage[index]
			}
			if index < len(model.CachedInputTokensUsage) {
				entry.CachedInput = model.CachedInputTokensUsage[index]
			}
			if index < len(model.InputTokensUsage) {
				entry.Input = model.InputTokensUsage[index]
			}
			if index < len(model.OutputTokensUsage) {
				entry.Output = model.OutputTokensUsage[index]
			}
			if index < len(model.TotalTokensUsage) {
				entry.Total = model.TotalTokensUsage[index]
			}
			modelView.Totals.UncachedInput += entry.UncachedInput
			modelView.Totals.CachedInput += entry.CachedInput
			modelView.Totals.Input += entry.Input
			modelView.Totals.Output += entry.Output
			modelView.Totals.Total += entry.Total
			modelView.Daily = append(modelView.Daily, entry)
		}
		view.Models = append(view.Models, modelView)
	}
	return view, nil
}

// fetchUsageDetail requests the upstream usage-detail report for the range.
//
// shortcut: upstream startTime/endTime zone semantics unverified, revisit after live check
func fetchUsageDetail(auth *zaiStorage, callbackID string, start, end time.Time) (UsageDetail, error) {
	sourceLoc, errLoad := time.LoadLocation("Asia/Shanghai")
	if errLoad != nil {
		return UsageDetail{}, fmt.Errorf("zai: load source timezone: %w", errLoad)
	}
	query := encodeQuery(map[string]string{
		"startTime": start.In(sourceLoc).Format("2006-01-02 15:04:05"),
		"endTime":   end.In(sourceLoc).Format("2006-01-02 15:04:05"),
		"usageType": "MODEL",
		"type":      "1",
	})
	response, okFetch := tryBearerCandidates(callbackID, usageDetailURL+"?"+query, auth)
	if !okFetch {
		if response != nil {
			return UsageDetail{}, upstreamError(response.StatusCode, response.Body)
		}
		return UsageDetail{}, fmt.Errorf("zai: usage detail request failed for every stored credential")
	}
	return decodeUsageDetail(response.Body)
}
