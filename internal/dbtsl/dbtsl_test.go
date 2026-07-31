// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package dbtsl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubServer stands in for the dbt Semantic Layer GraphQL API. Each request is
// routed by the GraphQL operation name in the query body, and the matching
// handler returns whatever the test wants for that operation.
type stubServer struct {
	t         *testing.T
	responses map[string][]string // operation -> queued response bodies
	calls     map[string]int
	requests  []map[string]any
	server    *httptest.Server
}

func newStubServer(t *testing.T) *stubServer {
	t.Helper()
	s := &stubServer{
		t:         t,
		responses: make(map[string][]string),
		calls:     make(map[string]int),
	}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			s.t.Errorf("stub received malformed JSON: %v", err)
		}
		s.requests = append(s.requests, req)

		query, _ := req["query"].(string)
		op := operationOf(query)
		s.calls[op]++

		queued := s.responses[op]
		if len(queued) == 0 {
			s.t.Errorf("stub had no queued response for operation %q", op)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		next := queued[0]
		if len(queued) > 1 {
			s.responses[op] = queued[1:]
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(next))
	}))
	t.Cleanup(s.server.Close)
	return s
}

// queue adds a response body for an operation. Responses are consumed in
// order; the last one queued is reused for any further calls.
func (s *stubServer) queue(operation, body string) {
	s.responses[operation] = append(s.responses[operation], body)
}

// lastVariables returns the variables sent on the most recent request.
func (s *stubServer) lastVariables() map[string]any {
	s.t.Helper()
	if len(s.requests) == 0 {
		s.t.Fatal("no requests were made")
	}
	vars, _ := s.requests[len(s.requests)-1]["variables"].(map[string]any)
	return vars
}

// operationOf extracts the GraphQL operation name from a query document.
func operationOf(query string) string {
	for _, line := range strings.Split(query, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, prefix := range []string{"query ", "mutation "} {
			if strings.HasPrefix(line, prefix) {
				rest := strings.TrimPrefix(line, prefix)
				if idx := strings.IndexAny(rest, "( {"); idx > 0 {
					return rest[:idx]
				}
				return rest
			}
		}
	}
	return "unknown"
}

func (s *stubServer) client(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient(Config{
		Host:          strings.TrimPrefix(s.server.URL, "http://"),
		EnvironmentID: "202356",
		Token:         "test-token",
	})
	if err != nil {
		t.Fatalf("failed to build client: %v", err)
	}
	// The stub is plain HTTP, so point the client at it directly rather than
	// through the https URL NewClient builds.
	client.graphqlURL = s.server.URL
	return client
}

// ---------------------------------------------------------------------------
// Allowlist and suggestions
// ---------------------------------------------------------------------------

// TestEverySearchableTopicMatchesAMetric pins the topic words offered on an
// empty search to the allowlist. A topic that stops matching sends the caller
// somewhere that returns nothing, which is the failure this list exists to
// prevent.
func TestEverySearchableTopicMatchesAMetric(t *testing.T) {
	for _, topic := range SearchableTopics() {
		matched := false
		for _, metric := range AllowedMetricNames() {
			if strings.Contains(metric, topic) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("topic %q matches no allowlisted metric name", topic)
		}
	}
}

func TestSuggestMetricsRecoversAGuessedName(t *testing.T) {
	suggestions := SuggestMetrics("contributor_count", 5)
	if !contains(suggestions, "total_contributors") {
		t.Errorf("expected total_contributors among suggestions, got %v", suggestions)
	}
}

func TestSuggestMetricsRanksDistinctiveWordsFirst(t *testing.T) {
	// "contributor" is distinctive, "count" is shared by much of the
	// allowlist, so contributor metrics must outrank generic count metrics.
	suggestions := SuggestMetrics("contributor_count", 3)
	if len(suggestions) == 0 {
		t.Fatal("expected suggestions")
	}
	if !strings.Contains(suggestions[0], "contribut") {
		t.Errorf("expected a contributor metric first, got %v", suggestions)
	}
}

func TestSuggestMetricsRespectsLimit(t *testing.T) {
	if got := len(SuggestMetrics("total", 2)); got != 2 {
		t.Errorf("expected 2 suggestions, got %d", got)
	}
}

func TestValidateMetricsRejectsUnknown(t *testing.T) {
	disallowed := ValidateMetrics([]string{"total_contributors", "user__email", "made_up"})
	if len(disallowed) != 2 {
		t.Fatalf("expected 2 disallowed, got %v", disallowed)
	}
}

