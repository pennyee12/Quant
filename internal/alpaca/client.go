package alpaca

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pennyee12/Quant/internal/quant"
)

const (
	defaultTradingBaseURL = "https://paper-api.alpaca.markets"
	defaultDataBaseURL    = "https://data.alpaca.markets"
)

type Client struct {
	KeyID          string
	SecretKey      string
	TradingBaseURL string
	DataBaseURL    string
	HTTP           *http.Client
}

type Account struct {
	AccountNumber    string `json:"account_number"`
	Status           string `json:"status"`
	Currency         string `json:"currency"`
	BuyingPower      string `json:"buying_power"`
	Cash             string `json:"cash"`
	PortfolioValue   string `json:"portfolio_value"`
	TradingBlocked   bool   `json:"trading_blocked"`
	AccountBlocked   bool   `json:"account_blocked"`
	TransfersBlocked bool   `json:"transfers_blocked"`
}

type Position struct {
	Symbol        string `json:"symbol"`
	Qty           string `json:"qty"`
	MarketValue   string `json:"market_value"`
	CurrentPrice  string `json:"current_price"`
	AvgEntryPrice string `json:"avg_entry_price"`
	Side          string `json:"side"`
}

type OrderRequest struct {
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	Type        string `json:"type"`
	TimeInForce string `json:"time_in_force"`
	Notional    string `json:"notional,omitempty"`
	Qty         string `json:"qty,omitempty"`
}

type Order struct {
	ID            string `json:"id"`
	ClientOrderID string `json:"client_order_id"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Type          string `json:"type"`
	Status        string `json:"status"`
	Notional      string `json:"notional"`
	Qty           string `json:"qty"`
	CreatedAt     string `json:"created_at"`
}

func NewFromEnv(envPath string) (*Client, error) {
	if envPath != "" {
		_ = LoadDotEnv(envPath)
	}
	c := &Client{
		KeyID:          os.Getenv("ALPACA_API_KEY_ID"),
		SecretKey:      os.Getenv("ALPACA_API_SECRET_KEY"),
		TradingBaseURL: strings.TrimRight(os.Getenv("ALPACA_TRADING_BASE_URL"), "/"),
		DataBaseURL:    strings.TrimRight(os.Getenv("ALPACA_DATA_BASE_URL"), "/"),
		HTTP:           &http.Client{Timeout: 20 * time.Second},
	}
	if c.TradingBaseURL == "" {
		c.TradingBaseURL = defaultTradingBaseURL
	}
	if c.DataBaseURL == "" {
		c.DataBaseURL = defaultDataBaseURL
	}
	if c.KeyID == "" || c.SecretKey == "" {
		return nil, fmt.Errorf("missing ALPACA_API_KEY_ID or ALPACA_API_SECRET_KEY")
	}
	return c, nil
}

func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if k != "" && os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
	return scanner.Err()
}

func (c *Client) GetAccount() (Account, string, error) {
	var out Account
	req, err := c.newRequest(http.MethodGet, c.TradingBaseURL+"/v2/account", nil)
	if err != nil {
		return out, "", err
	}
	requestID, err := c.doJSON(req, &out)
	return out, requestID, err
}

func (c *Client) GetPositions() ([]Position, string, error) {
	var out []Position
	req, err := c.newRequest(http.MethodGet, c.TradingBaseURL+"/v2/positions", nil)
	if err != nil {
		return nil, "", err
	}
	requestID, err := c.doJSON(req, &out)
	return out, requestID, err
}

func (c *Client) FetchDailyBars(symbol string, start, end time.Time, feed string) ([]quant.Bar, string, error) {
	if feed == "" {
		feed = "iex"
	}
	u, err := url.Parse(c.DataBaseURL + "/v2/stocks/" + url.PathEscape(symbol) + "/bars")
	if err != nil {
		return nil, "", err
	}
	q := u.Query()
	q.Set("timeframe", "1Day")
	q.Set("start", start.Format("2006-01-02"))
	q.Set("end", end.Format("2006-01-02"))
	q.Set("adjustment", "split")
	q.Set("feed", feed)
	q.Set("limit", "10000")
	q.Set("sort", "asc")
	u.RawQuery = q.Encode()

	req, err := c.newRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	var raw struct {
		Bars []struct {
			T string  `json:"t"`
			O float64 `json:"o"`
			H float64 `json:"h"`
			L float64 `json:"l"`
			C float64 `json:"c"`
			V int64   `json:"v"`
		} `json:"bars"`
		NextPageToken string `json:"next_page_token"`
	}
	requestID, err := c.doJSON(req, &raw)
	if err != nil {
		return nil, requestID, err
	}
	bars := make([]quant.Bar, 0, len(raw.Bars))
	for _, b := range raw.Bars {
		t, err := time.Parse(time.RFC3339, b.T)
		if err != nil || b.O <= 0 || b.C <= 0 {
			continue
		}
		bars = append(bars, quant.Bar{
			TimeMS: t.UnixMilli(),
			Open:   b.O,
			High:   b.H,
			Low:    b.L,
			Close:  b.C,
			Volume: b.V,
		})
	}
	return bars, requestID, nil
}

func (c *Client) SubmitMarketNotional(symbol, side string, notional float64) (Order, string, error) {
	var out Order
	body := OrderRequest{
		Symbol:      symbol,
		Side:        side,
		Type:        "market",
		TimeInForce: "day",
		Notional:    fmt.Sprintf("%.2f", notional),
	}
	req, err := c.newRequest(http.MethodPost, c.TradingBaseURL+"/v2/orders", body)
	if err != nil {
		return out, "", err
	}
	requestID, err := c.doJSON(req, &out)
	return out, requestID, err
}

func (c *Client) SubmitMarketQty(symbol, side string, qty float64) (Order, string, error) {
	var out Order
	body := OrderRequest{
		Symbol:      symbol,
		Side:        side,
		Type:        "market",
		TimeInForce: "day",
		Qty:         strconv.FormatFloat(qty, 'f', 6, 64),
	}
	req, err := c.newRequest(http.MethodPost, c.TradingBaseURL+"/v2/orders", body)
	if err != nil {
		return out, "", err
	}
	requestID, err := c.doJSON(req, &out)
	return out, requestID, err
}

func (c *Client) newRequest(method, rawURL string, body any) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, rawURL, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("APCA-API-KEY-ID", c.KeyID)
	req.Header.Set("APCA-API-SECRET-KEY", c.SecretKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) doJSON(req *http.Request, out any) (string, error) {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	requestID := resp.Header.Get("X-Request-ID")
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return requestID, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return requestID, fmt.Errorf("alpaca %s %s failed: HTTP %d request_id=%s body=%s", req.Method, req.URL.Path, resp.StatusCode, requestID, string(raw))
	}
	if len(raw) == 0 || out == nil {
		return requestID, nil
	}
	return requestID, json.Unmarshal(raw, out)
}

func ParseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
