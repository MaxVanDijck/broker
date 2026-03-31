package executor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"

	pb "broker/proto/agentpb"
)

type LogSink func(jobID string, batch *pb.LogBatch)

type Job struct {
	ID      string
	Setup   string
	Run     string
	Workdir string
	Env     map[string]string

	cmd    *exec.Cmd
	cancel context.CancelFunc
}

type Executor struct {
	logger  *slog.Logger
	logSink LogSink

	mu   sync.Mutex
	jobs map[string]*Job
}

func New(logger *slog.Logger, sink LogSink) *Executor {
	return &Executor{
		logger:  logger,
		logSink: sink,
		jobs:    make(map[string]*Job),
	}
}

func (e *Executor) Submit(ctx context.Context, msg *pb.SubmitJob, updateFn func(*pb.JobUpdate)) {
	job := &Job{
		ID:      msg.JobId,
		Setup:   msg.SetupScript,
		Run:     msg.RunScript,
		Workdir: msg.Workdir,
		Env:     msg.Env,
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
		e.mu.Lock()
		delete(e.jobs, job.ID)
		e.mu.Unlock()
	}()

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
