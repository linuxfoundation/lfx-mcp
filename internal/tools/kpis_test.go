// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
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

// kpisDescriptionCeiling is tighter than the hard schema budget: this
// description is read on every tools/list alongside ten siblings, so it is
// held to the space the contract actually needs, with headroom for the next
// parameter rather than for more prose.
const kpisDescriptionCeiling = 1500

// kpiParameters is the whole contract; anything else is not a parameter of
// this tool.
var kpiParameters = []string{
	"kpi", "project", "subprojects", "org", "subsidiaries",
	"since", "until", "as_of", "where", "order_by", "limit",
}

// TestKPIsDescription_FitsSchemaBudget holds the tool to the same budget as
// its siblings, and to the tighter ceiling above: everything past the limit is
// invisible to the model, and this description IS the parameter contract.
func TestKPIsDescription_FitsSchemaBudget(t *testing.T) {
	if got := len(kpisDescription); got > schemaDescriptionBudget {
		t.Errorf("query_lfx_kpis description is %d bytes; everything past %d is invisible to the model",
			got, schemaDescriptionBudget)
	}
	if got := len(kpisDescription); got > kpisDescriptionCeiling {
		t.Errorf("query_lfx_kpis description is %d bytes; the ceiling is %d - tighten the prose, keep the parameter lines",
			got, kpisDescriptionCeiling)
	}
	for _, property := range kpiParameters {
		if got := len(schemaPropertyDescription(t, listKPIsTool(t), property)); got > schemaDescriptionBudget {
			t.Errorf("kpis.%s description is %d bytes; limit %d", property, got, schemaDescriptionBudget)
		}
	}
}

// TestKPIsDescription_CarriesTheContract pins the contract the description is
// the only carrier of: every parameter line, both switch defaults, the
// resolve-first rule, the shape rule, and the routing to the guidance tools.
func TestKPIsDescription_CarriesTheContract(t *testing.T) {
	for _, want := range []string{
		"prefer it over explore + query",
		"read_lfx_kpi_guidance",
		"ALWAYS resolve names first",
		"search_projects",
		"search_b2b_orgs",
		"never pass one that has not come back from them",
		"kpi ",
		"project ",
		"subprojects",
		"Default separate",
		"subsidiaries",
		"Default none",
		"since, until",
		"as_of",
		"where",
		"order_by",
		"1..500",
		"yyyy-mm-dd",
		"FLOW",
		"SNAPSHOT",
		"naming the rule and the fix",
		"compiled_sql",
		"read_lfx_deck_building_guidance",
	} {
		if !strings.Contains(kpisDescription, want) {
			t.Errorf("description missing contract fragment %q", want)
		}
	}
}

// TestKPIsDescription_NamesNoUndeployedRecipe pins the deployed-only rule: any
// dbt saved query the routing surface names must exist in the deployed
// manifest, and the surface may not carry the parameters of the earlier
// contract.
func TestKPIsDescription_NamesNoUndeployedRecipe(t *testing.T) {
	surface := kpisDescription + "\n" + schemaPropertyDescription(t, listKPIsTool(t), "kpi")

	deployed := make(map[string]bool, len(kpiRecipes))
	for _, recipe := range kpiRecipes {
		deployed[recipe.savedQuery] = true
	}
	for _, named := range regexp.MustCompile(`\bkpi_[a-z0-9_]+`).FindAllString(surface, -1) {
		if !deployed[named] {
			t.Errorf("routing surface names the saved query %q, which is not deployed", named)
		}
	}

	for _, gone := range []string{"_rollup", "saved_query", "foundation ", "by=rollup"} {
		if strings.Contains(surface, gone) {
			t.Errorf("routing surface carries %q, which is not part of the contract", gone)
		}
	}
	for _, field := range []string{"SavedQuery", "Foundation", "By"} {
		if _, ok := reflect.TypeOf(KPIArgs{}).FieldByName(field); ok {
			t.Errorf("KPIArgs still carries %s; it is not part of the contract", field)
		}
	}
}

