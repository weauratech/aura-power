package notifications

import (
	"context"
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime"

	v1alpha1 "github.com/weauratech/aura-power/api/v1alpha1"
)

// Event represents a power event to notify about.
type Event struct {
	Action    string
	Target    TargetRef
	Result    string
	Reason    string
	RuleName  string
	Timestamp time.Time
}

// TargetRef identifies the affected workload.
type TargetRef struct {
	Namespace string
	Name      string
	Kind      string
}

// Dispatcher sends notifications to configured channels.
type Dispatcher struct {
	client   client.Client
	senders  map[string]Sender
	queue    chan Event
	mu       sync.Mutex
	throttle map[string]time.Time // key: "channel/target" → last sent
}

// Sender formats and sends a notification for a given provider type.
type Sender interface {
	Send(ctx context.Context, url string, event Event) error
	Type() string
}

// NewDispatcher creates a notification dispatcher.
func NewDispatcher(c client.Client) *Dispatcher {
	d := &Dispatcher{
		client:   c,
		senders:  make(map[string]Sender),
		queue:    make(chan Event, 500),
		throttle: make(map[string]time.Time),
	}
	// Register built-in senders
	d.RegisterSender(&GoogleChatSender{})
	d.RegisterSender(&GenericSender{})
	d.RegisterSender(&SlackSender{})
	return d
}

// RegisterSender adds a sender for a provider type.
func (d *Dispatcher) RegisterSender(s Sender) {
	d.senders[s.Type()] = s
}

// Enqueue adds an event to the notification queue.
func (d *Dispatcher) Enqueue(event Event) {
	select {
	case d.queue <- event:
	default:
		// Queue full, drop oldest
		ctrl.Log.WithName("notifications").Info("queue full, dropping event")
	}
}

// Run starts the dispatcher loop. Blocks until context is cancelled.
// Uses batching: collects events for 5 seconds, then dispatches grouped.
func (d *Dispatcher) Run(ctx context.Context) {
	log := ctrl.Log.WithName("notifications")

	// Wait for cache to sync before processing
	log.Info("notification dispatcher waiting for readiness...")
	time.Sleep(15 * time.Second)
	log.Info("notification dispatcher started (batch mode: 5s window)")

	for {
		// Wait for first event or context cancel
		select {
		case <-ctx.Done():
			log.Info("notification dispatcher stopped")
			return
		case first := <-d.queue:
			// Collect events for 5 seconds into a batch
			batch := []Event{first}
			timer := time.NewTimer(5 * time.Second)
		batchLoop:
			for {
				select {
				case ev := <-d.queue:
					batch = append(batch, ev)
				case <-timer.C:
					break batchLoop
				case <-ctx.Done():
					timer.Stop()
					return
				}
			}
			d.dispatchBatch(ctx, batch)
		}
	}
}

