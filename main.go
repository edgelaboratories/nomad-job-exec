package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"slice"
	"time"

	"github.com/google/shlex"
	"github.com/hashicorp/nomad/api"
	"github.com/neilotoole/errgroup"
)

func main() { //nolint: cyclop, funlen
	jobID := flag.String("job", "", "Job ID")
	taskID := flag.String("task", "", "Task ID")
	namespace := flag.String("namespace", "default", "Namespace")
	cmd := flag.String("command", "", "Command to execute on allocations")
	timeout := flag.String("timeout", "10m", "Timeout for the command to execute on all allocations")
	parallel := flag.Bool("parallel", false, "Execute in parallel if true")
	logLevel := flag.String("log-level", "info", "Log level")
	flag.Parse()

	// Configure logger

	debug := slog.Level(0)
	err := debug.UnmarshalText([]byte(*logLevel))
	if err != nil {
		log.Fatalf("failed to parse log level: %v", err)
	}

	lvl := new(slog.LevelVar)
	lvl.Set(debug)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
	}))
	slog.SetDefault(logger)

	// Check inputs

	if *jobID == "" {
		slog.Error("job ID cannot be empty")
		os.Exit(1)
	}

	if ns, ok := os.LookupEnv("NOMAD_NAMESPACE"); ok {
		*namespace = ns
	}

	splitCommand, err := shlex.Split(*cmd)
	if err != nil {
		slog.Error("failed to shlex the input", "command", cmd, "error", err)
		os.Exit(1)
	}

	timeoutDuration, err := time.ParseDuration(*timeout)
	if err != nil {
		slog.Error("failed to parse the timeout duration", "error", err)
		os.Exit(1)
	}

	// Create Nomad API client

	apiClient, err := api.NewClient(&api.Config{
		Address:   os.Getenv("NOMAD_ADDR"),
		SecretID:  os.Getenv("NOMAD_TOKEN"),
		TLSConfig: &api.TLSConfig{},
	})
	if err != nil {
		slog.Error("failed to create the Nomad API client", "error", err)
		os.Exit(1)
	}

	client := &apiClientWrap{
		apiClient,
	}

	// Get the information about the job's running allocations

	jobLogger := logger.With(
		slog.String("job_id", *jobID),
		slog.String("namespace", *namespace),
	)

	jobLogger.Info("listing all allocations for job")

	allocationsInfo, err := getAllocationsInfo(client, *jobID, *taskID, *namespace)
	if err != nil {
		slog.Error("failed to get the list of allocations", "error", err)
		os.Exit(1)
	}

	nbAllocs := len(allocationsInfo)
	if nbAllocs == 0 {
		jobLogger.Error("no allocations found for job")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)

	if *parallel {
		concurrency := nbAllocs

		slog.Info("running command", "command", *cmd, "allocations", nbAllocs, "concurrency", concurrency)

		if err := executeConcurrently(ctx, jobLogger, client, allocationsInfo, *namespace, splitCommand, concurrency); err != nil {
			cancel()

			slog.Error("failed to concurrently execute command on allocations", "error", err)
			os.Exit(1)
		}

		slog.Info("command ran successfully", "command", *cmd, "allocations", nbAllocs, "concurrency", concurrency)
		os.Exit(0)
	}

	slog.Info("running command", "command", *cmd, "allocations", nbAllocs, "concurrency", 1)

	if err := executeSequentially(ctx, jobLogger, client, allocationsInfo, *namespace, splitCommand); err != nil {
		cancel()

		slog.Error("failed to sequentially execute command on allocations", "error", err)
		os.Exit(1)
	}

	slog.Info("command ran successfully", "command", *cmd, "allocations", nbAllocs, "concurrency", 1)
}

func executeSequentially(ctx context.Context, logger *slog.Logger, c client, allocsInfo []*allocInfo, namespace string, cmd []string) error {
	for _, allocInfo := range allocsInfo {
		select {
		case <-ctx.Done():
			break

		default:
			logger := logger.With(
				slog.String("allocation_id", allocInfo.alloc.ID),
				slog.String("task_id", allocInfo.task),
				slog.String("group_id", allocInfo.alloc.TaskGroup),
			)

			allocInfo := allocInfo

			logger.Info("executing command")

			output, err := allocationExec(ctx, c, allocInfo, namespace, cmd)
			if err != nil {
				return fmt.Errorf("failed to execute command: %w", err)
			}

			logger.Info("command executed")

			if err := consumeExecOutput(output); err != nil {
				return fmt.Errorf("failed to consume command output: %w", err)
			}

			if output.exitCode != 0 {
				return fmt.Errorf("command failed with code %d", output.exitCode)
			}
		}
	}

	return nil
}

