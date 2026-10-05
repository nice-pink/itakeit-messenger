package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// NewAPI calls the Messages API with ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN or an
// `ant auth login` profile.
func NewAPI(model string) Ask {
	client := anthropic.NewClient()
	return func(ctx context.Context, system, user string, dest any) error {
		msg, err := client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
			Model:        anthropic.Model(model),
			MaxTokens:    1024,
			OutputConfig: anthropic.BetaOutputConfigParam{Format: anthropic.BetaJSONOutputFormatParam{Schema: dest}},
			System:       []anthropic.BetaTextBlockParam{{Text: system, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}},
			Messages:     []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(user))},
		})
		if err != nil {
			return err
		}
		switch msg.StopReason {
		case anthropic.BetaStopReasonRefusal:
			return ErrRefused
		case anthropic.BetaStopReasonMaxTokens:
			return fmt.Errorf("answer cut off")
		}
		var b strings.Builder
		for _, block := range msg.Content {
			if block.Type == "text" {
				b.WriteString(block.Text)
			}
		}
		if err := json.Unmarshal([]byte(b.String()), dest); err != nil {
			return fmt.Errorf("parse answer: %w", err)
		}
		return nil
	}
}
