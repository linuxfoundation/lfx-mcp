// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/linuxfoundation/lfx-mcp/internal/serviceapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func listKPIsTool(t *testing.T) *mcp.Tool {
	t.Helper()
	return listRegisteredTool(t, "query_lfx_kpis", RegisterKPIs)
}

// today is the reference date the as-of rule is tested against; the handler
// passes time.Now().UTC() in production.
var kpiTestToday = time.Date(2026, 9, 2, 11, 30, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// Description and schema
// ---------------------------------------------------------------------------

// TestKPIsDescription_FitsSchemaBudget holds the tool to the same budget as
// its siblings: everything past the limit is invisible to the model, and this
// description IS the recipe inventory and the parameter contract.
func TestKPIsDescription_FitsSchemaBudget(t *testing.T) {
	if got := len(kpisDescription); got > schemaDescriptionBudget {
		t.Errorf("query_lfx_kpis description is %d bytes; everything past %d is invisible to the model",
			got, schemaDescriptionBudget)
	}
	for _, property := range []string{"saved_query", "foundation", "project", "org", "by", "since", "until", "as_of", "where", "order_by", "limit"} {
		if got := len(schemaPropertyDescription(t, listKPIsTool(t), property)); got > schemaDescriptionBudget {
			t.Errorf("kpis.%s description is %d bytes; limit %d", property, got, schemaDescriptionBudget)
		}
	}
}

// TestKPIsDescription_ListsAdvertisedRecipes pins the routing surface: the
// description is the only place a model learns the names, so every advertised
// recipe must appear. The two per-event/per-course recipes stay callable but
// are deliberately off the routing surface — they are one metric and one name
// dimension, documented as two-line recipes in the semantic layer guidance.
func TestKPIsDescription_ListsAdvertisedRecipes(t *testing.T) {
	for _, name := range []string{
		"kpi_members_and_dues_by_account",
		"kpi_membership_tier_split",
		"kpi_new_members_by_year",
		"kpi_membership_churn",
		"kpi_contributions_by_org",
		"kpi_contributors_by_org",
		"kpi_contributors_by_project",
		"kpi_maintainers_by_org",
		"kpi_event_registrations_by_org",
		"kpi_training_enrollments_by_org",
	} {
		if !strings.Contains(kpisDescription, name) {
			t.Errorf("description does not list recipe %q", name)
		}
	}
	for _, name := range []string{"kpi_event_registrations,", "kpi_training_enrollments,"} {
		if strings.Contains(kpisDescription, name) {
			t.Errorf("description advertises %q, which was dropped from the routing surface", name)
		}
	}
}

// TestKPIsDescription_CarriesTheContract pins the uniform contract: every
// parameter line, the shape rule, and the routing to the guidance tools.
func TestKPIsDescription_CarriesTheContract(t *testing.T) {
	for _, want := range []string{
		"prefer it over explore + query",
		"read_lfx_kpi_guidance",
		"once per session",
		"saved_query",
		"foundation",
		"project",
		"search_b2b_orgs",
		"account | rollup",
		"since, until",
		"as_of",
		"where",
		"order_by",
		"ceiling 500",
		"yyyy-mm-dd",
		"FLOW",
		"SNAPSHOT",
		"error naming the recipe's shape",
		"compiled_sql",
		"read_lfx_deck_building_guidance",
	} {
		if !strings.Contains(kpisDescription, want) {
			t.Errorf("description missing contract fragment %q", want)
		}
	}
}

// TestKPIs_RequiredParamSurvivesCompaction mirrors the compaction contract on
// the other semantic layer tools: only the tool description and REQUIRED
// parameter descriptions reliably reach the model, so the fixed-recipe rule
// and the FLOW/SNAPSHOT split must be stated on saved_query, not only on the
// optional parameters that carry them.
func TestKPIs_RequiredParamSurvivesCompaction(t *testing.T) {
	desc := schemaPropertyDescription(t, listKPIsTool(t), "saved_query")
	for _, want := range []string{"metrics", "group_by", "read_lfx_kpi_guidance", "since/until", "as_of", "FLOW", "SNAPSHOT"} {
		if !strings.Contains(desc, want) {
			t.Errorf("saved_query description does not mention %q — the contract must survive schema compaction", want)
		}
	}
}

func TestKPIs_RegistersReadOnly(t *testing.T) {
	tool := listKPIsTool(t)
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Error("query_lfx_kpis must carry ReadOnlyHint")
	}
	required := schemaRequired(t, tool)
	if !reflect.DeepEqual(required, []string{"saved_query"}) {
		t.Errorf("saved_query must be the only required field, got %v", required)
	}
}

// ---------------------------------------------------------------------------
// Expansion
// ---------------------------------------------------------------------------

