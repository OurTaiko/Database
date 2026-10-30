// Package tjatitles writes the names the database gives a chart into the TJA it hands out.
package tjatitles

import "strings"

// titleLocales are the TITLE<LANG>/SUBTITLE<LANG> suffixes written back, in
// the order the game writes them.
var titleLocales = []string{"en", "ja", "ko", "zh"}

// WithTitles returns the TJA with its TITLE and SUBTITLE headers, and their
// translations (TITLEJA and the like), replaced by the given texts (R042): the
// stored file keeps the uploaded headers, while what the site hands out names
// the chart as the database does. The game rewrites its playable copy the same
// way. Lines between #START and #END are left alone; the new headers take the
// place of the first old one, and the file keeps its BOM and line endings.
func WithTitles(data []byte, title, subtitle string, titles, subtitles map[string]string) []byte {
	text := string(data)
	bom := strings.HasPrefix(text, "\ufeff")
	text = strings.TrimPrefix(text, "\ufeff")
	newline := "\n"
	if strings.Contains(text, "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines)+10)
	at, inChart := -1, false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(upper, "#START"):
			inChart = true
		case strings.HasPrefix(upper, "#END"):
			inChart = false
		}
		if !inChart {
			if key, _, ok := strings.Cut(upper, ":"); ok {
				key = strings.TrimSpace(key)
				if strings.HasPrefix(key, "TITLE") || strings.HasPrefix(key, "SUBTITLE") {
					if at < 0 {
						at = len(kept)
					}
					continue
				}
			}
		}
		kept = append(kept, line)
	}
	headers := []string{"TITLE:" + headerText(title), "SUBTITLE:" + headerText(subtitle)}
	for _, field := range []struct {
		key    string
		values map[string]string
	}{{"TITLE", titles}, {"SUBTITLE", subtitles}} {
		for _, locale := range titleLocales {
			if value := headerText(field.values[locale]); value != "" {
				headers = append(headers, field.key+strings.ToUpper(locale)+":"+value)
			}
		}
	}
	if at < 0 {
		at = 0
	}
	out := append(append(append(make([]string, 0, len(kept)+len(headers)), kept[:at]...), headers...), kept[at:]...)
	result := strings.Join(out, newline)
	if bom {
		result = "\ufeff" + result
	}
	return []byte(result)
}

// headerText keeps a header value on its one line.
func headerText(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ", "\x00", " ").Replace(strings.TrimSpace(s))
}
