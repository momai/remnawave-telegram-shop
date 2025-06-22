package payment

import (
	"context"
	"fmt"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"
	"remnawave-tg-shop-bot/internal/cache"
	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/cryptopay"
	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/rapyd"
	"remnawave-tg-shop-bot/internal/remnawave"
	"remnawave-tg-shop-bot/internal/translation"
	"remnawave-tg-shop-bot/internal/yookasa"
	"remnawave-tg-shop-bot/utils"
	"strconv"
	"time"
)

type PaymentService struct {
	purchaseRepository *database.PurchaseRepository
	remnawaveClient    *remnawave.Client
	customerRepository *database.CustomerRepository
	telegramBot        *bot.Bot
	translation        *translation.Manager
	cryptoPayClient    *cryptopay.Client
	yookasaClient      *yookasa.Client
	rapydClient        *rapyd.Client
	referralRepository *database.ReferralRepository
	cache              *cache.Cache
}

func NewPaymentService(
	translation *translation.Manager,
	purchaseRepository *database.PurchaseRepository,
	remnawaveClient *remnawave.Client,
	customerRepository *database.CustomerRepository,
	telegramBot *bot.Bot,
	cryptoPayClient *cryptopay.Client,
	yookasaClient *yookasa.Client,
	rapydClient *rapyd.Client,
	referralRepository *database.ReferralRepository,
	cache *cache.Cache,
) *PaymentService {
	return &PaymentService{
		purchaseRepository: purchaseRepository,
		remnawaveClient:    remnawaveClient,
		customerRepository: customerRepository,
		telegramBot:        telegramBot,
		translation:        translation,
		cryptoPayClient:    cryptoPayClient,
		yookasaClient:      yookasaClient,
		rapydClient:        rapydClient,
		referralRepository: referralRepository,
		cache:              cache,
	}
}

func (s PaymentService) ProcessPurchaseById(ctx context.Context, purchaseId int64) error {
	purchase, err := s.purchaseRepository.FindById(ctx, purchaseId)
	if err != nil {
		return err
	}
	if purchase == nil {
		return fmt.Errorf("purchase with crypto invoice id %d not found", utils.MaskHalfInt64(purchaseId))
	}

	customer, err := s.customerRepository.FindById(ctx, purchase.CustomerID)
	if err != nil {
		return err
	}
	if customer == nil {
		return fmt.Errorf("customer %s not found", utils.MaskHalfInt64(purchase.CustomerID))
	}

	if messageId, b := s.cache.Get(purchase.ID); b {
		_, err = s.telegramBot.DeleteMessage(ctx, &bot.DeleteMessageParams{
			ChatID:    customer.TelegramID,
			MessageID: messageId,
		})
		if err != nil {
			slog.Error("Error deleting message", err)
		}
	}

	user, err := s.remnawaveClient.CreateOrUpdateUser(ctx, customer.ID, customer.TelegramID, config.TrafficLimit(), purchase.Month*30)
	if err != nil {
		return err
	}

	err = s.purchaseRepository.MarkAsPaid(ctx, purchase.ID)
	if err != nil {
		return err
	}

	customerFilesToUpdate := map[string]interface{}{
		"subscription_link": user.SubscriptionUrl,
		"expire_at":         user.ExpireAt,
	}

	err = s.customerRepository.UpdateFields(ctx, customer.ID, customerFilesToUpdate)
	if err != nil {
		return err
	}

	_, err = s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: customer.TelegramID,
		Text:   s.translation.GetText(customer.Language, "subscription_activated"),
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: s.createConnectKeyboard(customer),
		},
	})
	if err != nil {
		return err
	}

	ctxReferee := context.Background()
	referee, err := s.referralRepository.FindByReferee(ctxReferee, customer.TelegramID)
	if referee == nil {
		return nil
	}
	if referee.BonusGranted {
		return nil
	}
	if err != nil {
		return err
	}
	refereeCustomer, err := s.customerRepository.FindByTelegramId(ctxReferee, referee.ReferrerID)
	if err != nil {
		return err
	}
	refereeUser, err := s.remnawaveClient.CreateOrUpdateUser(ctxReferee, refereeCustomer.ID, refereeCustomer.TelegramID, config.TrafficLimit(), config.GetReferralDays())
	if err != nil {
		return err
	}
	refereeUserFilesToUpdate := map[string]interface{}{
		"subscription_link": refereeUser.GetSubscriptionUrl(),
		"expire_at":         refereeUser.GetExpireAt(),
	}
	err = s.customerRepository.UpdateFields(ctxReferee, refereeCustomer.ID, refereeUserFilesToUpdate)
	if err != nil {
		return err
	}
	err = s.referralRepository.MarkBonusGranted(ctxReferee, referee.ID)
	if err != nil {
		return err
	}
	slog.Info("Granted referral bonus", "customer_id", utils.MaskHalfInt64(refereeCustomer.ID))
	_, err = s.telegramBot.SendMessage(ctxReferee, &bot.SendMessageParams{
		ChatID:    refereeCustomer.TelegramID,
		ParseMode: models.ParseModeHTML,
		Text:      s.translation.GetText(refereeCustomer.Language, "referral_bonus_granted"),
		ReplyMarkup: models.InlineKeyboardMarkup{
			InlineKeyboard: s.createConnectKeyboard(refereeCustomer),
		},
	})
	slog.Info("purchase processed", "purchase_id", utils.MaskHalfInt64(purchase.ID), "type", purchase.InvoiceType, "customer_id", utils.MaskHalfInt64(customer.ID))

	return nil
}

