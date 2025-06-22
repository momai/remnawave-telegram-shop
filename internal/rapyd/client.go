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

// CurrencyConfig конфигурация валют для стран
type CurrencyConfig struct {
	Country         string
	Currency        string
	SettlementCurrency string // Валюта для тебя (мерчанта)
}

func NewClient(baseURL, accessKey, secretKey string) *Client {
	return &Client{
		baseURL:    baseURL,
		accessKey:  accessKey,
		secretKey:  secretKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// GetOptimalCurrencyConfig определяет оптимальную валюту для пользователя
func (c *Client) GetOptimalCurrencyConfig(userCountry string) CurrencyConfig {
	// Карта стран и их предпочитаемых валют
	countryToCurrency := map[string]CurrencyConfig{
		// Основные рынки
		"US": {"US", "USD", "USD"},
		"CA": {"CA", "CAD", "USD"},
		"GB": {"GB", "GBP", "USD"},
		"AU": {"AU", "AUD", "USD"},
		"NZ": {"NZ", "NZD", "USD"},
		
		// Европа
		"DE": {"DE", "EUR", "USD"},
		"FR": {"FR", "EUR", "USD"},
		"IT": {"IT", "EUR", "USD"},
		"ES": {"ES", "EUR", "USD"},
		"NL": {"NL", "EUR", "USD"},
		"BE": {"BE", "EUR", "USD"},
		"AT": {"AT", "EUR", "USD"},
		"FI": {"FI", "EUR", "USD"},
		"IE": {"IE", "EUR", "USD"},
		"PT": {"PT", "EUR", "USD"},
		"GR": {"GR", "EUR", "USD"},
		
		// Израиль и регион
		"IL": {"IL", "ILS", "USD"}, // Шекели для локальных пользователей
		
		// Азия
		"JP": {"JP", "JPY", "USD"},
		"SG": {"SG", "SGD", "USD"},
		"HK": {"HK", "HKD", "USD"},
		"KR": {"KR", "KRW", "USD"},
		"TW": {"TW", "TWD", "USD"},
		"MY": {"MY", "MYR", "USD"},
		"TH": {"TH", "THB", "USD"},
		"IN": {"IN", "INR", "USD"},
		"ID": {"ID", "IDR", "USD"},
		"PH": {"PH", "PHP", "USD"},
		
		// Восточная Европа
		"PL": {"PL", "PLN", "USD"},
		"CZ": {"CZ", "CZK", "USD"},
		"HU": {"HU", "HUF", "USD"},
		"RO": {"RO", "RON", "USD"},
		"BG": {"BG", "BGN", "USD"},
		"HR": {"HR", "HRK", "USD"},
		
		// Скандинавия
		"SE": {"SE", "SEK", "USD"},
		"NO": {"NO", "NOK", "USD"},
		"DK": {"DK", "DKK", "USD"},
		"IS": {"IS", "ISK", "USD"},
		
		// Латинская Америка
		"MX": {"MX", "MXN", "USD"},
		"BR": {"BR", "BRL", "USD"},
		"AR": {"AR", "ARS", "USD"},
		"CL": {"CL", "CLP", "USD"},
		"CO": {"CO", "COP", "USD"},
		"PE": {"PE", "PEN", "USD"},
		
		// Африка
		"ZA": {"ZA", "ZAR", "USD"},
		
		// СНГ
		"RU": {"RU", "RUB", "USD"},
		"UA": {"UA", "UAH", "USD"},
		"KZ": {"KZ", "KZT", "USD"},
		"BY": {"BY", "BYN", "USD"},
		"MD": {"MD", "MDL", "USD"},
		"GE": {"GE", "GEL", "USD"},
	}
	
	if config, exists := countryToCurrency[userCountry]; exists {
		return config
	}
	
	// Fallback - USD для неизвестных стран
	return CurrencyConfig{"US", "USD", "USD"}
}

// CreateCheckout создает checkout-страницу для оплаты с автоматическим определением валюты
func (c *Client) CreateCheckout(amount int, currency, description, customerID, purchaseID string) (*CheckoutResponse, error) {
	// Определяем оптимальную конфигурацию валюты
	// В реальном приложении ты можешь получить страну пользователя из:
	// 1. IP-адреса (используя IP geolocation API)
	// 2. Настроек пользователя в Telegram
	// 3. Данных профиля пользователя
	
	// Пока используем переданную валюту или USD по умолчанию
	if currency == "" {
		currency = "USD"
	}
	
	// Список стран для поиска подходящих методов оплаты
	var countriesToTry []string
	
	// Определяем страны на основе валюты
	switch currency {
	case "USD":
		// В sandbox USD может не работать с US, попробуем европейские страны
		countriesToTry = []string{"GB", "DE", "CA", "AU", "SG"}
	case "EUR":
		countriesToTry = []string{"DE", "FR", "IT", "ES", "NL", "BE", "AT", "FI", "IE", "PT", "GR"}
	case "GBP":
		countriesToTry = []string{"GB", "IE"}
	case "ILS":
		countriesToTry = []string{"IL"}
	case "CAD":
		countriesToTry = []string{"CA", "US"}
	case "AUD":
		countriesToTry = []string{"AU", "NZ", "US"}
	case "JPY":
		countriesToTry = []string{"JP", "US"}
	case "SGD":
		countriesToTry = []string{"SG", "MY", "US"}
	default:
		// Для других валют пробуем сначала основные рынки
		countriesToTry = []string{"US", "GB", "DE", "CA", "AU"}
	}
	
	var selectedCountry string
	var availableMethods []PaymentMethodDetails
	
	// Ищем страну с подходящими методами оплаты
	for _, country := range countriesToTry {
		methods, err := c.GetPaymentMethodsByCountry(country, currency)
		if err != nil {
			log.Printf("[RAPYD] Warning: Could not get payment methods for %s/%s: %v", country, currency, err)
			continue
		}
		
		// Фильтруем только карточные методы оплаты
		var cardMethods []PaymentMethodDetails
		for _, method := range methods.Data {
			if method.Category == "card" && method.Status == 1 {
				cardMethods = append(cardMethods, method)
			}
		}
		
		if len(cardMethods) > 0 {
			selectedCountry = country
			availableMethods = cardMethods
			log.Printf("[RAPYD] Using country: %s for currency: %s (found %d card methods)", country, currency, len(cardMethods))
			break
		}
	}
	
	if selectedCountry == "" {
		// Если не нашли подходящую страну для USD, попробуем EUR с Германией
		if currency == "USD" {
			log.Printf("[RAPYD] Warning: USD not supported, trying EUR with DE as fallback")
			methods, err := c.GetPaymentMethodsByCountry("DE", "EUR")
			if err == nil {
				// Конвертируем USD в EUR (примерный курс 1 USD = 0.85 EUR)
				amount = int(float64(amount) * 0.85)
				currency = "EUR"
				selectedCountry = "DE"
				
				// Фильтруем карточные методы
				for _, method := range methods.Data {
					if method.Category == "card" && method.Status == 1 {
						availableMethods = append(availableMethods, method)
					}
				}
				log.Printf("[RAPYD] Fallback successful: converted to EUR, amount=%d, country=DE", amount)
			}
		}
		
		if selectedCountry == "" {
			selectedCountry = "DE" // final fallback
			currency = "EUR"
			log.Printf("[RAPYD] Warning: Using final fallback DE/EUR")
		}
	}
	
	// Логируем доступные методы
	for _, method := range availableMethods {
		log.Printf("[RAPYD] - Available: %s (%s) - supports: %v", method.Name, method.Type, method.Currencies)
	}

	request := CreateCheckoutRequest{
		Amount:   amount, // Используем возможно измененный amount
		Currency: currency, // Используем возможно измененную currency
		Country:  selectedCountry,
		PaymentMethod: PaymentMethod{
			Type: "any", // Принимаем любые методы оплаты
		},
		Metadata: map[string]string{
			"customer_id":  customerID,
			"purchase_id":  purchaseID,
			"currency_requested": currency,
			"country_selected": selectedCountry,
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

	log.Printf("[RAPYD] Checkout created successfully: ID=%s, URL=%s", response.Data.ID, response.Data.RedirectURL)
	return &response, nil
}

// GetCheckoutStatus получает статус checkout'а
func (c *Client) GetCheckoutStatus(checkoutID string) (*CheckoutStatusResponse, error) {
	endpoint := fmt.Sprintf("/v1/checkout/%s", checkoutID)
	
	resp, err := c.makeRequestRaw("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkout status: %w", err)
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

	var response CheckoutStatusResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	log.Printf("[RAPYD] Checkout status: ID=%s, Status=%s", response.Data.ID, response.Data.Status)
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
	var jsonData []byte
	var bodyString string
	var reqBody *bytes.Buffer
	
	if payload != nil {
		var err error
		jsonData, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		bodyString = string(jsonData)
		reqBody = bytes.NewBuffer(jsonData)
	} else {
		// Для GET-запросов тело пустое
		bodyString = ""
		reqBody = bytes.NewBuffer([]byte{})
	}

	// Логируем тело запроса
	log.Printf("[RAPYD] Request body: %s", bodyString)

	req, err := http.NewRequest(method, c.baseURL+endpoint, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Добавляем заголовки для аутентификации Rapyd
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	salt := c.generateSalt()
	signature := c.generateSignatureWithSalt(method, endpoint, bodyString, timestamp, salt)

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