// TestKPIs_RequiredParamSurvivesCompaction mirrors the compaction contract on
// the other semantic layer tools: only the tool description and REQUIRED
// parameter descriptions reliably reach the model, so the inventory of KPI
// names, the fixed-recipe rule and the FLOW/SNAPSHOT split must be stated on
// kpi, not only on the optional parameters that carry them.
func TestKPIs_RequiredParamSurvivesCompaction(t *testing.T) {
	desc := schemaPropertyDescription(t, listKPIsTool(t), "kpi")
	for _, want := range append([]string{
		"metrics", "group_by", "read_lfx_kpi_guidance", "since/until", "as_of", "FLOW", "SNAPSHOT",
	}, kpiNames...) {
		if !strings.Contains(desc, want) {
			t.Errorf("kpi description does not mention %q — the contract must survive schema compaction", want)
		}
	}
}

func TestKPIs_RegistersReadOnly(t *testing.T) {
	tool := listKPIsTool(t)
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Error("query_lfx_kpis must carry ReadOnlyHint")
	}
	required := schemaRequired(t, tool)
	if !reflect.DeepEqual(required, []string{"kpi"}) {
		t.Errorf("kpi must be the only required field, got %v", required)
	}
}

// ---------------------------------------------------------------------------
// Names
// ---------------------------------------------------------------------------

// Every advertised name resolves to a deployed dbt saved query, and the two
// unadvertised recipes stay callable by their dbt name.
func TestKPINames_ResolveToDeployedSavedQueries(t *testing.T) {
	want := map[string]string{
		"members_and_dues_by_org":     "kpi_members_and_dues_by_account",
		"membership_tiers":            "kpi_membership_tier_split",
		"new_members_by_year":         "kpi_new_members_by_year",
		"membership_churn_by_year":    "kpi_membership_churn",
		"contributions_by_org":        "kpi_contributions_by_org",
		"contributors_by_org":         "kpi_contributors_by_org",
		"contributors_by_project":     "kpi_contributors_by_project",
		"maintainers_by_org":          "kpi_maintainers_by_org",
		"event_registrations_by_org":  "kpi_event_registrations_by_org",
		"training_enrollments_by_org": "kpi_training_enrollments_by_org",
		// Not advertised, still callable by their dbt name.
		"kpi_event_registrations":  "kpi_event_registrations",
		"kpi_training_enrollments": "kpi_training_enrollments",
	}
	for name, savedQuery := range want {
		req := buildOrFail(t, KPIArgs{KPI: name})
		if req.savedQuery != savedQuery {
			t.Errorf("kpi %q resolved to saved query %q, want %q", name, req.savedQuery, savedQuery)
		}
	}
	if len(kpiNames) != 10 {
		t.Errorf("advertised inventory is %d names, want the 10 in the contract", len(kpiNames))
	}
	for _, name := range kpiNames {
		if _, ok := want[name]; !ok {
			t.Errorf("advertised name %q is not in the deployed mapping", name)
		}
	}
}

// The dbt name of an advertised KPI keeps working: callers that learned it
// from the manifest or an earlier contract are not broken.
func TestKPINames_AcceptTheDbtName(t *testing.T) {
	req := buildOrFail(t, KPIArgs{KPI: "kpi_contributors_by_org", Org: "Red Hat LLC"})
	if req.savedQuery != "kpi_contributors_by_org" {
		t.Errorf("saved query = %q", req.savedQuery)
	}
	want := []string{"{{ Dimension('account__account_name') }} = 'Red Hat LLC'"}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("where = %v, want %v", req.where, want)
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

// The default for subprojects is separate: a project name means the project
// and everything under it.
func TestKPIExpansion_ProjectDefaultsToSubprojectsSeparate(t *testing.T) {
	want := []string{"({{ Dimension('project__slug') }} = 'cncf' OR {{ Dimension('project__foundation_slug') }} = 'cncf' OR {{ Dimension('project__parent_project_slug') }} = 'cncf')"}
	for _, subprojects := range []string{"", "separate", "combined", "SEPARATE"} {
		req := buildOrFail(t, KPIArgs{KPI: "contributions_by_org", Project: "cncf", Subprojects: subprojects})
		if !reflect.DeepEqual(req.where, want) {
			t.Errorf("subprojects=%q expansion = %v, want %v", subprojects, req.where, want)
		}
	}
}

func TestKPIExpansion_ProjectSubprojectsNone(t *testing.T) {
	req := buildOrFail(t, KPIArgs{KPI: "contributors_by_project", Project: "k8s", Subprojects: "none"})
	want := []string{"{{ Dimension('project__slug') }} = 'k8s'"}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("subprojects=none expansion = %v, want %v", req.where, want)
	}
	if len(req.dropGroupBys) != 0 {
		t.Errorf("subprojects=none must drop nothing, got %v", req.dropGroupBys)
	}
}