func TestNoMetricsDetailNamesTopicsAndTheDimensionTrap(t *testing.T) {
	detail := NoMetricsDetail("vietnam")
	if !strings.Contains(detail, "membership") {
		t.Error("expected the message to name topic words")
	}
	if !strings.Contains(detail, "dimensions rather than metrics") {
		t.Error("expected the message to explain that country and region are dimensions")
	}
}

// ---------------------------------------------------------------------------
// Similarity, mirroring difflib.SequenceMatcher.ratio()
// ---------------------------------------------------------------------------

// TestSimilarityRatioMatchesDifflib checks similarityRatio against every
// ordered pair of allowlisted metric names, plus a handful of short
// adversarial pairs.
//
// The fixture holds ratios produced by Python's difflib itself:
//
//	python3 -c 'import difflib; print(difflib.SequenceMatcher(None, a, b).ratio())'
//
// An earlier version of this test used eight hand-picked pairs and passed
// while the implementation disagreed with difflib on a quarter of the real
// domain: every hand-picked pair happened to be one where the tie-break did
// not bite. Sweeping the domain is the only version of this test that would
// have caught it, and it still runs in milliseconds.
func TestSimilarityRatioMatchesDifflib(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "difflib_ratios.json"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var cases [][]any
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	if len(cases) < 3000 {
		t.Fatalf("fixture has only %d pairs, expected the full allowlist sweep", len(cases))
	}

	mismatched := 0
	for _, c := range cases {
		a, b, want := c[0].(string), c[1].(string), c[2].(float64)
		got := similarityRatio(a, b)
		if !nearlyEqual(got, want) {
			mismatched++
			if mismatched <= 5 {
				t.Errorf("similarityRatio(%q, %q) = %v, difflib = %v", a, b, got, want)
			}
		}
	}
	if mismatched > 0 {
		t.Errorf("%d of %d pairs disagree with difflib", mismatched, len(cases))
	}
}

func nearlyEqual(a, b float64) bool {
	const epsilon = 1e-9
	diff := a - b
	return diff < epsilon && diff > -epsilon
}

// ---------------------------------------------------------------------------
// Search fallbacks
// ---------------------------------------------------------------------------

func TestSingularVariants(t *testing.T) {
	tests := []struct {
		word string
		want []string
	}{
		{"contributions", []string{"contributions", "contribution"}},
		{"activities", []string{"activities", "activity"}},
		{"addresses", []string{"addresses", "address"}},
		{"as", []string{"as"}},         // too short to stem
		{"health", []string{"health"}}, // no plural suffix
	}
	for _, tc := range tests {
		got := singularVariants(tc.word)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("singularVariants(%q) = %v, want %v", tc.word, got, tc.want)
		}
	}
}

func TestMatchAnyWordRescuesAPluralSearch(t *testing.T) {
	metrics := []MetricInfo{
		{Name: "code_contribution_activities", Description: "Contribution activity"},
		{Name: "total_events", Description: "Events"},
		{Name: "not_allowlisted_metric", Description: "contribution"},
	}
	matched := matchAnyWord(metrics, "contributions")
	if len(matched) != 1 || matched[0].Name != "code_contribution_activities" {
		t.Errorf("expected only the allowlisted contribution metric, got %v", matched)
	}
}

func TestMatchAnyWordRescuesAMultiWordSearch(t *testing.T) {
	metrics := []MetricInfo{
		{Name: "total_contributing_organizations", Description: "Organizations contributing"},
		{Name: "total_events", Description: "Events"},
	}
	matched := matchAnyWord(metrics, "contributor organization country")
	if len(matched) != 1 || matched[0].Name != "total_contributing_organizations" {
		t.Errorf("expected the contributing organizations metric, got %v", matched)
	}
}

// ---------------------------------------------------------------------------
// Query construction
// ---------------------------------------------------------------------------

// TestSplitTimeGrain covers the translation the GraphQL API forces on us:
// callers write "metric_time__month" the way the dbt SDK accepts it, but the
// API takes the grain as its own field.
func TestSplitTimeGrain(t *testing.T) {
	tests := []struct {
		name      string
		wantBase  string
		wantGrain string
		wantOK    bool
	}{
		{"metric_time__month", "metric_time", "MONTH", true},
		{"metric_time__year", "metric_time", "YEAR", true},
		{"created_at__day", "created_at", "DAY", true},
		{"country__lf_region", "", "", false},
		{"metric_time", "", "", false},
		{"asset_id__membership_tier", "", "", false},
	}
	for _, tc := range tests {
		base, grain, ok := splitTimeGrain(tc.name)
		if ok != tc.wantOK || base != tc.wantBase || grain != tc.wantGrain {
			t.Errorf("splitTimeGrain(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.name, base, grain, ok, tc.wantBase, tc.wantGrain, tc.wantOK)
		}
	}
}

func TestBuildGroupByInputsSeparatesGrain(t *testing.T) {
	inputs := buildGroupByInputs([]string{"country__lf_region", "metric_time__month", "  "})
	if len(inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(inputs))
	}
	if inputs[0]["name"] != "country__lf_region" {
		t.Errorf("expected the plain dimension untouched, got %v", inputs[0])
	}
	if _, hasGrain := inputs[0]["grain"]; hasGrain {
		t.Error("a non-time dimension must not carry a grain")
	}
	if inputs[1]["name"] != "metric_time" || inputs[1]["grain"] != "MONTH" {
		t.Errorf("expected metric_time split from MONTH, got %v", inputs[1])
	}
}

