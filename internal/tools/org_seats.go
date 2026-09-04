// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// sfidPattern is the 18-character Salesforce Account SFID that identifies a
// b2b_org on the committee-service seats route.
var sfidPattern = regexp.MustCompile(`^[A-Za-z0-9]{18}$`)

// orgSeatsPageSize is the committee-service maximum page size.
const orgSeatsPageSize = 500

// orgSeatsMaxPages caps the drain, matching LFX Self Serve's safety stop.
// Hitting it is an error, never a truncated roster.
const orgSeatsMaxPages = 200

// rootProjectSlug is the administrative pseudo-project LFX Self Serve skips
// when resolving a foundation's family (ROOT_PROJECT_SLUG).
const rootProjectSlug = "ROOT"

// boardCommitteeCategory is the category that makes a seat a board seat
// (COMMITTEE_CATEGORY_BOARD in LFX Self Serve, compared trimmed and
// case-insensitively).
const boardCommitteeCategory = "board"

// orgSeatsNote travels with every summary.
const orgSeatsNote = "Seats of this organisation across the projects in scope, as LFX Self Serve's Board & Committee tab shows them; complete for the scope."

// orgSeatsForbiddenMessage is returned when committee-service answers 403.
const orgSeatsForbiddenMessage = "Error: your identity does not hold the organisation grant (auditor or writer) that LFX Self Serve requires to read this organisation's seats"

// OrgSeatsConfig holds configuration for get_org_committee_seats.
type OrgSeatsConfig struct {
	// Clients is the shared LFX v2 API client instance (token exchange and
	// the query service). Must be the instance created at startup.
	Clients *lfxv2.Clients
	// APIURL is the LFX API base URL the seats route is served under.
	APIURL string
	// HTTPClient performs the seats request. The exchanged token is set
	// explicitly per request, so this must be a plain client, not one wrapped
	// by the auth interceptor. Nil means a 30s-timeout default.
	HTTPClient *http.Client
}

var orgSeatsConfig *OrgSeatsConfig

// SetOrgSeatsConfig sets the configuration for get_org_committee_seats.
func SetOrgSeatsConfig(cfg *OrgSeatsConfig) {
	orgSeatsConfig = cfg
}

// GetOrgCommitteeSeatsArgs defines the input parameters for the get_org_committee_seats tool.
type GetOrgCommitteeSeatsArgs struct {
	OrgUID        string `json:"org_uid" jsonschema:"(required) Organization SFID, 18 characters, from search_b2b_orgs"`
	FoundationUID string `json:"foundation_uid,omitempty" jsonschema:"Scope seats to one membership foundation (its root project and every descendant). Omit for the organization's seats across all projects"`
	Category      string `json:"category,omitempty" jsonschema:"Exact committee category to keep, e.g. Board, Technical, Marketing; matched case-insensitively"`
	IncludeSeats  bool   `json:"include_seats,omitempty" jsonschema:"Return the seat rows as well as the summary (default false: summary only)"`
}

// orgCommitteeSeat is one seat row as committee-service 0.4.22 serves it
// (cmd/committee-api/design/type.go, OrgCommitteeSeatType). It is decoded
// here rather than through the vendored Goa client: the vendored v0.4.0
// client predates project_uid, project_slug, avatar and username and its
// decoder drops unknown keys silently.
type orgCommitteeSeat struct {
	UID               string `json:"uid"`
	CommitteeUID      string `json:"committee_uid"`
	CommitteeName     string `json:"committee_name"`
	CommitteeCategory string `json:"committee_category"`
	ProjectUID        string `json:"project_uid,omitempty"`
	ProjectSlug       string `json:"project_slug,omitempty"`
	FirstName         string `json:"first_name"`
	LastName          string `json:"last_name"`
	Email             string `json:"email"`
	JobTitle          string `json:"job_title,omitempty"`
	RoleName          string `json:"role_name"`
	VotingStatus      string `json:"voting_status"`
	AppointedBy       string `json:"appointed_by"`
	OrganizationID    string `json:"organization_id"`
	IsOrgEditable     bool   `json:"is_org_editable"`
	Reason            string `json:"reason,omitempty"`
	Avatar            string `json:"avatar,omitempty"`
	Username          string `json:"username,omitempty"`
}

