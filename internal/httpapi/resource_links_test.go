package httpapi

import (
	"context"
	"testing"
)

// The resource manifest lists the cover too, as Fanmade's does: the website's chart
// package carries it.
func TestResourceManifestCover(t *testing.T) {
	w := interactionFixture(t)
	ctx := context.Background()
	resource := func(extension string) (id int64, digest string) {
		t.Helper()
		digest = ID() + ID()
		if err := w.s.DB.QueryRow(ctx, `INSERT INTO chart_resources(storage_key,original_filename,sha256,byte_size,extension) VALUES($1||'.'||$2,'a.'||$2,$1,7,$2) RETURNING id`, digest, extension).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id, digest
	}
	tja, _ := resource("tja")
	audio, _ := resource("ogg")
	if _, err := w.s.DB.Exec(ctx, `UPDATE charts SET tja_file=$1,audio_file=$2,duration=10 WHERE id=$3`, tja, audio, w.song); err != nil {
		t.Fatal(err)
	}
	var manifest struct{ Resources map[string]resourceLink }
	if code := w.c.do("", "GET", w.chart(w.song)+"/resources", "", &manifest); code != 200 || len(manifest.Resources) != 2 {
		t.Fatal("without a cover", code, manifest.Resources)
	}
	cover, digest := resource("webp")
	if _, err := w.s.DB.Exec(ctx, `UPDATE charts SET cover_file=$1 WHERE id=$2`, cover, w.song); err != nil {
		t.Fatal(err)
	}
	if code := w.c.do("", "GET", w.chart(w.song)+"/resources", "", &manifest); code != 200 || manifest.Resources["cover"].SHA256 != digest || manifest.Resources["cover"].ContentType != "image/webp" || manifest.Resources["cover"].Size != 7 {
		t.Fatal("cover", code, manifest.Resources["cover"])
	}
}