func TestBuildOrderByInputsDistinguishesMetricsFromGroupBys(t *testing.T) {
	metrics := []string{"total_contributors"}
	inputs := buildOrderByInputs([]string{"-total_contributors", "country__lf_region"}, metrics)
	if len(inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(inputs))
	}

	if inputs[0]["descending"] != true {
		t.Error("expected the - prefix to mean descending")
	}
	if _, isMetric := inputs[0]["metric"]; !isMetric {
		t.Errorf("expected a query metric to order as a metric, got %v", inputs[0])
	}

	if inputs[1]["descending"] != false {
		t.Error("expected no prefix to mean ascending")
	}
	if _, isGroupBy := inputs[1]["groupBy"]; !isGroupBy {
		t.Errorf("expected a non-metric to order as a groupBy, got %v", inputs[1])
	}
}

func TestParseQueryResultStripsTheSyntheticIndex(t *testing.T) {
	raw := `{
	  "schema": {
	    "fields": [
	      {"name": "index", "type": "integer"},
	      {"name": "country__lf_region", "type": "string"},
	      {"name": "total_contributors", "type": "integer"}
	    ],
	    "primaryKey": ["index"]
	  },
	  "data": [
	    {"index": 0, "country__lf_region": "Asia Pacific", "total_contributors": 12},
	    {"index": 1, "country__lf_region": "Europe", "total_contributors": 34}
	  ]
	}`

	result, err := parseQueryResult(raw, "SELECT 1")
	if err != nil {
		t.Fatalf("parseQueryResult failed: %v", err)
	}
	if strings.Join(result.Columns, ",") != "country__lf_region,total_contributors" {
		t.Errorf("expected the index column stripped, got %v", result.Columns)
	}
	if result.RowCount != 2 {
		t.Errorf("expected 2 rows, got %d", result.RowCount)
	}
	if _, present := result.Data[0]["index"]; present {
		t.Error("expected the index key stripped from rows")
	}
	if result.CompiledSQL != "SELECT 1" {
		t.Errorf("expected the compiled SQL carried through, got %q", result.CompiledSQL)
	}
}

func TestParseQueryResultHandlesAnEmptyBody(t *testing.T) {
	result, err := parseQueryResult("", "")
	if err != nil {
		t.Fatalf("parseQueryResult failed: %v", err)
	}
	if result.RowCount != 0 || len(result.Columns) != 0 {
		t.Errorf("expected an empty result, got %+v", result)
	}
}

// ---------------------------------------------------------------------------
// Query execution over the poll loop
// ---------------------------------------------------------------------------

const successfulResultJSON = `{"data":{"query":{"status":"SUCCESSFUL","error":null,"sql":"SELECT 1","jsonResult":"{\"schema\":{\"fields\":[{\"name\":\"index\",\"type\":\"integer\"},{\"name\":\"country__lf_region\",\"type\":\"string\"}],\"primaryKey\":[\"index\"]},\"data\":[{\"index\":0,\"country__lf_region\":\"Asia Pacific\"}]}"}}}`

func TestQueryPollsUntilSuccessful(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("CreateQuery", `{"data":{"createQuery":{"queryId":"q-1"}}}`)
	stub.queue("GetQueryResult", `{"data":{"query":{"status":"RUNNING","error":null,"sql":null,"jsonResult":null}}}`)
	stub.queue("GetQueryResult", successfulResultJSON)

	client := stub.client(t)
	result, err := client.Query(context.Background(), QueryArgs{Metrics: []string{"total_contributors"}})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if result.RowCount != 1 {
		t.Errorf("expected 1 row, got %d", result.RowCount)
	}
	if stub.calls["GetQueryResult"] != 2 {
		t.Errorf("expected 2 polls, got %d", stub.calls["GetQueryResult"])
	}
}

