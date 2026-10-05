package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// NewLangdock calls Langdock's OpenAI-compatible chat completions endpoint
// (https://api.langdock.com/openai/{region}/v1/chat/completions) with the same JSON
// schema the other backends enforce. Any model the workspace exposes works.
func NewLangdock(region, model, apiKey string) Ask {
	url := fmt.Sprintf("https://api.langdock.com/openai/%s/v1/chat/completions", region)
	client := &http.Client{Timeout: callTimeout}
	return func(ctx context.Context, system, user string, dest any) error {
		schema, err := schemaOf(dest)
		if err != nil {
			return err
		}
		body, err := json.Marshal(map[string]any{
			"model": model,
			"messages": []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": user},
			},
			"response_format": map[string]any{
				"type":        "json_schema",
				"json_schema": map[string]any{"name": "verdict", "strict": true, "schema": json.RawMessage(schema)},
			},
		})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("langdock: %s: %s", resp.Status, bytes.TrimSpace(raw[:min(len(raw), 300)]))
		}
		var out struct {
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Message      struct {
					Content string `json:"content"`
					Refusal string `json:"refusal"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
			return fmt.Errorf("langdock: unexpected answer: %v", err)
		}
		c := out.Choices[0]
		switch {
		case c.Message.Refusal != "", c.FinishReason == "content_filter":
			return ErrRefused
		case c.FinishReason == "length":
			return fmt.Errorf("answer cut off")
		}
		if err := json.Unmarshal([]byte(c.Message.Content), dest); err != nil {
			return fmt.Errorf("parse answer: %w", err)
		}
		return nil
	}
}