func (d *Dispatcher) dispatchBatch(ctx context.Context, batch []Event) {
	log := ctrl.Log.WithName("notifications")

	if len(batch) == 0 {
		return
	}

	// Filter only notifiable actions and deduplicate
	var filtered []Event
	seen := map[string]bool{}
	for _, ev := range batch {
		key := ev.Action + "/" + ev.Target.Namespace + "/" + ev.Target.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		filtered = append(filtered, ev)
	}

	if len(filtered) == 0 {
		return
	}

	// Load all channels
	var channels v1alpha1.PowerNotificationChannelList
	if err := d.client.List(ctx, &channels); err != nil {
		log.Error(err, "failed to list notification channels")
		return
	}

	for i := range channels.Items {
		ch := &channels.Items[i]
		if !ch.Spec.Enabled {
			continue
		}

		// Filter events for this channel
		var channelEvents []Event
		for _, ev := range filtered {
			if len(ch.Spec.Events) > 0 && !contains(ch.Spec.Events, ev.Action) {
				continue
			}
			if len(ch.Spec.NamespaceFilter) > 0 && !contains(ch.Spec.NamespaceFilter, ev.Target.Namespace) {
				continue
			}
			channelEvents = append(channelEvents, ev)
		}

		if len(channelEvents) == 0 {
			continue
		}

		// Throttle: check if any event in batch was already sent recently
		throttleDur := 5 * time.Minute
		if ch.Spec.Throttle != "" {
			if parsed, err := time.ParseDuration(ch.Spec.Throttle); err == nil {
				throttleDur = parsed
			}
		}

		// For batch, throttle by first target (representative)
		key := ch.Name + "/batch/" + channelEvents[0].Action
		d.mu.Lock()
		lastSent, exists := d.throttle[key]
		if exists && time.Since(lastSent) < throttleDur {
			d.mu.Unlock()
			continue
		}
		d.throttle[key] = time.Now()
		d.mu.Unlock()

		// Resolve URL
		url := ch.Spec.URL
		if url == "" {
			continue
		}

		// Find sender
		sender, ok := d.senders[ch.Spec.Type]
		if !ok {
			continue
		}

		// Send batch as one message
		batchEvent := Event{
			Action:    channelEvents[0].Action,
			Target:    channelEvents[0].Target,
			Result:    channelEvents[0].Result,
			Reason:    fmt.Sprintf("%d workload(s) affected", len(channelEvents)),
			RuleName:  channelEvents[0].RuleName,
			Timestamp: channelEvents[0].Timestamp,
		}

		// Build reason with all targets
		if len(channelEvents) > 1 {
			names := ""
			for i, ev := range channelEvents {
				if i > 4 {
					names += fmt.Sprintf(" (+%d more)", len(channelEvents)-5)
					break
				}
				if i > 0 {
					names += ", "
				}
				names += ev.Target.Namespace + "/" + ev.Target.Name
			}
			batchEvent.Reason = names
		} else {
			batchEvent.Reason = channelEvents[0].Reason
		}

		if err := sender.Send(ctx, url, batchEvent); err != nil {
			log.Error(err, "notification send failed", "channel", ch.Name)
			ch.Status.TotalErrors++
			ch.Status.LastError = err.Error()
		} else {
			now := metav1.Now()
			ch.Status.TotalSent++
			ch.Status.LastNotification = &now
			ch.Status.LastError = ""
			log.Info("batch notification sent", "channel", ch.Name, "events", len(channelEvents))
		}

		if err := d.client.Status().Update(ctx, ch); err != nil {
			log.Error(err, "failed to update channel status", "channel", ch.Name)
		}
	}
}

func (d *Dispatcher) dispatch(ctx context.Context, event Event) {
	log := ctrl.Log.WithName("notifications")

	// Load all channels
	var channels v1alpha1.PowerNotificationChannelList
	if err := d.client.List(ctx, &channels); err != nil {
		log.Error(err, "failed to list notification channels")
		return
	}

	for i := range channels.Items {
		ch := &channels.Items[i]
		if !ch.Spec.Enabled {
			continue
		}

		// Event filter
		if len(ch.Spec.Events) > 0 && !contains(ch.Spec.Events, event.Action) {
			continue
		}

		// Namespace filter
		if len(ch.Spec.NamespaceFilter) > 0 && !contains(ch.Spec.NamespaceFilter, event.Target.Namespace) {
			continue
		}

		// Throttle check (default: 5m per target even if not configured)
		throttleDur := 5 * time.Minute
		if ch.Spec.Throttle != "" {
			if parsed, err := time.ParseDuration(ch.Spec.Throttle); err == nil {
				throttleDur = parsed
			}
		}
		key := ch.Name + "/" + event.Target.Namespace + "/" + event.Target.Name
		d.mu.Lock()
		lastSent, exists := d.throttle[key]
		if exists && time.Since(lastSent) < throttleDur {
			d.mu.Unlock()
			continue
		}
		d.throttle[key] = time.Now()
		d.mu.Unlock()

		// Resolve URL
		url := ch.Spec.URL
		if url == "" && ch.Spec.URLFrom != nil {
			// TODO: resolve from Secret
			log.Info("urlFrom not yet implemented, skipping", "channel", ch.Name)
			continue
		}
		if url == "" {
			continue
		}

		// Find sender
		sender, ok := d.senders[ch.Spec.Type]
		if !ok {
			log.Info("no sender for type", "type", ch.Spec.Type, "channel", ch.Name)
			continue
		}

		// Send
		if err := sender.Send(ctx, url, event); err != nil {
			log.Error(err, "notification send failed", "channel", ch.Name, "type", ch.Spec.Type)
			ch.Status.TotalErrors++
			ch.Status.LastError = err.Error()
		} else {
			now := metav1.Now()
			ch.Status.TotalSent++
			ch.Status.LastNotification = &now
			ch.Status.LastError = ""
			log.Info("notification sent", "channel", ch.Name, "type", ch.Spec.Type, "target", event.Target.Name)
		}

		// Update status
		if err := d.client.Status().Update(ctx, ch); err != nil {
			log.Error(err, "failed to update channel status", "channel", ch.Name)
		}
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
