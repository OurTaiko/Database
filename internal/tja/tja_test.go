package tja

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/japanese"
)

const mixed = "\xEF\xBB\xBFTITLE:Song\r\nTITLEJA:曲\r\nTITLEKO:\r\nSUBTITLE:--Artist\r\nBPM:120\r\nWAVE:Song.ogg\r\nOFFSET:-1.5\r\nDEMOSTART:30\r\nSCOREMODE:2\r\n\r\n" +
	"COURSE:Oni\r\nLEVEL:8\r\nBALLOON:5\r\nSTYLE:Single\r\n#START\r\n1000,\r\n#BRANCHSTART p,1,2\r\n#N\r\n1,\r\n#END\r\n" +
	"STYLE:Double\r\n#START P1\r\n1,\r\n#END\r\n#START P2\r\n2,\r\n#END\r\n" +
	"COURSE:2\r\nLEVEL:5\r\n#START\r\n1,\r\n#END\r\n"

func parseSource(t *testing.T, source, audio string) (Metadata, *Issue) {
	t.Helper()
	data, issue := NormalizeUTF8([]byte(source))
	if issue != nil {
		return Metadata{}, issue
	}
	return Parse(data, audio)
}

func TestParseAcceptsSingleAndDoubleBlocksTogether(t *testing.T) {
	m, issue := parseSource(t, mixed, "Song.ogg")
	if issue != nil {
		t.Fatal(issue)
	}
	slots := []string{}
	for _, d := range m.Difficulties {
		slots = append(slots, d.Course+"/"+d.Side)
	}
	if got := strings.Join(slots, ","); got != "Oni/single,Oni/p1,Oni/p2,Hard/single" {
		t.Fatalf("slots = %s", got)
	}
	if !m.Difficulties[0].Branching || m.Difficulties[1].Branching || m.Difficulties[3].Level != 5 {
		t.Fatalf("unexpected metadata: %+v", m)
	}
	if m.Title != "Song" || m.BPM != 120 || m.Offset != -1.5 || m.DemoStart != 30 || m.Wave != "Song.ogg" {
		t.Fatalf("unexpected headers: %+v", m)
	}
	// The dictionary is the file's: "origin" is TITLE itself, "en" only when written; an empty
	// TITLEKO stays as the file wrote it (the clients skip blank values).
	if m.TitleTranslations["origin"] != "Song" || m.TitleTranslations["ja"] != "曲" || m.TitleTranslations["ko"] != "" {
		t.Fatalf("translations = %v", m.TitleTranslations)
	}
	if _, ok := m.TitleTranslations["en"]; ok {
		t.Fatal("en is not defaulted from TITLE any more")
	}
}

func TestParseRejectsWhatTheDatabaseCannotHold(t *testing.T) {
	single := "TITLE:S\nBPM:120\nWAVE:a.ogg\nCOURSE:Oni\nLEVEL:8\n#START\n1,\n#END\n"
	for name, test := range map[string]struct{ source, audio, code string }{
		"dan":              {strings.Replace(single, "COURSE:Oni", "COURSE:Dan", 1), "", "TJA_COURSE_UNSUPPORTED"},
		"tower":            {strings.Replace(single, "COURSE:Oni", "COURSE:Tower", 1), "", "TJA_COURSE_UNSUPPORTED"},
		"repeated course":  {single + "COURSE:Oni\nLEVEL:1\n#START\n1,\n#END\n", "", "TJA_DIFFICULTY_DUPLICATE"},
		"level":            {strings.Replace(single, "LEVEL:8", "LEVEL:11", 1), "", "TJA_STRUCTURE_INVALID"},
		"no wave":          {strings.Replace(single, "WAVE:a.ogg\n", "", 1), "", "TJA_WAVE_MISSING"},
		"wave path":        {strings.Replace(single, "WAVE:a.ogg", "WAVE:../a.ogg", 1), "", "TJA_WAVE_PATH_INVALID"},
		"other audio":      {single, "b.ogg", "TJA_AUDIO_MISMATCH"},
		"unclosed":         {strings.TrimSuffix(single, "#END\n"), "", "TJA_STRUCTURE_INVALID"},
		"double, no side":  {strings.Replace(single, "#START", "STYLE:Double\n#START", 1), "", "TJA_PLAYER_REQUIRED"},
		"invalid encoding": {"TITLE:\xff\xfe\x00", "", "TJA_ENCODING_INVALID"},
		// The movie headers as Fanmade checks them.
		"lowercase movie":     {strings.Replace(single, "WAVE:a.ogg", "WAVE:a.ogg\nbgmovie:a.mp4", 1), "", "TJA_MOVIE_INVALID"},
		"repeated movie":      {strings.Replace(single, "WAVE:a.ogg", "WAVE:a.ogg\nBGMOVIE:a.mp4\nBGMOVIE:b.mp4", 1), "", "TJA_MOVIE_INVALID"},
		"late movie":          {strings.Replace(single, "1,\n", "1,\nBGMOVIE:a.mp4\n", 1), "", "TJA_MOVIE_INVALID"},
		"movie path":          {strings.Replace(single, "WAVE:a.ogg", "WAVE:a.ogg\nBGMOVIE:../a.mp4", 1), "", "TJA_MOVIE_PATH_INVALID"},
		"movie format":        {strings.Replace(single, "WAVE:a.ogg", "WAVE:a.ogg\nBGMOVIE:a.avi", 1), "", "TJA_MOVIE_PATH_INVALID"},
		"movie offset":        {strings.Replace(single, "WAVE:a.ogg", "WAVE:a.ogg\nMOVIEOFFSET:1201", 1), "", "TJA_MOVIE_OFFSET_INVALID"},
		"offset not a number": {strings.Replace(single, "WAVE:a.ogg", "WAVE:a.ogg\nMOVIEOFFSET:soon", 1), "", "TJA_MOVIE_OFFSET_INVALID"},
	} {
		if _, issue := parseSource(t, test.source, test.audio); issue == nil || issue.Code != test.code {
			t.Errorf("%s: issue = %v, want %s", name, issue, test.code)
		}
	}
}

