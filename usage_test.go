package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// usageFixtureBytes loads the verbatim user-supplied usage-detail sample.
func usageFixtureBytes(t *testing.T) []byte {
	t.Helper()
	raw, errRead := os.ReadFile("testdata/usage-detail.json")
	if errRead != nil {
		t.Fatalf("read usage fixture: %v", errRead)
	}
	return raw
}

func mustDecodeUsage(t *testing.T, raw []byte) UsageDetail {
	t.Helper()
	detail, errDecode := decodeUsageDetail(raw)
	if errDecode != nil {
		t.Fatalf("decodeUsageDetail: %v", errDecode)
	}
	return detail
}

func mustBuildView(t *testing.T, detail UsageDetail, query UsageQuery) UsageView {
	t.Helper()
	view, errBuild := buildUsageView(detail, query)
	if errBuild != nil {
		t.Fatalf("buildUsageView: %v", errBuild)
	}
	return view
}

// Acceptance: the user sample decodes exactly — account total, per-model
// totals, preserved decimal summary strings, seven aligned dates, and one
// nonzero Shanghai day carried through the view without double counting.
func TestUsageSampleTotals(t *testing.T) {
	detail := mustDecodeUsage(t, usageFixtureBytes(t))
	if got := detail.ModelUsage.TotalUsage.TotalTokens; got != 43826245 {
		t.Fatalf("totalUsage.totalTokens = %d, want 43826245", got)
	}
	if got := detail.Summary.CacheHitRate.Value; got != "0.9780" {
		t.Fatalf("cacheHitRate value = %q, want 0.9780", got)
	}
	if got := detail.Summary.OffPeakUsageRate.Value; got != "1.0000" {
		t.Fatalf("offPeakUsageRate value = %q, want 1.0000", got)
	}
	if len(detail.ModelUsage.XTime) != 7 {
		t.Fatalf("xTime length = %d, want 7", len(detail.ModelUsage.XTime))
	}
	totals := map[string]int64{}
	for _, summary := range detail.ModelUsage.ModelSummaryList {
		totals[summary.ModelCode] = summary.TotalTokens
	}
	if totals["glm-5.3"] != 43484230 || totals["glm-5.3-flash"] != 342015 {
		t.Fatalf("model summaries = %v", totals)
	}
	view := mustBuildView(t, detail, UsageQuery{
		Start: mustTime(t, "2026-10-17T00:00:00+08:00"),
		End:   mustTime(t, "2026-10-24T00:00:00+08:00"),
	})
	if len(view.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(view.Models))
	}
	glm := view.Models[0]
	if glm.Code != "glm-5.3" || len(glm.Daily) != 7 {
		t.Fatalf("glm-5.3 series: code=%q daily=%d", glm.Code, len(glm.Daily))
	}
	if got := glm.Daily[3].Total; got != 43484230 {
		t.Fatalf("glm-5.3 daily[3] total = %d, want 43484230", got)
	}
	for index, day := range glm.Daily {
		if index != 3 && day.Total != 0 {
			t.Fatalf("glm-5.3 daily[%d] total = %d, want 0", index, day.Total)
		}
	}
	if got := glm.Totals.Total; got != 43484230 {
		t.Fatalf("glm-5.3 view total = %d, want 43484230", got)
	}
	flash := view.Models[1]
	if flash.Code != "glm-5.3-flash" || flash.Totals.Total != 342015 {
		t.Fatalf("glm-5.3-flash view totals = %+v", flash.Totals)
	}
}

// Acceptance: cached input is a subset of input; the decoder must not add it
// again anywhere.
func TestUsageCachedInputWithinInput(t *testing.T) {
	detail := mustDecodeUsage(t, usageFixtureBytes(t))
	for _, model := range detail.ModelUsage.ModelDataList {
		for index := range detail.ModelUsage.XTime {
			if model.CachedInputTokensUsage[index] > model.InputTokensUsage[index] {
				t.Fatalf("model %s index %d: cached %d > input %d",
					model.ModelCode, index, model.CachedInputTokensUsage[index], model.InputTokensUsage[index])
			}
		}
	}
}