func TestQuerySurfacesAFailureAsAnApplicationError(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("CreateQuery", `{"data":{"createQuery":{"queryId":"q-1"}}}`)
	stub.queue("GetQueryResult", `{"data":{"query":{"status":"FAILED","error":"Unable to resolve metric","sql":null,"jsonResult":null}}}`)

	client := stub.client(t)
	_, err := client.Query(context.Background(), QueryArgs{Metrics: []string{"total_contributors"}})

	var queryErr *QueryFailedError
	if !errors.As(err, &queryErr) {
		t.Fatalf("expected a QueryFailedError, got %v", err)
	}
	if !strings.Contains(queryErr.Message, "Unable to resolve metric") {
		t.Errorf("expected the upstream reason preserved, got %q", queryErr.Message)
	}
}

func TestQueryStopsWhenTheContextIsCancelled(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("CreateQuery", `{"data":{"createQuery":{"queryId":"q-1"}}}`)
	stub.queue("GetQueryResult", `{"data":{"query":{"status":"RUNNING","error":null,"sql":null,"jsonResult":null}}}`)

	client := stub.client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	if _, err := client.Query(ctx, QueryArgs{Metrics: []string{"total_contributors"}}); err == nil {
		t.Fatal("expected the query to stop when the context expired")
	}
}

// pagedResultJSON builds a SUCCESSFUL response holding one row, declaring
// totalPages so the pagination loop has something to follow.
func pagedResultJSON(region string, totalPages int) string {
	return fmt.Sprintf(
		`{"data":{"query":{"status":"SUCCESSFUL","error":null,"sql":"SELECT 1","totalPages":%d,`+
			`"jsonResult":"{\"schema\":{\"fields\":[{\"name\":\"index\",\"type\":\"integer\"},`+
			`{\"name\":\"country__lf_region\",\"type\":\"string\"}],\"primaryKey\":[\"index\"]},`+
			`\"data\":[{\"index\":0,\"country__lf_region\":\"%s\"}]}"}}}`,
		totalPages, region)
}

// TestQueryFetchesEveryResultPage guards against silently truncating a result.
//
// The GraphQL API pages at about 1024 rows, where the Arrow Flight transport
// the Python implementation used streamed the whole result. Before this was
// handled, an unbounded live query came back with exactly 1024 rows and a
// row_count of 1024, indistinguishable from a complete answer.
func TestQueryFetchesEveryResultPage(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("CreateQuery", `{"data":{"createQuery":{"queryId":"q-1"}}}`)
	stub.queue("GetQueryResult", pagedResultJSON("Asia Pacific", 3))
	stub.queue("GetQueryResult", pagedResultJSON("Europe", 3))
	stub.queue("GetQueryResult", pagedResultJSON("North America", 3))

	client := stub.client(t)
	result, err := client.Query(context.Background(), QueryArgs{Metrics: []string{"total_contributors"}})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if result.RowCount != 3 {
		t.Errorf("expected all 3 pages concatenated, got row_count %d", result.RowCount)
	}
	if len(result.Data) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(result.Data))
	}
	for i, want := range []string{"Asia Pacific", "Europe", "North America"} {
		if got := result.Data[i]["country__lf_region"]; got != want {
			t.Errorf("row %d = %v, want %q", i, got, want)
		}
	}
}

// TestQueryRequestsThePageItIsAskingFor checks the pageNum argument is sent,
// since without it the API silently returns page 1 every time.
func TestQueryRequestsThePageItIsAskingFor(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("CreateQuery", `{"data":{"createQuery":{"queryId":"q-1"}}}`)
	stub.queue("GetQueryResult", pagedResultJSON("Asia Pacific", 2))
	stub.queue("GetQueryResult", pagedResultJSON("Europe", 2))

	client := stub.client(t)
	if _, err := client.Query(context.Background(), QueryArgs{Metrics: []string{"total_contributors"}}); err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if got := stub.lastVariables()["pageNum"]; got != float64(2) {
		t.Errorf("expected the second page requested, got pageNum %v", got)
	}
}

// TestQueryToleratesATransientPollFailure: a gateway error mid-poll says
// nothing about the query, which is still running upstream. The Python route
// retried once on transport errors, and dropping that made a 502 discard a
// query that was about to succeed.
func TestQueryToleratesATransientPollFailure(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("CreateQuery", `{"data":{"createQuery":{"queryId":"q-1"}}}`)
	stub.queue("GetQueryResult", `{"errors":[{"message":"502 Bad Gateway"}]}`)
	stub.queue("GetQueryResult", successfulResultJSON)

	client := stub.client(t)
	result, err := client.Query(context.Background(), QueryArgs{Metrics: []string{"total_contributors"}})
	if err != nil {
		t.Fatalf("expected the transient failure absorbed, got %v", err)
	}
	if result.RowCount != 1 {
		t.Errorf("expected 1 row, got %d", result.RowCount)
	}
}

