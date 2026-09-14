// Copyright The Linux Foundation and contributors.
// SPDX-License-Identifier: MIT

// Package tools provides MCP tool implementations for the LFX MCP server.
package tools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/linuxfoundation/lfx-mcp/internal/lfxv2"
	committeeservice "github.com/linuxfoundation/lfx-v2-committee-service/gen/committee_service"
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

// orgSeatsProjectChunk is the most project uids one seats request carries.
// The committee-service route takes the scope as one project_uids query
// parameter per uid; a large foundation's family in a single request
// overflows the request line, so the family is read in several requests.
const orgSeatsProjectChunk = 40

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
const orgSeatsForbiddenMessage = "Error: your identity does not hold the organisation grant (auditor or writer) that LFX Self Serve requires to read this organisation's seats (or the b2b_org_uid is not a known organisation — confirm the SFID with search_b2b_orgs)"

// seatKindBoard and seatKindCommittee label each seat row by its committee
// category, so a caller can tell a board seat from any other seat without
// re-deriving the category test.
const (
	seatKindBoard     = "board_seat"
	seatKindCommittee = "committee_seat"
)

// membershipContactKind labels every membership key-contact row.
const membershipContactKind = "membership_contact"

// votingContactRole is the key-contact role that names the membership's
// representative, as the member service stores it.
const votingContactRole = "Representative/Voting Contact"

// membershipContactsNote travels with membership_contacts and representation.
const membershipContactsNote = "Membership key contacts are the contacts of record for each membership (roles as stored; status Active or Inactive, no dates); board seats are roster rows; the two can name different people — cite which one you mean."

// membershipContactsNoneNote is appended when no key contact came back.
const membershipContactsNoneNote = " No key contact is indexed for this organization in scope; get_membership_key_contacts per membership is the fallback."

// OrgSeatsConfig holds configuration for get_org_committee_seats.
type OrgSeatsConfig struct {
	// Clients is the shared LFX v2 API client instance (committee service and
	// query service). Must be the instance created at startup.
	Clients *lfxv2.Clients
}

var orgSeatsConfig *OrgSeatsConfig

// SetOrgSeatsConfig sets the configuration for get_org_committee_seats.
func SetOrgSeatsConfig(cfg *OrgSeatsConfig) {
	orgSeatsConfig = cfg
}

// GetOrgCommitteeSeatsArgs defines the input parameters for the get_org_committee_seats tool.
type GetOrgCommitteeSeatsArgs struct {
	B2bOrgUID                 string `json:"b2b_org_uid" jsonschema:"(required) B2B organization UID: the 18-character SFID from search_b2b_orgs (the same identifier search_members calls b2b_org_uid)"`
	FoundationUID             string `json:"foundation_uid,omitempty" jsonschema:"Scope seats to one membership foundation (its root project and its direct child projects, as LFX Self Serve scopes it). Omit for the organization's seats across all projects"`
	Category                  string `json:"category,omitempty" jsonschema:"Exact committee category to keep, e.g. Board, Technical, Marketing; matched case-insensitively"`
	IncludeSeats              bool   `json:"include_seats,omitempty" jsonschema:"Return the seat rows as well as the summary (default false: summary only)"`
	IncludeMembershipContacts bool   `json:"include_membership_contacts,omitempty" jsonschema:"Also return the organization's membership key contacts on the projects in scope (contact of record per membership) and a per-project representation pairing them with the board seats; default false"`
}

