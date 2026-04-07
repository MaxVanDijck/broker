package executor

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"

	"broker/internal/workdir"
	pb "broker/proto/agentpb"
)

type LogSink func(jobID string, batch *pb.LogBatch)

type Job struct {
	ID        string
	Setup     string
	Run       string
	Workdir   string
	WorkdirID string
	Env       map[string]string

	cmd    *exec.Cmd
	cancel context.CancelFunc
}

type Executor struct {
	logger    *slog.Logger
	logSink   LogSink
	serverURL string // HTTP base URL for downloading workdirs
	token     string // auth token for server requests

	mu   sync.Mutex
	jobs map[string]*Job
}

func New(logger *slog.Logger, sink LogSink, serverURL string, token string) *Executor {
	return &Executor{
		logger:    logger,
		logSink:   sink,
		serverURL: serverURL,
		token:     token,
		jobs:      make(map[string]*Job),
	}
}

func (e *Executor) Submit(ctx context.Context, msg *pb.SubmitJob, updateFn func(*pb.JobUpdate)) {
	job := &Job{
		ID:        msg.JobId,
		Setup:     msg.SetupScript,
		Run:       msg.RunScript,
		Workdir:   msg.Workdir,
		WorkdirID: msg.WorkdirId,
		Env:       msg.Env,
	}

	e.mu.Lock()
	e.jobs[job.ID] = job
	e.mu.Unlock()

	go e.run(ctx, job, updateFn)
}

func (e *Executor) Cancel(jobID string, force bool) {
	e.mu.Lock()
	job, ok := e.jobs[jobID]
	e.mu.Unlock()

	if !ok {
		return
	}

	if job.cancel != nil {
		job.cancel()
	}
	if force && job.cmd != nil && job.cmd.Process != nil {
		job.cmd.Process.Kill()
	}
}

func (e *Executor) RunningJobIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	ids := make([]string, 0, len(e.jobs))
	for id := range e.jobs {
		ids = append(ids, id)
	}
	return ids
}

func (e *Executor) run(parent context.Context, job *Job, updateFn func(*pb.JobUpdate)) {
	ctx, cancel := context.WithCancel(parent)
	job.cancel = cancel
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("job execution panicked", "job_id", job.ID, "panic", r)
			updateFn(&pb.JobUpdate{
				JobId: job.ID,
				State: pb.JobState_JOB_STATE_FAILED,
				Error: fmt.Sprintf("internal panic: %v", r),
			})
		}
		e.mu.Lock()
		delete(e.jobs, job.ID)
		e.mu.Unlock()
	}()

	// Download and extract workdir if specified
	if job.WorkdirID != "" {
		updateFn(&pb.JobUpdate{JobId: job.ID, State: pb.JobState_JOB_STATE_PULLING})
		workdirPath, err := e.downloadWorkdir(job.WorkdirID)
		if err != nil {
			updateFn(&pb.JobUpdate{
				JobId: job.ID,
				State: pb.JobState_JOB_STATE_FAILED,
				Error: fmt.Sprintf("download workdir: %v", err),
			})
			return
		}
		job.Workdir = workdirPath
		e.logger.Info("workdir extracted", "job_id", job.ID, "path", workdirPath)
	}

	if job.Setup != "" {
		updateFn(&pb.JobUpdate{JobId: job.ID, State: pb.JobState_JOB_STATE_SETUP})
		if err := e.execScript(ctx, job, job.Setup); err != nil {
			updateFn(&pb.JobUpdate{
				JobId: job.ID,
				State: pb.JobState_JOB_STATE_FAILED,
				Error: fmt.Sprintf("setup failed: %v", err),
			})
			return
		}
	}

	if job.Run == "" {
		updateFn(&pb.JobUpdate{JobId: job.ID, State: pb.JobState_JOB_STATE_SUCCEEDED})
		return
	}

	updateFn(&pb.JobUpdate{JobId: job.ID, State: pb.JobState_JOB_STATE_RUNNING})
	if err := e.execScript(ctx, job, job.Run); err != nil {
		exitCode := int32(1)
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		}
		if ctx.Err() != nil {
			updateFn(&pb.JobUpdate{JobId: job.ID, State: pb.JobState_JOB_STATE_CANCELLED})
			return
		}
		updateFn(&pb.JobUpdate{
			JobId:    job.ID,
			State:    pb.JobState_JOB_STATE_FAILED,
			ExitCode: exitCode,
			Error:    err.Error(),
		})
		return
	}

	updateFn(&pb.JobUpdate{JobId: job.ID, State: pb.JobState_JOB_STATE_SUCCEEDED})
}

func (e *Executor) execScript(ctx context.Context, job *Job, script string) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)

	if job.Workdir != "" {
		cmd.Dir = job.Workdir
	}

	cmd.Env = os.Environ()
	for k, v := range job.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("start pty: %w", err)
	}
	defer ptmx.Close()

	job.cmd = cmd

	go e.streamOutput(job.ID, "stdout", ptmx)

	return cmd.Wait()
}

func (e *Executor) streamOutput(jobID string, stream string, r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			e.logSink(jobID, &pb.LogBatch{
				JobId: jobID,
				Entries: []*pb.LogEntry{{
					TimestampUnixNano: time.Now().UnixNano(),
					Stream:            stream,
					Data:              data,
				}},
			})
		}
		if err != nil {
			return
		}
	}
}

func (e *Executor) downloadWorkdir(id string) (string, error) {
	httpBase := e.serverURL
	httpBase = strings.Replace(httpBase, "wss://", "https://", 1)
	httpBase = strings.Replace(httpBase, "ws://", "http://", 1)

	url := httpBase + "/api/v1/workdir/" + id
	e.logger.Info("downloading workdir", "url", url)

	const maxRetries = 5
	const retryDelay = 3 * time.Second

	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			e.logger.Warn("retrying workdir download", "attempt", attempt+1, "url", url)
			time.Sleep(retryDelay)
		}

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return "", fmt.Errorf("create request: %w", err)
		}
		if e.token != "" {
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("broker:"+e.token)))
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("download %s: %w", url, err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("download %s: status %d", url, resp.StatusCode)
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				return "", lastErr
			}
			continue
		}

		targetDir := filepath.Join(os.TempDir(), "broker-workdir-"+id)
		if err := workdir.Extract(resp.Body, targetDir); err != nil {
			resp.Body.Close()
			lastErr = fmt.Errorf("extract: %w", err)
			continue
		}
		resp.Body.Close()

		return targetDir, nil
	}

	return "", fmt.Errorf("workdir download failed after %d attempts: %w", maxRetries, lastErr)
}