// TestQueryGivesUpAfterRepeatedPollFailures: tolerance is bounded, so a
// genuinely broken endpoint still ends the call.
func TestQueryGivesUpAfterRepeatedPollFailures(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("CreateQuery", `{"data":{"createQuery":{"queryId":"q-1"}}}`)
	stub.queue("GetQueryResult", `{"errors":[{"message":"502 Bad Gateway"}]}`)

	client := stub.client(t)
	_, err := client.Query(context.Background(), QueryArgs{Metrics: []string{"total_contributors"}})
	if err == nil {
		t.Fatal("expected the query to give up on a persistently failing endpoint")
	}
	if !strings.Contains(err.Error(), "502 Bad Gateway") {
		t.Errorf("expected the underlying reason preserved, got %v", err)
	}
	if stub.calls["GetQueryResult"] != maxConsecutivePollFailures {
		t.Errorf("expected %d attempts, got %d", maxConsecutivePollFailures, stub.calls["GetQueryResult"])
	}
}

// A FAILED status is an application error, not a transport one, so it must
// abort immediately rather than consume the transient-failure budget.
func TestQueryDoesNotRetryAnApplicationFailure(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("CreateQuery", `{"data":{"createQuery":{"queryId":"q-1"}}}`)
	stub.queue("GetQueryResult", `{"data":{"query":{"status":"FAILED","error":"Unable to resolve metric","sql":null,"jsonResult":null}}}`)

	client := stub.client(t)
	if _, err := client.Query(context.Background(), QueryArgs{Metrics: []string{"total_contributors"}}); err == nil {
		t.Fatal("expected a failure")
	}
	if stub.calls["GetQueryResult"] != 1 {
		t.Errorf("expected a single poll, got %d", stub.calls["GetQueryResult"])
	}
}

func TestQuerySurfacesGraphQLErrors(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("CreateQuery", `{"errors":[{"message":"Metric 'nope' not found"}]}`)

	client := stub.client(t)
	_, err := client.Query(context.Background(), QueryArgs{Metrics: []string{"nope"}})
	if err == nil || !strings.Contains(err.Error(), "Metric 'nope' not found") {
		t.Fatalf("expected the GraphQL error surfaced, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Dimension values: the gate, the guard, the escape
// ---------------------------------------------------------------------------

const dimensionsForContributors = `{"data":{"dimensionsPaginated":{"items":[
  {"name":"country__lf_region","type":"categorical","description":"Region","label":"Region","queryableGranularities":[]},
  {"name":"country__country_name","type":"categorical","description":"Country","label":"Country","queryableGranularities":[]}
]}}}`

func TestFetchDimensionValuesRejectsAnInjectionShapedName(t *testing.T) {
	stub := newStubServer(t)
	client := stub.client(t)

	_, err := client.FetchDimensionValues(context.Background(),
		"country__lf_region') }} = 'x' OR 1=1 --", []string{"total_contributors"}, "", 100)

	var unknown *UnknownDimensionError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected an UnknownDimensionError, got %v", err)
	}
	if len(stub.requests) != 0 {
		t.Error("expected the name rejected before any request was made")
	}
}

// TestFetchDimensionValuesRejectsAnEmptyMetricList closes the hole in the
// gate. An empty list has nothing disallowed in it, so it passed the allowlist
// check and then asked for dimensions scoped to no metric at all, which the
// live API answers with all 295 dimensions in the environment. The only
// caller rejects an empty list first, but the gate is documented as living
// here, so it has to hold here.
func TestFetchDimensionValuesRejectsAnEmptyMetricList(t *testing.T) {
	for _, metrics := range [][]string{nil, {}, {"", "  "}} {
		stub := newStubServer(t)
		client := stub.client(t)

		_, err := client.FetchDimensionValues(context.Background(),
			"country__lf_region", metrics, "", 100)

		var unknown *UnknownDimensionError
		if !errors.As(err, &unknown) {
			t.Fatalf("metrics %q: expected an UnknownDimensionError, got %v", metrics, err)
		}
		if len(stub.requests) != 0 {
			t.Errorf("metrics %q: expected rejection before any request was made", metrics)
		}
	}
}

func TestFetchDimensionValuesRejectsAMetricOutsideTheAllowlist(t *testing.T) {
	stub := newStubServer(t)
	client := stub.client(t)

	_, err := client.FetchDimensionValues(context.Background(),
		"user__email", []string{"some_internal_metric"}, "", 100)

	var unknown *UnknownDimensionError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected an UnknownDimensionError, got %v", err)
	}
	if len(stub.requests) != 0 {
		t.Error("expected the metric rejected before any request was made")
	}
}

// TestFetchDimensionValuesRejectsADimensionTheMetricDoesNotExpose is the load
// bearing half of the gate: a dimension-only query does not consult the metric
// allowlist, so without this check any dimension in the semantic layer,
// including PII-bearing ones, would be enumerable.
func TestFetchDimensionValuesRejectsADimensionTheMetricDoesNotExpose(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("GetDimensions", dimensionsForContributors)
	client := stub.client(t)

	_, err := client.FetchDimensionValues(context.Background(),
		"user__email", []string{"total_contributors"}, "", 100)

	var unknown *UnknownDimensionError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected an UnknownDimensionError, got %v", err)
	}
	if !strings.Contains(unknown.Message, "get_dimensions") {
		t.Errorf("expected the message to name the way forward, got %q", unknown.Message)
	}
	if stub.calls["CreateQuery"] != 0 {
		t.Error("expected no query to run for a dimension outside the metric")
	}
}