// Acceptance: the model summary list repeats the same totals as the aligned
// arrays; totals must agree or the data is unusable.
func TestUsageModelTotalsMatchSummary(t *testing.T) {
	detail := mustDecodeUsage(t, usageFixtureBytes(t))
	summaries := map[string]int64{}
	for _, summary := range detail.ModelUsage.ModelSummaryList {
		summaries[summary.ModelCode] = summary.TotalTokens
	}
	for _, model := range detail.ModelUsage.ModelDataList {
		var sum int64
		for _, value := range model.TotalTokensUsage {
			sum += value
		}
		if sum != summaries[model.ModelCode] {
			t.Fatalf("model %s array sum %d != summary total %d", model.ModelCode, sum, summaries[model.ModelCode])
		}
	}
}

// Acceptance: every per-model array must align with xTime; a short array is a
// decode error naming the model.
func TestUsageLengthMismatchFails(t *testing.T) {
	var parsed map[string]any
	if errUnmarshal := json.Unmarshal(usageFixtureBytes(t), &parsed); errUnmarshal != nil {
		t.Fatalf("decode fixture: %v", errUnmarshal)
	}
	data := parsed["data"].(map[string]any)
	modelUsage := data["modelUsage"].(map[string]any)
	list := modelUsage["modelDataList"].([]any)
	first := list[0].(map[string]any)
	arr := first["totalTokensUsage"].([]any)
	first["totalTokensUsage"] = arr[:len(arr)-1]
	corrupted, errMarshal := json.Marshal(parsed)
	if errMarshal != nil {
		t.Fatalf("marshal corrupted fixture: %v", errMarshal)
	}
	_, errDecode := decodeUsageDetail(corrupted)
	if errDecode == nil {
		t.Fatalf("short array must fail decoding")
	}
	if !strings.Contains(errDecode.Error(), "glm-5.3") {
		t.Fatalf("error must name the model, got %q", errDecode.Error())
	}
}

// Acceptance: a business error inside HTTP 200 is an error carrying the
// upstream message, not a successful empty detail.
func TestUsageBusinessErrorFails(t *testing.T) {
	raw := []byte(`{"code":1113,"msg":"usage query rejected","success":false}`)
	_, errDecode := decodeUsageDetail(raw)
	if errDecode == nil {
		t.Fatalf("business error must fail decoding")
	}
	if !strings.Contains(errDecode.Error(), "usage query rejected") {
		t.Fatalf("error must carry upstream msg, got %q", errDecode.Error())
	}
}

// Acceptance: a Shanghai source-day total renders its full interval in the
// requested zone with the SAME total — it is never relabeled into a
// Bucharest calendar-day total.
func TestBuildUsageViewProviderDayNotRelabeled(t *testing.T) {
	detail := mustDecodeUsage(t, usageFixtureBytes(t))
	// Move the glm-5.3 total onto the 2026-10-17 Shanghai day to prove the
	// total follows the source day into the requested-zone interval.
	detail.ModelUsage.ModelDataList[0].TotalTokensUsage[0] = 43484230
	detail.ModelUsage.ModelDataList[0].TotalTokensUsage[3] = 0
	view := mustBuildView(t, detail, UsageQuery{
		Start:    mustTime(t, "2026-10-17T00:00:00+08:00"),
		End:      mustTime(t, "2026-10-24T00:00:00+08:00"),
		Timezone: "Europe/Bucharest",
	})
	if view.AggregationBasis != "provider-day-boundaries-in-requested-tz" {
		t.Fatalf("aggregation_basis = %q", view.AggregationBasis)
	}
	day := view.Models[0].Daily[0]
	if day.Date != "2026-10-17" {
		t.Fatalf("daily[0].date = %q", day.Date)
	}
	if day.IntervalStart != "2026-10-16T19:00:00+03:00" {
		t.Fatalf("interval_start = %q, want 2026-10-16T19:00:00+03:00", day.IntervalStart)
	}
	if day.IntervalEnd != "2026-10-17T19:00:00+03:00" {
		t.Fatalf("interval_end = %q, want 2026-10-17T19:00:00+03:00", day.IntervalEnd)
	}
	if day.Total != 43484230 {
		t.Fatalf("provider-day total changed: %d, want 43484230", day.Total)
	}
}

