package system

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// UmamiStats Umami 統計指標
type UmamiStats struct {
	Pageviews int64 `json:"pageviews"`
	Visitors  int64 `json:"visitors"`
	Visits    int64 `json:"visits"`
	Bounces   int64 `json:"bounces"`
	Totaltime int64 `json:"totaltime"`
}

// FunnelStep funnel 步驟定義
type FunnelStep struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// FunnelResult funnel 查詢結果
type FunnelResult struct {
	Type      string  `json:"type"`
	Value     string  `json:"value"`
	Visitors  int64   `json:"visitors"`
	Previous  int64   `json:"previous"`
	Dropped   int64   `json:"dropped"`
	Dropoff   float64 `json:"dropoff"`
	Remaining float64 `json:"remaining"`
}

// UmamiService Umami API client 介面
type UmamiService interface {
	GetStats(websiteID, channelCode string, startAt, endAt int64) (*UmamiStats, error)
	GetFunnelSteps(reportID string) ([]FunnelStep, int, error)
	RunFunnel(websiteID, channelCode string, steps []FunnelStep, window int, startAt, endAt int64) ([]FunnelResult, error)
}

type umamiService struct {
	configSvc  ConfigService
	httpClient *http.Client
}

// NewUmamiService 建立 Umami service
func NewUmamiService(configSvc ConfigService) UmamiService {
	return &umamiService{
		configSvc: configSvc,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *umamiService) baseURL() string {
	v, _ := s.configSvc.GetConfig("umami.base_url")
	return v
}

func (s *umamiService) apiToken() string {
	v, _ := s.configSvc.GetConfig("umami.api_token")
	return v
}

// doRequest 執行帶認證的 HTTP 請求
func (s *umamiService) doRequest(req *http.Request) ([]byte, error) {
	req.Header.Set("Authorization", "Bearer "+s.apiToken())
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("umami request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("umami returned status %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// GetStats 取得網站統計指標，filter by ad=channel_code
func (s *umamiService) GetStats(websiteID, channelCode string, startAt, endAt int64) (*UmamiStats, error) {
	endpoint := fmt.Sprintf("%s/api/websites/%s/stats", s.baseURL(), websiteID)
	params := url.Values{}
	params.Set("startAt", strconv.FormatInt(startAt, 10))
	params.Set("endAt", strconv.FormatInt(endAt, 10))
	params.Set("unit", "day")
	params.Set("query", "c."+channelCode)

	req, err := http.NewRequest(http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	body, err := s.doRequest(req)
	if err != nil {
		return nil, err
	}

	var stats UmamiStats
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, fmt.Errorf("parse stats response failed: %w", err)
	}

	return &stats, nil
}

// GetFunnelSteps 取得已存 funnel report 的 steps 定義和 window
func (s *umamiService) GetFunnelSteps(reportID string) ([]FunnelStep, int, error) {
	url := fmt.Sprintf("%s/api/reports/%s", s.baseURL(), reportID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}

	body, err := s.doRequest(req)
	if err != nil {
		return nil, 0, err
	}

	var report struct {
		Parameters struct {
			Steps  []FunnelStep `json:"steps"`
			Window interface{}  `json:"window"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		return nil, 0, fmt.Errorf("parse report response failed: %w", err)
	}

	window := 60
	switch v := report.Parameters.Window.(type) {
	case float64:
		if v > 0 {
			window = int(v)
		}
	case string:
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			window = n
		}
	}

	return report.Parameters.Steps, window, nil
}

// RunFunnel 用指定 steps 執行 funnel 查詢，filter by ad=channel_code
func (s *umamiService) RunFunnel(websiteID, channelCode string, steps []FunnelStep, window int, startAt, endAt int64) ([]FunnelResult, error) {
	startDate := time.UnixMilli(startAt).UTC().Format(time.RFC3339)
	endDate := time.UnixMilli(endAt).UTC().Format(time.RFC3339)

	payload := map[string]interface{}{
		"websiteId": websiteID,
		"type":      "funnel",
		"filters": map[string]string{
			"query": "c." + channelCode,
		},
		"parameters": map[string]interface{}{
			"startDate": startDate,
			"endDate":   endDate,
			"steps":     steps,
			"window":    window,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/reports/funnel", s.baseURL())
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}

	body, err := s.doRequest(req)
	if err != nil {
		return nil, err
	}

	var results []FunnelResult
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("parse funnel response failed: %w", err)
	}

	return results, nil
}