// The default for subsidiaries is none: an organization name means that
// company, not the group.
func TestKPIExpansion_OrgDefaultsToSubsidiariesNone(t *testing.T) {
	want := []string{"{{ Dimension('account__account_name') }} = 'Red Hat LLC'"}
	for _, subsidiaries := range []string{"", "none", "NONE"} {
		req := buildOrFail(t, KPIArgs{KPI: "contributions_by_org", Org: "Red Hat LLC", Subsidiaries: subsidiaries})
		if !reflect.DeepEqual(req.where, want) {
			t.Errorf("subsidiaries=%q expansion = %v, want %v", subsidiaries, req.where, want)
		}
		if len(req.dropGroupBys) != 0 {
			t.Errorf("subsidiaries=%q must drop nothing, got %v", subsidiaries, req.dropGroupBys)
		}
	}
}

// Widening to subsidiaries needs BOTH organization columns: the named
// account's own row carries its parent's name in the rollup column, and its
// subsidiaries carry the named account there.
func TestKPIExpansion_OrgSubsidiariesSeparate(t *testing.T) {
	req := buildOrFail(t, KPIArgs{KPI: "contributions_by_org", Org: "International Business Machines Corporation", Subsidiaries: "separate"})
	want := []string{"({{ Dimension('account__account_name') }} = 'International Business Machines Corporation' OR {{ Dimension('account__account_rollup_name') }} = 'International Business Machines Corporation')"}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("subsidiaries=separate expansion = %v, want %v", req.where, want)
	}
	if len(req.dropGroupBys) != 0 {
		t.Errorf("subsidiaries=separate keeps one row per account, got drops %v", req.dropGroupBys)
	}
}

// maintainers_by_org has no account entity, so its single-account filter runs
// on the maintainer's own employer column.
func TestKPIExpansion_OrgOnMaintainersUsesTheLocalAccountName(t *testing.T) {
	req := buildOrFail(t, KPIArgs{KPI: "maintainers_by_org", Org: "Red Hat LLC"})
	want := []string{"{{ Dimension('maintainer_key__account_name') }} = 'Red Hat LLC'"}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("org expansion on maintainers = %v, want %v", req.where, want)
	}
}