// orgCommitteeSeatPage is the seats route's response body.
type orgCommitteeSeatPage struct {
	Seats     []orgCommitteeSeat `json:"seats"`
	PageToken string             `json:"page_token,omitempty"`
}

// orgSeatsSummary is the output of get_org_committee_seats.
type orgSeatsSummary struct {
	OrgUID               string             `json:"org_uid"`
	FoundationUID        string             `json:"foundation_uid,omitempty"`
	ProjectUIDsInScope   int                `json:"project_uids_in_scope,omitempty"`
	Category             string             `json:"category,omitempty"`
	SeatsTotal           int                `json:"seats_total"`
	People               int                `json:"people"`
	BoardSeats           int                `json:"board_seats"`
	CommitteeSeats       int                `json:"committee_seats"`
	ByCategory           map[string]int     `json:"by_category"`
	ByProject            map[string]int     `json:"by_project"`
	ByRole               map[string]int     `json:"by_role"`
	Editable             int                `json:"editable"`
	FoundationControlled int                `json:"foundation_controlled"`
	Visibility           string             `json:"visibility"`
	Note                 string             `json:"note"`
	Seats                []orgCommitteeSeat `json:"seats,omitempty"`
}

// orgSeatsErrorBodyLimit bounds how much of an upstream error body is kept.
const orgSeatsErrorBodyLimit = 512

// orgSeatsHTTPError carries the status of a non-2xx seats response. Body is
// truncated to orgSeatsErrorBodyLimit and is only surfaced to callers for
// 4xx statuses; 5xx bodies stay in the server log.
type orgSeatsHTTPError struct {
	Status int
	Body   string
}

func (e *orgSeatsHTTPError) Error() string {
	if e.Status >= 500 {
		return fmt.Sprintf("invalid response code %d", e.Status)
	}
	return fmt.Sprintf("invalid response code %d: %s", e.Status, strings.TrimSpace(e.Body))
}

// truncateBody keeps at most orgSeatsErrorBodyLimit bytes of an error body.
func truncateBody(body []byte) string {
	if len(body) > orgSeatsErrorBodyLimit {
		return string(body[:orgSeatsErrorBodyLimit]) + "…"
	}
	return string(body)
}

// RegisterGetOrgCommitteeSeats registers the get_org_committee_seats tool with the MCP server.
func RegisterGetOrgCommitteeSeats(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_org_committee_seats",
		Description: "Summarise an organization's committee seats as LFX Self Serve's Org Lens Board & Committee tab shows them. " +
			"org_uid is the 18-character SFID from search_b2b_orgs. Scope is one membership foundation (foundation_uid: its root project and every descendant) or, when omitted, the organization's seats across all projects. " +
			"Returns seats_total, people (distinct e-mails), board_seats vs committee_seats, by_category, by_project, by_role, editable vs foundation_controlled; include_seats adds the rows (name, e-mail, role, voting status, appointed_by, committee, project). " +
			"category keeps one committee category, matched case-insensitively. The caller needs the organization grant (auditor or writer) LFX Self Serve requires; the result is complete for the scope, never truncated.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Organization Committee Seats",
			ReadOnlyHint: true,
		},
	}, handleGetOrgCommitteeSeats)
}