// getUserCountryFromIP определяет страну пользователя по IP (заглушка)
// В реальном приложении здесь должен быть вызов к IP geolocation API
func (s *PaymentService) getUserCountryFromIP(userIP string) string {
	// TODO: Реализовать определение страны по IP
	// Можно использовать:
	// 1. MaxMind GeoIP2
	// 2. IPinfo API
	// 3. IP2Location API
	// 4. Любой другой IP geolocation сервис
	
	// Пока возвращаем дефолтную страну
	// В продакшене здесь должен быть реальный API вызов
	return "US" // fallback
}

// getOptimalCurrencyForUser определяет оптимальную валюту для пользователя
func (s *PaymentService) getOptimalCurrencyForUser(customer *database.Customer) string {
	// 1. Сначала проверяем настройки пользователя (если есть)
	// if customer.PreferredCurrency != "" {
	//     return customer.PreferredCurrency
	// }
	
	// 2. Определяем по стране пользователя
	userCountry := s.getUserCountryFromIP("") // В реальности передавать IP
	config := s.rapydClient.GetOptimalCurrencyConfig(userCountry)
	
	// 3. Логируем выбор валюты
	slog.Info("Currency selection", 
		"user_id", customer.ID,
		"detected_country", userCountry, 
		"selected_currency", config.Currency,
		"settlement_currency", config.SettlementCurrency)
	
	return config.Currency
}

func (s PaymentService) createRapydInvoice(ctx context.Context, amount int, months int, customer *database.Customer) (url string, purchaseId int64, err error) {
	purchaseId, err = s.purchaseRepository.Create(ctx, &database.Purchase{
		InvoiceType:       database.InvoiceTypeRapyd,
		Status:            database.PurchaseStatusNew,
		Amount:            float64(amount),
		Currency:          "USD",
		CustomerID:        customer.ID,
		Month:             months,
		RapydCheckoutID:   nil,
		RapydURL:          nil,
	})
	if err != nil {
		slog.Error("Error creating purchase", err)
		return "", 0, err
	}

	// Определяем оптимальную валюту для пользователя
	currency := s.getOptimalCurrencyForUser(customer)
	
	// Пересчитываем сумму в зависимости от валюты
	finalAmount := s.convertAmountToCurrency(amount, currency)
	
	slog.Info("Creating Rapyd invoice", 
		"customer_id", customer.ID,
		"original_amount_usd", amount,
		"final_amount", finalAmount,
		"currency", currency,
		"months", months)

	checkout, err := s.rapydClient.CreateCheckout(
		finalAmount,
		currency,
		fmt.Sprintf("Subscription for %d month(s)", months),
		strconv.FormatInt(customer.ID, 10),
		strconv.FormatInt(purchaseId, 10),
	)
	if err != nil {
		slog.Error("Error creating Rapyd checkout", err)
		return "", 0, err
	}

	updates := map[string]interface{}{
		"rapyd_checkout_id": checkout.Data.ID,
		"rapyd_url":         checkout.Data.RedirectURL,
		"status":            database.PurchaseStatusPending,
	}

	err = s.purchaseRepository.UpdateFields(ctx, purchaseId, updates)
	if err != nil {
		slog.Error("Error updating purchase", err)
		return "", 0, err
	}

	return checkout.Data.RedirectURL, purchaseId, nil
}

