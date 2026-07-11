package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

var errTaskExecutionLeaseLost = errors.New("task execution lease lost")

const (
	localImageDispatchInterval = time.Second
	localImageRetryBaseDelay   = 15 * time.Second
	localImageRetryMaxDelay    = 5 * time.Minute
	localImageLeaseOwnerLimit  = 191
)

type localImageTaskRunner func(ctx context.Context, task *model.Task, leaseOwner string) error

type localImageTaskDispatcher struct {
	slots         chan struct{}
	leaseDuration time.Duration
	ownerPrefix   string
	run           localImageTaskRunner
	wake          chan struct{}
	wg            sync.WaitGroup
}

func newLocalImageTaskDispatcher(concurrency int, leaseDuration time.Duration, run localImageTaskRunner) *localImageTaskDispatcher {
	if concurrency < 1 {
		concurrency = 1
	}
	if leaseDuration < 30*time.Second {
		leaseDuration = 30 * time.Second
	}
	return &localImageTaskDispatcher{
		slots:         make(chan struct{}, concurrency),
		leaseDuration: leaseDuration,
		ownerPrefix:   localImageLeaseOwnerPrefix(),
		run:           run,
		wake:          make(chan struct{}, 1),
	}
}

func (d *localImageTaskDispatcher) Concurrency() int {
	return cap(d.slots)
}

func (d *localImageTaskDispatcher) LeaseDuration() time.Duration {
	return d.leaseDuration
}

func (d *localImageTaskDispatcher) Wait() {
	d.wg.Wait()
}

func (d *localImageTaskDispatcher) Run(ctx context.Context, interval time.Duration, queryLimit int) {
	if interval <= 0 {
		interval = localImageDispatchInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	d.markLegacyTasks(ctx, queryLimit)

	for {
		if ctx.Err() != nil {
			return
		}
		d.dispatchPending(ctx, queryLimit)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-d.wake:
		}
	}
}

func (d *localImageTaskDispatcher) markLegacyTasks(ctx context.Context, queryLimit int) {
	if queryLimit < 1 {
		queryLimit = 1000
	}
	var afterID int64
	for {
		if ctx.Err() != nil {
			return
		}
		tasks, err := model.GetLegacyLocalImageTaskCandidates(afterID, queryLimit)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("query legacy local image tasks failed: %s", err.Error()))
			return
		}
		for _, task := range tasks {
			afterID = task.ID
			if !isLocalImageExecutionTask(task) {
				continue
			}
			if err := model.MarkTaskAsLocalImage(task.ID); err != nil {
				logger.LogError(ctx, fmt.Sprintf("mark legacy local image task %s failed: %s", task.TaskID, err.Error()))
			}
		}
		if len(tasks) < queryLimit {
			return
		}
	}
}

func (d *localImageTaskDispatcher) dispatchPending(ctx context.Context, queryLimit int) {
	if len(d.slots) >= cap(d.slots) {
		return
	}
	tasks, err := model.GetPendingLocalImageTasks(time.Now().Unix(), queryLimit)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("query pending local image tasks failed: %s", err.Error()))
		return
	}
	for _, task := range tasks {
		if len(d.slots) >= cap(d.slots) {
			return
		}
		d.TryDispatch(task)
	}
}

func (d *localImageTaskDispatcher) notify() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *localImageTaskDispatcher) TryDispatch(task *model.Task) bool {
	if !isLocalImageExecutionTask(task) || d.run == nil {
		return false
	}
	select {
	case d.slots <- struct{}{}:
	default:
		return false
	}

	now := time.Now()
	leaseID := common.GetUUID()
	leaseOwner := d.ownerPrefix + ":" + leaseID
	if len(leaseOwner) > localImageLeaseOwnerLimit {
		leaseOwner = leaseID
	}
	leaseUntil := now.Add(d.leaseDuration).Unix()
	claimed, err := model.TryClaimTaskExecutionLease(task.ID, leaseOwner, now.Unix(), leaseUntil)
	if err != nil {
		<-d.slots
		logger.LogError(context.Background(), fmt.Sprintf("claim local image task %s failed: %s", task.TaskID, err.Error()))
		return false
	}
	if !claimed {
		<-d.slots
		return false
	}

	task.ExecutionLeaseOwner = leaseOwner
	task.ExecutionLeaseUntil = leaseUntil
	task.ExecutionAttempts++
	task.Status = model.TaskStatusInProgress
	task.Progress = "30%"
	task.StartTime = now.Unix()
	d.wg.Add(1)
	go d.execute(task, leaseOwner)
	return true
}

