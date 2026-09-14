package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GoogleChatSender sends card-formatted messages to Google Chat.
type GoogleChatSender struct{}

func (s *GoogleChatSender) Type() string { return "google-chat" }

func (s *GoogleChatSender) Send(ctx context.Context, url string, event Event) error {
	_, err := s.SendWithResult(ctx, url, event)
	return err
}

func (s *GoogleChatSender) SendWithResult(ctx context.Context, url string, event Event) (DeliveryResponse, error) {
	payload := map[string]interface{}{
		"cardsV2": []map[string]interface{}{
			{
				"cardId": "aura-power-event",
				"card": map[string]interface{}{
					"header": map[string]interface{}{
						"title":    "Aura Power",
						"subtitle": formatActionLabel(event.Action),
					},
					"sections": []map[string]interface{}{
						{
							"widgets": []map[string]interface{}{
								{"decoratedText": map[string]string{"topLabel": "Target", "text": fmt.Sprintf("%s/%s (%s)", event.Target.Namespace, event.Target.Name, event.Target.Kind)}},
								{"decoratedText": map[string]string{"topLabel": "Action", "text": event.Reason}},
								{"decoratedText": map[string]string{"topLabel": "Rule", "text": event.RuleName}},
								{"decoratedText": map[string]string{"topLabel": "Result", "text": event.Result}},
								{"decoratedText": map[string]string{"topLabel": "Time", "text": event.Timestamp.Format(time.RFC3339)}},
								{"decoratedText": map[string]string{"topLabel": "Correlation", "text": event.AttemptID}},
							},
						},
					},
				},
			},
		},
	}
	return httpPostDetailedWithKey(ctx, url, payload, event.IdempotencyKey)
}

// SlackSender sends messages to Slack via incoming webhook.
type SlackSender struct{}

func (s *SlackSender) Type() string { return "slack" }

func (s *SlackSender) Send(ctx context.Context, url string, event Event) error {
	_, err := s.SendWithResult(ctx, url, event)
	return err
}

func (s *SlackSender) SendWithResult(ctx context.Context, url string, event Event) (DeliveryResponse, error) {
	payload := map[string]interface{}{
		"blocks": []map[string]interface{}{
			{
				"type": "header",
				"text": map[string]string{"type": "plain_text", "text": "Aura Power: " + formatActionLabel(event.Action)},
			},
			{
				"type": "section",
				"fields": []map[string]string{
					{"type": "mrkdwn", "text": fmt.Sprintf("*Target:*\n%s/%s", event.Target.Namespace, event.Target.Name)},
					{"type": "mrkdwn", "text": fmt.Sprintf("*Kind:*\n%s", event.Target.Kind)},
					{"type": "mrkdwn", "text": fmt.Sprintf("*Rule:*\n%s", event.RuleName)},
					{"type": "mrkdwn", "text": fmt.Sprintf("*Result:*\n%s", event.Result)},
				},
			},
			{
				"type": "context",
				"elements": []map[string]string{
					{"type": "mrkdwn", "text": event.Reason + " • " + event.Timestamp.Format("15:04 UTC") + " • " + event.AttemptID},
				},
			},
		},
	}
	return httpPostDetailedWithKey(ctx, url, payload, event.IdempotencyKey)
}

// GenericSender sends a raw JSON payload to any webhook endpoint.
type GenericSender struct{}

func (s *GenericSender) Type() string { return "generic" }

func (s *GenericSender) Send(ctx context.Context, url string, event Event) error {
	_, err := s.SendWithResult(ctx, url, event)
	return err
}

func (s *GenericSender) SendWithResult(ctx context.Context, url string, event Event) (DeliveryResponse, error) {
	payload := map[string]interface{}{
		"version":   "1",
		"event":     event.Action,
		"timestamp": event.Timestamp.Format(time.RFC3339),
		"correlation": map[string]interface{}{
			"attemptID":      event.AttemptID,
			"eventIDs":       event.EventIDs,
			"auditEventRefs": event.AuditEventRefs,
		},
		"target": map[string]string{
			"namespace": event.Target.Namespace,
			"name":      event.Target.Name,
			"kind":      event.Target.Kind,
			"uid":       event.Target.UID,
		},
		"action": map[string]string{
			"type":     event.Action,
			"result":   event.Result,
			"reason":   event.Reason,
			"ruleName": event.RuleName,
		},
	}
	return httpPostDetailedWithKey(ctx, url, payload, event.IdempotencyKey)
}

// DeliveryResponse is safe provider metadata. Response bodies and destination
// URLs are deliberately excluded because they may contain secrets.
type DeliveryResponse struct {
	StatusCode int
	Attempts   int
}

// httpPost sends a JSON POST request with retry (3 attempts, exponential backoff).
func httpPost(ctx context.Context, url string, payload interface{}) error {
	_, err := httpPostDetailed(ctx, url, payload)
	return err
}

func httpPostDetailed(ctx context.Context, url string, payload interface{}) (DeliveryResponse, error) {
	return httpPostDetailedWithKey(ctx, url, payload, "")
}

func httpPostDetailedWithKey(ctx context.Context, url string, payload interface{}, idempotencyKey string) (DeliveryResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return DeliveryResponse{}, fmt.Errorf("marshal payload: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error
	lastStatus := 0

	maxAttempts := 3
	if idempotencyKey != "" {
		// Durable outbox attempts are checkpointed individually. Retrying here
		// would create an unobservable crash boundary inside one record attempt.
		maxAttempts = 1
	}
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			backoff := time.NewTimer(time.Duration(attempt*attempt) * time.Second)
			select {
			case <-ctx.Done():
				if !backoff.Stop() {
					select {
					case <-backoff.C:
					default:
					}
				}
				return DeliveryResponse{Attempts: attempt}, ctx.Err()
			case <-backoff.C:
			}
		}
		if err := ctx.Err(); err != nil {
			return DeliveryResponse{Attempts: attempt}, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return DeliveryResponse{Attempts: attempt + 1}, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		lastStatus = resp.StatusCode

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return DeliveryResponse{StatusCode: resp.StatusCode, Attempts: attempt + 1}, nil
		}
		lastErr = fmt.Errorf("webhook returned %d", resp.StatusCode)
	}

	return DeliveryResponse{StatusCode: lastStatus, Attempts: maxAttempts}, lastErr
}

func formatActionLabel(action string) string {
	labels := map[string]string{
		"workload.powered_down": "Workload Powered Down",
		"workload.restored":     "Workload Restored",
		"execution.error":       "Execution Error",
		"override.created":      "Override Created",
		"override.expired":      "Override Expired",
	}
	if l, ok := labels[action]; ok {
		return l
	}
	return action
}