// Acceptance: a range crossing the Europe/Bucharest 2026-10-25 DST transition
// renders both offsets correctly and never errors.
func TestBuildUsageViewDST(t *testing.T) {
	detail := UsageDetail{
		Granularity: "DAY",
		Timezone:    "Asia/Shanghai",
	}
	detail.ModelUsage.XTime = []string{"2026-10-24", "2026-10-25", "2026-10-26"}
	detail.ModelUsage.ModelDataList = []usageModelSeries{{
		ModelCode: "glm-5.3", ModelName: "GLM-5.3", SortOrder: 1,
		TotalTokensUsage: []int64{1, 2, 3},
	}}
	view := mustBuildView(t, detail, UsageQuery{
		Start:    mustTime(t, "2026-10-23T00:00:00+08:00"),
		End:      mustTime(t, "2026-10-27T00:00:00+08:00"),
		Timezone: "Europe/Bucharest",
	})
	day := view.Models[0].Daily[1]
	if day.Date != "2026-10-25" {
		t.Fatalf("daily[1].date = %q", day.Date)
	}
	if day.IntervalStart != "2026-10-24T19:00:00+03:00" {
		t.Fatalf("interval_start = %q, want 2026-10-24T19:00:00+03:00", day.IntervalStart)
	}
	if day.IntervalEnd != "2026-10-25T18:00:00+02:00" {
		t.Fatalf("interval_end = %q, want 2026-10-25T18:00:00+02:00", day.IntervalEnd)
	}
}

// Acceptance: an unknown IANA zone is rejected, never silently defaulted.
func TestBuildUsageViewUnknownTimezoneFails(t *testing.T) {
	detail := mustDecodeUsage(t, usageFixtureBytes(t))
	_, errBuild := buildUsageView(detail, UsageQuery{
		Start:    mustTime(t, "2026-10-17T00:00:00+08:00"),
		End:      mustTime(t, "2026-10-18T00:00:00+08:00"),
		Timezone: "Mars/Olympus",
	})
	if errBuild == nil {
		t.Fatalf("unknown timezone must fail")
	}
}

// Acceptance: an empty requested zone falls back to the source timezone and
// keeps the provider-day aggregation basis.
func TestBuildUsageViewDefaultsToSourceZone(t *testing.T) {
	detail := mustDecodeUsage(t, usageFixtureBytes(t))
	view := mustBuildView(t, detail, UsageQuery{
		Start: mustTime(t, "2026-10-17T00:00:00+08:00"),
		End:   mustTime(t, "2026-10-18T00:00:00+08:00"),
	})
	if view.RequestedTimezone != "Asia/Shanghai" {
		t.Fatalf("requested_timezone = %q, want Asia/Shanghai", view.RequestedTimezone)
	}
	if view.SourceTimezone != "Asia/Shanghai" {
		t.Fatalf("source_timezone = %q", view.SourceTimezone)
	}
	if view.AggregationBasis != "provider-day" {
		t.Fatalf("aggregation_basis = %q, want provider-day", view.AggregationBasis)
	}
}

func mustTime(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, errParse := time.Parse(time.RFC3339, raw)
	if errParse != nil {
		t.Fatalf("parse time %q: %v", raw, errParse)
	}
	return parsed
}

// Acceptance: a usage business error cannot echo a stored credential through
// the management API, even when it appears inside the provider message.
func TestUsageBusinessErrorRedactsCredential(t *testing.T) {
	raw := []byte(`{"code":401,"msg":"Bearer tok-oauth rejected","success":false}`)
	_, err := decodeUsageDetail(raw, "tok-oauth")
	if err == nil {
		t.Fatalf("business error must fail decode")
	}
	if strings.Contains(err.Error(), "tok-oauth") || !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error did not redact the credential: %q", err)
	}
}

// Acceptance: a hostile upstream msg cannot flood the management 502 body.
func TestUsageBusinessErrorDetailCapped(t *testing.T) {
	huge := strings.Repeat("x", 5000)
	_, err := decodeUsageDetail([]byte(`{"code":1113,"msg":"` + huge + `","success":false}`))
	if err == nil {
		t.Fatalf("business error must fail decode")
	}
	if len(err.Error()) > 400 {
		t.Fatalf("error detail not capped: %d chars", len(err.Error()))
	}
}