func (d *localImageTaskDispatcher) execute(task *model.Task, leaseOwner string) {
	ctx, cancel := context.WithCancel(context.Background())
	heartbeatDone := make(chan struct{})
	go d.maintainLease(ctx, cancel, heartbeatDone, task, leaseOwner)

	var runErr error
	defer func() {
		if recovered := recover(); recovered != nil {
			runErr = fmt.Errorf("local image task panic: %v", recovered)
		}
		cancel()
		<-heartbeatDone
		d.finish(task, leaseOwner, runErr)
		<-d.slots
		d.wg.Done()
		d.notify()
	}()

	runErr = d.run(ctx, task, leaseOwner)
}

func (d *localImageTaskDispatcher) maintainLease(ctx context.Context, cancel context.CancelFunc, done chan<- struct{}, task *model.Task, leaseOwner string) {
	defer close(done)
	interval := d.leaseDuration / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			renewed, err := model.RenewTaskExecutionLease(task.ID, leaseOwner, now.Add(d.leaseDuration).Unix())
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("renew local image task %s lease failed: %s", task.TaskID, err.Error()))
				cancel()
				return
			}
			if !renewed {
				logger.LogWarn(ctx, fmt.Sprintf("local image task %s execution lease was lost", task.TaskID))
				cancel()
				return
			}
		}
	}
}

func (d *localImageTaskDispatcher) finish(task *model.Task, leaseOwner string, runErr error) {
	ctx := context.Background()
	if runErr != nil {
		retryDelay := localImageRetryDelay(task.ExecutionAttempts)
		retryAt := time.Now().Add(retryDelay).Unix()
		retried, err := model.RetryTaskExecutionLease(task.ID, leaseOwner, retryAt)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("release local image task %s for retry failed: %s", task.TaskID, err.Error()))
		} else if retried {
			logger.LogWarn(ctx, fmt.Sprintf("local image task %s will retry in %s after execution error: %s", task.TaskID, retryDelay, runErr.Error()))
		}
		return
	}

	if _, err := model.ReleaseTaskExecutionLease(task.ID, leaseOwner); err != nil {
		logger.LogError(ctx, fmt.Sprintf("release local image task %s lease failed: %s", task.TaskID, err.Error()))
	}
}

func localImageRetryDelay(attempts int) time.Duration {
	delay := localImageRetryBaseDelay
	for attempt := 1; attempt < attempts && delay < localImageRetryMaxDelay; attempt++ {
		delay *= 2
		if delay > localImageRetryMaxDelay {
			return localImageRetryMaxDelay
		}
	}
	return delay
}

func runLeasedLocalImageTask(ctx context.Context, task *model.Task, leaseOwner string) error {
	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		return fmt.Errorf("get local image channel %d failed: %w", task.ChannelId, err)
	}
	adaptor := GetTaskAdaptorFunc(task.Platform)
	if adaptor == nil {
		return fmt.Errorf("local image task adaptor not found")
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: channel.GetBaseURL(),
		},
	}
	info.ApiKey = channel.Key
	adaptor.Init(info)
	return updateTask(ctx, adaptor, channel, task, leaseOwner)
}

func isLocalImageExecutionTask(task *model.Task) bool {
	return task != nil &&
		task.Platform == constant.TaskPlatformImage &&
		task.PrivateData.LocalImageTask != nil
}

func localImageLeaseOwnerPrefix() string {
	nodeName := strings.TrimSpace(common.NodeName)
	if nodeName == "" {
		nodeName, _ = os.Hostname()
	}
	if nodeName == "" {
		nodeName = "new-api"
	}
	return fmt.Sprintf("%s-%d", nodeName, os.Getpid())
}
