package redeemer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type PlayerInfo struct {
	Nickname     string `json:"nickname"`
	Kingdom      int    `json:"kingdom"`
	Level        int    `json:"level"`
	LevelImage   any    `json:"levelImage"`
	ProfilePhoto string `json:"profilePhoto"`
}

type Result struct {
	AccountID  string      `json:"accountId"`
	Status     string      `json:"status"`
	Message    string      `json:"message"`
	PlayerInfo *PlayerInfo `json:"playerInfo,omitempty"`
}

type Summary struct {
	GiftCode  string    `json:"giftCode"`
	Timestamp time.Time `json:"timestamp"`
	Total     int       `json:"total"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Results   []Result  `json:"results"`
}

type streamEvent struct {
	Status     string      `json:"status"`
	Message    string      `json:"message"`
	AccountID  string      `json:"accountId"`
	PlayerInfo *PlayerInfo `json:"playerInfo,omitempty"`
}

// Redeem sends a bulk redeem request for the given gift code and player IDs.
// It streams the SSE response and returns a Summary of results.
func Redeem(ctx context.Context, code string, playerIDs []string, redeemURL string) (*Summary, error) {
	payload, err := json.Marshal(map[string]any{
		"giftCode":   code,
		"accountIds": playerIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, redeemURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", res.StatusCode)
	}

	var results []Result
	scanner := bufio.NewScanner(res.Body)

	var buf strings.Builder
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		var event streamEvent
		if err := json.Unmarshal([]byte(buf.String()), &event); err == nil {
			if event.AccountID != "" && event.Status != "processing" {
				results = append(results, Result{
					AccountID:  event.AccountID,
					Status:     event.Status,
					Message:    event.Message,
					PlayerInfo: event.PlayerInfo,
				})
			}
		}
		buf.Reset()
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			flush()
			buf.WriteString(strings.TrimPrefix(line, "data: "))
		} else if buf.Len() > 0 {
			buf.WriteString(line)
		}
	}
	flush()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}

	summary := &Summary{
		GiftCode:  code,
		Timestamp: time.Now().UTC(),
		Total:     len(playerIDs),
		Results:   results,
	}
	for _, r := range results {
		if r.Status == "success" {
			summary.Succeeded++
		} else {
			summary.Failed++
		}
	}

	return summary, nil
}
