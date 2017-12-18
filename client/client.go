package client

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
)

// Client is ECS Client.
type Client struct {
	svc          *ecs.ECS
	logger       *log.Logger
	pollInterval time.Duration
}

// New create Client Struct.
func New(region string, logger *log.Logger) *Client {
	sess := session.New(&aws.Config{Region: &region})
	svc := ecs.New(sess)
	return &Client{
		svc:          svc,
		pollInterval: time.Second * 5,
		logger:       logger,
	}
}

// RegisterTaskDefinition updates the existing task definition's image.
func (c *Client) RegisterTaskDefinition(task, image, tag string) (string, error) {
	taskDef, err := c.GetTaskDefinition(task)
	if err != nil {
		return "", err
	}

	defs := taskDef.ContainerDefinitions
	for _, d := range defs {
		if strings.HasPrefix(*d.Image, image) {
			i := fmt.Sprintf("%s:%s", image, tag)
			d.Image = &i
		}
	}
	input := &ecs.RegisterTaskDefinitionInput{
		Family:               &task,
		TaskRoleArn:          taskDef.TaskRoleArn,
		NetworkMode:          taskDef.NetworkMode,
		ContainerDefinitions: defs,
		Volumes:              taskDef.Volumes,
		PlacementConstraints: taskDef.PlacementConstraints,
	}
	resp, err := c.svc.RegisterTaskDefinition(input)
	if err != nil {
		return "", err
	}
	return *resp.TaskDefinition.TaskDefinitionArn, nil
}

// UpdateService updates the service to use the new task definition.
func (c *Client) UpdateService(cluster, service string, count int, arn string) error {
	input := &ecs.UpdateServiceInput{
		Cluster: &cluster,
		Service: &service,
	}
	if count != -1 {
		dc := int64(count)
		input.DesiredCount = &dc
	}
	if arn != "" {
		input.TaskDefinition = &arn
	}
	_, err := c.svc.UpdateService(input)
	if err != nil {
		return err
	}

	return nil
}

func divideTaskDefAndTag(taskDef string) (string, string, error) {
	res := strings.Split(taskDef, ":")
	if len(res) >= 7 {
		return "", "", errors.New("task def format is wrong")
	}
	return res[5], res[6], nil

}

// StopCurrentTasks stops tasks of service.
func (c *Client) StopCurrentTasks(cluster, taskDef, service string) error {
	inListTasks := &ecs.ListTasksInput{
		Cluster: &cluster,
	}
	output, err := c.svc.ListTasks(inListTasks)
	if err != nil {
		return err
	}
	inDescribeTasks := &ecs.DescribeTasksInput{
		Cluster: &cluster,
		Tasks:   output.TaskArns,
	}
	out, err := c.svc.DescribeTasks(inDescribeTasks)
	if err != nil {
		return err
	}

	t1, _, _ := divideTaskDefAndTag(taskDef)
	for _, task := range out.Tasks {
		if t2, _, _ := divideTaskDefAndTag(taskDef); t1 != t2 {
			c.logger.Printf("[warn] task diff t1 = %s, t2 = %s", t1, t2)
			continue
		}
		inStopTasks := &ecs.StopTaskInput{
			Cluster: &cluster,
			Task:    task.TaskArn,
		}
		_, err := c.svc.StopTask(inStopTasks)
		if err != nil {
			return err
		}
	}
	return nil
}

// Wait waits for the service to finish being updated.
func (c *Client) Wait(cluster, service, arn string) error {
	t := time.NewTicker(c.pollInterval)
	for {
		select {
		case <-t.C:
			s, err := c.GetDeployment(cluster, service, arn)
			if err != nil {
				return err
			}
			c.logger.Printf("[info] --> desired: %d, pending: %d, running: %d", *s.DesiredCount, *s.PendingCount, *s.RunningCount)
			if *s.RunningCount == *s.DesiredCount {
				return nil
			}
		}
	}
}

// GetDeployment gets the deployment for the arn.
func (c *Client) GetDeployment(cluster, service, arn string) (*ecs.Deployment, error) {
	input := &ecs.DescribeServicesInput{
		Cluster:  &cluster,
		Services: []*string{&service},
	}
	output, err := c.svc.DescribeServices(input)
	if err != nil {
		return nil, err
	}
	ds := output.Services[0].Deployments
	for _, d := range ds {
		if *d.TaskDefinition == arn {
			return d, nil
		}
	}
	return nil, nil
}

// GetTaskDefinition gets the latest revision for the given task definition
func (c *Client) GetTaskDefinition(task string) (*ecs.TaskDefinition, error) {
	output, err := c.svc.DescribeTaskDefinition(&ecs.DescribeTaskDefinitionInput{
		TaskDefinition: &task,
	})
	if err != nil {
		return nil, err
	}
	return output.TaskDefinition, nil
}