// orgCommitteeSeat is one seat row as returned to the caller: the
// committee-service OrgCommitteeSeat (client v0.4.22, the version prod runs)
// flattened so optional fields serialise as plain strings.
type orgCommitteeSeat struct {
	Kind              string `json:"kind"`
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

// seatFromService flattens a committee-service seat (derefStr from committee_write.go).
func seatFromService(in *committeeservice.OrgCommitteeSeat) orgCommitteeSeat {
	kind := seatKindCommittee
	if isBoardCategory(in.CommitteeCategory) {
		kind = seatKindBoard
	}
	return orgCommitteeSeat{
		Kind:              kind,
		UID:               in.UID,
		CommitteeUID:      in.CommitteeUID,
		CommitteeName:     in.CommitteeName,
		CommitteeCategory: in.CommitteeCategory,
		ProjectUID:        derefStr(in.ProjectUID),
		ProjectSlug:       derefStr(in.ProjectSlug),
		FirstName:         in.FirstName,
		LastName:          in.LastName,
		Email:             in.Email,
		JobTitle:          derefStr(in.JobTitle),
		RoleName:          in.RoleName,
		VotingStatus:      in.VotingStatus,
		AppointedBy:       in.AppointedBy,
		OrganizationID:    in.OrganizationID,
		IsOrgEditable:     in.IsOrgEditable,
		Reason:            derefStr(in.Reason),
		Avatar:            derefStr(in.Avatar),
		Username:          derefStr(in.Username),
	}
}

// orgSeatsSummary is the output of get_org_committee_seats.
type orgSeatsSummary struct {
	B2bOrgUID            string             `json:"b2b_org_uid"`
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
	ByVotingStatus       map[string]int     `json:"by_voting_status"`
	Editable             int                `json:"editable"`
	FoundationControlled int                `json:"foundation_controlled"`
	Visibility           string             `json:"visibility"`
	Note                 string             `json:"note"`
	Seats                []orgCommitteeSeat `json:"seats,omitempty"`

	// Only with include_membership_contacts; present (possibly empty) whenever
	// the flag is set, so an empty list is a stated answer, not an omission.
	MembershipContacts *[]membershipContact     `json:"membership_contacts,omitempty"`
	Representation     *[]projectRepresentation `json:"representation,omitempty"`
	ContactsNote       string                   `json:"contacts_note,omitempty"`
}

// membershipContact is one key_contact record of the organization's
// memberships, as the member service indexes it (values as stored).
type membershipContact struct {
	Kind           string `json:"kind"`
	UID            string `json:"uid"`
	MembershipUID  string `json:"membership_uid"`
	ProjectUID     string `json:"project_uid"`
	ProjectName    string `json:"project_name,omitempty"`
	Role           string `json:"role"`
	Status         string `json:"status"`
	BoardMember    bool   `json:"board_member"`
	PrimaryContact bool   `json:"primary_contact"`
	FirstName      string `json:"first_name"`
	LastName       string `json:"last_name"`
	Email          string `json:"email"`
	Title          string `json:"title,omitempty"`
}

// projectRepresentation pairs, for one project, the membership's voting
// contacts with the organization's board seats; neither is merged into the
// other.
type projectRepresentation struct {
	ProjectUID     string              `json:"project_uid"`
	ProjectSlug    string              `json:"project_slug,omitempty"`
	VotingContacts []membershipContact `json:"voting_contacts"`
	BoardSeats     []orgCommitteeSeat  `json:"board_seats"`
}

// RegisterGetOrgCommitteeSeats registers the get_org_committee_seats tool with the MCP server.
func RegisterGetOrgCommitteeSeats(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_org_committee_seats",
		Description: "Summarise an organization's committee seats as LFX Self Serve's Org Lens Board & Committee tab shows them. " +
			"b2b_org_uid is the 18-character SFID from search_b2b_orgs. With foundation_uid, scope is its root project plus direct child projects as visible to the caller, the way LFX Self Serve scopes it; an organization grant does not make project discovery exhaustive. Omit foundation_uid for the organization's seats across all projects. " +
			"Returns seats_total, people (distinct e-mails), board_seats vs committee_seats, by_category, by_project, by_role, editable vs foundation_controlled; include_seats adds the rows (name, e-mail, role, voting status, appointed_by, committee, project). " +
			"category keeps one committee category, matched case-insensitively. The caller needs the organization grant (auditor or writer) LFX Self Serve requires; the result is complete for the scope, never truncated. Seats only; the membership's contact of record is get_membership_key_contacts and can be a different person. " +
			"include_membership_contacts=true adds membership_contacts (the organization's key contacts on the projects in scope: contact of record per membership, roles and status as stored) and representation (per project: the Representative/Voting Contact rows beside the board seats with their voting_status) — the two answers to who represents the organization, labelled, never merged; by_voting_status is always returned. " +
			"Large foundations are read in several requests; the result is still complete for the scope.",
		Annotations: &mcp.ToolAnnotations{
			Title:        "Get Organization Committee Seats",
			ReadOnlyHint: true,
		},
	}, handleGetOrgCommitteeSeats)
}

