package executor

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	pb "broker/proto/agentpb"
)

func newTestExecutor(t *testing.T) (*Executor, chan *pb.JobUpdate, chan *pb.LogBatch) {
	t.Helper()
	updates := make(chan *pb.JobUpdate, 100)
	logs := make(chan *pb.LogBatch, 1000)

	sink := func(jobID string, batch *pb.LogBatch) {
		logs <- batch
	}

	e := New(slog.Default(), sink, "http://localhost:8080")
	return e, updates, logs
}

func collectUpdates(updates chan *pb.JobUpdate, done chan struct{}) []*pb.JobUpdate {
	var result []*pb.JobUpdate
	for {
		select {
		case u := <-updates:
			result = append(result, u)
		case <-done:
			for {
				select {
				case u := <-updates:
					result = append(result, u)
				default:
					return result
				}
			}
		}
	}
}

func TestExecutor_SetupAndRun(t *testing.T) {
	t.Run("given a job with setup and run scripts, when submitted, then state transitions are SETUP RUNNING SUCCEEDED in order", func(t *testing.T) {
		e, _, _ := newTestExecutor(t)
		ctx := context.Background()

		var mu sync.Mutex
		var updates []*pb.JobUpdate
		done := make(chan struct{})

		updateFn := func(u *pb.JobUpdate) {
			mu.Lock()
			updates = append(updates, u)
			mu.Unlock()
			if u.State == pb.JobState_JOB_STATE_SUCCEEDED ||
				u.State == pb.JobState_JOB_STATE_FAILED ||
				u.State == pb.JobState_JOB_STATE_CANCELLED {
				close(done)
			}
		}

		msg := &pb.SubmitJob{
			JobId:       "j-setup-run",
			SetupScript: "echo setup-phase",
			RunScript:   "echo run-phase",
		}

		e.Submit(ctx, msg, updateFn)

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for job to complete")
		}

		mu.Lock()
		defer mu.Unlock()

		if len(updates) != 3 {
			states := make([]string, len(updates))
			for i, u := range updates {
				states[i] = u.State.String()
			}
			t.Fatalf("expected 3 updates [SETUP, RUNNING, SUCCEEDED], got %d: %v", len(updates), states)
		}

		expected := []pb.JobState{
			pb.JobState_JOB_STATE_SETUP,
			pb.JobState_JOB_STATE_RUNNING,
			pb.JobState_JOB_STATE_SUCCEEDED,
		}
		for i, want := range expected {
			if updates[i].State != want {
				t.Errorf("update[%d]: expected %s, got %s", i, want, updates[i].State)
			}
			if updates[i].JobId != "j-setup-run" {
				t.Errorf("update[%d]: expected job_id j-setup-run, got %s", i, updates[i].JobId)
			}
		}
	})
}

func TestExecutor_RunOnlySucceeds(t *testing.T) {
	t.Run("given a job with only a run script, when submitted, then state transitions are RUNNING SUCCEEDED", func(t *testing.T) {
		e, _, _ := newTestExecutor(t)
		ctx := context.Background()

		var mu sync.Mutex
		var updates []*pb.JobUpdate
		done := make(chan struct{})

		updateFn := func(u *pb.JobUpdate) {
			mu.Lock()
			updates = append(updates, u)
			mu.Unlock()
			if u.State == pb.JobState_JOB_STATE_SUCCEEDED ||
				u.State == pb.JobState_JOB_STATE_FAILED ||
				u.State == pb.JobState_JOB_STATE_CANCELLED {
				close(done)
			}
		}

		msg := &pb.SubmitJob{
			JobId:     "j-run-only",
			RunScript: "echo hello",
		}

		e.Submit(ctx, msg, updateFn)

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for job to complete")
		}

		mu.Lock()
		defer mu.Unlock()

		if len(updates) != 2 {
			states := make([]string, len(updates))
			for i, u := range updates {
				states[i] = u.State.String()
			}
			t.Fatalf("expected 2 updates [RUNNING, SUCCEEDED], got %d: %v", len(updates), states)
		}

		if updates[0].State != pb.JobState_JOB_STATE_RUNNING {
			t.Errorf("update[0]: expected RUNNING, got %s", updates[0].State)
		}
		if updates[1].State != pb.JobState_JOB_STATE_SUCCEEDED {
			t.Errorf("update[1]: expected SUCCEEDED, got %s", updates[1].State)
		}
	})
}

func TestExecutor_FailedRunScript(t *testing.T) {
	t.Run("given a job with a failing run script, when submitted, then state is FAILED with exit code", func(t *testing.T) {
		e, _, _ := newTestExecutor(t)
		ctx := context.Background()

		var mu sync.Mutex
		var updates []*pb.JobUpdate
		done := make(chan struct{})

		updateFn := func(u *pb.JobUpdate) {
			mu.Lock()
			updates = append(updates, u)
			mu.Unlock()
			if u.State == pb.JobState_JOB_STATE_SUCCEEDED ||
				u.State == pb.JobState_JOB_STATE_FAILED ||
				u.State == pb.JobState_JOB_STATE_CANCELLED {
				close(done)
			}
		}

		msg := &pb.SubmitJob{
			JobId:     "j-fail",
			RunScript: "exit 42",
		}

		e.Submit(ctx, msg, updateFn)

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for job to complete")
		}

		mu.Lock()
		defer mu.Unlock()

		if len(updates) < 2 {
			t.Fatalf("expected at least 2 updates, got %d", len(updates))
		}

		last := updates[len(updates)-1]
		if last.State != pb.JobState_JOB_STATE_FAILED {
			t.Errorf("expected final state FAILED, got %s", last.State)
		}
		if last.ExitCode != 42 {
			t.Errorf("expected exit code 42, got %d", last.ExitCode)
		}
		if last.Error == "" {
			t.Error("expected non-empty error message on failure")
		}
	})
}

