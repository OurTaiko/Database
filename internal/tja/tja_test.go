package tja

import (
	"strings"
	"testing"
)

const mixed = "\xEF\xBB\xBFTITLE:Song\r\nTITLEJA:曲\r\nTITLEKO:\r\nSUBTITLE:--Artist\r\nBPM:120\r\nWAVE:Song.ogg\r\nOFFSET:-1.5\r\nDEMOSTART:30\r\nSCOREMODE:2\r\n\r\n" +
	"COURSE:Oni\r\nLEVEL:8\r\nBALLOON:5\r\nSTYLE:Single\r\n#START\r\n1000,\r\n#BRANCHSTART p,1,2\r\n#N\r\n1,\r\n#END\r\n" +
	"STYLE:Double\r\n#START P1\r\n1,\r\n#END\r\n#START P2\r\n2,\r\n#END\r\n" +
	"COURSE:2\r\nLEVEL:5\r\n#START\r\n1,\r\n#END\r\n"

func parse(t *testing.T, source, audio string) (Metadata, *Issue) {
	t.Helper()
	data, issue := NormalizeUTF8([]byte(source), DetectEncoding([]byte(source)))
	if issue != nil {
		return Metadata{}, issue
	}
	return Parse(data, audio)
}

func TestParseAcceptsSingleAndDoubleBlocksTogether(t *testing.T) {
	m, issue := parse(t, mixed, "Song.ogg")
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
	// An empty translation is not a translation; English defaults to the title.
	if _, ok := m.TitleTranslations["ko"]; ok || m.TitleTranslations["ja"] != "曲" || m.TitleTranslations["en"] != "Song" {
		t.Fatalf("translations = %v", m.TitleTranslations)
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
	} {
		if _, issue := parse(t, test.source, test.audio); issue == nil || issue.Code != test.code {
			t.Errorf("%s: issue = %v, want %s", name, issue, test.code)
		}
	}
}

func TestSetWaveChangesOnlyTheHeader(t *testing.T) {
	data, _ := NormalizeUTF8([]byte(mixed), "utf-8")
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
