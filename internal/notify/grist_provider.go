package notify

import (
	"context"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

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
		msg := fmt.Sprintf("<b>🚨 Отчёт MTL Fund за %s не создан!</b>\n\nОжидаемый отчёт отсутствует в базе данных.\n\n<a href=\"%s\">Проверить вручную</a>", date, r.ReportURL)
		if mentions != "" {
			msg += "\n\n" + mentions
		}
		return msg
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<b>📊 Отчёт MTL Fund за %s</b>\n", date)

	if len(r.KeyIndicators) > 0 {
		sb.WriteString("\n<b>Ключевые индикаторы:</b>\n")
		for _, ind := range r.KeyIndicators {
			fmt.Fprintf(&sb, "I%d %s: %s %s\n", ind.ID, ind.Name, formatDecimal(ind.Value), ind.Unit)
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
				formatDecimal(a.Previous),
				formatDecimal(a.Indicator.Value),
				a.Indicator.Unit,
				sign,
				a.ChangePercent.StringFixed(2),
			)
		}
	}

	fmt.Fprintf(&sb, "\n<a href=\"%s\">Полный отчёт</a>", r.ReportURL)

	return sb.String()
}

// formatDecimal formats a decimal number with space as the thousands separator.
// Example: 1827956.42 → "1 827 956.42"
func formatDecimal(d decimal.Decimal) string {
	s := d.String()

	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}

	intPart, fracPart, hasFrac := strings.Cut(s, ".")

	var buf strings.Builder
	n := len(intPart)
	for i, c := range intPart {
		if i > 0 && (n-i)%3 == 0 {
			buf.WriteByte(' ')
		}
		buf.WriteRune(c)
	}

	result := buf.String()
	if hasFrac {
		result += "." + fracPart
	}
	if neg {
		result = "-" + result
	}
	return result
}