// resolveFoundationFamily returns the foundation uid plus every uid of a
// project whose parent is the foundation (direct children only — the project
// indexer's parent ref carries the immediate parent, and this mirrors LFX
// Self Serve's getFoundationProjectUids), skipping the ROOT pseudo-project,
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

// drainOrgSeats reads the organisation's seats for the scope. Without a
// family it is one drain with no project_uids; with a family larger than
// orgSeatsProjectChunk the family is split into chunks and one drain runs
// per chunk, seats concatenated (each seat belongs to one project, so the
// chunks are disjoint). An error in any chunk is the call's error.
func drainOrgSeats(ctx context.Context, clients *lfxv2.Clients, orgUID string, projectUIDs []string) ([]orgCommitteeSeat, error) {
	// One drain covers the unscoped call (no project_uids at all) and any
	// family that fits one request.
	if len(projectUIDs) <= orgSeatsProjectChunk {
		return drainOrgSeatsScope(ctx, clients, orgUID, projectUIDs)
	}
	var seats []orgCommitteeSeat
	for _, chunk := range chunkStrings(projectUIDs, orgSeatsProjectChunk) {
		part, err := drainOrgSeatsScope(ctx, clients, orgUID, chunk)
		if err != nil {
			return nil, err
		}
		seats = append(seats, part...)
	}
	return seats, nil
}

