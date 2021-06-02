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

	apiClient, err := api.NewClient(&api.Config{
		Address:   os.Getenv("NOMAD_ADDR"),
		SecretID:  os.Getenv("NOMAD_TOKEN"),
		TLSConfig: &api.TLSConfig{},
	})
	if err != nil {
		log.Fatalf("failed to create the Nomad API client: %v", err)
	}

	client := client{
		client: apiClient,
	}

	log.Infof("listing all allocations for job %s", *jobID)

	allocationsInfo, err := client.getAllocationsInfo(*jobID, *taskID)
	if err != nil {
		log.Fatalf("failed to get the list of allocations: %v", err)
	}

	nbAllocs := len(allocationsInfo)

	if nbAllocs == 0 {
		log.Infof("no allocations found for job %q", *jobID)

		os.Exit(0)
	}

	ctx, cancel := context.WithCancel(context.Background())

	timeoutCh := time.After(timeoutDuration)
	go func() {
		<-timeoutCh

		cancel()
	}()

	execOutputCh := make(chan *execOutput, nbAllocs)
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
	}()

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

				output, err := client.exec(ctx, allocInfo, splitCommand)
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