func buildOrFail(t *testing.T, args KPIArgs) kpiRequest {
	t.Helper()
	req, rejection := buildKPIRequest(args, kpiTestToday)
	if rejection != "" {
		t.Fatalf("unexpected rejection: %s", rejection)
	}
	return req
}

func TestKPIExpansion_Foundation(t *testing.T) {
	req := buildOrFail(t, KPIArgs{SavedQuery: "kpi_contributions_by_org", Foundation: "cncf"})
	want := []string{"{{ Dimension('project__foundation_slug') }} = 'cncf'"}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("foundation expansion = %v, want %v", req.where, want)
	}
}

func TestKPIExpansion_Project(t *testing.T) {
	req := buildOrFail(t, KPIArgs{SavedQuery: "kpi_contributors_by_project", Project: "k8s"})
	want := []string{"{{ Dimension('project__slug') }} = 'k8s'"}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("project expansion = %v, want %v", req.where, want)
	}
}

func TestKPIExpansion_OrgUsesAccountRollup(t *testing.T) {
	req := buildOrFail(t, KPIArgs{SavedQuery: "kpi_contributions_by_org", Org: "International Business Machines Corporation"})
	want := []string{"{{ Dimension('account__account_rollup_name') }} = 'International Business Machines Corporation'"}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("org expansion = %v, want %v", req.where, want)
	}
}

// Maintainers have no account entity yet, so org filters their own exact
// account name instead of the rollup. The expansion table is what encodes
// that; getting it wrong returns zero rows, not an error.
func TestKPIExpansion_OrgOnMaintainersUsesMaintainerAccountName(t *testing.T) {
	req := buildOrFail(t, KPIArgs{SavedQuery: "kpi_maintainers_by_org", Org: "Red Hat LLC"})
	want := []string{"{{ Dimension('maintainer_key__account_name') }} = 'Red Hat LLC'"}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("maintainer org expansion = %v, want %v", req.where, want)
	}
}

func TestKPIExpansion_SinceUntilOnMetricTime(t *testing.T) {
	req := buildOrFail(t, KPIArgs{SavedQuery: "kpi_contributions_by_org", Since: "2025-01-01", Until: "2025-12-31"})
	want := []string{
		"{{ TimeDimension('metric_time','DAY') }} >= '2025-01-01'",
		"{{ TimeDimension('metric_time','DAY') }} <= '2025-12-31'",
	}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("window expansion = %v, want %v", req.where, want)
	}
}

// Registrations are windowed on the EVENT start date, not the sign-up date,
// so a window means "events in the window".
func TestKPIExpansion_SinceUsesRecipeTimeAxis(t *testing.T) {
	req := buildOrFail(t, KPIArgs{SavedQuery: "kpi_event_registrations_by_org", Since: "2026-01-01"})
	want := []string{"{{ TimeDimension('registration_id__event_start_date','DAY') }} >= '2026-01-01'"}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("event window expansion = %v, want %v", req.where, want)
	}
}

func TestKPIExpansion_AsOfTodayAddsNoFilter(t *testing.T) {
	req := buildOrFail(t, KPIArgs{SavedQuery: "kpi_members_and_dues_by_account", AsOf: "2026-09-02"})
	if len(req.where) != 0 {
		t.Errorf("as_of=today must not add a filter, got %v", req.where)
	}
	if req.savedQuery != "kpi_members_and_dues_by_account" {
		t.Errorf("as_of must not rewrite the recipe name, got %q", req.savedQuery)
	}
}

func TestKPIExpansion_ByRollupPicksTheTwin(t *testing.T) {
	req := buildOrFail(t, KPIArgs{SavedQuery: "kpi_contributors_by_org", By: "rollup"})
	if req.savedQuery != "kpi_contributors_by_org_rollup" {
		t.Errorf("by=rollup must run the twin, got %q", req.savedQuery)
	}
	if !req.rollup {
		t.Error("by=rollup must record that the twin was selected")
	}
}

func TestKPIExpansion_ByAccountKeepsTheRecipe(t *testing.T) {
	req := buildOrFail(t, KPIArgs{SavedQuery: "kpi_contributors_by_org", By: "account"})
	if req.savedQuery != "kpi_contributors_by_org" || req.rollup {
		t.Errorf("by=account must run the recipe itself, got %q (rollup=%v)", req.savedQuery, req.rollup)
	}
}

// A single quote in a literal must not end the literal: doubling it is the
// escape MetricFlow's SQL literals take.
func TestKPIExpansion_EscapesSingleQuotes(t *testing.T) {
	req := buildOrFail(t, KPIArgs{SavedQuery: "kpi_contributions_by_org", Org: "O'Reilly Media, Inc."})
	want := []string{"{{ Dimension('account__account_rollup_name') }} = 'O''Reilly Media, Inc.'"}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("quote escaping = %v, want %v", req.where, want)
	}
}