// convertAmountToCurrency конвертирует сумму в указанную валюту
func (s *PaymentService) convertAmountToCurrency(amountUSD int, currency string) int {
	// Примерные курсы валют (в продакшене используй реальные курсы)
	rates := map[string]float64{
		"USD": 1.0,
		"EUR": 0.85,    // 1 USD = 0.85 EUR
		"GBP": 0.75,    // 1 USD = 0.75 GBP  
		"ILS": 3.7,     // 1 USD = 3.7 ILS
		"CAD": 1.35,    // 1 USD = 1.35 CAD
		"AUD": 1.5,     // 1 USD = 1.5 AUD
		"JPY": 150.0,   // 1 USD = 150 JPY
		"SGD": 1.35,    // 1 USD = 1.35 SGD
		"SEK": 10.5,    // 1 USD = 10.5 SEK
		"NOK": 10.8,    // 1 USD = 10.8 NOK
		"DKK": 6.8,     // 1 USD = 6.8 DKK
		"CHF": 0.9,     // 1 USD = 0.9 CHF
		"PLN": 4.0,     // 1 USD = 4.0 PLN
		"CZK": 23.0,    // 1 USD = 23.0 CZK
		"HUF": 360.0,   // 1 USD = 360 HUF
		"RON": 4.6,     // 1 USD = 4.6 RON
		"BGN": 1.8,     // 1 USD = 1.8 BGN
		"HRK": 6.8,     // 1 USD = 6.8 HRK
		"MXN": 17.0,    // 1 USD = 17.0 MXN
		"BRL": 5.0,     // 1 USD = 5.0 BRL
		"ZAR": 18.0,    // 1 USD = 18.0 ZAR
		"RUB": 75.0,    // 1 USD = 75.0 RUB (может быть неактуально)
		"UAH": 36.0,    // 1 USD = 36.0 UAH
		"INR": 83.0,    // 1 USD = 83.0 INR
		"KRW": 1300.0,  // 1 USD = 1300 KRW
		"TWD": 31.0,    // 1 USD = 31.0 TWD
		"THB": 35.0,    // 1 USD = 35.0 THB
		"MYR": 4.6,     // 1 USD = 4.6 MYR
		"IDR": 15000.0, // 1 USD = 15000 IDR
		"PHP": 55.0,    // 1 USD = 55.0 PHP
	}
	
	rate, exists := rates[currency]
	if !exists {
		slog.Warn("Unknown currency, using USD", "currency", currency)
		return amountUSD
	}
	
	// Конвертируем и округляем
	convertedAmount := float64(amountUSD) * rate
	
	// Для некоторых валют используем разные правила округления
	switch currency {
	case "JPY", "KRW", "IDR": // Валюты без копеек
		return int(convertedAmount)
	default: // Валюты с копейками - округляем до целых
		return int(convertedAmount + 0.5)
	}
}

func (s PaymentService) createConnectKeyboard(customer *database.Customer) [][]models.InlineKeyboardButton {
	var inlineCustomerKeyboard [][]models.InlineKeyboardButton

	if config.GetMiniAppURL() != "" {
		inlineCustomerKeyboard = append(inlineCustomerKeyboard, []models.InlineKeyboardButton{
			{Text: s.translation.GetText(customer.Language, "connect_button"), WebApp: &models.WebAppInfo{
				URL: config.GetMiniAppURL(),
			}},
		})
	} else {
		inlineCustomerKeyboard = append(inlineCustomerKeyboard, []models.InlineKeyboardButton{
			{Text: s.translation.GetText(customer.Language, "connect_button"), CallbackData: "connect"},
		})
	}

	inlineCustomerKeyboard = append(inlineCustomerKeyboard, []models.InlineKeyboardButton{
		{Text: s.translation.GetText(customer.Language, "back_button"), CallbackData: "start"},
	})
	return inlineCustomerKeyboard
}

func (s PaymentService) CreatePurchase(ctx context.Context, amount int, months int, customer *database.Customer, invoiceType database.InvoiceType) (url string, purchaseId int64, err error) {
	switch invoiceType {
	case database.InvoiceTypeCrypto:
		return s.createCryptoInvoice(ctx, amount, months, customer)
	case database.InvoiceTypeYookasa:
		return s.createYookasaInvoice(ctx, amount, months, customer)
	case database.InvoiceTypeTelegram:
		return s.createTelegramInvoice(ctx, amount, months, customer)
	case database.InvoiceTypeRapyd:
		return s.createRapydInvoice(ctx, amount, months, customer)
	default:
		return "", 0, fmt.Errorf("unknown invoice type: %s", invoiceType)
	}
}

