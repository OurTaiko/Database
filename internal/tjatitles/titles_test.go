package tjatitles

import (
	"strings"
	"testing"

	"ourtaiko.dev/sitekit/tja"
)

func TestWithTitles(t *testing.T) {
	// A line inside the block that looks like a title header is left alone (the game reads it
	// as an unknown command and warns).
	source := "\ufeffTITLE:Old\r\nTITLEJA:古い\r\nSUBTITLE:--Old artist\r\nBPM:120\r\nWAVE:a.ogg\r\nCOURSE:Oni\r\nLEVEL:8\r\n#START\r\n#TITLE:not a header\r\n1,\r\n#END\r\n"
	got := string(WithTitles([]byte(source), "New", "--New artist",
		map[string]string{"en": "New", "zh": "新曲", "ja": ""}, map[string]string{"en": "New artist"}))
	want := "\ufeffTITLE:New\r\nSUBTITLE:--New artist\r\nTITLEEN:New\r\nTITLEZH:新曲\r\nSUBTITLEEN:New artist\r\nBPM:120\r\nWAVE:a.ogg\r\nCOURSE:Oni\r\nLEVEL:8\r\n#START\r\n#TITLE:not a header\r\n1,\r\n#END\r\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	// The result still parses, with the new names.
	m, issue := tja.Parse([]byte(strings.TrimPrefix(got, "\ufeff")), "a.ogg")
	if issue != nil || m.Title != "New" || m.Subtitle != "--New artist" || m.TitleTranslations["origin"] != "New" || m.TitleTranslations["en"] != "New" || m.TitleTranslations["zh"] != "新曲" || m.TitleTranslations["ja"] != "" || m.BPM != 120 {
		t.Fatalf("parsed %+v %v", m, issue)
	}
	// Without title headers the new ones go first; values stay on one line.
	if got := string(WithTitles([]byte("BPM:100\n"), "A\nB", "", nil, nil)); got != "TITLE:A B\nSUBTITLE:\nBPM:100\n" {
		t.Fatalf("no headers: %q", got)
	}
}
