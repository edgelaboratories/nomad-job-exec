package main

import (
	"context"
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

func main() { // nolint: cyclop, funlen, gocognit
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

	client, err := api.NewClient(&api.Config{
		Address:   os.Getenv("NOMAD_ADDR"),
		SecretID:  os.Getenv("NOMAD_TOKEN"),
		TLSConfig: &api.TLSConfig{},
	})
	if err != nil {
		log.Fatalf("failed to create the Nomad API client: %v", err)
	}

	queryOptions := &api.QueryOptions{}

	log.Infof("listing all allocations for job %s", *jobID)

	allocations, err := getAllocations(client, *jobID, *taskID, queryOptions)
	if err != nil {
		log.Fatalf("failed to get the list of allocations: %v", err)
	}

	if len(allocations) == 0 {
		log.Info("no allocations found")

		os.Exit(0)
	}

	ctx, cancel := context.WithCancel(context.Background())

	timeoutCh := time.After(timeoutDuration)
	go func() {
		<-timeoutCh

		cancel()
	}()

	execOutputCh := make(chan *execOutput, len(allocations))
	done := make(chan bool, 1)

	go func() {
		defer func() {
			done <- true
		}()

		for out := range execOutputCh {
			logger := log.WithField("allocation_id", out.allocID)

			if len(out.stderr) != 0 {
				logger.Info("flushing command output to stderr")

				if _, err := io.WriteString(
					os.Stderr,
					fmt.Sprintf("[AllocID: %s, Node: [%s]]\n%s", out.allocID, out.node, out.stderr),
				); err != nil {
					log.Errorf("failed to copy to stderr: %v", err)
				}
			}

			if len(out.stdout) != 0 {
				logger.Info("flushing command output to stdout")

				if _, err := io.WriteString(
					os.Stdout,
					fmt.Sprintf("[AllocID: %s, Node: [%s]]\n%s", out.allocID, out.node, out.stdout),
				); err != nil {
					log.Errorf("failed to copy to stdout: %v", err)
				}
			}
		}
	}()

	concurrency := 1
	if *parallel {
		concurrency = len(allocations)
	}

	log.Infof("running command on %d allocations with concurrency %d", len(allocations), concurrency)

	eg, ctx := errgroup.WithContextN(ctx, concurrency, concurrency)

	for _, a := range allocations {
		select {
		case <-ctx.Done():
			break

		default:
			a := a

			logger := log.WithFields(log.Fields{
				"allocation_id": a.alloc.ID,
				"task":          a.task,
			})

			eg.Go(func() error {
				output, err := executor{
					client: &nomadClient{
						client:       client,
						queryOptions: queryOptions,
					},
					logger: logger,
				}.exec(ctx, a.alloc, a.task, splitCommand)
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

type allocation struct {
	alloc *api.Allocation
	task  string
}

func getAllocations(c *api.Client, jobID, taskID string, queryOptions *api.QueryOptions) ([]*allocation, error) {
	allocations, _, err := c.Jobs().Allocations(jobID, true, queryOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to get the list of allocations for job %s: %w", jobID, err)
	}

	res := []*allocation{}
	for _, alloc := range allocations {
		if alloc.ClientStatus != "running" {
			continue
		}

		alloc, _, err := c.Allocations().Info(alloc.ID, queryOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve allocation %s info: %w", alloc.ID, err)
		}

		tg := alloc.Job.LookupTaskGroup(alloc.TaskGroup)
		if tg == nil {
			return nil, fmt.Errorf("failed to retrieve task group for allocation %s: %w", alloc.ID, err)
		}

		for _, task := range tg.Tasks {
			taskName := task.Name

			if taskID != "" && taskID != taskName {
				continue
			}

			res = append(res, &allocation{
				alloc: alloc,
				task:  taskName,
			})
		}
	}

	return res, nil
}

type nomadClient struct {
	client       *api.Client
	queryOptions *api.QueryOptions
}

func (c *nomadClient) NodeInfo(nodeID string) (*nodeInfo, error) {
	node, _, err := c.client.Nodes().Info(nodeID, c.queryOptions)
	if err != nil {
		return nil, err
	}

	return &nodeInfo{
		id:   node.ID,
		name: node.Name,
		addr: node.HTTPAddr,
	}, nil
}

func (c *nomadClient) Exec(ctx context.Context, alloc *api.Allocation, taskID string, command []string, stdout, stderr io.Writer) error {
	_, err := c.client.Allocations().Exec(ctx, alloc, taskID, false, command, os.Stdin, stdout, stderr, nil, c.queryOptions)

	return err
}
