package schwab

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/pennyee12/Quant/internal/quant"
)

type Client struct {
	BaseURL          string
	TokenURL         string
	TokenFile        string
	CredentialScript string
	HTTP             *http.Client
}

func New(baseURL, tokenURL, tokenFile, credentialScript string) *Client {
	return &Client{
		BaseURL:          baseURL,
		TokenURL:         tokenURL,
		TokenFile:        tokenFile,
		CredentialScript: credentialScript,
		HTTP:             &http.Client{Timeout: 20 * time.Second},
	}
}

type tokenEnvelope struct {
	Token tokenPayload `json:"token"`
}

type tokenPayload struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IssuedAt     int64  `json:"issued_at"`
	ExpiresIn    int64  `json:"expires_in"`
}

func (c *Client) accessToken(forceRefresh bool) (string, error) {
	blob, tok, err := c.loadToken()
	if err != nil {
		return "", err
	}
	if !forceRefresh && tok.AccessToken != "" && tokenStillGood(tok) {
		return tok.AccessToken, nil
	}
	if tok.RefreshToken == "" {
		if tok.AccessToken == "" {
			return "", fmt.Errorf("token file %s does not contain access_token", c.TokenFile)
		}
		return tok.AccessToken, nil
	}
	refreshed, err := c.refreshToken(tok.RefreshToken)
	if err != nil {
		if tok.AccessToken != "" {
			return tok.AccessToken, nil
		}
		return "", err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = tok.RefreshToken
	}
	refreshed.IssuedAt = time.Now().Unix()
	if err := c.saveToken(blob, refreshed); err != nil {
		return "", err
	}
	return refreshed.AccessToken, nil
}

func (c *Client) loadToken() (map[string]json.RawMessage, tokenPayload, error) {
	raw, err := os.ReadFile(c.TokenFile)
	if err != nil {
		return nil, tokenPayload{}, err
	}
	var blob map[string]json.RawMessage
	if err := json.Unmarshal(raw, &blob); err != nil {
		return nil, tokenPayload{}, err
	}
	if wrappedRaw, ok := blob["token"]; ok {
		var wrapped tokenEnvelope
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			return nil, tokenPayload{}, err
		}
		if len(wrappedRaw) > 0 {
			return blob, wrapped.Token, nil
		}
	}
	var direct tokenPayload
	if err := json.Unmarshal(raw, &direct); err != nil {
		return nil, tokenPayload{}, err
	}
	return blob, direct, nil
}