func (s PaymentService) createCryptoInvoice(ctx context.Context, amount int, months int, customer *database.Customer) (url string, purchaseId int64, err error) {
	purchaseId, err = s.purchaseRepository.Create(ctx, &database.Purchase{
		InvoiceType:       database.InvoiceTypeCrypto,
		Status:            database.PurchaseStatusNew,
		Amount:            float64(amount),
		Currency:          "USD",
		CustomerID:        customer.ID,
		Month:             months,
		RapydCheckoutID:   nil,
		RapydURL:          nil,
	})
	if err != nil {
		slog.Error("Error creating purchase", err)
		return "", 0, err
	}

	invoice, err := s.cryptoPayClient.CreateInvoice(&cryptopay.InvoiceRequest{
		CurrencyType:   "fiat",
		Fiat:           "USD",
		Amount:         fmt.Sprintf("%d", amount),
		AcceptedAssets: "USDT",
		Payload:        fmt.Sprintf("purchaseId=%d&username=%s", purchaseId, ctx.Value("username")),
		Description:    fmt.Sprintf("Subscription for %d month", months),
		PaidBtnName:    "callback",
		PaidBtnUrl:     config.BotURL(),
	})
	if err != nil {
		slog.Error("Error creating invoice", err)
		return "", 0, err
	}

	updates := map[string]interface{}{
		"crypto_invoice_url": invoice.BotInvoiceUrl,
		"crypto_invoice_id":  invoice.InvoiceID,
		"status":             database.PurchaseStatusPending,
	}

	err = s.purchaseRepository.UpdateFields(ctx, purchaseId, updates)
	if err != nil {
		slog.Error("Error updating purchase", err)
		return "", 0, err
	}

	return invoice.BotInvoiceUrl, purchaseId, nil
}

func (s PaymentService) createYookasaInvoice(ctx context.Context, amount int, months int, customer *database.Customer) (url string, purchaseId int64, err error) {
	purchaseId, err = s.purchaseRepository.Create(ctx, &database.Purchase{
		InvoiceType:       database.InvoiceTypeYookasa,
		Status:            database.PurchaseStatusNew,
		Amount:            float64(amount),
		Currency:          "RUB",
		CustomerID:        customer.ID,
		Month:             months,
		RapydCheckoutID:   nil,
		RapydURL:          nil,
	})
	if err != nil {
		slog.Error("Error creating purchase", err)
		return "", 0, err
	}

	invoice, err := s.yookasaClient.CreateInvoice(ctx, amount, months, customer.ID, purchaseId)
	if err != nil {
		slog.Error("Error creating invoice", err)
		return "", 0, err
	}

	updates := map[string]interface{}{
		"yookasa_url": invoice.Confirmation.ConfirmationURL,
		"yookasa_id":  invoice.ID,
		"status":      database.PurchaseStatusPending,
	}

	err = s.purchaseRepository.UpdateFields(ctx, purchaseId, updates)
	if err != nil {
		slog.Error("Error updating purchase", err)
		return "", 0, err
	}

	return invoice.Confirmation.ConfirmationURL, purchaseId, nil
}

func (s PaymentService) createTelegramInvoice(ctx context.Context, amount int, months int, customer *database.Customer) (url string, purchaseId int64, err error) {
	purchaseId, err = s.purchaseRepository.Create(ctx, &database.Purchase{
		InvoiceType:       database.InvoiceTypeTelegram,
		Status:            database.PurchaseStatusNew,
		Amount:            float64(amount),
		Currency:          "STARS",
		CustomerID:        customer.ID,
		Month:             months,
		RapydCheckoutID:   nil,
		RapydURL:          nil,
	})
	if err != nil {
		slog.Error("Error creating purchase", err)
		return "", 0, nil
	}

	invoiceUrl, err := s.telegramBot.CreateInvoiceLink(ctx, &bot.CreateInvoiceLinkParams{
		Title:    s.translation.GetText(customer.Language, "invoice_title"),
		Currency: "XTR",
		Prices: []models.LabeledPrice{
			{
				Label:  s.translation.GetText(customer.Language, "invoice_label"),
				Amount: amount,
			},
		},
		Description: s.translation.GetText(customer.Language, "invoice_description"),
		Payload:     fmt.Sprintf("%d&%s", purchaseId, ctx.Value("username")),
	})

	updates := map[string]interface{}{
		"status": database.PurchaseStatusPending,
	}

	err = s.purchaseRepository.UpdateFields(ctx, purchaseId, updates)
	if err != nil {
		slog.Error("Error updating purchase", err)
		return "", 0, err
	}

	return invoiceUrl, purchaseId, nil
}