// Valid movie headers are accepted, and STYLE:1 is one player as OpenTaiko reads it.
func TestParseAcceptsMovieHeadersAndStyleOne(t *testing.T) {
	source := "TITLE:S\nBPM:120\nWAVE:a.ogg\nBGMOVIE:a.webm\nMOVIEOFFSET:-1.5\nCOURSE:Oni\nSTYLE:1\nLEVEL:8\n#START\n1,\n#END\n"
	if _, issue := parseSource(t, source, ""); issue != nil {
		t.Fatal(issue)
	}
}

func TestSetWaveChangesOnlyTheHeader(t *testing.T) {
	data, _ := NormalizeUTF8([]byte(mixed))
	out, ok := SetWave(data, "abc.ogg")
	if !ok || WaveName(out) != "abc.ogg" {
		t.Fatalf("WAVE = %q, ok = %t", WaveName(out), ok)
	}
	if want := strings.Replace(string(data), "WAVE:Song.ogg", "WAVE:abc.ogg", 1); string(out) != want {
		t.Fatal("bytes outside the WAVE line changed")
	}
	if _, ok = SetWave([]byte("TITLE:x\n#START\nWAVE:late.ogg\n"), "abc.ogg"); ok {
		t.Fatal("a WAVE after #START must not be rewritten")
	}
}

func TestNormalizeUTF8(t *testing.T) {
	want := []byte("TITLE:初音ミク\r\nWAVE:音楽.ogg\r\n")
	for _, data := range [][]byte{want, append([]byte("\xEF\xBB\xBF"), want...)} {
		got, issue := NormalizeUTF8(data)
		if issue != nil || !bytes.Equal(got, want) {
			t.Fatalf("%q: %v, %q", data, issue, got)
		}
	}
	// Uploads are UTF-8 only; the browser converts Shift-JIS and the rest first.
	sjis, err := japanese.ShiftJIS.NewEncoder().Bytes(want)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		data []byte
		code string
	}{
		{sjis, "TJA_ENCODING_INVALID"},
		{[]byte{0xff}, "TJA_ENCODING_INVALID"},
		{[]byte("\xEF\xBF\xBD"), "TJA_ENCODING_INVALID"},
		{[]byte("TITLE:a\x00"), "TJA_ENCODING_INVALID"},
		{[]byte("\xEF\xBB\xBF"), "FILE_SIZE_INVALID"},
		{bytes.Repeat([]byte("a"), MaxTJA+1), "FILE_SIZE_INVALID"},
	} {
		_, issue := NormalizeUTF8(tc.data)
		if issue == nil || issue.Code != tc.code {
			t.Fatalf("%q: %v", tc.data[:min(len(tc.data), 16)], issue)
		}
	}
}