func (c *Client) saveToken(blob map[string]json.RawMessage, tok tokenPayload) error {
	rawToken, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	if _, ok := blob["token"]; ok {
		blob["token"] = rawToken
		raw, err := json.MarshalIndent(blob, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(c.TokenFile, raw, 0o600)
	}
	return os.WriteFile(c.TokenFile, rawToken, 0o600)
}

func tokenStillGood(tok tokenPayload) bool {
	if tok.IssuedAt <= 0 || tok.ExpiresIn <= 0 {
		return false
	}
	return time.Now().Unix()-tok.IssuedAt < tok.ExpiresIn-240
}

func (c *Client) refreshToken(refreshToken string) (tokenPayload, error) {
	appKey, appSecret := c.loadCredentials()
	if appKey == "" || appSecret == "" {
		return tokenPayload{}, fmt.Errorf("missing SCHWAB_APP_KEY/SCHWAB_SECRET")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequest(http.MethodPost, c.TokenURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return tokenPayload{}, err
	}
	basic := base64.StdEncoding.EncodeToString([]byte(appKey + ":" + appSecret))
	req.Header.Set("Authorization", "Basic "+basic)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return tokenPayload{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return tokenPayload{}, fmt.Errorf("schwab token refresh failed: HTTP %d %s", resp.StatusCode, string(body))
	}
	var tok tokenPayload
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return tokenPayload{}, err
	}
	return tok, nil
}

func (c *Client) loadCredentials() (string, string) {
	appKey := os.Getenv("SCHWAB_APP_KEY")
	appSecret := os.Getenv("SCHWAB_SECRET")
	if appKey != "" && appSecret != "" {
		return appKey, appSecret
	}
	if c.CredentialScript == "" {
		return appKey, appSecret
	}
	raw, err := os.ReadFile(c.CredentialScript)
	if err != nil {
		return appKey, appSecret
	}
	text := string(raw)
	if appKey == "" {
		appKey = firstMatch(text, `SCHWAB_APP_KEY="([^"]+)"`)
	}
	if appSecret == "" {
		appSecret = firstMatch(text, `SCHWAB_SECRET="([^"]+)"`)
	}
	return appKey, appSecret
}

func firstMatch(text, pattern string) string {
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(text)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func (c *Client) PriceHistory(symbol, periodType string, period int, frequencyType string, frequency int, start, end time.Time) ([]quant.Bar, error) {
	return c.priceHistory(symbol, periodType, period, frequencyType, frequency, start, end, false)
}

func (c *Client) AccountNumbers() ([]AccountNumber, error) {
	token, err := c.accessToken(false)
	if err != nil {
		return nil, err
	}
	var out []AccountNumber
	if err := c.getJSON("/trader/v1/accounts/accountNumbers", nil, token, &out); err != nil {
		if isUnauthorized(err) {
			token, refreshErr := c.accessToken(true)
			if refreshErr != nil {
				return nil, refreshErr
			}
			return out, c.getJSON("/trader/v1/accounts/accountNumbers", nil, token, &out)
		}
		return nil, err
	}
	return out, nil
}

func (c *Client) Account(hashValue string, fields string) (AccountResponse, error) {
	token, err := c.accessToken(false)
	if err != nil {
		return AccountResponse{}, err
	}
	q := url.Values{}
	if fields != "" {
		q.Set("fields", fields)
	}
	var out AccountResponse
	path := "/trader/v1/accounts/" + hashValue
	if err := c.getJSON(path, q, token, &out); err != nil {
		if isUnauthorized(err) {
			token, refreshErr := c.accessToken(true)
			if refreshErr != nil {
				return AccountResponse{}, refreshErr
			}
			return out, c.getJSON(path, q, token, &out)
		}
		return AccountResponse{}, err
	}
	return out, nil
}

func (c *Client) priceHistory(symbol, periodType string, period int, frequencyType string, frequency int, start, end time.Time, forceRefresh bool) ([]quant.Bar, error) {
	token, err := c.accessToken(forceRefresh)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(c.BaseURL + "/marketdata/v1/pricehistory")
	if err != nil {
		return nil, err
	}
	q := endpoint.Query()
	q.Set("symbol", symbol)
	q.Set("periodType", periodType)
	q.Set("period", fmt.Sprintf("%d", period))
	q.Set("frequencyType", frequencyType)
	q.Set("frequency", fmt.Sprintf("%d", frequency))
	q.Set("startDate", fmt.Sprintf("%d", start.UnixMilli()))
	q.Set("endDate", fmt.Sprintf("%d", end.UnixMilli()))
	q.Set("needExtendedHoursData", "false")
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		if !forceRefresh {
			return c.priceHistory(symbol, periodType, period, frequencyType, frequency, start, end, true)
		}
		return nil, fmt.Errorf("schwab token unauthorized after refresh; reauthorize token file %s", c.TokenFile)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("schwab pricehistory %s failed: HTTP %d", symbol, resp.StatusCode)
	}
	var out struct {
		Candles []quant.Bar `json:"candles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Candles, nil
}

func (c *Client) getJSON(path string, q url.Values, token string, out any) error {
	endpoint := c.BaseURL + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return httpStatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type httpStatusError struct {
	StatusCode int
	Body       string
}

func (e httpStatusError) Error() string {
	return fmt.Sprintf("schwab HTTP %d %s", e.StatusCode, e.Body)
}

func isUnauthorized(err error) bool {
	statusErr, ok := err.(httpStatusError)
	return ok && statusErr.StatusCode == http.StatusUnauthorized
}

type AccountNumber struct {
	AccountNumber string `json:"accountNumber"`
	HashValue     string `json:"hashValue"`
}

type AccountResponse struct {
	SecuritiesAccount SecuritiesAccount `json:"securitiesAccount"`
}

type SecuritiesAccount struct {
	Type            string            `json:"type"`
	AccountNumber   string            `json:"accountNumber"`
	CurrentBalances AccountBalances   `json:"currentBalances"`
	Positions       []AccountPosition `json:"positions"`
}

type AccountBalances struct {
	CashAvailableForTrading    float64 `json:"cashAvailableForTrading"`
	CashAvailableForWithdrawal float64 `json:"cashAvailableForWithdrawal"`
	TotalCash                  float64 `json:"totalCash"`
	LongMarketValue            float64 `json:"longMarketValue"`
	LiquidationValue           float64 `json:"liquidationValue"`
}

type AccountPosition struct {
	Instrument                     Instrument `json:"instrument"`
	LongQuantity                   float64    `json:"longQuantity"`
	AveragePrice                   float64    `json:"averagePrice"`
	MarketValue                    float64    `json:"marketValue"`
	CurrentDayProfitLoss           float64    `json:"currentDayProfitLoss"`
	CurrentDayProfitLossPercentage float64    `json:"currentDayProfitLossPercentage"`
}

type Instrument struct {
	Symbol string `json:"symbol"`
}

func LoadCachedBars(cacheDir, symbol string) ([]quant.Bar, error) {
	raw, err := os.ReadFile(cachePath(cacheDir, symbol))
	if err != nil {
		return nil, err
	}
	var bars []quant.Bar
	if err := json.Unmarshal(raw, &bars); err != nil {
		return nil, err
	}
	return bars, nil
}

func SaveCachedBars(cacheDir, symbol string, bars []quant.Bar) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(bars, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cachePath(cacheDir, symbol), raw, 0o644)
}

func cachePath(cacheDir, symbol string) string {
	return filepath.Join(cacheDir, symbol+"_daily.json")
}