func TestExecutor_CancelMidExecution(t *testing.T) {
	t.Run("given a long-running job, when cancelled mid-execution, then state is CANCELLED", func(t *testing.T) {
		e, _, _ := newTestExecutor(t)
		ctx := context.Background()

		var mu sync.Mutex
		var updates []*pb.JobUpdate
		done := make(chan struct{})
		running := make(chan struct{})

		updateFn := func(u *pb.JobUpdate) {
			mu.Lock()
			updates = append(updates, u)
			mu.Unlock()
			if u.State == pb.JobState_JOB_STATE_RUNNING {
				select {
				case <-running:
				default:
					close(running)
				}
			}
			if u.State == pb.JobState_JOB_STATE_SUCCEEDED ||
				u.State == pb.JobState_JOB_STATE_FAILED ||
				u.State == pb.JobState_JOB_STATE_CANCELLED {
				close(done)
			}
		}

		msg := &pb.SubmitJob{
			JobId:     "j-cancel",
			RunScript: "sleep 10",
		}

		e.Submit(ctx, msg, updateFn)

		select {
		case <-running:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for job to reach RUNNING state")
		}

		time.Sleep(100 * time.Millisecond)
		e.Cancel("j-cancel", false)

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for job to finish after cancel")
		}

		mu.Lock()
		defer mu.Unlock()

		last := updates[len(updates)-1]
		if last.State != pb.JobState_JOB_STATE_CANCELLED {
			t.Errorf("expected final state CANCELLED, got %s", last.State)
		}
	})
}

func TestExecutor_WorkdirDownloadFails(t *testing.T) {
	t.Run("given a job with workdir_id and no server, when submitted, then state is FAILED with download error", func(t *testing.T) {
		updates := make(chan *pb.JobUpdate, 100)
		logs := make(chan *pb.LogBatch, 1000)

		sink := func(jobID string, batch *pb.LogBatch) {
			logs <- batch
		}

		// Point to a server that doesn't exist
		e := New(slog.Default(), sink, "http://127.0.0.1:1")

		ctx := context.Background()

		var mu sync.Mutex
		var collected []*pb.JobUpdate
		done := make(chan struct{})

		updateFn := func(u *pb.JobUpdate) {
			mu.Lock()
			collected = append(collected, u)
			mu.Unlock()
			select {
			case updates <- u:
			default:
			}
			if u.State == pb.JobState_JOB_STATE_SUCCEEDED ||
				u.State == pb.JobState_JOB_STATE_FAILED ||
				u.State == pb.JobState_JOB_STATE_CANCELLED {
				close(done)
			}
		}

		msg := &pb.SubmitJob{
			JobId:     "j-workdir-fail",
			RunScript: "echo should-not-run",
			WorkdirId: "nonexistent-workdir-id",
		}

		e.Submit(ctx, msg, updateFn)

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for job to complete")
		}

		mu.Lock()
		defer mu.Unlock()

		if len(collected) < 2 {
			t.Fatalf("expected at least 2 updates (PULLING, FAILED), got %d", len(collected))
		}

		if collected[0].State != pb.JobState_JOB_STATE_PULLING {
			t.Errorf("update[0]: expected PULLING, got %s", collected[0].State)
		}

		last := collected[len(collected)-1]
		if last.State != pb.JobState_JOB_STATE_FAILED {
			t.Errorf("expected final state FAILED, got %s", last.State)
		}
		if last.Error == "" {
			t.Error("expected non-empty error message on workdir download failure")
		}
		hasDownloadRef := false
		lower := strings.ToLower(last.Error)
		if strings.Contains(lower, "download") {
			hasDownloadRef = true
		}
		if !hasDownloadRef {
			t.Errorf("expected error to reference 'download', got: %s", last.Error)
		}
	})
}

func TestExecutor_FailedSetupScript(t *testing.T) {
	t.Run("given a job with failing setup script, when submitted, then state is FAILED and run is never reached", func(t *testing.T) {
		e, _, _ := newTestExecutor(t)
		ctx := context.Background()

		var mu sync.Mutex
		var updates []*pb.JobUpdate
		done := make(chan struct{})

		updateFn := func(u *pb.JobUpdate) {
			mu.Lock()
			updates = append(updates, u)
			mu.Unlock()
			if u.State == pb.JobState_JOB_STATE_SUCCEEDED ||
				u.State == pb.JobState_JOB_STATE_FAILED ||
				u.State == pb.JobState_JOB_STATE_CANCELLED {
				close(done)
			}
		}

		msg := &pb.SubmitJob{
			JobId:       "j-setup-fail",
			SetupScript: "exit 1",
			RunScript:   "echo should-not-run",
		}

		e.Submit(ctx, msg, updateFn)

		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for job to complete")
		}

		mu.Lock()
		defer mu.Unlock()

		if len(updates) != 2 {
			states := make([]string, len(updates))
			for i, u := range updates {
				states[i] = u.State.String()
			}
			t.Fatalf("expected 2 updates [SETUP, FAILED], got %d: %v", len(updates), states)
		}

		if updates[0].State != pb.JobState_JOB_STATE_SETUP {
			t.Errorf("update[0]: expected SETUP, got %s", updates[0].State)
		}
		if updates[1].State != pb.JobState_JOB_STATE_FAILED {
			t.Errorf("update[1]: expected FAILED, got %s", updates[1].State)
		}
		if updates[1].Error == "" {
			t.Error("expected non-empty error on setup failure")
		}
	})
}