func TestKPIExpansion_SinceUntilOnMetricTime(t *testing.T) {
	req := buildOrFail(t, KPIArgs{KPI: "contributions_by_org", Since: "2025-01-01", Until: "2025-12-31"})
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
func TestKPIExpansion_SinceUsesTheKPITimeAxis(t *testing.T) {
	for _, name := range []string{"event_registrations_by_org", "kpi_event_registrations"} {
		req := buildOrFail(t, KPIArgs{KPI: name, Since: "2026-01-01"})
		want := []string{"{{ TimeDimension('registration_id__event_start_date','DAY') }} >= '2026-01-01'"}
		if !reflect.DeepEqual(req.where, want) {
			t.Errorf("%s window expansion = %v, want %v", name, req.where, want)
		}
	}
}

func TestKPIExpansion_AsOfTodayAddsNoFilter(t *testing.T) {
	req := buildOrFail(t, KPIArgs{KPI: "members_and_dues_by_org", AsOf: "2026-09-02"})
	if len(req.where) != 0 {
		t.Errorf("as_of=today must not add a filter, got %v", req.where)
	}
	if req.savedQuery != "kpi_members_and_dues_by_account" {
		t.Errorf("as_of must not rewrite the KPI, got %q", req.savedQuery)
	}
}

// A single quote in a literal must not end the literal: doubling it is the
// escape MetricFlow's SQL literals take.
func TestKPIExpansion_EscapesSingleQuotes(t *testing.T) {
	req := buildOrFail(t, KPIArgs{KPI: "contributions_by_org", Org: "O'Reilly Media, Inc."})
	want := []string{"{{ Dimension('account__account_name') }} = 'O''Reilly Media, Inc.'"}
	if !reflect.DeepEqual(req.where, want) {
		t.Errorf("quote escaping = %v, want %v", req.where, want)
	}
}

// Every expansion travels as its own where entry alongside the caller's own
// filter; lens ANDs the list.
func TestKPIExpansion_CombinedParameters(t *testing.T) {
	req := buildOrFail(t, KPIArgs{
		KPI:          "contributions_by_org",
		Project:      "cncf",
		Subprojects:  "none",
		Org:          "International Business Machines Corporation",
		Subsidiaries: "separate",
		Since:        "2025-01-01",
		Until:        "2025-12-31",
		Where:        "{{ Dimension('activity_project_id__is_org_contribution') }} = true",
		OrderBy:      "-code_contribution_activities",
		Limit:        20,
	})
	want := kpiRequest{
		savedQuery: "kpi_contributions_by_org",
		where: []string{
			"{{ Dimension('project__slug') }} = 'cncf'",
			"({{ Dimension('account__account_name') }} = 'International Business Machines Corporation' OR {{ Dimension('account__account_rollup_name') }} = 'International Business Machines Corporation')",
			"{{ TimeDimension('metric_time','DAY') }} >= '2025-01-01'",
			"{{ TimeDimension('metric_time','DAY') }} <= '2025-12-31'",
			"{{ Dimension('activity_project_id__is_org_contribution') }} = true",
		},
		orderBy: []string{"-code_contribution_activities"},
		limit:   20,
	}
	if !reflect.DeepEqual(req, want) {
		t.Errorf("combined expansion =\n%#v\nwant\n%#v", req, want)
	}
}

// ---------------------------------------------------------------------------
// Combined switches: dropped group-bys
// ---------------------------------------------------------------------------

func TestKPIDropGroupBys(t *testing.T) {
	for _, tc := range []struct {
		name string
		args KPIArgs
		want []string
	}{
		{
			// With org, both organization columns go: the folded row is
			// that organization with its subsidiaries.
			name: "subsidiaries combined with org",
			args: KPIArgs{KPI: "contributions_by_org", Org: "International Business Machines Corporation", Subsidiaries: "combined"},
			want: []string{"account__account_name", "account__account_rollup_name"},
		},
		{
			// Without org, only the account column goes, which leaves one
			// row per parent organization.
			name: "subsidiaries combined without org",
			args: KPIArgs{KPI: "contributors_by_org", Subsidiaries: "combined"},
			want: []string{"account__account_name"},
		},
		{
			// The recipe does not group by an account column at all, so
			// there is nothing to drop.
			name: "subsidiaries combined on a KPI with no account grouping",
			args: KPIArgs{KPI: "membership_tiers", Org: "Red Hat LLC", Subsidiaries: "combined"},
			want: nil,
		},
		{
			// The only per-project KPI: combined folds its four project
			// columns into one row.
			name: "subprojects combined on the per-project KPI",
			args: KPIArgs{KPI: "contributors_by_project", Project: "cncf", Subprojects: "combined"},
			want: []string{"project__foundation_slug", "project__foundation_name", "project__slug", "project__name"},
		},
		{
			// Rows are already one per organization: combined is the
			// filter alone.
			name: "subprojects combined elsewhere changes nothing",
			args: KPIArgs{KPI: "event_registrations_by_org", Project: "cncf", Subprojects: "combined"},
			want: nil,
		},
		{
			name: "both switches combined",
			args: KPIArgs{KPI: "contributors_by_project", Project: "cncf", Subprojects: "combined", Org: "Red Hat LLC", Subsidiaries: "combined"},
			// The per-project recipe groups by project columns only, so
			// the organization drops have nothing to remove.
			want: []string{"project__foundation_slug", "project__foundation_name", "project__slug", "project__name"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := buildOrFail(t, tc.args)
			if !reflect.DeepEqual(req.dropGroupBys, tc.want) {
				t.Errorf("drop_group_bys = %v, want %v", req.dropGroupBys, tc.want)
			}
		})
	}
}

// Every name a combined switch sends must be a group-by the deployed recipe
// actually has: lens removes named columns from the recipe's own definition,
// and a name that is not there is a request it cannot honour.
func TestKPIDropGroupBys_AreAlwaysRecipeColumns(t *testing.T) {
	for name := range kpiRecipes {
		recipe, ok := kpiRecipeFor(name)
		if !ok {
			t.Fatalf("kpi %q does not resolve", name)
		}
		args := KPIArgs{KPI: name, Project: "cncf", Subprojects: "combined"}
		if recipe.parentDimension != kpiNoParentLens {
			args.Org = "Red Hat LLC"
			args.Subsidiaries = "combined"
		}
		req := buildOrFail(t, args)
		for _, drop := range req.dropGroupBys {
			if !recipe.groupsBy(drop) {
				t.Errorf("kpi %q asks lens to drop %q, which it does not group by", name, drop)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// order_by
// ---------------------------------------------------------------------------

func TestKPIOrderBy_MapsClientColumnsBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		args KPIArgs
		want []string
	}{
		{
			name: "account and parent_org",
			args: KPIArgs{KPI: "contributions_by_org", OrderBy: "account,-parent_org"},
			want: []string{"account__account_name", "-account__account_rollup_name"},
		},
		{
			name: "project columns",
			args: KPIArgs{KPI: "contributors_by_project", OrderBy: "foundation,project,-project_name,foundation_name"},
			want: []string{"project__foundation_slug", "project__slug", "-project__name", "project__foundation_name"},
		},
		{
			name: "metrics pass through",
			args: KPIArgs{KPI: "contributors_by_org", OrderBy: "-total_contributors"},
			want: []string{"-total_contributors"},
		},
		{
			// A caller that already knows the qualified name keeps working.
			name: "qualified names pass through",
			args: KPIArgs{KPI: "contributions_by_org", OrderBy: "-account__account_rollup_name"},
			want: []string{"-account__account_rollup_name"},
		},
		{
			// A column the combined switch dropped is not in the result,
			// so it is left alone and lens says so.
			name: "dropped column is not mapped",
			args: KPIArgs{KPI: "contributors_by_org", Subsidiaries: "combined", OrderBy: "account"},
			want: []string{"account"},
		},
		{
			// maintainers has no account entity, so its employer column
			// is neither renamed in the result nor mapped here.
			name: "maintainers employer column",
			args: KPIArgs{KPI: "maintainers_by_org", OrderBy: "-maintainer_key__account_name"},
			want: []string{"-maintainer_key__account_name"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := buildOrFail(t, tc.args)
			if !reflect.DeepEqual(req.orderBy, tc.want) {
				t.Errorf("order_by = %v, want %v", req.orderBy, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Result vocabulary
// ---------------------------------------------------------------------------

func TestKPIRelabelJSON_RenamesResultColumns(t *testing.T) {
	body := []byte(`{"data":[{"account__account_name":"Red Hat LLC","account__account_rollup_name":"International Business Machines Corporation","code_contribution_activities":42}],"columns":["account__account_name","account__account_rollup_name","code_contribution_activities"]}`)
	got := string(kpiRelabelJSON(body))
	for _, want := range []string{
		`"account":"Red Hat LLC"`,
		`"parent_org":"International Business Machines Corporation"`,
		`"code_contribution_activities":42`,
		`["account","parent_org","code_contribution_activities"]`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("relabelled body %s missing %s", got, want)
		}
	}
	if strings.Contains(got, "account__") {
		t.Errorf("qualified column names survived relabelling: %s", got)
	}
}

func TestKPIRelabelJSON_RenamesProjectColumns(t *testing.T) {
	body := []byte(`{"data":[{"project__foundation_slug":"cncf","project__foundation_name":"CNCF","project__slug":"k8s","project__name":"Kubernetes","project__parent_project_slug":"cncf","total_contributors":7}]}`)
	got := string(kpiRelabelJSON(body))
	for _, want := range []string{
		`"foundation":"cncf"`,
		`"foundation_name":"CNCF"`,
		`"project":"k8s"`,
		`"project_name":"Kubernetes"`,
		// No explicit label, so the entity prefix is stripped.
		`"parent_project_slug":"cncf"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("relabelled body %s missing %s", got, want)
		}
	}
}

// An ambiguous label hides which column a value came from, so both columns
// keep the names they arrived with.
func TestKPIRelabelJSON_LeavesAmbiguousColumnsAlone(t *testing.T) {
	body := []byte(`{"data":[{"account__account_name":"Red Hat LLC","account":"something else"}]}`)
	got := string(kpiRelabelJSON(body))
	if !strings.Contains(got, `"account__account_name":"Red Hat LLC"`) || !strings.Contains(got, `"account":"something else"`) {
		t.Errorf("ambiguous columns must be left as they arrived, got %s", got)
	}
}

// compiled_sql is provenance: it is passed through character for character,
// comparison operators included.
func TestKPIRelabelJSON_PassesCompiledSQLThrough(t *testing.T) {
	sql := "SELECT account__account_name, SUM(x) FROM t WHERE d >= '2025-01-01' AND n < 5"
	body, err := json.Marshal(map[string]any{"compiled_sql": sql, "data": []any{}})
	if err != nil {
		t.Fatalf("failed to build body: %v", err)
	}
	var got struct {
		CompiledSQL string `json:"compiled_sql"`
	}
	if err := json.Unmarshal(kpiRelabelJSON(body), &got); err != nil {
		t.Fatalf("relabelled body is not JSON: %v", err)
	}
	if got.CompiledSQL != sql {
		t.Errorf("compiled_sql = %q, want %q", got.CompiledSQL, sql)
	}
	if !strings.Contains(string(kpiRelabelJSON(body)), ">=") {
		t.Error("compiled_sql operators must stay readable, not HTML-escaped")
	}
}

// Key order is part of how a result reads; relabelling must not shuffle it,
// and a body this cannot rewrite comes back exactly as it arrived.
func TestKPIRelabelJSON_KeepsOrderAndPassesNonJSONThrough(t *testing.T) {
	body := []byte(`{"project__slug":"k8s","total_contributors":3,"z":1}`)
	if got := string(kpiRelabelJSON(body)); got != `{"project":"k8s","total_contributors":3,"z":1}` {
		t.Errorf("relabelled body = %s", got)
	}
	notJSON := []byte("upstream returned prose")
	if got := kpiRelabelJSON(notJSON); !reflect.DeepEqual(got, notJSON) {
		t.Errorf("non-JSON body = %q, want it passed through", got)
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
			name:  "missing kpi",
			args:  KPIArgs{},
			wants: []string{"kpi is required", "members_and_dues_by_org", "training_enrollments_by_org"},
		},
		{
			name:  "unknown kpi",
			args:  KPIArgs{KPI: "contributors_by_country"},
			wants: []string{`unknown kpi "contributors_by_country"`, "Valid names", "contributors_by_org", "read_lfx_kpi_guidance"},
		},
		{
			name:  "limit over the ceiling",
			args:  KPIArgs{KPI: "contributions_by_org", Limit: 501},
			wants: []string{"limit must be between 1 and 500", "501"},
		},
		{
			// A negative limit is not "no limit": passed through it would
			// reach the semantic layer as a nonsense bound.
			name:  "negative limit",
			args:  KPIArgs{KPI: "contributions_by_org", Limit: -1},
			wants: []string{"limit must be between 1 and 500", "-1", "Omit it entirely"},
		},
		{
			name:  "unknown subprojects value",
			args:  KPIArgs{KPI: "contributions_by_org", Project: "cncf", Subprojects: "all"},
			wants: []string{"subprojects must be none, separate or combined", `"all"`, "default (separate)"},
		},
		{
			name:  "unknown subsidiaries value",
			args:  KPIArgs{KPI: "contributions_by_org", Org: "Red Hat LLC", Subsidiaries: "yes"},
			wants: []string{"subsidiaries must be none, separate or combined", `"yes"`, "default (none)"},
		},
		{
			name: "subsidiaries on a KPI with no parent lens",
			args: KPIArgs{KPI: "maintainers_by_org", Org: "Red Hat LLC", Subsidiaries: "combined"},
			wants: []string{
				"maintainers_by_org",
				"no parent-company lens yet",
				"subsidiaries must be none",
				"maintainer_key__account_name",
				"per-account, not per parent",
			},
		},
		{
			name: "subsidiaries separate on a KPI with no parent lens",
			args: KPIArgs{KPI: "maintainers_by_org", Subsidiaries: "separate"},
			wants: []string{
				"no parent-company lens yet",
				"subsidiaries must be none",
			},
		},
		{
			name:  "since on a SNAPSHOT KPI",
			args:  KPIArgs{KPI: "members_and_dues_by_org", Since: "2025-01-01"},
			wants: []string{"members_and_dues_by_org", "SNAPSHOT", "since/until do not apply", "as_of"},
		},
		{
			name:  "until on a SNAPSHOT KPI",
			args:  KPIArgs{KPI: "maintainers_by_org", Until: "2025-12-31"},
			wants: []string{"maintainers_by_org", "SNAPSHOT", "since/until do not apply"},
		},
		{
			name:  "as_of on a FLOW KPI",
			args:  KPIArgs{KPI: "contributions_by_org", AsOf: "2026-09-02"},
			wants: []string{"contributions_by_org", "FLOW", "metric_time", "as_of does not apply", "since/until"},
		},
		{
			name:  "as_of before today on a SNAPSHOT KPI",
			args:  KPIArgs{KPI: "maintainers_by_org", AsOf: "2025-06-30"},
			wants: []string{"SNAPSHOT", "as-of history is not deployed yet", "one call per period once it is", "2026-09-02"},
		},
		{
			name:  "invalid since",
			args:  KPIArgs{KPI: "contributions_by_org", Since: "01/01/2025"},
			wants: []string{"since must be a yyyy-mm-dd date"},
		},
		{
			name:  "invalid until",
			args:  KPIArgs{KPI: "contributions_by_org", Until: "last friday"},
			wants: []string{"until must be a yyyy-mm-dd date"},
		},
		{
			name:  "invalid as_of",
			args:  KPIArgs{KPI: "maintainers_by_org", AsOf: "2026-13-45"},
			wants: []string{"as_of must be a yyyy-mm-dd date"},
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

// The unknown-name error stays in the client vocabulary: dbt saved-query names
// are an implementation detail a caller is never asked to learn.
func TestKPIRejections_UnknownNameListsClientNamesOnly(t *testing.T) {
	_, rejection := buildKPIRequest(KPIArgs{KPI: "nope"}, kpiTestToday)
	for _, recipe := range kpiRecipes {
		if strings.Contains(rejection, recipe.savedQuery) {
			t.Errorf("unknown-kpi error names the dbt saved query %q: %q", recipe.savedQuery, rejection)
		}
	}
	for _, name := range kpiNames {
		if !strings.Contains(rejection, name) {
			t.Errorf("unknown-kpi error does not list %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

func TestKPIs_SendsExpansionsAsSeparateWhereEntries(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{
		KPI:         "contributions_by_org",
		Project:     "cncf",
		Subprojects: "none",
		Where:       "{{ Dimension('account__account_name') }} = 'Red Hat LLC'",
		Limit:       10,
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
		SavedQuery   string   `json:"saved_query"`
		Where        []string `json:"where"`
		DropGroupBys []string `json:"drop_group_bys"`
		Limit        int      `json:"limit"`
	}
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("failed to parse captured body: %v", err)
	}
	if body.SavedQuery != "kpi_contributions_by_org" || body.Limit != 10 {
		t.Errorf("unexpected payload: %+v", body)
	}
	if len(body.DropGroupBys) != 0 {
		t.Errorf("no combined switch was set, so nothing may be dropped, got %v", body.DropGroupBys)
	}
	want := []string{
		"{{ Dimension('project__slug') }} = 'cncf'",
		"{{ Dimension('account__account_name') }} = 'Red Hat LLC'",
	}
	if !reflect.DeepEqual(body.Where, want) {
		t.Errorf("where = %v, want %v", body.Where, want)
	}
}

func TestKPIs_SendsDropGroupBysForCombined(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{
		KPI:          "event_registrations_by_org",
		Org:          "International Business Machines Corporation",
		Subsidiaries: "combined",
		OrderBy:      "-total_registrations",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}

	var body struct {
		SavedQuery   string   `json:"saved_query"`
		Where        []string `json:"where"`
		DropGroupBys []string `json:"drop_group_bys"`
		OrderBy      []string `json:"order_by"`
	}
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("failed to parse captured body: %v", err)
	}
	if body.SavedQuery != "kpi_event_registrations_by_org" {
		t.Errorf("saved_query = %q", body.SavedQuery)
	}
	wantDrops := []string{"account__account_name", "account__account_rollup_name"}
	if !reflect.DeepEqual(body.DropGroupBys, wantDrops) {
		t.Errorf("drop_group_bys = %v, want %v", body.DropGroupBys, wantDrops)
	}
	wantWhere := []string{"({{ Dimension('account__account_name') }} = 'International Business Machines Corporation' OR {{ Dimension('account__account_rollup_name') }} = 'International Business Machines Corporation')"}
	if !reflect.DeepEqual(body.Where, wantWhere) {
		t.Errorf("where = %v, want %v", body.Where, wantWhere)
	}
	if !reflect.DeepEqual(body.OrderBy, []string{"-total_registrations"}) {
		t.Errorf("order_by = %v", body.OrderBy)
	}
}

// setupLensResponseTest points lensConfig at a stub lens API that returns the
// given body with 200, and captures the request.
func setupLensResponseTest(t *testing.T, response string) *capturedLensRequest {
	t.Helper()

	captured := &capturedLensRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.Method = r.Method
		captured.Path = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		captured.Body = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
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

	return captured
}

// The caller asked for an organization and a project; the result says
// account, parent_org, project and foundation.
func TestKPIs_RelabelsTheResult(t *testing.T) {
	setupLensResponseTest(t, `{"data":[{"account__account_name":"Red Hat LLC","account__account_rollup_name":"International Business Machines Corporation","total_registrations":9}],"compiled_sql":"SELECT 1 WHERE x >= 2"}`)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{KPI: "event_registrations_by_org"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %s", resultText(t, res))
	}
	text := resultText(t, res)
	for _, want := range []string{`"account"`, `"parent_org"`, `"total_registrations"`} {
		if !strings.Contains(text, want) {
			t.Errorf("result %s missing %s", text, want)
		}
	}
	if strings.Contains(text, "account__account_name") {
		t.Errorf("result still carries qualified column names: %s", text)
	}
	var got struct {
		CompiledSQL string `json:"compiled_sql"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if got.CompiledSQL != "SELECT 1 WHERE x >= 2" {
		t.Errorf("compiled_sql = %q, want it passed through", got.CompiledSQL)
	}
}

func TestKPIs_RejectionDoesNotCallLens(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{
		KPI:   "members_and_dues_by_org",
		Since: "2025-01-01",
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

// An unknown KPI name is rejected before any call: the round trip would look
// like it worked, and the caller would read a server error as a deployment
// state rather than a name it invented.
func TestKPIs_UnknownKPINeverReachesLens(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{KPI: "kpi_not_real"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	if captured.Method != "" {
		t.Errorf("an unknown kpi must not reach lens, got %s %s", captured.Method, captured.Path)
	}
	if text := resultText(t, res); !strings.Contains(text, "unknown kpi") {
		t.Errorf("rejection %q does not name the rule", text)
	}
}

// subsidiaries on a KPI with no parent-company lens is rejected before any
// call: a per-account figure returned for a group question is a confident
// wrong answer, and the round trip would look like it worked.
func TestKPIs_SubsidiariesOnMaintainersNeverReachesLens(t *testing.T) {
	captured := setupLensTest(t)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{
		KPI:          "maintainers_by_org",
		Org:          "International Business Machines Corporation",
		Subsidiaries: "combined",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	if captured.Method != "" {
		t.Errorf("subsidiaries on a KPI with no parent lens must not reach lens, got %s %s", captured.Method, captured.Path)
	}
	if text := resultText(t, res); !strings.Contains(text, "no parent-company lens") {
		t.Errorf("rejection %q does not name the missing lens", text)
	}
}

// Lens errors are never rewritten: a friendlier message invented here would
// have the model correct the wrong thing.
func TestKPIs_PassesLensErrorsThrough(t *testing.T) {
	setupLensErrorTest(t, http.StatusInternalServerError, `{"detail":"semantic layer timeout"}`)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{KPI: "contributors_by_org"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	if text := resultText(t, res); !strings.Contains(text, "semantic layer timeout") {
		t.Errorf("lens errors must pass through verbatim, got %q", text)
	}
}

// A 400 that names the recipe but is a different failure (a bad order_by
// column) must still arrive verbatim: the recipe name appearing in an error
// body is not evidence the recipe is missing.
func TestKPIs_NamedRecipeInAnotherErrorPassesThrough(t *testing.T) {
	setupLensErrorTest(t, http.StatusBadRequest,
		`{"detail":"order_by column 'total_contributorz' is not a result column of kpi_contributors_by_org"}`)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{
		KPI:     "contributors_by_org",
		OrderBy: "-total_contributorz",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	text := resultText(t, res)
	if !strings.Contains(text, "is not a result column") {
		t.Errorf("order_by error must pass through verbatim, got %q", text)
	}
	if strings.Contains(text, "not deployed") {
		t.Errorf("a query-time error must not be reported as a missing recipe, got %q", text)
	}
}

// A saved query the server does not know is the dbt side not having deployed
// it; the server's own error is the signal, returned verbatim.
func TestKPIs_UndeployedRecipeErrorPassesThrough(t *testing.T) {
	setupLensErrorTest(t, http.StatusBadRequest, `{"detail":"saved query kpi_training_enrollments does not exist"}`)

	res, _, err := handleKPIs(context.Background(), &mcp.CallToolRequest{}, KPIArgs{KPI: "kpi_training_enrollments"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result")
	}
	if text := resultText(t, res); !strings.Contains(text, "does not exist") {
		t.Errorf("undeployed recipe error must pass through verbatim, got %q", text)
	}
}
