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
	if *taskID == "" {
		log.Fatal("task ID cannot be empty")
	}

	splitCommand, err := shlex.Split(*cmd)
	if err != nil {
		log.Fatalf("failed to shlex the input command: %v", err)
	}

	log.Info(splitCommand)

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

	log.Infof("listing all allocations for job %s", *jobID)

	allocations, _, err := client.Jobs().Allocations(*jobID, true, &api.QueryOptions{})
	if err != nil {
		log.Fatalf("failed to get the list of allocations for job %s: %v", *jobID, err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	timeoutCh := time.After(timeoutDuration)
	go func() {
		<-timeoutCh

		cancel()
	}()

	execOutputCh := make(chan execOutput, len(allocations))
	done := make(chan bool, 1)

	go func() {
		defer func() {
			done <- true
		}()

		for out := range execOutputCh {
			logger := log.WithField("allocation_id", out.allocID)

			if len(out.stderr) != 0 {
				logger.Info("flushing command output to stderr")

				_, err = io.WriteString(os.Stderr, fmt.Sprintf("[AllocID: %s, Node: [%s]]\n%s", out.allocID, out.node, out.stderr))
				if err != nil {
					log.Errorf("failed to copy to stderr: %v", err)
				}
			}

			if len(out.stdout) != 0 {
				logger.Info("flushing command output to stdout")

				_, err = io.WriteString(os.Stdout, fmt.Sprintf("[AllocID: %s, Node: [%s]]\n%s", out.allocID, out.node, out.stdout))
				if err != nil {
					log.Errorf("failed to copy to stdout: %v", err)
				}
			}
		}
	}()

	concurrency := 1
	if *parallel {
		concurrency = len(allocations)
	}

	eg, ctx := errgroup.WithContextN(ctx, concurrency, concurrency)

	for _, a := range allocations {
		select {
		case <-ctx.Done():
			break

		default:
			allocID := a.ID

			logger := log.WithField("allocation_id", allocID)

			if a.ClientStatus != "running" {
				logger.Infof("allocation %s is %s", allocID, a.ClientStatus)

				continue
			}

			eg.Go(func() error {
				return executor{
					client:       client,
					queryOptions: &api.QueryOptions{},
					logger:       logger,
				}.exec(ctx, allocID, *taskID, splitCommand, execOutputCh)
			})
		}
	}

	if err := eg.Wait(); err != nil {
		log.Fatalf("failed to exec on all the allocations: %v", err)
	}

	close(execOutputCh)

	<-done
}