// drainOrgSeatsScope follows page_token until exhausted or the page cap; the
// cap is an error because LFX Self Serve never shows a partial roster. The
// shared auth interceptor injects the caller's exchanged token on every
// request.
func drainOrgSeatsScope(ctx context.Context, clients *lfxv2.Clients, orgUID string, projectUIDs []string) ([]orgCommitteeSeat, error) {
	var seats []orgCommitteeSeat
	var pageToken *string
	pageSize := orgSeatsPageSize
	for pages := 0; pages < orgSeatsMaxPages; pages++ {
		pg, err := clients.Committee.GetOrgCommitteeSeats(ctx, &committeeservice.GetOrgCommitteeSeatsPayload{
			Version:     "1",
			UID:         orgUID,
			ProjectUids: projectUIDs,
			PageSize:    &pageSize,
			PageToken:   pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, seat := range pg.Seats {
			if seat != nil {
				seats = append(seats, seatFromService(seat))
			}
		}
		if pg.PageToken == nil || *pg.PageToken == "" {
			return seats, nil
		}
		pageToken = pg.PageToken
	}
	if len(projectUIDs) == 0 {
		return nil, fmt.Errorf("the organisation's roster exceeds the %d-page cap; scope with foundation_uid", orgSeatsMaxPages)
	}
	return nil, fmt.Errorf("the organisation's roster exceeds the %d-page cap for the projects in scope", orgSeatsMaxPages)
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
		Category:       category,
		SeatsTotal:     len(seats),
		ByCategory:     map[string]int{},
		ByProject:      map[string]int{},
		ByRole:         map[string]int{},
		ByVotingStatus: map[string]int{},
		Visibility:     "organization",
		Note:           orgSeatsNote,
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
		out.ByVotingStatus[s.VotingStatus]++

		if s.IsOrgEditable {
			out.Editable++
		} else {
			out.FoundationControlled++
		}
	}
	out.People = len(people)

	sortSeats(seats)
	out.Seats = seats
	return out
}

// sortSeats orders rows by committee, then last name, first name, e-mail.
func sortSeats(seats []orgCommitteeSeat) {
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
}

// drainMembershipContacts reads every key_contact of the organization
// (tags_all b2b_org_uid:<sfid>), following page_token to the same cap as the
// family resolution, and keeps those on a project of the family when one
// was given. The family is never sent as tags: a large foundation would
// overflow the request line, so the scope filter is applied here.
func drainMembershipContacts(ctx context.Context, clients *lfxv2.Clients, orgUID string, projectUIDs []string) ([]membershipContact, error) {
	var inFamily map[string]struct{}
	if len(projectUIDs) > 0 {
		inFamily = make(map[string]struct{}, len(projectUIDs))
		for _, uid := range projectUIDs {
			inFamily[uid] = struct{}{}
		}
	}

	contacts := []membershipContact{}
	resourceType := keyContactResourceType
	var pageToken *string
	for pages := 0; ; pages++ {
		if pages >= participantMaxDrainPages {
			return nil, fmt.Errorf("the organisation's key contacts exceed the %d-page cap", participantMaxDrainPages)
		}
		result, err := clients.QuerySvc.QueryResources(ctx, &querysvc.QueryResourcesPayload{
			Version:   "1",
			Type:      &resourceType,
			TagsAll:   []string{"b2b_org_uid:" + orgUID},
			PageSize:  participantDrainPageSize,
			Sort:      "name_asc",
			PageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range result.Resources {
			if r == nil {
				continue
			}
			c := membershipContactFromResource(r)
			if inFamily != nil {
				if _, ok := inFamily[c.ProjectUID]; !ok {
					continue
				}
			}
			contacts = append(contacts, c)
		}
		if result.PageToken == nil || *result.PageToken == "" {
			return contacts, nil
		}
		pageToken = result.PageToken
	}
}

// membershipContactFromResource copies the key_contact data fields the tool
// returns, values as stored.
func membershipContactFromResource(r *querysvc.Resource) membershipContact {
	data, _ := r.Data.(map[string]any)
	str := func(key string) string {
		v, _ := data[key].(string)
		return v
	}
	flag := func(key string) bool {
		v, _ := data[key].(bool)
		return v
	}
	c := membershipContact{
		Kind:           membershipContactKind,
		UID:            str("uid"),
		MembershipUID:  str("membership_uid"),
		ProjectUID:     str("project_uid"),
		ProjectName:    str("project_name"),
		Role:           str("role"),
		Status:         str("status"),
		BoardMember:    flag("board_member"),
		PrimaryContact: flag("primary_contact"),
		FirstName:      str("first_name"),
		LastName:       str("last_name"),
		Email:          str("email"),
		Title:          str("title"),
	}
	if c.UID == "" && r.ID != nil {
		c.UID = *r.ID
	}
	return c
}

// buildRepresentation pairs, per project, the voting contacts with the board
// seats; one row per project uid that has at least one of either, ordered by
// project uid. It reads every drained seat, not the category-filtered list:
// a category filter narrows the summary counts, never the pairing. A contact
// or seat with no project uid cannot be paired and earns no row.
// Voting-status buckets are not derived: each seat row carries its own
// voting_status.
func buildRepresentation(contacts []membershipContact, seats []orgCommitteeSeat) []projectRepresentation {
	byProject := map[string]*projectRepresentation{}
	row := func(projectUID string) *projectRepresentation {
		r, ok := byProject[projectUID]
		if !ok {
			r = &projectRepresentation{ProjectUID: projectUID, VotingContacts: []membershipContact{}, BoardSeats: []orgCommitteeSeat{}}
			byProject[projectUID] = r
		}
		return r
	}
	// The slug comes from the first sorted seat row of the project, board
	// or not; the drained order is not relied on.
	sorted := append([]orgCommitteeSeat(nil), seats...)
	sortSeats(sorted)
	seats = sorted
	slugByProject := map[string]string{}
	for _, s := range seats {
		if s.ProjectUID != "" && s.ProjectSlug != "" {
			if _, ok := slugByProject[s.ProjectUID]; !ok {
				slugByProject[s.ProjectUID] = s.ProjectSlug
			}
		}
	}
	for _, c := range contacts {
		if c.Role != votingContactRole || c.ProjectUID == "" {
			continue
		}
		r := row(c.ProjectUID)
		r.VotingContacts = append(r.VotingContacts, c)
	}
	for _, s := range seats {
		if s.Kind != seatKindBoard || s.ProjectUID == "" {
			continue
		}
		r := row(s.ProjectUID)
		r.BoardSeats = append(r.BoardSeats, s)
	}
	out := make([]projectRepresentation, 0, len(byProject))
	for _, r := range byProject {
		r.ProjectSlug = slugByProject[r.ProjectUID]
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProjectUID < out[j].ProjectUID })
	return out
}

// handleGetOrgCommitteeSeats implements the get_org_committee_seats tool logic.
func handleGetOrgCommitteeSeats(ctx context.Context, req *mcp.CallToolRequest, args GetOrgCommitteeSeatsArgs) (*mcp.CallToolResult, any, error) {
	logger := newToolLogger(ctx, req)

	if orgSeatsConfig == nil || orgSeatsConfig.Clients == nil {
		logger.ErrorContext(ctx, "org seats tool not configured")
		return errorResult("Error: org seats tool not configured"), nil, nil
	}

	if !sfidPattern.MatchString(args.B2bOrgUID) {
		return errorResult("Error: b2b_org_uid must be the organization's 18-character SFID; resolve it with search_b2b_orgs"), nil, nil
	}

	mcpToken, err := lfxv2.ExtractMCPToken(req.Extra.TokenInfo)
	if err != nil {
		logger.ErrorContext(ctx, "failed to extract MCP token", "error", err)
		return errorResult(fmt.Sprintf("Error: failed to extract MCP token: %v", err)), nil, nil
	}
	ctx = orgSeatsConfig.Clients.WithMCPToken(ctx, mcpToken)
	clients := orgSeatsConfig.Clients

	logger.InfoContext(ctx, "fetching org committee seats", "b2b_org_uid", args.B2bOrgUID, "foundation_uid", args.FoundationUID, "category", args.Category, "include_seats", args.IncludeSeats, "include_membership_contacts", args.IncludeMembershipContacts)

	var projectUIDs []string
	if args.FoundationUID != "" {
		projectUIDs, err = resolveFoundationFamily(ctx, clients, args.FoundationUID)
		if err != nil {
			logger.ErrorContext(ctx, "foundation family resolution failed", "error", err)
			return errorResult(friendlyAPIError("failed to resolve the foundation's projects", err)), nil, nil
		}
	}

	seats, err := drainOrgSeats(ctx, clients, args.B2bOrgUID, projectUIDs)
	if err != nil {
		logger.ErrorContext(ctx, "org seats fetch failed", "error", err)
		// Heimdall answers 403 when the caller lacks the b2b_org auditor grant;
		// the Goa client surfaces it as an invalid-response error.
		if strings.Contains(err.Error(), "response code 403") {
			return errorResult(orgSeatsForbiddenMessage), nil, nil
		}
		return errorResult(friendlyAPIError("failed to get organization committee seats", err)), nil, nil
	}

	out := summariseOrgSeats(seats, args.Category)
	out.B2bOrgUID = args.B2bOrgUID
	out.FoundationUID = args.FoundationUID
	out.ProjectUIDsInScope = len(projectUIDs)

	if args.IncludeMembershipContacts {
		contacts, err := drainMembershipContacts(ctx, clients, args.B2bOrgUID, projectUIDs)
		if err != nil {
			logger.ErrorContext(ctx, "membership key contacts fetch failed", "error", err)
			return errorResult(friendlyAPIError("failed to get membership key contacts", err)), nil, nil
		}
		out.MembershipContacts = &contacts
		// Pair against every drained seat: category narrows the counts above,
		// not which board seats stand beside the voting contacts.
		representation := buildRepresentation(contacts, seats)
		out.Representation = &representation
		out.ContactsNote = membershipContactsNote
		if len(contacts) == 0 {
			out.ContactsNote += membershipContactsNoneNote
		}
	}

	if !args.IncludeSeats {
		out.Seats = nil
	}

	logger.InfoContext(ctx, "get_org_committee_seats succeeded", "b2b_org_uid", args.B2bOrgUID, "seats_total", out.SeatsTotal, "people", out.People)
	return jsonResult(ctx, logger, "get_org_committee_seats formatted", out)
}