// resolveFoundationFamily returns the foundation uid plus every uid of a
// project whose parent is the foundation, skipping the ROOT pseudo-project,
// draining every page. Errors propagate: the caller fails closed rather than
// silently scoping to the root alone.
func resolveFoundationFamily(ctx context.Context, clients *lfxv2.Clients, foundationUID string) ([]string, error) {
	family := []string{foundationUID}
	resourceType := projectResourceType
	parent := "project:" + foundationUID
	var pageToken *string
	for pages := 0; ; pages++ {
		if pages >= participantMaxDrainPages {
			return nil, fmt.Errorf("the foundation's project list exceeds the %d-page cap", participantMaxDrainPages)
		}
		result, err := clients.QuerySvc.QueryResources(ctx, &querysvc.QueryResourcesPayload{
			Version:   "1",
			Type:      &resourceType,
			Parent:    &parent,
			PageSize:  participantDrainPageSize,
			Sort:      "name_asc",
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range result.Resources {
			data, _ := r.Data.(map[string]any)
			slug, _ := data["slug"].(string)
			if slug == rootProjectSlug {
				continue
			}
			uid, _ := data["uid"].(string)
			if uid == "" && r.ID != nil {
				uid = *r.ID
			}
			if uid != "" {
				family = append(family, uid)
			}
		}
		if result.PageToken == nil || *result.PageToken == "" {
			return family, nil
		}
		pageToken = result.PageToken
	}
}

// fetchOrgSeatsPage performs one authenticated GET on the seats route.
func fetchOrgSeatsPage(ctx context.Context, cfg *OrgSeatsConfig, token, orgUID string, projectUIDs []string, pageToken string) (*orgCommitteeSeatPage, error) {
	q := url.Values{}
	q.Set("v", "1")
	for _, uid := range projectUIDs {
		q.Add("project_uids", uid)
	}
	q.Set("page_size", fmt.Sprintf("%d", orgSeatsPageSize))
	if pageToken != "" {
		q.Set("page_token", pageToken)
	}
	endpoint := strings.TrimSuffix(cfg.APIURL, "/") + "/committees/b2b-org/" + url.PathEscape(orgUID) + "/seats?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build seats request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("seats request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Response body close errors are not actionable after reading.

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("failed to read seats response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &orgSeatsHTTPError{Status: resp.StatusCode, Body: truncateBody(body)}
	}
	var pg orgCommitteeSeatPage
	if err := json.Unmarshal(body, &pg); err != nil {
		return nil, fmt.Errorf("failed to decode seats response: %w", err)
	}
	return &pg, nil
}

// drainOrgSeats follows page_token until exhausted or the page cap; the cap
// is an error because LFX Self Serve never shows a partial roster.
func drainOrgSeats(ctx context.Context, cfg *OrgSeatsConfig, token, orgUID string, projectUIDs []string) ([]orgCommitteeSeat, error) {
	var seats []orgCommitteeSeat
	pageToken := ""
	for pages := 0; pages < orgSeatsMaxPages; pages++ {
		pg, err := fetchOrgSeatsPage(ctx, cfg, token, orgUID, projectUIDs, pageToken)
		if err != nil {
			return nil, err
		}
		seats = append(seats, pg.Seats...)
		if pg.PageToken == "" {
			return seats, nil
		}
		pageToken = pg.PageToken
	}
	return nil, fmt.Errorf("the organisation's roster exceeds the %d-page cap; scope with foundation_uid", orgSeatsMaxPages)
}

// isBoardCategory mirrors LFX Self Serve's isBoardCategory.
func isBoardCategory(category string) bool {
	return strings.ToLower(strings.TrimSpace(category)) == boardCommitteeCategory
}

// summariseOrgSeats computes the Board & Committee tab arithmetic over seats,
// optionally keeping only one category first.
func summariseOrgSeats(seats []orgCommitteeSeat, category string) orgSeatsSummary {
	if category != "" {
		want := strings.ToLower(strings.TrimSpace(category))
		kept := make([]orgCommitteeSeat, 0, len(seats))
		for _, s := range seats {
			if strings.ToLower(strings.TrimSpace(s.CommitteeCategory)) == want {
				kept = append(kept, s)
			}
		}
		seats = kept
	}

	out := orgSeatsSummary{
		Category:   category,
		SeatsTotal: len(seats),
		ByCategory: map[string]int{},
		ByProject:  map[string]int{},
		ByRole:     map[string]int{},
		Visibility: "organization",
		Note:       orgSeatsNote,
	}
	people := map[string]struct{}{}
	for _, s := range seats {
		email := strings.ToLower(strings.TrimSpace(s.Email))
		if email == "" {
			email = "uid:" + s.UID
		}
		people[email] = struct{}{}

		if isBoardCategory(s.CommitteeCategory) {
			out.BoardSeats++
		} else {
			out.CommitteeSeats++
		}
		out.ByCategory[s.CommitteeCategory]++

		project := s.ProjectSlug
		if project == "" {
			project = s.ProjectUID
		}
		if project == "" {
			project = "(none)"
		}
		out.ByProject[project]++
		out.ByRole[s.RoleName]++

		if s.IsOrgEditable {
			out.Editable++
		} else {
			out.FoundationControlled++
		}
	}
	out.People = len(people)

	// Stable row order: committee, then last name, first name, e-mail.
	sort.SliceStable(seats, func(i, j int) bool {
		a, b := seats[i], seats[j]
		if a.CommitteeName != b.CommitteeName {
			return a.CommitteeName < b.CommitteeName
		}
		if a.LastName != b.LastName {
			return a.LastName < b.LastName
		}
		if a.FirstName != b.FirstName {
			return a.FirstName < b.FirstName
		}
		return a.Email < b.Email
	})
	out.Seats = seats
	return out
}

// handleGetOrgCommitteeSeats implements the get_org_committee_seats tool logic.
func handleGetOrgCommitteeSeats(ctx context.Context, req *mcp.CallToolRequest, args GetOrgCommitteeSeatsArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if orgSeatsConfig == nil || orgSeatsConfig.Clients == nil || orgSeatsConfig.APIURL == "" {
		logger.ErrorContext(ctx, "org seats tool not configured")
		return errorResult("Error: org seats tool not configured"), nil, nil
	}

	if !sfidPattern.MatchString(args.OrgUID) {
		return errorResult("Error: org_uid must be the organization's 18-character SFID; resolve it with search_b2b_orgs"), nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return errorResult(fmt.Sprintf("Error: failed to extract MCP token: %v", err)), nil, nil
	}
	ctx = orgSeatsConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := orgSeatsConfig.Clients

	logger.InfoContext(ctx, "fetching org committee seats", "org_uid", args.OrgUID, "foundation_uid", args.FoundationUID, "category", args.Category, "include_seats", args.IncludeSeats)

	var projectUIDs []string
	if args.FoundationUID != "" {
		projectUIDs, err = resolveFoundationFamily(ctx, clients, args.FoundationUID)
		if err != nil {
			logger.ErrorContext(ctx, "foundation family resolution failed", "error", err)
			return errorResult(friendlyAPIError("failed to resolve the foundation's projects", err)), nil, nil
		}
	}

	token, err := clients.GetExchangedToken(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "token exchange failed", "error", err)
		return errorResult(fmt.Sprintf("Error: failed to obtain an LFX API token: %v", err)), nil, nil
	}

	seats, err := drainOrgSeats(ctx, orgSeatsConfig, token, args.OrgUID, projectUIDs)
	if err != nil {
		var httpErr *orgSeatsHTTPError
		if e, ok := err.(*orgSeatsHTTPError); ok {
			httpErr = e
			// Full (truncated) body for operators; callers get Error().
			logger.ErrorContext(ctx, "org seats fetch failed", "status", e.Status, "body", e.Body)
		} else {
			logger.ErrorContext(ctx, "org seats fetch failed", "error", err)
		}
		if httpErr != nil && httpErr.Status == http.StatusForbidden {
			return errorResult(orgSeatsForbiddenMessage), nil, nil
		}
		return errorResult(friendlyAPIError("failed to get organization committee seats", err)), nil, nil
	}

	out := summariseOrgSeats(seats, args.Category)
	out.OrgUID = args.OrgUID
	out.FoundationUID = args.FoundationUID
	out.ProjectUIDsInScope = len(projectUIDs)
	if !args.IncludeSeats {
		out.Seats = nil
	}

	logger.InfoContext(ctx, "get_org_committee_seats succeeded", "org_uid", args.OrgUID, "seats_total", out.SeatsTotal, "people", out.People)
	return jsonResult(ctx, logger, "get_org_committee_seats formatted", out)
}
