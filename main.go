package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/shlex"
	"github.com/hashicorp/nomad/api"
	"github.com/neilotoole/errgroup"
	log "github.com/sirupsen/logrus"
)

func main() { // nolint: cyclop, funlen
	jobID := flag.String("job", "", "Job ID")
	taskID := flag.String("task", "", "Task ID")
	cmd := flag.String("command", "", "Command to execute on allocations")
	timeout := flag.String("timeout", "10m", "Timeout for the command to execute on all allocations")
	parallel := flag.Bool("parallel", false, "Execute in parallel if true")
	logLevel := flag.String("log-level", "info", "Log level")
	flag.Parse()

	// Configure logger

	level, err := log.ParseLevel(*logLevel)
	if err != nil {
		log.Fatalf("failed to parse log level: %v", err)
	}
	log.SetLevel(level)
	log.SetOutput(os.Stdout)

	// Check inputs

	if *jobID == "" {
		log.Fatal("job ID cannot be empty")
	}

	splitCommand, err := shlex.Split(*cmd)
	if err != nil {
		log.Fatalf("failed to shlex the input command: %v", err)
	}

	timeoutDuration, err := time.ParseDuration(*timeout)
	if err != nil {
		log.Fatalf("failed to parse the timeout duration: %v", err)
	}

	// Create Nomad API client

	apiClient, err := api.NewClient(&api.Config{
		Address:   os.Getenv("NOMAD_ADDR"),
		SecretID:  os.Getenv("NOMAD_TOKEN"),
		TLSConfig: &api.TLSConfig{},
	})
	if err != nil {
		log.Fatalf("failed to create the Nomad API client: %v", err)
	}

	client := &apiClientWrap{
		apiClient,
	}

	// Get the information about the job's running allocations

	log.Infof("listing all allocations for job %s", *jobID)

	allocationsInfo, err := getAllocationsInfo(client, *jobID, *taskID)
	if err != nil {
		log.Fatalf("failed to get the list of allocations: %v", err)
	}

	nbAllocs := len(allocationsInfo)
	if nbAllocs == 0 {
		log.Infof("no allocations found for job %q", *jobID)

		os.Exit(0)
	}

	logger := log.WithFields(log.Fields{
		"job_id": *jobID,
	})

	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)

	if *parallel {
		concurrency := nbAllocs

		log.Infof("running command `%s` on %d allocations with concurrency %d", *cmd, nbAllocs, concurrency)

		if err := executeConcurrently(ctx, logger, client, allocationsInfo, splitCommand, concurrency); err != nil {
			cancel()

			log.Fatalf("failed to concurrently execute command on allocations: %v", err)
		}

		log.Infof("command `%s` ran successfully on %d allocations with concurrency %d", *cmd, nbAllocs, concurrency)
		os.Exit(0)
	}

	log.Infof("running command `%s` sequentially on %d allocations", *cmd, nbAllocs)

	if err := executeSequentially(ctx, logger, client, allocationsInfo, splitCommand); err != nil {
		cancel()

		log.Fatalf("failed to sequentially execute command on allocations: %v", err)
	}

	log.Infof("command `%s` ran successfully on %d allocations sequentially", *cmd, nbAllocs)
}

func executeSequentially(ctx context.Context, logger *log.Entry, c client, allocsInfo []*allocInfo, cmd []string) error {
	for _, allocInfo := range allocsInfo {
		select {
		case <-ctx.Done():
			break

		default:
			logger := logger.WithFields(log.Fields{
				"allocation_id": allocInfo.alloc.ID,
				"task_id":       allocInfo.task,
				"group_id":      allocInfo.alloc.TaskGroup,
			})

			allocInfo := allocInfo

			logger.Info("executing command")

			output, err := allocationExec(ctx, c, allocInfo, cmd)
			if err != nil {
				return fmt.Errorf("failed to exec on allocation: %w", err)
			}

			logger.Info("command executed")

			exitCode, err := consumeExecOutput(output)
			if err != nil {
				return fmt.Errorf("failed to consume command output: %w", err)
			}

			if exitCode != 0 {
				return fmt.Errorf("command failed with code %d", exitCode)
			}
		}
	}

	return nil
}

func executeConcurrently(ctx context.Context, logger *log.Entry, c client, allocsInfo []*allocInfo, cmd []string, concurrency int) error { // nolint: funlen
	nbAllocs := len(allocsInfo)

	execOutputCh := make(chan *execOutput, nbAllocs)
	done := make(chan bool, 1)

	defer func() {
		close(execOutputCh)
		<-done
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var exitCode int
	go func(exitCode *int) {
		defer func() {
			done <- true
		}()

		for output := range execOutputCh {
			c, err := consumeExecOutput(output)
			if err != nil {
				log.Errorf("failed to consume command output: %v", err)
			}

			if c != 0 {
				*exitCode = c
				cancel()

				return
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

			logger := logger.WithFields(log.Fields{
				"allocation_id": allocInfo.alloc.ID,
				"task_id":       allocInfo.task,
				"group_id":      allocInfo.alloc.TaskGroup,
			})

			eg.Go(func() error {
				logger.Info("executing command")
				defer logger.Info("command executed")

				output, err := allocationExec(ctx, c, allocInfo, cmd)
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

	if exitCode != 0 {
		return fmt.Errorf("command failed with code %d", exitCode)
	}

	return nil
}

func consumeExecOutput(out *execOutput) (int, error) {
	logger := log.WithField("allocation_id", out.allocID)

	if len(out.stderr) != 0 {
		logger.Debug("flushing command output to stderr")

		if _, err := io.WriteString(
			os.Stderr,
			fmt.Sprintf("[AllocID: %s]\n%s", out.allocID, out.stderr),
		); err != nil {
			return out.exitCode, fmt.Errorf("failed to copy to stderr: %w", err)
		}
	}

	if len(out.stdout) != 0 {
		logger.Debug("flushing command output to stdout")

		if _, err := io.WriteString(
			os.Stdout,
			fmt.Sprintf("[AllocID: %s]\n%s", out.allocID, out.stdout),
		); err != nil {
			return out.exitCode, fmt.Errorf("failed to copy to stdout: %w", err)
		}
	}

	return out.exitCode, nil
}

type client interface {
	jobAllocations(jobID string) ([]*api.AllocationListStub, error)
	allocationInfo(allocID string) (*api.Allocation, error)
	allocationExec(ctx context.Context, alloc *api.Allocation, task string, cmd []string) (*execOutput, error)
}

type allocInfo struct {
	alloc *api.Allocation
	task  string
}

func getAllocationsInfo(c client, jobID, taskID string) ([]*allocInfo, error) {
	allocations, err := c.jobAllocations(jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get the list of allocations for job %s: %w", jobID, err)
	}

	res := []*allocInfo{}
	for _, alloc := range allocations {
		if alloc.ClientStatus != "running" {
			continue
		}

		alloc, err := c.allocationInfo(alloc.ID)
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

			if contains(tasks, taskID) {
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

func allocationExec(ctx context.Context, c client, allocInfo *allocInfo, cmd []string) (*execOutput, error) {
	return c.allocationExec(ctx, allocInfo.alloc, allocInfo.task, cmd)
}

func contains(tasks []string, task string) bool {
	for _, t := range tasks {
		if t == task {
			return true
		}
	}

	return false
}