func (s PaymentService) ActivateTrial(ctx context.Context, telegramId int64) (string, error) {
	if config.TrialDays() == 0 {
		return "", nil
	}
	customer, err := s.customerRepository.FindByTelegramId(ctx, telegramId)
	if err != nil {
		slog.Error("Error finding customer", err)
		return "", err
	}
	if customer == nil {
		return "", fmt.Errorf("customer %d not found", telegramId)
	}
	user, err := s.remnawaveClient.CreateOrUpdateUser(ctx, customer.ID, telegramId, config.TrialTrafficLimit(), config.TrialDays())
	if err != nil {
		slog.Error("Error creating user", err)
		return "", err
	}

	customerFilesToUpdate := map[string]interface{}{
		"subscription_link": user.GetSubscriptionUrl(),
		"expire_at":         user.GetExpireAt(),
	}

	err = s.customerRepository.UpdateFields(ctx, customer.ID, customerFilesToUpdate)
	if err != nil {
		return "", err
	}

	return user.GetSubscriptionUrl(), nil

}

func (s PaymentService) CancelPayment(purchaseId int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	purchase, err := s.purchaseRepository.FindById(ctx, purchaseId)
	if err != nil {
		return err
	}
	if purchase == nil {
		return fmt.Errorf("purchase with crypto invoice id %d not found", utils.MaskHalfInt64(purchaseId))
	}

	purchaseFieldsToUpdate := map[string]interface{}{
		"status": database.PurchaseStatusCancel,
	}

	err = s.purchaseRepository.UpdateFields(ctx, purchaseId, purchaseFieldsToUpdate)
	if err != nil {
		return err
	}

	return nil
}

// CheckRapydPaymentStatus проверяет статус Rapyd платежа и активирует подписку если оплачен
func (s PaymentService) CheckRapydPaymentStatus(ctx context.Context, purchaseId int64) (bool, error) {
	// Получаем информацию о покупке
	purchase, err := s.purchaseRepository.FindById(ctx, purchaseId)
	if err != nil {
		return false, fmt.Errorf("failed to find purchase: %w", err)
	}
	if purchase == nil {
		return false, fmt.Errorf("purchase %d not found", purchaseId)
	}

	// Проверяем что это Rapyd платеж
	if purchase.InvoiceType != database.InvoiceTypeRapyd {
		return false, fmt.Errorf("purchase %d is not a Rapyd payment", purchaseId)
	}

	// Проверяем что платеж еще не обработан
	if purchase.Status == database.PurchaseStatusPaid {
		return true, nil // Уже оплачен
	}

	// Проверяем что есть RapydCheckoutID
	if purchase.RapydCheckoutID == nil || *purchase.RapydCheckoutID == "" {
		return false, fmt.Errorf("purchase %d has no Rapyd checkout ID", purchaseId)
	}

	// Получаем статус checkout'а от Rapyd
	checkoutStatus, err := s.rapydClient.GetCheckoutStatus(*purchase.RapydCheckoutID)
	if err != nil {
		return false, fmt.Errorf("failed to get checkout status: %w", err)
	}

	slog.Info("Rapyd checkout status check", 
		"purchase_id", purchaseId,
		"checkout_id", *purchase.RapydCheckoutID,
		"status", checkoutStatus.Data.Status,
		"payment_status", func() string {
			if checkoutStatus.Data.Payment != nil {
				return checkoutStatus.Data.Payment.Status
			}
			return "no_payment"
		}())

	// Проверяем статус платежа
	isPaid := false
	if checkoutStatus.Data.Status == "COMPLETED" || 
	   (checkoutStatus.Data.Payment != nil && checkoutStatus.Data.Payment.Status == "CLO") {
		isPaid = true
	}

	if isPaid {
		// Активируем подписку
		err = s.ProcessPurchaseById(ctx, purchaseId)
		if err != nil {
			return false, fmt.Errorf("failed to process purchase: %w", err)
		}
		slog.Info("Rapyd payment processed successfully", "purchase_id", purchaseId)
		return true, nil
	}

	return false, nil // Еще не оплачен
}