func executeConcurrently(ctx context.Context, logger *slog.Logger, c client, allocsInfo []*allocInfo, namespace string, cmd []string, concurrency int) error { //nolint: funlen
	nbAllocs := len(allocsInfo)

	execOutputCh := make(chan *execOutput, nbAllocs)
	done := make(chan bool, 1)

	var exitCode int
	go func(exitCode *int) {
		defer func() {
			done <- true
		}()

		for output := range execOutputCh {
			if err := consumeExecOutput(output); err != nil {
				logger.Error("failed to consume command output", "error", err)
			}

			if output.exitCode != 0 {
				*exitCode = output.exitCode
			}
		}
	}(&exitCode)

	eg, ctx := errgroup.WithContextN(ctx, concurrency, concurrency)

	for _, allocInfo := range allocsInfo {
		select {
		case <-ctx.Done():
			break

		default:
			allocInfo := allocInfo

			logger := logger.With(
				slog.String("allocation_id", allocInfo.alloc.ID),
				slog.String("task_id", allocInfo.task),
				slog.String("group_id", allocInfo.alloc.TaskGroup),
			)

			eg.Go(func() error {
				logger.Info("executing command")
				defer logger.Info("command executed")

				output, err := allocationExec(ctx, c, allocInfo, namespace, cmd)
				if err != nil {
					return err
				}

				execOutputCh <- output

				return nil
			})
		}
	}

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("failed to exec on all the allocations: %w", err)
	}

	close(execOutputCh)
	<-done

	if exitCode != 0 {
		return fmt.Errorf("command failed with code %d", exitCode)
	}

	return nil
}

func consumeExecOutput(out *execOutput) error {
	logger := slog.With(slog.String("allocation_id", out.allocID))

	if len(out.stderr) != 0 {
		logger.Debug("flushing command output to stderr")

		if _, err := io.WriteString(
			os.Stderr,
			fmt.Sprintf("[AllocID: %s]\n%s", out.allocID, out.stderr),
		); err != nil {
			return fmt.Errorf("failed to copy to stderr: %w", err)
		}
	}

	if len(out.stdout) != 0 {
		logger.Debug("flushing command output to stdout")

		if _, err := io.WriteString(
			os.Stdout,
			fmt.Sprintf("[AllocID: %s]\n%s", out.allocID, out.stdout),
		); err != nil {
			return fmt.Errorf("failed to copy to stdout: %w", err)
		}
	}

	return nil
}

type client interface {
	jobAllocations(jobID, namespace string) ([]*api.AllocationListStub, error)
	allocationInfo(allocID, namespace string) (*api.Allocation, error)
	allocationExec(ctx context.Context, alloc *api.Allocation, task, namespace string, cmd []string) (*execOutput, error)
}

type allocInfo struct {
	alloc *api.Allocation
	task  string
}

func getAllocationsInfo(c client, jobID, taskID, namespace string) ([]*allocInfo, error) {
	allocations, err := c.jobAllocations(jobID, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get the list of allocations for job %s: %w", jobID, err)
	}

	res := []*allocInfo{}
	for _, alloc := range allocations {
		if alloc.ClientStatus != "running" {
			continue
		}

		alloc, err := c.allocationInfo(alloc.ID, namespace)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve allocation %s info: %w", alloc.ID, err)
		}

		if taskID != "" {
			// If task is given, the task is known, so we filter out allocations
			// that don't have this specific task.

			tasks, err := getAllTasks(alloc)
			if err != nil {
				return nil, fmt.Errorf("failed to retrieve tasks for allocation %s", alloc.ID)
			}

			if slice.Contains(tasks, taskID) {
				res = append(res, &allocInfo{
					alloc: alloc,
					task:  taskID,
				})
			}

			continue
		}

		// The task was not provided so we must retrieve it

		task, err := retrieveTask(alloc)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve task name for allocation %s: %w", alloc.ID, err)
		}

		res = append(res, &allocInfo{
			alloc: alloc,
			task:  task,
		})
	}

	return res, nil
}

func retrieveTask(alloc *api.Allocation) (string, error) {
	taskGroup := alloc.Job.LookupTaskGroup(alloc.TaskGroup)
	if taskGroup == nil {
		return "", errors.New("failed to retrieve task group")
	}

	tasks, err := getAllTasks(alloc)
	if err != nil {
		return "", errors.New("failed to retrieve tasks")
	}

	if nbTasks := len(tasks); nbTasks != 1 {
		return "", fmt.Errorf("found %d tasks, please specify the task name", nbTasks)
	}

	return tasks[0], nil
}

func getAllTasks(alloc *api.Allocation) ([]string, error) {
	taskGroup := alloc.Job.LookupTaskGroup(alloc.TaskGroup)
	if taskGroup == nil {
		return nil, errors.New("failed to retrieve task group")
	}

	res := []string{}
	for _, t := range taskGroup.Tasks {
		res = append(res, t.Name)
	}

	return res, nil
}

func allocationExec(ctx context.Context, c client, allocInfo *allocInfo, namespace string, cmd []string) (*execOutput, error) {
	return c.allocationExec(ctx, allocInfo.alloc, allocInfo.task, namespace, cmd)
}
