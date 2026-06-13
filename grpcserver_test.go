package ledger

import (
	"context"
	"net"
	"testing"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

const bufSize = 1 << 20

// grpcClients bundles all test clients for convenience.
type grpcClients struct {
	issue   cwbv1.IssueServiceClient
	project cwbv1.ProjectServiceClient
	org     cwbv1.OrgServiceClient
	admin   cwbv1.AdminServiceClient
}

// newTestGRPCServer starts an in-process gRPC server over bufconn and
// returns connected clients + the underlying service for direct DB setup.
func newTestGRPCServer(t *testing.T) (*grpcClients, *Service) {
	t.Helper()
	svc := newTestService(t)

	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	cwbv1.RegisterIssueServiceServer(srv, NewIssueServer(svc))
	cwbv1.RegisterProjectServiceServer(srv, NewProjectServer(svc))
	cwbv1.RegisterOrgServiceServer(srv, NewOrgServer(svc))
	cwbv1.RegisterAdminServiceServer(srv, NewAdminServer(svc))
	go func() {
		if err := srv.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			t.Logf("grpc serve: %v", err)
		}
	}()
	t.Cleanup(func() {
		srv.GracefulStop()
		lis.Close()
	})

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return &grpcClients{
		issue:   cwbv1.NewIssueServiceClient(conn),
		project: cwbv1.NewProjectServiceClient(conn),
		org:     cwbv1.NewOrgServiceClient(conn),
		admin:   cwbv1.NewAdminServiceClient(conn),
	}, svc
}

// mdOutCtx returns an outgoing metadata context for gRPC clients.
func mdOutCtx(org, subject string, scopes ...string) context.Context {
	scopeStr := ""
	for i, s := range scopes {
		if i > 0 {
			scopeStr += " "
		}
		scopeStr += s
	}
	md := metadata.Pairs(
		"cwb-org", org,
		"cwb-subject", subject,
		"cwb-scopes", scopeStr,
	)
	return metadata.NewOutgoingContext(context.Background(), md)
}

func grpcCode(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	return status.Code(err)
}

