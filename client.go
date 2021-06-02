package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/hashicorp/nomad/api"
)

type client struct {
	client *api.Client
}

type allocInfo struct {
	alloc *api.Allocation
	task  string
}

func (c *client) getAllocationsInfo(jobID, taskID string) ([]*allocInfo, error) {
	allocations, _, err := c.client.Jobs().Allocations(jobID, true, &api.QueryOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get the list of allocations for job %s: %w", jobID, err)
	}

	res := []*allocInfo{}
	for _, alloc := range allocations {
		if alloc.ClientStatus != "running" {
			continue
		}

		allocInfo, err := c.getAllocationInfo(alloc.ID, taskID)
		if err != nil {
			return nil, err
		}

		res = append(res, allocInfo)
	}

	return res, nil
}

func (c *client) getAllocationInfo(allocID, task string) (*allocInfo, error) {
	alloc, _, err := c.client.Allocations().Info(allocID, &api.QueryOptions{})
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

type execOutput struct {
	allocID string
	stdout  string
	stderr  string
}

func (c *client) exec(ctx context.Context, allocInfo *allocInfo, cmd []string) (*execOutput, error) {
	var bufStdout, bufStderr bytes.Buffer

	if _, err := c.client.Allocations().Exec(ctx, allocInfo.alloc, allocInfo.task, false, cmd, os.Stdin, &bufStdout, &bufStderr, nil, &api.QueryOptions{}); err != nil {
		return nil, fmt.Errorf("failed to exec command on allocation: %w", err)
	}

	return &execOutput{
		allocID: allocInfo.alloc.ID,
		stdout:  bufStdout.String(),
		stderr:  bufStderr.String(),
	}, nil
}
