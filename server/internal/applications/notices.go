package applications

import (
	"fmt"
	"strings"
)

// lines склеивает непустые строки в текст уведомления: уведомление показывается и на сайте, и в письме.
func lines(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n")
}

func vacancyLine(title, org string) string {
	return fmt.Sprintf("Вакансия «%s», %s.", title, org)
}

func withNote(label, text string) string {
	if text == "" {
		return ""
	}
	return label + ": " + text
}