// setupTestOrg creates an org + user + member + project for test fixtures.
// Returns the project key.
func setupTestOrg(t *testing.T, svc *Service, org, project string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.CreateOrganisation(ctx, org, org+" Inc"); err != nil {
		t.Fatalf("CreateOrganisation: %v", err)
	}
	if _, err := svc.CreateUser(ctx, "agent:test", "ai"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := svc.AddOrgMember(ctx, org, "agent:test", "member"); err != nil {
		t.Fatalf("AddOrgMember: %v", err)
	}
	p := Project{Key: project, Name: project + " project", Organisation: org}
	if err := svc.CreateProject(ContextWithAuth(ctx, &AuthClaims{Sub: "agent:test", Org: org}), p); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return project
}

// ---------- happy-path: lifecycle ----------

func TestGRPC_CreateClaimTransition_Lifecycle(t *testing.T) {
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")

	writeCtx := mdOutCtx("acme", "agent:test", "issue:write")
	claimCtx := mdOutCtx("acme", "agent:test", "issue:claim")

	// 1. CreateIssue
	createResp, err := clients.issue.CreateIssue(writeCtx, &cwbv1.CreateIssueRequest{
		Project:          "PROJ",
		Type:             "Story",
		Summary:          "hello",
		Description:      "desc",
		DefinitionOfDone: "- [x] done",
		Priority:         "Medium",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	key := createResp.Issue.Key
	if key == "" {
		t.Fatal("CreateIssue: expected non-empty Issue.Key")
	}
	if createResp.Issue.Status != "To Do" {
		t.Errorf("status = %q, want To Do", createResp.Issue.Status)
	}
	if createResp.Issue.GetCategory() != cwbv1.StatusCategory_STATUS_CATEGORY_DRAFT {
		t.Errorf("create category = %v, want DRAFT", createResp.Issue.GetCategory())
	}

	// 2. ClaimIssue → should transition to In Progress
	claimResp, err := clients.issue.ClaimIssue(claimCtx, &cwbv1.ClaimIssueRequest{Key: key})
	if err != nil {
		t.Fatalf("ClaimIssue: %v", err)
	}
	if claimResp.Issue.Status != "In Progress" {
		t.Errorf("after claim: status = %q, want In Progress", claimResp.Issue.Status)
	}
	if claimResp.Issue.GetCategory() != cwbv1.StatusCategory_STATUS_CATEGORY_ACTIVE {
		t.Errorf("claim category = %v, want ACTIVE", claimResp.Issue.GetCategory())
	}

	// 3. TransitionIssue → In Review
	_, err = clients.issue.TransitionIssue(writeCtx, &cwbv1.TransitionIssueRequest{
		Key:    key,
		Status: "In Review",
	})
	if err != nil {
		t.Fatalf("TransitionIssue to In Review: %v", err)
	}

	// 4. TransitionIssue → Done (DoD has all checked items)
	_, err = clients.issue.TransitionIssue(writeCtx, &cwbv1.TransitionIssueRequest{
		Key:    key,
		Status: "Done",
	})
	if err != nil {
		t.Fatalf("TransitionIssue to Done: %v", err)
	}

	// 5. GetIssue round-trip
	readCtx := mdOutCtx("acme", "agent:test", "issue:read")
	getResp, err := clients.issue.GetIssue(readCtx, &cwbv1.GetIssueRequest{Key: key})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if getResp.Issue.Status != "Done" {
		t.Errorf("final status = %q, want Done", getResp.Issue.Status)
	}
	if getResp.Issue.GetCategory() != cwbv1.StatusCategory_STATUS_CATEGORY_DONE {
		t.Errorf("final category = %v, want DONE", getResp.Issue.GetCategory())
	}
}

// ---------- DoD gate ----------

func TestGRPC_TransitionDone_DoDGate_InvalidArgument(t *testing.T) {
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")

	ctx := mdOutCtx("acme", "agent:test", "issue:write", "issue:claim")

	createResp, err := clients.issue.CreateIssue(ctx, &cwbv1.CreateIssueRequest{
		Project:          "PROJ",
		Type:             "Story",
		Summary:          "dod test",
		DefinitionOfDone: "- [ ] unticked item",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	key := createResp.Issue.Key

	claimCtx := mdOutCtx("acme", "agent:test", "issue:claim")
	if _, err := clients.issue.ClaimIssue(claimCtx, &cwbv1.ClaimIssueRequest{Key: key}); err != nil {
		t.Fatalf("ClaimIssue: %v", err)
	}

	// transition In Progress → In Review
	if _, err := clients.issue.TransitionIssue(ctx, &cwbv1.TransitionIssueRequest{Key: key, Status: "In Review"}); err != nil {
		t.Fatalf("TransitionIssue to In Review: %v", err)
	}

	// Attempt transition to Done with unticked DoD — should be InvalidArgument
	_, err = clients.issue.TransitionIssue(ctx, &cwbv1.TransitionIssueRequest{Key: key, Status: "Done"})
	if grpcCode(err) != codes.InvalidArgument {
		t.Errorf("premature Done: code=%v, want InvalidArgument", grpcCode(err))
	}
}

func TestGRPC_ProjectWorkflow_SetGetRoundTrip(t *testing.T) {
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")

	writeCtx := mdOutCtx("acme", "agent:test", "issue:write")
	readCtx := mdOutCtx("acme", "agent:test", "issue:read")
	want := &cwbv1.Workflow{
		States: []*cwbv1.WorkflowState{
			{Name: "Open", Category: cwbv1.StatusCategory_STATUS_CATEGORY_DRAFT},
			{Name: "Closed", Category: cwbv1.StatusCategory_STATUS_CATEGORY_DONE, DodGate: true},
		},
		Transitions: []*cwbv1.WorkflowTransition{
			{From: "Open", To: []string{"Closed"}},
			{From: "Closed", To: []string{}},
		},
	}
	if _, err := clients.issue.SetProjectWorkflow(writeCtx, &cwbv1.SetProjectWorkflowRequest{Project: "PROJ", Workflow: want}); err != nil {
		t.Fatalf("SetProjectWorkflow: %v", err)
	}

	got, err := clients.issue.GetProjectWorkflow(readCtx, &cwbv1.GetProjectWorkflowRequest{Project: "PROJ"})
	if err != nil {
		t.Fatalf("GetProjectWorkflow: %v", err)
	}
	if !proto.Equal(got.Workflow, want) {
		t.Fatalf("workflow mismatch\ngot:  %v\nwant: %v", got.Workflow, want)
	}
}

func TestGRPC_ProjectWorkflow_DefaultFallback(t *testing.T) {
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")

	resp, err := clients.issue.GetProjectWorkflow(
		mdOutCtx("acme", "agent:test", "issue:read"),
		&cwbv1.GetProjectWorkflowRequest{Project: "PROJ"},
	)
	if err != nil {
		t.Fatalf("GetProjectWorkflow: %v", err)
	}
	if !proto.Equal(resp.Workflow, defaultWorkflow()) {
		t.Fatal("GetProjectWorkflow fallback did not return default workflow")
	}
}

func TestGRPC_GetIssue_PopulatesStatusCategory(t *testing.T) {
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")

	writeCtx := mdOutCtx("acme", "agent:test", "issue:write")
	readCtx := mdOutCtx("acme", "agent:test", "issue:read")
	createResp, err := clients.issue.CreateIssue(writeCtx, &cwbv1.CreateIssueRequest{
		Project:          "PROJ",
		Type:             "Story",
		Summary:          "category",
		DefinitionOfDone: "- [ ] something",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	getResp, err := clients.issue.GetIssue(readCtx, &cwbv1.GetIssueRequest{Key: createResp.Issue.Key})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if getResp.Issue.GetCategory() != cwbv1.StatusCategory_STATUS_CATEGORY_DRAFT {
		t.Fatalf("GetIssue category = %v, want DRAFT", getResp.Issue.GetCategory())
	}
}

func TestGRPC_SearchIssues_PopulatesStatusCategoryAcrossProjects(t *testing.T) {
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")
	if err := svc.CreateProject(
		ContextWithAuth(context.Background(), &AuthClaims{Sub: "agent:test", Org: "acme"}),
		Project{Key: "CUST", Name: "custom project", Organisation: "acme"},
	); err != nil {
		t.Fatalf("CreateProject CUST: %v", err)
	}

	writeCtx := mdOutCtx("acme", "agent:test", "issue:write")
	readCtx := mdOutCtx("acme", "agent:test", "issue:read")
	customWorkflow := &cwbv1.Workflow{
		States: []*cwbv1.WorkflowState{
			{Name: "To Do", Category: cwbv1.StatusCategory_STATUS_CATEGORY_READY},
			{Name: "In Progress", Category: cwbv1.StatusCategory_STATUS_CATEGORY_ACTIVE},
		},
		Transitions: []*cwbv1.WorkflowTransition{
			{From: "To Do", To: []string{"In Progress"}},
			{From: "In Progress", To: []string{}},
		},
	}
	if _, err := clients.issue.SetProjectWorkflow(writeCtx, &cwbv1.SetProjectWorkflowRequest{
		Project:  "CUST",
		Workflow: customWorkflow,
	}); err != nil {
		t.Fatalf("SetProjectWorkflow: %v", err)
	}

	defaultResp, err := clients.issue.CreateIssue(writeCtx, &cwbv1.CreateIssueRequest{
		Project:          "PROJ",
		Type:             "Story",
		Summary:          "default category",
		DefinitionOfDone: "- [ ] something",
	})
	if err != nil {
		t.Fatalf("CreateIssue default: %v", err)
	}
	customResp, err := clients.issue.CreateIssue(writeCtx, &cwbv1.CreateIssueRequest{
		Project:          "CUST",
		Type:             "Story",
		Summary:          "custom category",
		DefinitionOfDone: "- [ ] something",
	})
	if err != nil {
		t.Fatalf("CreateIssue custom: %v", err)
	}

	searchResp, err := clients.issue.SearchIssues(readCtx, &cwbv1.SearchIssuesRequest{
		Filter: &cwbv1.SearchFilter{Projects: []string{"PROJ", "CUST"}},
	})
	if err != nil {
		t.Fatalf("SearchIssues: %v", err)
	}
	got := map[string]cwbv1.StatusCategory{}
	for _, ref := range searchResp.GetRefs() {
		got[ref.GetKey()] = ref.GetCategory()
	}
	if got[defaultResp.Issue.Key] != cwbv1.StatusCategory_STATUS_CATEGORY_DRAFT {
		t.Fatalf("default issue category = %v, want DRAFT", got[defaultResp.Issue.Key])
	}
	if got[customResp.Issue.Key] != cwbv1.StatusCategory_STATUS_CATEGORY_READY {
		t.Fatalf("custom issue category = %v, want READY", got[customResp.Issue.Key])
	}
}

func TestGRPC_GetIssue_UnmappedStatusCategoryUnspecified(t *testing.T) {
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")

	writeCtx := mdOutCtx("acme", "agent:test", "issue:write")
	readCtx := mdOutCtx("acme", "agent:test", "issue:read")
	createResp, err := clients.issue.CreateIssue(writeCtx, &cwbv1.CreateIssueRequest{
		Project:          "PROJ",
		Type:             "Story",
		Summary:          "unknown category",
		DefinitionOfDone: "- [ ] something",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if _, err := svc.db.ExecContext(context.Background(),
		`UPDATE issues SET status = ? WHERE key = ?`,
		"Unmapped",
		createResp.Issue.Key,
	); err != nil {
		t.Fatalf("force unmapped status: %v", err)
	}

	getResp, err := clients.issue.GetIssue(readCtx, &cwbv1.GetIssueRequest{Key: createResp.Issue.Key})
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if getResp.Issue.GetCategory() != cwbv1.StatusCategory_STATUS_CATEGORY_UNSPECIFIED {
		t.Fatalf("GetIssue category = %v, want UNSPECIFIED", getResp.Issue.GetCategory())
	}
}

func TestGRPC_SetProjectWorkflow_RequiresWriteScope(t *testing.T) {
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")

	_, err := clients.issue.SetProjectWorkflow(
		mdOutCtx("acme", "agent:test", "issue:read"),
		&cwbv1.SetProjectWorkflowRequest{Project: "PROJ", Workflow: defaultWorkflow()},
	)
	if grpcCode(err) != codes.PermissionDenied {
		t.Fatalf("SetProjectWorkflow code = %v, want PermissionDenied", grpcCode(err))
	}
}

// ---------- claim conflict ----------

func TestGRPC_ClaimIssue_AlreadyClaimed_Aborted(t *testing.T) {
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")

	// Create a second user who will also try to claim.
	if _, err := svc.CreateUser(context.Background(), "agent:other", "ai"); err != nil {
		t.Fatalf("CreateUser other: %v", err)
	}
	if err := svc.AddOrgMember(context.Background(), "acme", "agent:other", "member"); err != nil {
		t.Fatalf("AddOrgMember other: %v", err)
	}

	writeCtx := mdOutCtx("acme", "agent:test", "issue:write")
	createResp, err := clients.issue.CreateIssue(writeCtx, &cwbv1.CreateIssueRequest{
		Project:          "PROJ",
		Type:             "Story",
		Summary:          "race target",
		DefinitionOfDone: "- [ ] something",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	key := createResp.Issue.Key

	// First claim by agent:test
	claimCtx1 := mdOutCtx("acme", "agent:test", "issue:claim")
	if _, err := clients.issue.ClaimIssue(claimCtx1, &cwbv1.ClaimIssueRequest{Key: key}); err != nil {
		t.Fatalf("first ClaimIssue: %v", err)
	}

	// Second claim by agent:other — should get Aborted
	claimCtx2 := mdOutCtx("acme", "agent:other", "issue:claim")
	_, err = clients.issue.ClaimIssue(claimCtx2, &cwbv1.ClaimIssueRequest{Key: key})
	if grpcCode(err) != codes.Aborted {
		t.Errorf("cross-claimer: code=%v, want Aborted", grpcCode(err))
	}
}

// ---------- cross-org isolation ----------

func TestGRPC_GetIssue_CrossOrg_NotFound(t *testing.T) {
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")

	writeCtx := mdOutCtx("acme", "agent:test", "issue:write")
	createResp, err := clients.issue.CreateIssue(writeCtx, &cwbv1.CreateIssueRequest{
		Project:          "PROJ",
		Type:             "Story",
		Summary:          "secret",
		DefinitionOfDone: "- [ ] something",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	key := createResp.Issue.Key

	// Setup a different org.
	if _, err := svc.CreateOrganisation(context.Background(), "evil", "Evil Inc"); err != nil {
		t.Fatalf("CreateOrganisation evil: %v", err)
	}
	if _, err := svc.CreateUser(context.Background(), "agent:evil", "ai"); err != nil {
		t.Fatalf("CreateUser evil: %v", err)
	}

	// Cross-org caller should get NotFound (hide-existence).
	crossCtx := mdOutCtx("evil", "agent:evil", "issue:read")
	_, err = clients.issue.GetIssue(crossCtx, &cwbv1.GetIssueRequest{Key: key})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("cross-org GetIssue: code=%v, want NotFound", grpcCode(err))
	}
}

func TestGRPC_ListComments_CrossOrg_NotFound(t *testing.T) {
	// ListComments calls Timeline internally. A cross-org caller who
	// knows an issue key must get NotFound (hide-existence), not the
	// comment bodies.
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")

	writeCtx := mdOutCtx("acme", "agent:test", "issue:write")
	createResp, err := clients.issue.CreateIssue(writeCtx, &cwbv1.CreateIssueRequest{
		Project:          "PROJ",
		Type:             "Story",
		Summary:          "secret issue",
		DefinitionOfDone: "- [ ] something",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	key := createResp.Issue.Key

	// Add a comment to make the timeline non-empty.
	if _, err := clients.issue.CommentIssue(writeCtx, &cwbv1.CommentIssueRequest{
		Key: key, Body: "sensitive comment",
	}); err != nil {
		t.Fatalf("CommentIssue: %v", err)
	}

	// Setup cross-org attacker.
	if _, err := svc.CreateOrganisation(context.Background(), "evil", "Evil Inc"); err != nil {
		t.Fatalf("CreateOrganisation evil: %v", err)
	}
	if _, err := svc.CreateUser(context.Background(), "agent:evil", "ai"); err != nil {
		t.Fatalf("CreateUser evil: %v", err)
	}

	// Cross-org caller must get NotFound, not the comment list.
	crossCtx := mdOutCtx("evil", "agent:evil", "issue:read")
	_, err = clients.issue.ListComments(crossCtx, &cwbv1.ListCommentsRequest{Key: key})
	if grpcCode(err) != codes.NotFound {
		t.Errorf("cross-org ListComments: code=%v, want NotFound", grpcCode(err))
	}
}

func TestGRPC_ListComments_SameOrg_Allowed(t *testing.T) {
	// Same-org caller can read comments — confirms the gate doesn't
	// over-block the happy path.
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")

	writeCtx := mdOutCtx("acme", "agent:test", "issue:write")
	createResp, err := clients.issue.CreateIssue(writeCtx, &cwbv1.CreateIssueRequest{
		Project:          "PROJ",
		Type:             "Story",
		Summary:          "with comments",
		DefinitionOfDone: "- [ ] something",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	key := createResp.Issue.Key
	if _, err := clients.issue.CommentIssue(writeCtx, &cwbv1.CommentIssueRequest{
		Key: key, Body: "hello",
	}); err != nil {
		t.Fatalf("CommentIssue: %v", err)
	}

	readCtx := mdOutCtx("acme", "agent:test", "issue:read")
	resp, err := clients.issue.ListComments(readCtx, &cwbv1.ListCommentsRequest{Key: key})
	if err != nil {
		t.Errorf("in-org ListComments: %v", err)
	}
	if len(resp.GetComments()) != 1 {
		t.Errorf("in-org ListComments: got %d comments, want 1", len(resp.GetComments()))
	}
}

// ---------- PurgeOrg ----------

func TestGRPC_PurgeOrg_CascadeAndIdempotent(t *testing.T) {
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "purgeme", "PROJ")

	writeCtx := mdOutCtx("purgeme", "agent:test", "issue:write")
	for _, sum := range []string{"issue1", "issue2"} {
		if _, err := clients.issue.CreateIssue(writeCtx, &cwbv1.CreateIssueRequest{
			Project:          "PROJ",
			Type:             "Story",
			Summary:          sum,
			DefinitionOfDone: "- [ ] something",
		}); err != nil {
			t.Fatalf("CreateIssue %s: %v", sum, err)
		}
	}

	purgeCtx := mdOutCtx("purgeme", "agent:test", "org:purge")
	resp, err := clients.org.PurgeOrg(purgeCtx, &cwbv1.OrgServicePurgeOrgRequest{})
	if err != nil {
		t.Fatalf("PurgeOrg: %v", err)
	}
	if resp.Purged != "purgeme" {
		t.Errorf("Purged=%q, want purgeme", resp.Purged)
	}

	// Idempotent: second call should succeed with no error.
	_, err = clients.org.PurgeOrg(purgeCtx, &cwbv1.OrgServicePurgeOrgRequest{})
	if err != nil {
		t.Fatalf("PurgeOrg (idempotent): %v", err)
	}
}

// ---------- scope matrix ----------

func TestGRPC_ScopeMatrix(t *testing.T) {
	clients, svc := newTestGRPCServer(t)
	setupTestOrg(t, svc, "acme", "PROJ")

	// Create an issue to operate on.
	writeCtx := mdOutCtx("acme", "agent:test", "issue:write")
	createResp, _ := clients.issue.CreateIssue(writeCtx, &cwbv1.CreateIssueRequest{
		Project:          "PROJ",
		Type:             "Story",
		Summary:          "scope test",
		DefinitionOfDone: "- [ ] something",
	})
	var key string
	if createResp != nil && createResp.Issue != nil {
		key = createResp.Issue.Key
	}

	tests := []struct {
		name     string
		call     func() error
		scope    string
		wantCode codes.Code
	}{
		{
			name: "GetIssue requires issue:read",
			call: func() error {
				_, err := clients.issue.GetIssue(mdOutCtx("acme", "agent:test", "issue:write"), &cwbv1.GetIssueRequest{Key: key})
				return err
			},
			wantCode: codes.PermissionDenied,
		},
		{
			name: "CreateIssue requires issue:write",
			call: func() error {
				_, err := clients.issue.CreateIssue(mdOutCtx("acme", "agent:test", "issue:read"), &cwbv1.CreateIssueRequest{})
				return err
			},
			wantCode: codes.PermissionDenied,
		},
		{
			name: "ClaimIssue requires issue:claim",
			call: func() error {
				_, err := clients.issue.ClaimIssue(mdOutCtx("acme", "agent:test", "issue:write"), &cwbv1.ClaimIssueRequest{Key: key})
				return err
			},
			wantCode: codes.PermissionDenied,
		},
		{
			name: "CreateProject requires issue:admin",
			call: func() error {
				_, err := clients.project.CreateProject(mdOutCtx("acme", "agent:test", "issue:write"), &cwbv1.CreateProjectRequest{Key: "X"})
				return err
			},
			wantCode: codes.PermissionDenied,
		},
		{
			name: "PurgeOrg requires org:purge",
			call: func() error {
				_, err := clients.org.PurgeOrg(mdOutCtx("acme", "agent:test", "issue:admin"), &cwbv1.OrgServicePurgeOrgRequest{})
				return err
			},
			wantCode: codes.PermissionDenied,
		},
		{
			name: "AdminCreateOrg requires issue:admin",
			call: func() error {
				_, err := clients.admin.CreateOrg(mdOutCtx("acme", "agent:test", "issue:read"), &cwbv1.CreateOrgRequest{Slug: "x"})
				return err
			},
			wantCode: codes.PermissionDenied,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if grpcCode(err) != tc.wantCode {
				t.Errorf("code=%v, want %v (err=%v)", grpcCode(err), tc.wantCode, err)
			}
		})
	}
}

// ---------- issue:admin superset (not for org:purge) ----------

func TestGRPC_IssueAdmin_NotOrgPurge(t *testing.T) {
	clients, _ := newTestGRPCServer(t)
	// issue:admin does NOT satisfy org:purge
	ctx := mdOutCtx("acme", "agent:test", "issue:admin")
	_, err := clients.org.PurgeOrg(ctx, &cwbv1.OrgServicePurgeOrgRequest{})
	if grpcCode(err) != codes.PermissionDenied {
		t.Errorf("issue:admin for org:purge: code=%v, want PermissionDenied", grpcCode(err))
	}
}

// ---------- missing identity ----------

func TestGRPC_MissingIdentity_Unauthenticated(t *testing.T) {
	clients, _ := newTestGRPCServer(t)
	_, err := clients.issue.GetIssue(context.Background(), &cwbv1.GetIssueRequest{Key: "X-1"})
	if grpcCode(err) != codes.Unauthenticated {
		t.Errorf("no metadata: code=%v, want Unauthenticated", grpcCode(err))
	}
}

// ---------- admin service round-trip ----------

func TestGRPC_Admin_OrgUserMemberLifecycle(t *testing.T) {
	clients, _ := newTestGRPCServer(t)
	adminCtx := mdOutCtx("sys", "admin:op", "issue:admin")

	// CreateOrg
	orgResp, err := clients.admin.CreateOrg(adminCtx, &cwbv1.CreateOrgRequest{Slug: "testorg", Name: "Test Org"})
	if err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}
	if orgResp.Org.Slug != "testorg" {
		t.Errorf("Org.Slug=%q, want testorg", orgResp.Org.Slug)
	}

	// CreateUser
	userResp, err := clients.admin.CreateUser(adminCtx, &cwbv1.CreateUserRequest{Id: "u:alice", Kind: "human"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if userResp.User.Id != "u:alice" {
		t.Errorf("User.Id=%q, want u:alice", userResp.User.Id)
	}

	// AddMember
	if _, err := clients.admin.AddMember(adminCtx, &cwbv1.AddMemberRequest{Slug: "testorg", UserId: "u:alice", Role: "member"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	// ListMembers
	memberResp, err := clients.admin.ListMembers(adminCtx, &cwbv1.ListMembersRequest{Slug: "testorg"})
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(memberResp.Members) != 1 {
		t.Errorf("ListMembers count=%d, want 1", len(memberResp.Members))
	}

	// RemoveMember
	if _, err := clients.admin.RemoveMember(adminCtx, &cwbv1.RemoveMemberRequest{Slug: "testorg", UserId: "u:alice"}); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	// DeleteUser
	if _, err := clients.admin.DeleteUser(adminCtx, &cwbv1.DeleteUserRequest{Id: "u:alice"}); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	// DeleteOrg
	if _, err := clients.admin.DeleteOrg(adminCtx, &cwbv1.DeleteOrgRequest{Slug: "testorg"}); err != nil {
		t.Fatalf("DeleteOrg: %v", err)
	}
}
