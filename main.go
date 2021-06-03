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

	// Execute command on allocations and dump output in stdout/stderr

	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)
	defer cancel()

	execOutputCh := make(chan *execOutput, nbAllocs)
	done := make(chan bool, 1)

	go consumeExecOutput(execOutputCh, done)

	// Only use concurrency if --parallel is true
	concurrency := 1
	if *parallel {
		concurrency = nbAllocs
	}

	log.Infof("running command on %d allocations with concurrency %d", nbAllocs, concurrency)

	eg, ctx := errgroup.WithContextN(ctx, concurrency, concurrency)

	for _, allocInfo := range allocationsInfo {
		select {
		case <-ctx.Done():
			break

		default:
			allocInfo := allocInfo

			logger := log.WithFields(log.Fields{
				"job_id":        jobID,
				"allocation_id": allocInfo.alloc.ID,
				"task_id":       allocInfo.task,
			})

			logger.Infof("executing command: %q", *cmd)

			eg.Go(func() error {
				defer logger.Info("command executed")

				output, err := allocationExec(ctx, client, allocInfo, splitCommand)
				if err != nil {
					return err
				}

				execOutputCh <- output

				return nil
			})
		}
	}

	if err := eg.Wait(); err != nil {
		log.Errorf("failed to exec on all the allocations: %v", err)
	}

	close(execOutputCh)

	<-done
}

func consumeExecOutput(in <-chan *execOutput, done chan<- bool) {
	defer func() {
		done <- true
	}()

	for out := range in {
		logger := log.WithField("allocation_id", out.allocID)

		if len(out.stderr) != 0 {
			logger.Debug("flushing command output to stderr")

			if _, err := io.WriteString(
				os.Stderr,
				fmt.Sprintf("[AllocID: %s]\n%s", out.allocID, out.stderr),
			); err != nil {
				log.Errorf("failed to copy to stderr: %v", err)
			}
		}

		if len(out.stdout) != 0 {
			logger.Debug("flushing command output to stdout")

			if _, err := io.WriteString(
				os.Stdout,
				fmt.Sprintf("[AllocID: %s]\n%s", out.allocID, out.stdout),
			); err != nil {
				log.Errorf("failed to copy to stdout: %v", err)
			}
		}
	}
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
			// If task is given, the task is know so we filter out allocations
			// that don't have the specified task

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