// Every expansion travels as its own where entry alongside the caller's own
// filter; lens ANDs the list.
func TestKPIExpansion_CombinedParameters(t *testing.T) {
	req := buildOrFail(t, KPIArgs{
		SavedQuery: "kpi_contributions_by_org",
		Foundation: "cncf",
		Project:    "k8s",
		Org:        "International Business Machines Corporation",
		By:         "rollup",
		Since:      "2025-01-01",
		Until:      "2025-12-31",
		Where:      "{{ Dimension('account__account_name') }} = 'Red Hat LLC'",
		OrderBy:    "-code_contribution_activities",
		Limit:      20,
	})
	want := kpiRequest{
		savedQuery: "kpi_contributions_by_org_rollup",
		where: []string{
			"{{ Dimension('project__foundation_slug') }} = 'cncf'",
			"{{ Dimension('project__slug') }} = 'k8s'",
			"{{ Dimension('account__account_rollup_name') }} = 'International Business Machines Corporation'",
			"{{ TimeDimension('metric_time','DAY') }} >= '2025-01-01'",
			"{{ TimeDimension('metric_time','DAY') }} <= '2025-12-31'",
			"{{ Dimension('account__account_name') }} = 'Red Hat LLC'",
		},
		orderBy: []string{"-code_contribution_activities"},
		limit:   20,
		rollup:  true,
	}
	if !reflect.DeepEqual(req, want) {
		t.Errorf("combined expansion =\n%#v\nwant\n%#v", req, want)
	}
}

// A recipe deployed in lf-dbt before this table learns about it must still
// honour every parameter: FLOW on metric_time, org on the account rollup.
func TestKPIExpansion_UnknownRecipeDefaultsToFlow(t *testing.T) {
	recipe := kpiRecipeFor("kpi_something_not_deployed_here")
	if recipe.shape != kpiFlow || recipe.timeAxis != "metric_time" || recipe.orgDimension != "account__account_rollup_name" {
		t.Errorf("unknown recipe defaults = %+v; want FLOW on metric_time with the account rollup", recipe)
	}
	req := buildOrFail(t, KPIArgs{SavedQuery: "kpi_something_not_deployed_here", Since: "2025-01-01", Org: "Acme Corp"})
	want := []string{
		"{{ Dimension('account__account_rollup_name') }} = 'Acme Corp'",
		"{{ TimeDimension('metric_time','DAY') }} >= '2025-01-01'",
	}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("unknown recipe expansion = %v, want %v", req.where, want)
	}
}

// ---------------------------------------------------------------------------
// Rejections
// ---------------------------------------------------------------------------

