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

	log.Infof("listing all allocations for job %s", *jobID)

	queryOptions := &api.QueryOptions{}

	allocations, _, err := client.Jobs().Allocations(*jobID, true, queryOptions)
	if err != nil {
		log.Fatalf("failed to get the list of allocations for job %s: %v", *jobID, err)
	}

	if len(allocations) == 0 {
		log.Infof("no allocations found for job \"%s\"", *jobID)

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

			logger.Infof("retrieving allocation info")

			alloc, _, err := client.Allocations().Info(allocID, queryOptions)
			if err != nil {
				logger.Errorf("failed to retrieve allocation info: %v", err)

				continue
			}

			tg := alloc.Job.LookupTaskGroup(alloc.TaskGroup)
			if tg == nil {
				logger.Errorf("failed to retrieve allocation task group: %v", err)

				continue
			}

			for _, task := range tg.Tasks {
				taskName := task.Name

				if *taskID != "" && *taskID != taskName {
					logger.Infof("skipping allocation because it is not part of task \"%s\"", taskName)

					continue
				}

				eg.Go(func() error {
					output, err := executor{
						client:       client,
						queryOptions: queryOptions,
						logger:       logger,
					}.exec(ctx, alloc, taskName, splitCommand)
					if err != nil {
						return err
					}

					execOutputCh <- output

					return nil
				})
			}
		}
	}

	if err := eg.Wait(); err != nil {
		log.Fatalf("failed to exec on all the allocations: %v", err)
	}

	close(execOutputCh)

	<-done
}
