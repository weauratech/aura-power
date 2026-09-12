package notifications

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	UID       string
}

// Dispatcher sends notifications to configured channels.
type Dispatcher struct {
	client   client.Client
	senders  map[string]Sender
	queue    chan Event
	mu       sync.Mutex
	throttle map[string]time.Time // key: "channel/target" → expiry
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
		// Preserve the most recent controller state when producers outrun the
		// dispatcher. The first non-blocking receive makes room by evicting the
		// oldest pending event; a concurrent consumer may already have done so.
		select {
		case <-d.queue:
		default:
		}
		select {
		case d.queue <- event:
			ctrl.Log.WithName("notifications").Info("queue full, dropped oldest event")
		default:
			ctrl.Log.WithName("notifications").Info("queue remained full, dropping newest event")
		}
	}
}

// Start runs the dispatcher under the controller manager after cache readiness.
// Only the elected leader dispatches, preventing duplicate deliveries when the
// controller has multiple replicas.
func (d *Dispatcher) Start(ctx context.Context) error {
	d.Run(ctx)
	return nil
}

// NeedLeaderElection makes Dispatcher a leader-only manager runnable.
func (d *Dispatcher) NeedLeaderElection() bool { return true }

// Run starts the dispatcher loop and blocks until context is cancelled. It
// batches events received within five seconds into one delivery per channel.
func (d *Dispatcher) Run(ctx context.Context) {
	log := ctrl.Log.WithName("notifications")
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
	d.pruneThrottle(time.Now())

	if len(batch) == 0 {
		return
	}

	// Filter only notifiable actions and deduplicate
	var filtered []Event
	seen := map[string]bool{}
	for _, ev := range batch {
		key := eventIdentity(ev)
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

		// A message must have one truthful action, result, and rule. Combining
		// different decisions under the first event's metadata makes the webhook
		// claim that unrelated workloads had the same outcome.
		groups := groupBatchEvents(channelEvents)

		// Throttle each semantically homogeneous group independently.
		throttleDur := 5 * time.Minute
		if ch.Spec.Throttle != "" {
			if parsed, err := time.ParseDuration(ch.Spec.Throttle); err == nil && parsed > 0 {
				throttleDur = parsed
			}
		}

		// Resolve URL
		url, err := d.resolveWebhookURL(ctx, ch)
		if err != nil {
			d.recordFailure(ctx, ch, "resolve webhook URL", err, "")
			continue
		}

		// Find sender
		sender, ok := d.senders[ch.Spec.Type]
		if !ok {
			d.recordFailure(ctx, ch, "select notification sender", fmt.Errorf("unsupported provider type %q", ch.Spec.Type), "")
			continue
		}

		for _, group := range groups {
			key := batchThrottleKey(ch, group)
			d.mu.Lock()
			expiresAt, exists := d.throttle[key]
			d.mu.Unlock()
			if exists && time.Now().Before(expiresAt) {
				continue
			}

			batchEvent := summarizeBatch(group)
			if err := sender.Send(ctx, url, batchEvent); err != nil {
				d.recordFailure(ctx, ch, "send notification", err, url)
				continue
			}

			d.mu.Lock()
			d.throttle[key] = time.Now().Add(throttleDur)
			d.mu.Unlock()
			now := metav1.Now()
			ch.Status.TotalSent++
			ch.Status.LastNotification = &now
			ch.Status.LastError = ""
			log.Info("batch notification sent", "channel", ch.Name, "events", len(group), "action", batchEvent.Action, "result", batchEvent.Result, "rule", batchEvent.RuleName)

			if err := d.client.Status().Update(ctx, ch); err != nil {
				log.Error(err, "failed to update channel status", "channel", ch.Name, "namespace", ch.Namespace)
			}
		}
	}
}

func groupBatchEvents(events []Event) [][]Event {
	type semanticKey struct {
		action string
		result string
		rule   string
	}
	groups := make([][]Event, 0)
	indexes := make(map[semanticKey]int)
	for _, event := range events {
		key := semanticKey{action: event.Action, result: event.Result, rule: event.RuleName}
		index, ok := indexes[key]
		if !ok {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, nil)
		}
		groups[index] = append(groups[index], event)
	}
	return groups
}

func summarizeBatch(events []Event) Event {
	batchEvent := events[0]
	if len(events) == 1 {
		return batchEvent
	}

	names := fmt.Sprintf("%d workload(s): ", len(events))
	for i, event := range events {
		if i > 4 {
			names += fmt.Sprintf(" (+%d more)", len(events)-5)
			break
		}
		if i > 0 {
			names += ", "
		}
		names += fmt.Sprintf("%s/%s (%s)", event.Target.Namespace, event.Target.Name, event.Target.Kind)
	}
	batchEvent.Reason = names
	return batchEvent
}

