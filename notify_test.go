package ledger

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
)

type captureNotifier struct {
	mu        sync.Mutex
	aspectMsg map[string][]string
	opStream  []string
}

func (n *captureNotifier) NotifyAspect(_ context.Context, aspect, msg string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.aspectMsg == nil {
		n.aspectMsg = map[string][]string{}
	}
	n.aspectMsg[aspect] = append(n.aspectMsg[aspect], msg)
	return nil
}

func (n *captureNotifier) NotifyOperatorStream(_ context.Context, msg string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.opStream = append(n.opStream, msg)
	return nil
}

func newTestServiceWithNotifier(t *testing.T, n Notifier) *Service {
	t.Helper()
	svc, err := New(context.Background(), Config{DBPath: filepath.Join(t.TempDir(), "ledger.db"), Notifier: n})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestAssign_PushesNotification(t *testing.T) {
	ctx := context.Background()
	n := &captureNotifier{}
	svc := newTestServiceWithNotifier(t, n)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.AssignIssue(ctx, issue.Key, "anvil", "", "shadow")
	if len(n.aspectMsg["anvil"]) != 1 {
		t.Errorf("anvil should have 1 notification; got %v", n.aspectMsg)
	}
	if len(n.opStream) == 0 {
		t.Errorf("operator stream should have an entry; got %v", n.opStream)
	}
}

func TestComment_NotifiesMentions(t *testing.T) {
	ctx := context.Background()
	n := &captureNotifier{}
	svc := newTestServiceWithNotifier(t, n)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.CommentIssue(ctx, issue.Key, "shadow", "ping @anvil and @plumb")
	if len(n.aspectMsg["anvil"]) != 1 || len(n.aspectMsg["plumb"]) != 1 {
		t.Errorf("mention notifications missing: %v", n.aspectMsg)
	}
}

func TestTransition_NotifiesWatchersOnBlocker(t *testing.T) {
	ctx := context.Background()
	n := &captureNotifier{}
	svc := newTestServiceWithNotifier(t, n)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.AssignIssue(ctx, issue.Key, "anvil", "", "shadow")
	_ = svc.WatchIssue(ctx, issue.Key, "plumb", "plumb")

	// Transition to In Progress then Blocked — watchers should get notified.
	_ = svc.TransitionIssue(ctx, issue.Key, "In Progress", "anvil")
	if err := svc.TransitionIssue(ctx, issue.Key, "Blocked", "anvil"); err != nil {
		t.Fatal(err)
	}
	if len(n.aspectMsg["plumb"]) == 0 {
		t.Errorf("plumb watcher should have received blocker notification; got %v", n.aspectMsg)
	}
}

func TestTransition_OperatorStreamPopulated(t *testing.T) {
	ctx := context.Background()
	n := &captureNotifier{}
	svc := newTestServiceWithNotifier(t, n)
	defer svc.Close()
	_ = svc.CreateProject(ctx, Project{Key: "NEX", Name: "Nexus"})
	issue, err := svc.CreateIssue(ctx, IssueDraft{
		Project: "NEX", Type: "Story", Summary: "x",
		DefinitionOfDone: "- [ ] go", Reporter: "shadow",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.TransitionIssue(ctx, issue.Key, "In Progress", "anvil")
	if len(n.opStream) < 1 {
		t.Errorf("operator stream should have transition event; got %v", n.opStream)
	}
}