func TestFetchDimensionValuesBuildsAnILIKEFilter(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("GetDimensions", dimensionsForContributors)
	stub.queue("CreateQuery", `{"data":{"createQuery":{"queryId":"q-1"}}}`)
	stub.queue("GetQueryResult", `{"data":{"query":{"status":"SUCCESSFUL","error":null,"sql":"","jsonResult":"{\"schema\":{\"fields\":[{\"name\":\"country__country_name\",\"type\":\"string\"}],\"primaryKey\":[]},\"data\":[{\"country__country_name\":\"Viet Nam\"}]}"}}}`)

	client := stub.client(t)
	values, err := client.FetchDimensionValues(context.Background(),
		"country__country_name", []string{"total_contributors"}, "viet", 100)
	if err != nil {
		t.Fatalf("FetchDimensionValues failed: %v", err)
	}
	if len(values.Values) != 1 || values.Values[0] != "Viet Nam" {
		t.Errorf("expected the ISO spelling returned, got %v", values.Values)
	}

	// The create call carries the filter and no metrics.
	var createVars map[string]any
	for _, req := range stub.requests {
		if query, _ := req["query"].(string); operationOf(query) == "CreateQuery" {
			createVars, _ = req["variables"].(map[string]any)
		}
	}
	where, _ := createVars["where"].([]any)
	if len(where) != 1 {
		t.Fatalf("expected one where clause, got %v", createVars["where"])
	}
	clause, _ := where[0].(map[string]any)["sql"].(string)
	if !strings.Contains(clause, "ILIKE '%viet%'") {
		t.Errorf("expected an ILIKE filter, got %q", clause)
	}
	if metrics, _ := createVars["metrics"].([]any); len(metrics) != 0 {
		t.Errorf("expected a dimension-only query to pass no metrics, got %v", metrics)
	}
}

func TestEscapeSQLLiteralEscapesQuotes(t *testing.T) {
	if got := escapeSQLLiteral("d'Ivoire"); got != "d''Ivoire" {
		t.Errorf("escapeSQLLiteral(d'Ivoire) = %q, want d''Ivoire", got)
	}
	if got := escapeSQLLiteral(`back\slash`); got != `back\\slash` {
		t.Errorf("expected the backslash escaped, got %q", got)
	}
}

func TestDimensionValuesFlagsTruncation(t *testing.T) {
	full := newDimensionValues("country__country_name", []string{"a", "b"}, 2)
	if !full.Truncated {
		t.Error("expected a result at the limit to be flagged truncated")
	}
	partial := newDimensionValues("country__country_name", []string{"a"}, 2)
	if partial.Truncated {
		t.Error("expected a result under the limit not to be flagged truncated")
	}
}

func TestDistinctValuesDropsNullsAndDuplicates(t *testing.T) {
	result := &QueryResult{
		Columns: []string{"country__lf_region"},
		Data: []map[string]any{
			{"country__lf_region": "Europe"},
			{"country__lf_region": nil},
			{"country__lf_region": "Asia Pacific"},
			{"country__lf_region": "Europe"},
		},
	}
	values := distinctValues(result, "country__lf_region")
	if strings.Join(values, ",") != "Asia Pacific,Europe" {
		t.Errorf("expected sorted distinct non-null values, got %v", values)
	}
}

func TestNoDimensionValuesDetailNamesTheISOSpelling(t *testing.T) {
	detail := NoDimensionValuesDetail("country__country_name", "vietnam")
	if !strings.Contains(detail, "Viet Nam") {
		t.Error("expected the message to name the ISO spelling")
	}
	if !strings.Contains(NoDimensionValuesDetail("x", ""), "no non-null values") {
		t.Error("expected a different message when there was no search")
	}
}

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

