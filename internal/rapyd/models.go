package rapyd

import "time"

// Общие структуры для статусов и ошибок
type Status struct {
	ErrorCode     string `json:"error_code"`
	Status        string `json:"status"`
	Message       string `json:"message"`
	ResponseCode  string `json:"response_code"`
	OperationID   string `json:"operation_id"`
}

type ErrorResponse struct {
	Status Status `json:"status"`
}

// Структуры для создания checkout
type CreateCheckoutRequest struct {
	Amount int `json:"amount"`
	Currency               string            `json:"currency"`
	Country                string            `json:"country"`
	PaymentMethod          PaymentMethod     `json:"payment_method"`
	Metadata               map[string]string `json:"metadata,omitempty"`
	Description            string            `json:"description,omitempty"`
	CompletePaymentURL     string            `json:"complete_payment_url,omitempty"`
	ErrorPaymentURL        string            `json:"error_payment_url,omitempty"`
	CancelCheckoutURL      string            `json:"cancel_checkout_url,omitempty"`
	MerchantReferenceID    string            `json:"merchant_reference_id,omitempty"`
	PaymentMethodTypeCategories []string     `json:"payment_method_type_categories,omitempty"`
}

type PaymentMethod struct {
	Type string `json:"type"`
}

// Устаревшая структура - оставляем для совместимости
type CreateCheckoutResponse struct {
	Status CheckoutStatus `json:"status"`
	Data   OldCheckoutData   `json:"data"`
}

type CheckoutStatus struct {
	ErrorCode    string `json:"error_code"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	ResponseCode string `json:"response_code"`
}

type OldCheckoutData struct {
	ID                 string    `json:"id"`
	Amount             float64   `json:"amount"`
	Currency           string    `json:"currency"`
	Status             string    `json:"status"`
	RedirectURL        string    `json:"redirect_url"`
	CreatedAt          time.Time `json:"created_at"`
	MerchantReferenceID string   `json:"merchant_reference_id"`
}

// Новые правильные структуры
type CheckoutResponse struct {
	Status Status       `json:"status"`
	Data   CheckoutData `json:"data"`
}

type CheckoutData struct {
	ID                string            `json:"id"`
	Status            string            `json:"status"`
	RedirectURL       string            `json:"redirect_url"`
	Country           string            `json:"country"`
	Currency          string            `json:"currency"`
	Amount            int               `json:"amount"`
	Description       string            `json:"description"`
	MerchantReference string            `json:"merchant_reference_id"`
	Metadata          map[string]string `json:"metadata"`
}

// Webhook структуры
type WebhookRequest struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data"`
	TriggerOperationID string        `json:"trigger_operation_id"`
	Status    string                 `json:"status"`
	CreatedAt int64                  `json:"created_at"`
}

// PaymentMethodsResponse ответ от API получения методов оплаты
type PaymentMethodsResponse struct {
	Status Status                 `json:"status"`
	Data   []PaymentMethodDetails `json:"data"`
}

// PaymentMethodDetails детали метода оплаты
type PaymentMethodDetails struct {
	Type                            string                    `json:"type"`
	Name                            string                    `json:"name"`
	Category                        string                    `json:"category"`
	Image                           string                    `json:"image"`
	Country                         string                    `json:"country"`
	PaymentFlowType                 string                    `json:"payment_flow_type"`
	Currencies                      []string                  `json:"currencies"`
	Status                          int                       `json:"status"`
	IsCancelable                    bool                      `json:"is_cancelable"`
	PaymentOptions                  []PaymentOption           `json:"payment_options"`
	IsExpirable                     bool                      `json:"is_expirable"`
	IsOnline                        bool                      `json:"is_online"`
	IsRefundable                    bool                      `json:"is_refundable"`
	MinimumExpirationSeconds        int                       `json:"minimum_expiration_seconds"`
	MaximumExpirationSeconds        int                       `json:"maximum_expiration_seconds"`
	VirtualPaymentMethodType        *string                   `json:"virtual_payment_method_type"`
	IsVirtual                       bool                      `json:"is_virtual"`
	MultipleOverageAllowed          bool                      `json:"multiple_overage_allowed"`
	AmountRangePerCurrency          []AmountRangePerCurrency  `json:"amount_range_per_currency"`
	IsTokenizable                   bool                      `json:"is_tokenizable"`
	SupportedDigitalWalletProviders []string                  `json:"supported_digital_wallet_providers"`
	IsRestricted                    bool                      `json:"is_restricted"`
	SupportsSubscription            bool                      `json:"supports_subscription"`
}

// PaymentOption опция метода оплаты
type PaymentOption struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Regex        string `json:"regex"`
	Description  string `json:"description"`
	IsRequired   bool   `json:"is_required"`
	IsUpdatable  bool   `json:"is_updatable"`
}

// AmountRangePerCurrency диапазон сумм для валюты
type AmountRangePerCurrency struct {
	Currency      string  `json:"currency"`
	MaximumAmount *int    `json:"maximum_amount"`
	MinimumAmount *int    `json:"minimum_amount"`
} 