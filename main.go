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
	flag.Parse()

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

	ctx, cancel := context.WithCancel(context.Background())

	timeoutCh := time.After(timeoutDuration)
	go func() {
		<-timeoutCh

		cancel()
	}()

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
				"allocation_id": allocInfo.alloc.ID,
				"task_id":       allocInfo.task,
			})

			logger.Info("executing command")

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
		log.Fatalf("failed to exec on all the allocations: %v", err)
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
			logger.Info("flushing command output to stderr")

			if _, err := io.WriteString(
				os.Stderr,
				fmt.Sprintf("[AllocID: %s]\n%s", out.allocID, out.stderr),
			); err != nil {
				log.Errorf("failed to copy to stderr: %v", err)
			}
		}

		if len(out.stdout) != 0 {
			logger.Info("flushing command output to stdout")

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

		allocInfo, err := getAllocationInfo(c, alloc.ID, taskID)
		if err != nil {
			return nil, err
		}

		res = append(res, allocInfo)
	}

	return res, nil
}

func getAllocationInfo(c client, allocID, task string) (*allocInfo, error) {
	alloc, err := c.allocationInfo(allocID)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve allocation %s info: %w", allocID, err)
	}

	if task != "" {
		return &allocInfo{
			alloc: alloc,
			task:  task,
		}, nil
	}

	taskName, err := getTaskName(alloc)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve task name for allocation %s: %w", allocID, err)
	}

	return &allocInfo{
		alloc: alloc,
		task:  taskName,
	}, nil
}

func getTaskName(alloc *api.Allocation) (string, error) {
	taskGroup := alloc.Job.LookupTaskGroup(alloc.TaskGroup)
	if taskGroup == nil {
		return "", errors.New("failed to retrieve task group")
	}

	if nbTasks := len(taskGroup.Tasks); nbTasks != 1 {
		return "", fmt.Errorf("found %d tasks, please specify the task name", nbTasks)
	}

	return taskGroup.Tasks[0].Name, nil
}

func allocationExec(ctx context.Context, c client, allocInfo *allocInfo, cmd []string) (*execOutput, error) {
	return c.allocationExec(ctx, allocInfo.alloc, allocInfo.task, cmd)
}