func (d *Dispatcher) dispatch(ctx context.Context, event Event) {
	log := ctrl.Log.WithName("notifications")
	d.pruneThrottle(time.Now())

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
			if parsed, err := time.ParseDuration(ch.Spec.Throttle); err == nil && parsed > 0 {
				throttleDur = parsed
			}
		}
		key := ch.Name + "/" + eventIdentity(event)
		d.mu.Lock()
		expiresAt, exists := d.throttle[key]
		if exists && time.Now().Before(expiresAt) {
			d.mu.Unlock()
			continue
		}
		d.mu.Unlock()

		// Resolve URL
		url, err := d.resolveWebhookURL(ctx, ch)
		if err != nil {
			d.recordFailure(ctx, ch, "resolve webhook URL", err, "")
			continue
		}

		// Find sender
		sender, ok := d.senders[ch.Spec.Type]
		if !ok {
			d.recordFailure(ctx, ch, "select notification sender", fmt.Errorf("unsupported provider type %q", ch.Spec.Type), "")
			continue
		}

		// Send
		if err := sender.Send(ctx, url, event); err != nil {
			d.recordFailure(ctx, ch, "send notification", err, url)
			continue
		} else {
			d.mu.Lock()
			d.throttle[key] = time.Now().Add(throttleDur)
			d.mu.Unlock()
			now := metav1.Now()
			ch.Status.TotalSent++
			ch.Status.LastNotification = &now
			ch.Status.LastError = ""
			log.Info("notification sent", "channel", ch.Name, "type", ch.Spec.Type, "target", event.Target.Name)
		}

		// Update status
		if err := d.client.Status().Update(ctx, ch); err != nil {
			log.Error(err, "failed to update channel status", "channel", ch.Name, "namespace", ch.Namespace)
		}
	}
}

func (d *Dispatcher) pruneThrottle(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, expiresAt := range d.throttle {
		if !now.Before(expiresAt) {
			delete(d.throttle, key)
		}
	}
}

func (d *Dispatcher) resolveWebhookURL(ctx context.Context, channel *v1alpha1.PowerNotificationChannel) (string, error) {
	if channel.Spec.URL != "" {
		return channel.Spec.URL, nil
	}
	if channel.Spec.URLFrom == nil {
		return "", fmt.Errorf("channel has neither url nor urlFrom configured")
	}

	var secret corev1.Secret
	key := client.ObjectKey{Namespace: channel.Namespace, Name: channel.Spec.URLFrom.Name}
	if err := d.client.Get(ctx, key, &secret); err != nil {
		return "", fmt.Errorf("read referenced Secret %s/%s: %w", key.Namespace, key.Name, err)
	}
	value, ok := secret.Data[channel.Spec.URLFrom.Key]
	if !ok {
		return "", fmt.Errorf("referenced Secret %s/%s does not contain key %q", key.Namespace, key.Name, channel.Spec.URLFrom.Key)
	}
	url := strings.TrimSpace(string(value))
	if url == "" {
		return "", fmt.Errorf("referenced Secret %s/%s contains an empty key %q", key.Namespace, key.Name, channel.Spec.URLFrom.Key)
	}
	return url, nil
}

func (d *Dispatcher) recordFailure(ctx context.Context, channel *v1alpha1.PowerNotificationChannel, operation string, err error, secretURL string) {
	safeErr := redactDeliveryError(err, secretURL)
	ctrl.Log.WithName("notifications").Error(safeErr, "notification delivery failed",
		"operation", operation, "channel", channel.Name, "namespace", channel.Namespace, "type", channel.Spec.Type)
	channel.Status.TotalErrors++
	channel.Status.LastError = safeErr.Error()
	if updateErr := d.client.Status().Update(ctx, channel); updateErr != nil {
		ctrl.Log.WithName("notifications").Error(updateErr, "failed to update channel failure status", "channel", channel.Name, "namespace", channel.Namespace)
	}
}

func redactDeliveryError(err error, secretURL string) error {
	message := err.Error()
	if secretURL != "" {
		message = strings.ReplaceAll(message, secretURL, "<redacted>")
	}
	return fmt.Errorf("%s", message)
}

func batchThrottleKey(channel *v1alpha1.PowerNotificationChannel, events []Event) string {
	identities := make([]string, 0, len(events))
	for _, event := range events {
		identities = append(identities, eventIdentity(event))
	}
	sort.Strings(identities)
	digest := sha256.Sum256([]byte(strings.Join(identities, "\n")))
	return fmt.Sprintf("%s/%s/batch/%x", channel.Namespace, channel.Name, digest[:16])
}

func eventIdentity(event Event) string {
	return event.Action + "/" + event.Result + "/" + event.RuleName + "/" + event.Target.Namespace + "/" + event.Target.Kind + "/" + event.Target.Name + "/" + event.Target.UID
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
