package rapyd

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"log"
)

type Client struct {
	baseURL    string
	accessKey  string
	secretKey  string
	httpClient *http.Client
}

func NewClient(baseURL, accessKey, secretKey string) *Client {
	return &Client{
		baseURL:    baseURL,
		accessKey:  accessKey,
		secretKey:  secretKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) CreateCheckout(amount int, currency, description, customerID, purchaseID string) (*CheckoutResponse, error) {
	// Сначала проверим поддерживаемые методы оплаты для данной валюты
	paymentMethods, err := c.GetPaymentMethodsByCountry("US", currency)
	if err != nil {
		log.Printf("[RAPYD] Warning: Could not get payment methods: %v", err)
	} else {
		log.Printf("[RAPYD] Available payment methods for %s in US: %d methods", currency, len(paymentMethods.Data))
		for _, method := range paymentMethods.Data {
			log.Printf("[RAPYD] - %s (%s): %v", method.Name, method.Type, method.Currencies)
		}
	}

	// Попробуем несколько стран для USD
	countries := []string{"US", "CA", "GB", "AU"}
	var selectedCountry string
	
	for _, country := range countries {
		methods, err := c.GetPaymentMethodsByCountry(country, currency)
		if err == nil && len(methods.Data) > 0 {
			selectedCountry = country
			log.Printf("[RAPYD] Using country: %s (found %d payment methods)", country, len(methods.Data))
			break
		}
	}
	
	if selectedCountry == "" {
		selectedCountry = "US" // fallback
	}

	request := CreateCheckoutRequest{
		Amount:   amount,
		Currency: currency,
		Country:  selectedCountry,
		PaymentMethod: PaymentMethod{
			Type: "any",
		},
		Metadata: map[string]string{
			"customer_id":  customerID,
			"purchase_id":  purchaseID,
		},
		Description:             description,
		MerchantReferenceID:     fmt.Sprintf("purchase_%s", purchaseID),
		PaymentMethodTypeCategories: []string{"card", "bank_transfer", "ewallet"},
	}

	resp, err := c.makeRequestRaw("POST", "/v1/checkout", request)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[RAPYD] Error response: %s", string(body))
		var errorResp ErrorResponse
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return nil, fmt.Errorf("rapyd error: %s - %s", errorResp.Status.ErrorCode, errorResp.Status.Message)
		}
		return nil, fmt.Errorf("HTTP error: %d - %s", resp.StatusCode, string(body))
	}

	var response CheckoutResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &response, nil
}

func (c *Client) GetPaymentMethodsByCountry(country, currency string) (*PaymentMethodsResponse, error) {
	endpoint := fmt.Sprintf("/v1/payment_methods/countries/%s?currency=%s", country, currency)
	
	resp, err := c.makeRequestRaw("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get payment methods: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[RAPYD] Error response: %s", string(body))
		var errorResp ErrorResponse
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return nil, fmt.Errorf("rapyd error: %s - %s", errorResp.Status.ErrorCode, errorResp.Status.Message)
		}
		return nil, fmt.Errorf("HTTP error: %d - %s", resp.StatusCode, string(body))
	}

	var response PaymentMethodsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &response, nil
}

func (c *Client) makeRequestRaw(method, endpoint string, payload interface{}) (*http.Response, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Логируем тело запроса
	log.Printf("[RAPYD] Request body: %s", string(jsonData))

	req, err := http.NewRequest(method, c.baseURL+endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Добавляем заголовки для аутентификации Rapyd
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	salt := c.generateSalt()
	signature := c.generateSignatureWithSalt(method, endpoint, string(jsonData), timestamp, salt)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("access_key", c.accessKey)
	req.Header.Set("signature", signature)
	req.Header.Set("timestamp", timestamp)
	req.Header.Set("salt", salt)

	log.Printf("[RAPYD] Headers: method=%s endpoint=%s salt=%s timestamp=%s access_key=%s signature=%s", method, endpoint, salt, timestamp, c.accessKey, signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	return resp, nil
}

func (c *Client) generateSignature(method, endpoint, body, timestamp string) string {
	// Rapyd требует специальный формат для подписи
	salt := c.generateSalt()
	return c.generateSignatureWithSalt(method, endpoint, body, timestamp, salt)
}

func (c *Client) generateSignatureWithSalt(method, endpoint, body, timestamp, salt string) string {
	// Используем официальный алгоритм из статьи Rapyd
	toSign := strings.ToLower(method) + endpoint + salt + timestamp + c.accessKey + c.secretKey + body

	// Создаем HMAC с секретным ключом
	hash := hmac.New(sha256.New, []byte(c.secretKey))
	hash.Write([]byte(toSign))

	// Получаем hex digest и кодируем в base64 (как в официальном примере)
	hexdigest := make([]byte, hex.EncodedLen(hash.Size()))
	hex.Encode(hexdigest, hash.Sum(nil))
	signature := base64.StdEncoding.EncodeToString(hexdigest)

	log.Printf("[RAPYD] Signature debug (official algorithm):\n  method: %s\n  endpoint: %s\n  salt: %s\n  timestamp: %s\n  access_key: %s\n  body: %s\n  toSign: %s\n  signature: %s",
		strings.ToLower(method), endpoint, salt, timestamp, c.accessKey, body, toSign, signature)

	return signature
}

func (c *Client) generateSalt() string {
	b := make([]byte, 8) // 8 байт = 16 hex-символов
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(b)
} 