func TestKPIRejections(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  KPIArgs
		wants []string
	}{
		{
			name:  "missing saved_query",
			args:  KPIArgs{},
			wants: []string{"saved_query is required"},
		},
		{
			name:  "limit over the ceiling",
			args:  KPIArgs{SavedQuery: "kpi_contributions_by_org", Limit: 501},
			wants: []string{"limit must be 500 or less"},
		},
		{
			name:  "since on a SNAPSHOT recipe",
			args:  KPIArgs{SavedQuery: "kpi_members_and_dues_by_account", Since: "2025-01-01"},
			wants: []string{"kpi_members_and_dues_by_account", "SNAPSHOT", "since/until do not apply", "as_of"},
		},
		{
			name:  "until on a SNAPSHOT recipe",
			args:  KPIArgs{SavedQuery: "kpi_maintainers_by_org", Until: "2025-12-31"},
			wants: []string{"kpi_maintainers_by_org", "SNAPSHOT", "since/until do not apply"},
		},
		{
			name:  "as_of on a FLOW recipe",
			args:  KPIArgs{SavedQuery: "kpi_contributions_by_org", AsOf: "2026-09-02"},
			wants: []string{"kpi_contributions_by_org", "FLOW", "metric_time", "as_of does not apply", "since/until"},
		},
		{
			name:  "as_of before today on a SNAPSHOT recipe",
			args:  KPIArgs{SavedQuery: "kpi_maintainers_by_org", AsOf: "2025-06-30"},
			wants: []string{"SNAPSHOT", "as-of history is not deployed yet", "one call per period once it is", "2026-09-02"},
		},
		{
			name:  "invalid since",
			args:  KPIArgs{SavedQuery: "kpi_contributions_by_org", Since: "01/01/2025"},
			wants: []string{"since must be a yyyy-mm-dd date"},
		},
		{
			name:  "invalid until",
			args:  KPIArgs{SavedQuery: "kpi_contributions_by_org", Until: "last friday"},
			wants: []string{"until must be a yyyy-mm-dd date"},
		},
		{
			name:  "invalid as_of",
			args:  KPIArgs{SavedQuery: "kpi_maintainers_by_org", AsOf: "2026-13-45"},
			wants: []string{"as_of must be a yyyy-mm-dd date"},
		},
		{
			name:  "unknown by",
			args:  KPIArgs{SavedQuery: "kpi_contributions_by_org", By: "parent"},
			wants: []string{"by must be account or rollup", "parent"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, rejection := buildKPIRequest(tc.args, kpiTestToday)
			if rejection == "" {
				t.Fatalf("expected a rejection, got request %+v", req)
			}
			for _, want := range tc.wants {
				if !strings.Contains(rejection, want) {
					t.Errorf("rejection %q missing %q", rejection, want)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

func TestKPIs_SendsExpansionsAsSeparateWhereEntries(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{
		SavedQuery: "kpi_contributions_by_org",
		Foundation: "cncf",
		Where:      "{{ Dimension('account__account_name') }} = 'Red Hat LLC'",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	if captured.Method != http.MethodPost || captured.Path != "/lfx-lens/semantic-layer/saved-query" {
		t.Errorf("unexpected request: %s %s", captured.Method, captured.Path)
	}

	var body struct {
		SavedQuery string   `json:"saved_query"`
		Where      []string `json:"where"`
		Limit      int      `json:"limit"`
	}
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("failed to parse captured body: %v", err)
	}
	if body.SavedQuery != "kpi_contributions_by_org" || body.Limit != 10 {
		t.Errorf("unexpected payload: %+v", body)
	}
	want := []string{
		"{{ Dimension('project__foundation_slug') }} = 'cncf'",
		"{{ Dimension('account__account_name') }} = 'Red Hat LLC'",
	}
	if !reflect.DeepEqual(body.Where, want) {
		t.Errorf("where = %v, want %v", body.Where, want)
	}
}

func TestKPIs_RejectionDoesNotCallLens(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{
		SavedQuery: "kpi_members_and_dues_by_account",
		Since:      "2025-01-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	if captured.Method != "" {
		t.Errorf("rejected calls must not reach lens, got %s %s", captured.Method, captured.Path)
	}
}

// setupLensErrorTest points lensConfig at a stub lens API that always fails
// with the given status and body.
func setupLensErrorTest(t *testing.T, status int, body string) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	client, err := serviceapi.NewClient(serviceapi.Config{
		BaseURL:     srv.URL,
		TokenSource: stubTokenSource{},
	})
	if err != nil {
		t.Fatalf("failed to create service API client: %v", err)
	}
	prev := lensConfig
	SetLensConfig(&LensConfig{ServiceClient: client})
	t.Cleanup(func() { lensConfig = prev })
}

// A missing '<name>_rollup' twin is the deployment state, not a caller
// mistake: it becomes the by=account instruction, and never an invitation to
// sum account rows into a parent headcount.
func TestKPIs_TranslatesMissingRollupTwin(t *testing.T) {
	setupLensErrorTest(t, http.StatusBadRequest, `{"detail":"saved query kpi_contributors_by_org_rollup does not exist"}`)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{
		SavedQuery: "kpi_contributors_by_org",
		By:         "rollup",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	text := resultText(t, res)
	for _, want := range []string{"rollup grain for this recipe is not deployed yet", "use by=account", "never sum rows"} {
		if !strings.Contains(text, want) {
			t.Errorf("translated error %q missing %q", text, want)
		}
	}
}

// Only the missing-twin case is translated: a genuine query failure must
// reach the caller verbatim, or the model corrects the wrong thing.
func TestKPIs_PassesOtherLensErrorsThrough(t *testing.T) {
	setupLensErrorTest(t, http.StatusInternalServerError, `{"detail":"semantic layer timeout"}`)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{
		SavedQuery: "kpi_contributors_by_org",
		By:         "rollup",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	text := resultText(t, res)
	if !strings.Contains(text, "semantic layer timeout") || strings.Contains(text, "not deployed yet") {
		t.Errorf("non-existence errors must pass through verbatim, got %q", text)
	}
}

// An unknown recipe name (by=account) is the dbt side not having deployed it;
// the server's own error is the signal, returned verbatim.
func TestKPIs_UnknownRecipeErrorPassesThrough(t *testing.T) {
	setupLensErrorTest(t, http.StatusBadRequest, `{"detail":"saved query kpi_not_real does not exist"}`)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{SavedQuery: "kpi_not_real"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	if text := resultText(t, res); !strings.Contains(text, "kpi_not_real does not exist") {
		t.Errorf("unknown recipe error must pass through verbatim, got %q", text)
	}
}
