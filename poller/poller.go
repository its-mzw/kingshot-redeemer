package poller

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

type healthResponse struct {
	Status string `json:"status"`
	Checks struct {
		Database string `json:"database"`
		Server   string `json:"server"`
	} `json:"checks"`
	Error *string `json:"error"`
}

// CheckHealth calls the health endpoint and returns an error if the API is not healthy.
func CheckHealth(url string) error {
	res, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("health check: unexpected status %d", res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("health check: read body: %w", err)
	}

	var h healthResponse
	if err := json.Unmarshal(body, &h); err != nil {
		return fmt.Errorf("health check: decode: %w", err)
	}

	if h.Status != "healthy" {
		if h.Error != nil {
			return fmt.Errorf("api unhealthy: %s", *h.Error)
		}
		return fmt.Errorf("api unhealthy (db=%s, server=%s)", h.Checks.Database, h.Checks.Server)
	}

	return nil
}

// GiftCode represents an active gift code returned by the API.
type GiftCode struct {
	ID        int     `json:"id"`
	Code      string  `json:"code"`
	ExpiresAt *string `json:"expiresAt"`
	CreatedAt string  `json:"createdAt"`
}

type apiResponse struct {
	Status string `json:"status"`
	Data   struct {
		GiftCodes    []GiftCode `json:"giftCodes"`
		Total        int        `json:"total"`
		ActiveCount  int        `json:"activeCount"`
		ExpiredCount int        `json:"expiredCount"`
	} `json:"data"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// FetchActiveCodes retrieves the list of currently active gift codes from the API.
func FetchActiveCodes(url string) ([]GiftCode, error) {
	res, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch codes: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch codes: unexpected status %d", res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("fetch codes: read body: %w", err)
	}

	var result apiResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("fetch codes: decode: %w", err)
	}

	return result.Data.GiftCodes, nil
}
