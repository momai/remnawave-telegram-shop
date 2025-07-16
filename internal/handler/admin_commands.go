package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"log/slog"
	"remnawave-tg-shop-bot/internal/config"
)

// ApproveUserCommandHandler обрабатывает команду /approve для добавления пользователя в список одобренных
func (h Handler) ApproveUserCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	ctxWithTime, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	text := update.Message.Text
	parts := strings.Fields(text)
	
	if len(parts) != 2 {
		_, err := b.SendMessage(ctxWithTime, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "❌ Использование: /approve <telegram_id>",
		})
		if err != nil {
			slog.Error("Error sending approve usage message", "error", err)
		}
		return
	}

	userIDStr := parts[1]
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		_, err := b.SendMessage(ctxWithTime, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "❌ Неверный формат Telegram ID",
		})
		if err != nil {
			slog.Error("Error sending invalid ID message", "error", err)
		}
		return
	}

	err = h.accessControl.ApproveUser(userID)
	if err != nil {
		slog.Error("Error approving user", "userId", userID, "error", err)
		_, err := b.SendMessage(ctxWithTime, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "❌ Ошибка при добавлении пользователя",
		})
		if err != nil {
			slog.Error("Error sending error message", "error", err)
		}
		return
	}

	_, err = b.SendMessage(ctxWithTime, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("✅ Пользователь %d добавлен в список одобренных", userID),
	})
	if err != nil {
		slog.Error("Error sending success message", "error", err)
	}
}

// ListApprovedUsersCommandHandler обрабатывает команду /list_approved для показа списка одобренных пользователей
func (h Handler) ListApprovedUsersCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	ctxWithTime, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	approvedUsers := h.accessControl.GetApprovedUsers()
	
	if len(approvedUsers) == 0 {
		_, err := b.SendMessage(ctxWithTime, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "📝 Список одобренных пользователей пуст",
		})
		if err != nil {
			slog.Error("Error sending empty list message", "error", err)
		}
		return
	}

	message := "📝 Одобренные пользователи:\n\n"
	for i, userID := range approvedUsers {
		message += fmt.Sprintf("%d. %d\n", i+1, userID)
	}

	_, err := b.SendMessage(ctxWithTime, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   message,
	})
	if err != nil {
		slog.Error("Error sending approved users list", "error", err)
	}
}

// ActivationCodeHandler обрабатывает код активации от пользователей
func (h Handler) ActivationCodeHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}

	ctxWithTime, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	messageText := strings.TrimSpace(update.Message.Text)
	userID := update.Message.From.ID

	// Проверяем, является ли сообщение кодом активации
	if messageText == config.GetActivationCode() {
		err := h.accessControl.ApproveUser(userID)
		if err != nil {
			slog.Error("Error approving user via activation code", "userId", userID, "error", err)
			return
		}

		_, err = b.SendMessage(ctxWithTime, &bot.SendMessageParams{
			ChatID: update.Message.Chat.ID,
			Text:   "🎉 Добро пожаловать! Доступ активирован.",
		})
		if err != nil {
			slog.Error("Error sending activation success message", "error", err)
		}

		slog.Info("User activated via code", "userId", userID)
	}
} 