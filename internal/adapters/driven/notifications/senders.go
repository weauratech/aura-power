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
							},
						},
					},
				},
			},
		},
	}
	return httpPost(ctx, url, payload)
}

// SlackSender sends messages to Slack via incoming webhook.
type SlackSender struct{}

func (s *SlackSender) Type() string { return "slack" }

func (s *SlackSender) Send(ctx context.Context, url string, event Event) error {
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
					{"type": "mrkdwn", "text": event.Reason + " • " + event.Timestamp.Format("15:04 UTC")},
				},
			},
		},
	}
	return httpPost(ctx, url, payload)
}

// GenericSender sends a raw JSON payload to any webhook endpoint.
type GenericSender struct{}

func (s *GenericSender) Type() string { return "generic" }

func (s *GenericSender) Send(ctx context.Context, url string, event Event) error {
	payload := map[string]interface{}{
		"version":   "1",
		"event":     event.Action,
		"timestamp": event.Timestamp.Format(time.RFC3339),
		"target": map[string]string{
			"namespace": event.Target.Namespace,
			"name":      event.Target.Name,
			"kind":      event.Target.Kind,
		},
		"action": map[string]string{
			"type":     event.Action,
			"result":   event.Result,
			"reason":   event.Reason,
			"ruleName": event.RuleName,
		},
	}
	return httpPost(ctx, url, payload)
}

// httpPost sends a JSON POST request with retry (3 attempts, exponential backoff).
func httpPost(ctx context.Context, url string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*attempt) * time.Second)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("webhook returned %d", resp.StatusCode)
	}

	return lastErr
}

func formatActionLabel(action string) string {
	labels := map[string]string{
		"workload.powered_down": "Workload Powered Down",
		"workload.restored":     "Workload Restored",
		"workload.error":        "Execution Error",
		"override.created":      "Override Created",
		"override.expired":      "Override Expired",
	}
	if l, ok := labels[action]; ok {
		return l
	}
	return action
}