func TestFetchAllowedMetricsFiltersToTheAllowlist(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("GetMetrics", `{"data":{"metricsPaginated":{"items":[
	  {"name":"total_contributors","label":"Contributors","description":"Contributors","type":"simple"},
	  {"name":"internal_secret_metric","label":"Secret","description":"Not for callers","type":"simple"}
	]}}}`)
	stub.queue("GetMetricsWithRelated", `{"data":{"metricsPaginated":{"items":[
	  {"name":"total_contributors","label":"Contributors","description":"Contributors","type":"simple","dimensions":[{"name":"country__lf_region"}],"entities":[{"name":"country"}]}
	]}}}`)

	client := stub.client(t)
	metrics, err := client.FetchAllowedMetrics(context.Background(), "")
	if err != nil {
		t.Fatalf("FetchAllowedMetrics failed: %v", err)
	}
	if len(metrics) != 1 || metrics[0].Name != "total_contributors" {
		t.Fatalf("expected only the allowlisted metric, got %v", metrics)
	}
	if len(metrics[0].Dimensions) != 1 {
		t.Errorf("expected dimensions inlined for a small result, got %v", metrics[0].Dimensions)
	}
}

// TestFetchAllowedMetricsRetriesPerWord covers the rescue for a plural or
// natural-language search, which the Semantic Layer matches as a single exact
// phrase and so returns nothing for.
func TestFetchAllowedMetricsRetriesPerWord(t *testing.T) {
	stub := newStubServer(t)
	// The phrase search finds nothing.
	stub.queue("GetMetrics", `{"data":{"metricsPaginated":{"items":[]}}}`)
	// The unfiltered retry finds the singular form.
	stub.queue("GetMetrics", `{"data":{"metricsPaginated":{"items":[
	  {"name":"code_contribution_activities","label":"Contributions","description":"Contribution activity","type":"simple"}
	]}}}`)
	stub.queue("GetMetricsWithRelated", `{"data":{"metricsPaginated":{"items":[
	  {"name":"code_contribution_activities","label":"Contributions","description":"Contribution activity","type":"simple","dimensions":[{"name":"country__lf_region"}],"entities":[]}
	]}}}`)

	client := stub.client(t)
	metrics, err := client.FetchAllowedMetrics(context.Background(), "contributions")
	if err != nil {
		t.Fatalf("FetchAllowedMetrics failed: %v", err)
	}
	if len(metrics) != 1 || metrics[0].Name != "code_contribution_activities" {
		t.Fatalf("expected the singular stem to rescue the search, got %v", metrics)
	}
}

func TestFetchAllowedMetricsCachesSearches(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("GetMetrics", `{"data":{"metricsPaginated":{"items":[
	  {"name":"total_contributors","label":"Contributors","description":"Contributors","type":"simple"}
	]}}}`)
	stub.queue("GetMetricsWithRelated", `{"data":{"metricsPaginated":{"items":[
	  {"name":"total_contributors","label":"Contributors","description":"Contributors","type":"simple","dimensions":[],"entities":[]}
	]}}}`)

	client := stub.client(t)
	for range 3 {
		if _, err := client.FetchAllowedMetrics(context.Background(), "contributor"); err != nil {
			t.Fatalf("FetchAllowedMetrics failed: %v", err)
		}
	}
	if stub.calls["GetMetrics"] != 1 {
		t.Errorf("expected the search cached after the first call, got %d calls", stub.calls["GetMetrics"])
	}
}

func TestFetchDimensionsCachesByNormalizedMetricNames(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("GetDimensions", dimensionsForContributors)

	client := stub.client(t)
	first, err := client.FetchDimensions(context.Background(), []string{"total_contributors"})
	if err != nil {
		t.Fatalf("FetchDimensions failed: %v", err)
	}
	// Same metrics, different order and spacing, must hit the same cache entry.
	if _, err := client.FetchDimensions(context.Background(), []string{" total_contributors "}); err != nil {
		t.Fatalf("FetchDimensions failed: %v", err)
	}
	if stub.calls["GetDimensions"] != 1 {
		t.Errorf("expected one upstream call, got %d", stub.calls["GetDimensions"])
	}
	if len(first) != 2 {
		t.Errorf("expected 2 dimensions, got %d", len(first))
	}
}

func TestFetchDimensionsSendsSortedMetricInputs(t *testing.T) {
	stub := newStubServer(t)
	stub.queue("GetDimensions", dimensionsForContributors)

	client := stub.client(t)
	if _, err := client.FetchDimensions(context.Background(), []string{"total_events", "total_contributors"}); err != nil {
		t.Fatalf("FetchDimensions failed: %v", err)
	}

	metrics, _ := stub.lastVariables()["metrics"].([]any)
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metric inputs, got %v", metrics)
	}
	if name, _ := metrics[0].(map[string]any)["name"].(string); name != "total_contributors" {
		t.Errorf("expected metric names sorted, got %v", metrics)
	}
}

