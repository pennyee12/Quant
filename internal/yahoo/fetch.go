package yahoo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pennyee12/Quant/internal/quant"
)

const userAgent = "Mozilla/5.0 (compatible; quant-paper-trader/1.0)"

// FetchDaily fetches daily OHLCV bars for a symbol from Yahoo Finance.
// startTime is inclusive; pass zero to get maximum available history.
func FetchDaily(symbol string, startTime time.Time, endTime time.Time) ([]quant.Bar, error) {
	period1 := int64(0)
	if !startTime.IsZero() {
		period1 = startTime.Unix()
	}
	period2 := endTime.Unix()

	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d&period1=%d&period2=%d&includePrePost=false",
		symbol, period1, period2,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yahoo fetch %s: %w", symbol, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("yahoo fetch %s: HTTP %d", symbol, resp.StatusCode)
	}

	var raw struct {
		Chart struct {
			Result []struct {
				Timestamps []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Open   []float64 `json:"open"`
						High   []float64 `json:"high"`
						Low    []float64 `json:"low"`
						Close  []float64 `json:"close"`
						Volume []int64   `json:"volume"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
			Error *struct {
				Code        string `json:"code"`
				Description string `json:"description"`
			} `json:"error"`
		} `json:"chart"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("yahoo decode %s: %w", symbol, err)
	}
	if raw.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo error %s: %s — %s", symbol, raw.Chart.Error.Code, raw.Chart.Error.Description)
	}
	if len(raw.Chart.Result) == 0 {
		return nil, fmt.Errorf("yahoo: no data for %s", symbol)
	}

	r := raw.Chart.Result[0]
	if len(r.Indicators.Quote) == 0 {
		return nil, fmt.Errorf("yahoo: no quote data for %s", symbol)
	}
	q := r.Indicators.Quote[0]
	n := len(r.Timestamps)

	bars := make([]quant.Bar, 0, n)
	for i := 0; i < n; i++ {
		if i >= len(q.Open) || i >= len(q.Close) {
			break
		}
		// Skip bars with missing data (Yahoo uses null → 0 after decode)
		if q.Open[i] == 0 || q.Close[i] == 0 {
			continue
		}
		bars = append(bars, quant.Bar{
			TimeMS: r.Timestamps[i] * 1000,
			Open:   q.Open[i],
			High:   q.High[i],
			Low:    q.Low[i],
			Close:  q.Close[i],
			Volume: q.Volume[i],
		})
	}
	return bars, nil
}
