package notify

import (
	"context"
	"fmt"
	"strings"

	"github.com/mtlprog/stat/internal/grist"
)

// GristProvider writes a notification row to a Grist Messages table.
// The Grist worker reads the row and forwards the message to Telegram.
type GristProvider struct {
	client  *grist.Client
	tableID string
	chatID  int64
	topicID int64
}

// NewGristProvider creates a GristProvider.
func NewGristProvider(client *grist.Client, tableID string, chatID, topicID int64) *GristProvider {
	return &GristProvider{
		client:  client,
		tableID: tableID,
		chatID:  chatID,
		topicID: topicID,
	}
}

func (p *GristProvider) Send(ctx context.Context, report Report) error {
	msg := formatHTML(report)
	record := map[string]any{
		"chat_id":  p.chatID,
		"topik_id": p.topicID,
		"messsage": msg,
	}
	if err := p.client.AddRecords(ctx, p.tableID, []map[string]any{record}); err != nil {
		return fmt.Errorf("grist provider send: %w", err)
	}
	return nil
}

func formatHTML(r Report) string {
	date := r.Date.Format("2006-01-02")
	mentions := strings.Join(r.Mentions, " ")

	if r.ReportMissing {
		return fmt.Sprintf(
			"<b>🚨 Отчёт MTL Fund за %s не создан!</b>\n\nОжидаемый отчёт отсутствует в базе данных.\n\n<a href=\"%s\">Проверить вручную</a>\n\n%s",
			date, r.ReportURL, mentions,
		)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<b>📊 Отчёт MTL Fund за %s</b>\n", date)

	if len(r.KeyIndicators) > 0 {
		sb.WriteString("\n<b>Ключевые индикаторы:</b>\n")
		for _, ind := range r.KeyIndicators {
			fmt.Fprintf(&sb, "I%d %s: %s %s\n", ind.ID, ind.Name, ind.Value.String(), ind.Unit)
		}
	}

	if len(r.Alerts) > 0 {
		sb.WriteString("\n<b>⚠️ Изменения &gt;5%:</b>\n")
		for _, a := range r.Alerts {
			sign := "+"
			if a.ChangePercent.IsNegative() {
				sign = ""
			}
			fmt.Fprintf(&sb, "I%d %s: %s → %s %s (%s%s%%)\n",
				a.Indicator.ID,
				a.Indicator.Name,
				a.Previous.String(),
				a.Indicator.Value.String(),
				a.Indicator.Unit,
				sign,
				a.ChangePercent.StringFixed(2),
			)
		}
	}

	fmt.Fprintf(&sb, "\n<a href=\"%s\">Полный отчёт</a>", r.ReportURL)

	return sb.String()
}