// ---------------------------------------------------------------------------
// Client construction and transport
// ---------------------------------------------------------------------------

func TestNewClientValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"missing host", Config{EnvironmentID: "1", Token: "t"}},
		{"missing token", Config{Host: "h", EnvironmentID: "1"}},
		{"missing environment", Config{Host: "h", Token: "t"}},
		{"non-numeric environment", Config{Host: "h", EnvironmentID: "abc", Token: "t"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewClient(tc.cfg); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestNewClientNormalizesTheHost(t *testing.T) {
	client, err := NewClient(Config{Host: "https://example.dbt.com/", EnvironmentID: "42", Token: "t"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client.graphqlURL != "https://example.dbt.com/api/graphql" {
		t.Errorf("unexpected GraphQL URL %q", client.graphqlURL)
	}
}

func TestGraphQLRequestSendsBearerTokenAndEnvironment(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"dimensionsPaginated":{"items":[]}}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{Host: "example.dbt.com", EnvironmentID: "202356", Token: "secret-token"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.graphqlURL = server.URL

	if _, err := client.FetchDimensions(context.Background(), []string{"total_contributors"}); err != nil {
		t.Fatalf("FetchDimensions failed: %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("expected a bearer token, got %q", gotAuth)
	}
}

func TestGraphQLRequestSurfacesNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("token expired"))
	}))
	defer server.Close()

	client, err := NewClient(Config{Host: "example.dbt.com", EnvironmentID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	client.graphqlURL = server.URL

	_, err = client.FetchDimensions(context.Background(), []string{"total_contributors"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected the HTTP status surfaced, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Cache
// ---------------------------------------------------------------------------

func TestTTLCacheExpiresEntries(t *testing.T) {
	cache := newTTLCache[string, int](10*time.Millisecond, 10)
	cache.put("a", 1)

	if _, ok := cache.get("a"); !ok {
		t.Fatal("expected a fresh entry to be present")
	}
	time.Sleep(20 * time.Millisecond)
	if _, ok := cache.get("a"); ok {
		t.Error("expected an expired entry to be dropped")
	}
}

func TestTTLCacheEvictsWhenFull(t *testing.T) {
	cache := newTTLCache[string, int](time.Minute, 2)
	cache.put("a", 1)
	cache.put("b", 2)
	cache.put("c", 3)

	if len(cache.store) != 2 {
		t.Errorf("expected the cache bounded at 2 entries, got %d", len(cache.store))
	}
	if _, ok := cache.get("c"); !ok {
		t.Error("expected the newest entry retained")
	}
}

func TestTTLCacheOverwriteDoesNotEvict(t *testing.T) {
	cache := newTTLCache[string, int](time.Minute, 2)
	cache.put("a", 1)
	cache.put("b", 2)
	cache.put("a", 3)

	if len(cache.store) != 2 {
		t.Errorf("expected 2 entries, got %d", len(cache.store))
	}
	if got, _ := cache.get("a"); got != 3 {
		t.Errorf("expected the overwritten value, got %d", got)
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// TestParseQueryResultKeepsLargeIntegersExact guards against float64 decoding,
// which would render a 4239559 contributor count as 4.239559e+06 by the time
// the model reads it.
func TestParseQueryResultKeepsLargeIntegersExact(t *testing.T) {
	raw := `{"schema":{"fields":[{"name":"total_contributors","type":"integer"}],"primaryKey":[]},
	         "data":[{"total_contributors":4239559}]}`

	result, err := parseQueryResult(raw, "")
	if err != nil {
		t.Fatalf("parseQueryResult failed: %v", err)
	}

	encoded, err := json.Marshal(result.Data[0])
	if err != nil {
		t.Fatalf("failed to re-encode the row: %v", err)
	}
	if !strings.Contains(string(encoded), "4239559") {
		t.Errorf("expected the exact integer, got %s", encoded)
	}
	if strings.Contains(string(encoded), "e+") {
		t.Errorf("expected no scientific notation, got %s", encoded)
	}
}

func TestNewClientHonoursAnExplicitHTTPScheme(t *testing.T) {
	client, err := NewClient(Config{Host: "http://127.0.0.1:9999", EnvironmentID: "1", Token: "t"})
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client.graphqlURL != "http://127.0.0.1:9999/api/graphql" {
		t.Errorf("expected the http scheme preserved, got %q", client.graphqlURL)
	}
}
