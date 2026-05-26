package redeemer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Result struct {
	PlayerID string `json:"playerId"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

type redeemResponse struct {
	Status  string `json:"status"`
	Data    *struct {
		Redemption string `json:"redemption"`
	} `json:"data"`
	Message string `json:"message"`
}

// Redeem redeems a gift code for a single player using the cookie-auth endpoint.
func Redeem(ctx context.Context, code, playerID, redeemURL, sessionToken string) (*Result, error) {
	payload, err := json.Marshal(map[string]any{
		"giftCode": code,
		"playerId": playerID,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, redeemURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "__Secure-next-auth.session-token="+sessionToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		var errResp redeemResponse
		json.NewDecoder(res.Body).Decode(&errResp)
		msg := errResp.Message
		if msg == "" {
			msg = fmt.Sprintf("unexpected status: %d", res.StatusCode)
		}
		return nil, fmt.Errorf("redeem failed: %s", msg)
	}

	var resp redeemResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	status := "error"
	if resp.Data != nil && resp.Data.Redemption == "SUCCESS" {
		status = "success"
	}

	return &Result{
		PlayerID: playerID,
		Status:   status,
		Message:  resp.Message,
	}, nil
}
