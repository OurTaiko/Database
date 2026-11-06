-- Migration 021: the titles 3d7aea8's backfill overwrote, back as they were.
-- The startup backfill that put every block under hash algorithm v2 also merged each stored
-- file's TITLE and SUBTITLE lines into the chart's title dictionaries, as it did when the
-- dictionaries were first filled (010). Titles edited on the site since are not written back to
-- the stored file, so for 20 charts the merge brought the file's old ones back (and an empty
-- "ja" where the file has an empty line) and moved update_at. The backfill no longer does that
-- (content_hash.go). This puts the 20 back from the public catalog as it was read before the
-- release: each row only if it is still exactly as the backfill left it, with update_at as it
-- was, which charts_touch_update_at would not let go back.
CREATE TEMP TABLE restore_titles (
  id text PRIMARY KEY,
  title_before jsonb NOT NULL, subtitle_before jsonb NOT NULL, update_before timestamptz NOT NULL,
  title_left jsonb NOT NULL, subtitle_left jsonb NOT NULL, update_left timestamptz NOT NULL
) ON COMMIT DROP;
INSERT INTO restore_titles VALUES
  -- 151 Bukan Cinta Biasa
  ('382d8ab610f14c23818416af49ca75f7',
   $j${"en": "Bukan Cinta Biasa", "ja": "Bukan Cinta Biasa", "origin": "Bukan Cinta Biasa", "zh": "Bukan Cinta Biasa", "zh_tw": "Bukan Cinta Biasa"}$j$,
   $j${"origin": "--"}$j$, '2026-10-29T18:28:30.511319Z',
   $j${"en": "Bukan Cinta Biasa", "ja": "Bukan Cinta Biasa", "origin": "Bukan Cinta Biasa", "zh": "Bukan Cinta Biasa", "zh_tw": "Bukan Cinta Biasa"}$j$,
   $j${"ja": "", "origin": "--"}$j$, '2026-11-06T07:52:34.110604Z'),
  -- 242 Make Inu Henjou
  ('5b5888ce23ef43b68796476e9055cb9a',
   $j${"en": "Underdog Returns! ~Muscle Effort Theme~", "ja": "負け犬返上 ～マッスル努力のテーマ～", "origin": "Make Inu Henjou", "zh": "摆脱“丧家犬”之名 ～肌肉努力之歌～"}$j$,
   $j${"en": "feat. Kani Prince From \" MUSCLE MARCH \"", "ja": "feat. 蟹プリンス 「マッスル行進曲」より", "origin": "--AJURIKA feat. Crab Prince/Muscle March", "zh": "feat. 蟹Prince 出自\"MUSCLE行进曲\""}$j$, '2026-10-29T21:26:51.577293Z',
   $j${"en": "Underdog Returns! ~Muscle Effort Theme~", "ja": "負け犬返上", "origin": "Make Inu Henjou", "zh": "摆脱“丧家犬”之名 ～肌肉努力之歌～"}$j$,
   $j${"en": "feat. Kani Prince From \" MUSCLE MARCH \"", "ja": "「マッスル行進曲」より", "origin": "--AJURIKA feat. Crab Prince/Muscle March", "zh": "feat. 蟹Prince 出自\"MUSCLE行进曲\""}$j$, '2026-11-06T07:59:04.702729Z'),
  -- 404 Monster Strike Medley: Star Graveyard EX, Unhinged Egotistical Devotion
  ('501c827bdda94c358ef36accae8d88cc',
   $j${"en": "Monster Strike Medley: Star Graveyard EX, Unhinged Egotistical Devotion", "ja": "モンスターストライクメドレー ～破界の星墓EX・絶対狂愛エゴイスト～", "ko": "몬스터 스트라이크 메들리 ~파계의 성묘 EX·절대광애 에고이스트~", "origin": "Monster Strike Medley: Star Graveyard EX, Unhinged Egotistical Devotion", "zh": "《怪物弹珠》组曲 ～破界之星墓EX・绝对狂爱利己主义者～"}$j$,
   $j${"origin": "--"}$j$, '2026-10-29T18:03:31.171779Z',
   $j${"en": "Monster Strike Medley: Star Graveyard EX, Unhinged Egotistical Devotion", "ja": "モンスターストライクメドレー ～破界の星墓EX・絶対狂愛エゴイスト～", "ko": "몬스터 스트라이크 메들리 ~파계의 성묘 EX·절대광애 에고이스트~", "origin": "Monster Strike Medley: Star Graveyard EX, Unhinged Egotistical Devotion", "zh": "《怪物弹珠》组曲 ～破界之星墓EX・绝对狂爱利己主义者～"}$j$,
   $j${"ja": "", "origin": "--"}$j$, '2026-11-06T07:56:53.385185Z'),
  -- 477 Ao to Natsu
  ('eb77eb9781b64c609bf5d75696718644',
   $j${"en": "Ao to Natsu", "ja": "青と夏", "ko": "Ao to Natsu", "origin": "Ao to Natsu", "zh": "Ao to Natsu", "zh_tw": "Ao to Natsu"}$j$,
   $j${"origin": "--"}$j$, '2026-10-29T18:26:44.752627Z',
   $j${"en": "Ao to Natsu", "ja": "青と夏", "ko": "Ao to Natsu", "origin": "Ao to Natsu", "zh": "Ao to Natsu", "zh_tw": "Ao to Natsu"}$j$,
   $j${"ja": "", "ko": "", "origin": "--", "zh": ""}$j$, '2026-11-06T08:23:41.274105Z'),
  -- 683 Miishi Kakure
  ('2c2c0a44a86146b8909119d27d9335d7',
   $j${"en": "Miishi Kakure", "ja": "御石かくれ", "ko": "Miishi Kakure", "origin": "Miishi Kakure", "zh": "御石隐"}$j$,
   $j${"en": "Touhou Project × NAMCO SOUNDS / Sho Okada(BNSI) & Yura Hatsuki feat. Kaori Aihara", "ja": "東方Project × NAMCO SOUNDS Sho Okada(BNSI) ＆ 葉月ゆら feat. 愛原圭織", "ko": "Touhou Project × NAMCO SOUNDS / Sho Okada(BNSI) & Yura Hatsuki feat. Kaori Aihara", "origin": "--Touhou Project × NAMCO SOUNDS / Sho Okada(BNSI) & Yura Hatsuki feat. Kaori Aihara", "zh": "东方Project × NAMCO SOUNDS Sho Okada(BNSI) ＆ 叶月由罗 feat. 爱原圭织"}$j$, '2026-10-29T18:12:31.028204Z',
   $j${"en": "Miishi Kakure", "ja": "御石かくれ", "ko": "Miishi Kakure", "origin": "Miishi Kakure", "zh": "Miishi Kakure"}$j$,
   $j${"en": "Touhou Project × NAMCO SOUNDS / Sho Okada(BNSI) & Yura Hatsuki feat. Kaori Aihara", "ja": "東方Project × NAMCO SOUNDS Sho Okada(BNSI) ＆ 葉月ゆら feat. 愛原圭織", "ko": "Touhou Project × NAMCO SOUNDS / Sho Okada(BNSI) & Yura Hatsuki feat. Kaori Aihara", "origin": "--Touhou Project × NAMCO SOUNDS / Sho Okada(BNSI) & Yura Hatsuki feat. Kaori Aihara", "zh": "東方Project × NAMCO SOUNDS / Sho Okada(BNSI) & Yura Hatsuki feat. Kaori Aihara"}$j$, '2026-11-06T07:50:32.249182Z'),
  -- 1041 Gakuen Tengoku -Old Audio/Chart-
  ('e694d70c8a664e84a4ebc2f9441fb89e',
   $j${"en": "Gakuen Tengoku -Old Audio/Chart-", "ja": "学園天国 旧音源・譜面", "origin": "Gakuen Tengoku -Old Audio/Chart-"}$j$,
   $j${"en": "Fushigiboshi no Futagohime Gyu!", "ja": "小泉今日子", "origin": "--Fushigiboshi no Futagohime Gyu!"}$j$, '2026-10-29T18:41:38.667493Z',
   $j${"en": "Gakuen Tengoku -Old Audio/Chart-", "ja": "学園天国", "origin": "Gakuen Tengoku -Old Audio/Chart-"}$j$,
   $j${"en": "Fushigiboshi no Futagohime Gyu!", "ja": "旧音源・譜面", "origin": "--Fushigiboshi no Futagohime Gyu!"}$j$, '2026-11-06T08:22:45.838856Z'),
  -- 1103 DANCE MONKEY
  ('e6a4cc5ac9c14d5bbc480205599436d6',
   $j${"en": "DANCE MONKEY", "ja": "DANCE MONKEY", "origin": "DANCE MONKEY", "zh": "DANCE MONKEY", "zh_tw": "DANCE MONKEY"}$j$,
   $j${"origin": "--"}$j$, '2026-10-29T18:34:50.306598Z',
   $j${"en": "DANCE MONKEY", "ja": "DANCE MONKEY", "origin": "DANCE MONKEY", "zh": "DANCE MONKEY", "zh_tw": "DANCE MONKEY"}$j$,
   $j${"ja": "", "origin": "--"}$j$, '2026-11-06T08:22:49.363622Z'),
  -- 1159 Candy Pop
  ('1bca84e32fb04350ab955b4489b58d50',
   $j${"en": "Candy Pop", "ja": "Candy Pop", "origin": "Candy Pop"}$j$,
   $j${"origin": "--"}$j$, '2026-10-29T18:29:46.340075Z',
   $j${"en": "Candy Pop", "ja": "Candy Pop", "origin": "Candy Pop"}$j$,
   $j${"ja": "", "origin": "--"}$j$, '2026-11-06T07:47:57.77879Z'),
  -- 1359 じゃがりこ　りみっくす
  ('b7e3f134edcf45478640a73d11c733ff',
   $j${"en": "Jagarico Remix", "ja": "じゃがりこ　りみっくす", "ko": "Jagarico Remix", "origin": "じゃがりこ　りみっくす", "zh": "Jagarico Remix", "zh_tw": "Jagarico Remix"}$j$,
   $j${"en": "\"Jagarico × Taiko no Tatsujin\" Yoshihito Yano(BNSI)", "ja": "「じゃがりこ × 太鼓の達人」 矢野義人(BNSI)", "ko": "\"Jagarico × 태고의 달인\" Yoshihito Yano(BNSI)", "origin": "--「じゃがりこ × 太鼓の達人」 矢野義人(BNSI)", "zh": "\"Jagarico × 太鼓之达人\" 矢野義人(BNSI)", "zh_tw": "--「Jagarico × 太鼓之達人」 矢野義人(BNSI)"}$j$, '2026-10-29T17:53:30.279022Z',
   $j${"en": "Jagarico Remix", "ja": "じゃがりこ　りみっくす", "ko": "Jagarico Remix", "origin": "Jagariko Remix", "zh": "Jagarico Remix", "zh_tw": "Jagarico Remix"}$j$,
   $j${"en": "\"Jagarico × Taiko no Tatsujin\" Yoshihito Yano(BNSI)", "ja": "「じゃがりこ × 太鼓の達人」 矢野義人(BNSI)", "ko": "\"Jagarico × 태고의 달인\" Yoshihito Yano(BNSI)", "origin": "--Jagariko x Taiko no Tatsujin / Yoshito Yano (BNSI)", "zh": "\"Jagarico × 太鼓之达人\" 矢野義人(BNSI)", "zh_tw": "--「Jagarico × 太鼓之達人」 矢野義人(BNSI)"}$j$, '2026-11-06T08:14:29.776065Z'),
  -- 1435 Hololive Ondo
  ('24f48912f72e451db00748d70734cbda',
   $j${"en": "Hololive Ondo", "ja": "ホロメン音頭", "ko": "hololive ondo", "origin": "Hololive Ondo", "zh": "hololive ondo", "zh_tw": "hololive ondo"}$j$,
   $j${"en": "hololive IDOL PROJECT", "ja": "hololive IDOL PROJECT", "origin": "hololive IDOL PROJECT", "zh": "hololive IDOL PROJECT", "zh_tw": "--hololive IDOL PROJECT"}$j$, '2026-10-29T18:49:52.717448Z',
   $j${"en": "Hololive Ondo", "ja": "ホロメン音頭", "ko": "hololive ondo", "origin": "Hololive Ondo", "zh": "hololive ondo", "zh_tw": "hololive ondo"}$j$,
   $j${"en": "hololive IDOL PROJECT", "ja": "--hololive IDOL PROJECT", "origin": "hololive IDOL PROJECT", "zh": "hololive IDOL PROJECT", "zh_tw": "--hololive IDOL PROJECT"}$j$, '2026-11-06T07:49:24.678386Z'),
  -- 1446 Ai no Katachi feat. HIDE(GReeeeN)
  ('2a6277099719403aaa0d4eed62cbd5da',
   $j${"en": "Ai no Katachi feat. HIDE(GReeeeN)", "ja": "アイノカタチ feat. HIDE(GReeeeN)", "origin": "Ai no Katachi feat. HIDE(GReeeeN)", "zh": "爱的形状 feat. HIDE(GReeeeN)"}$j$,
   $j${"origin": "--"}$j$, '2026-10-29T18:26:03.75748Z',
   $j${"en": "Ai no Katachi feat. HIDE(GReeeeN)", "ja": "アイノカタチ feat. HIDE(GReeeeN)", "origin": "Ai no Katachi feat. HIDE(GReeeeN)", "zh": "爱的形状 feat. HIDE(GReeeeN)"}$j$,
   $j${"ja": "", "origin": "--"}$j$, '2026-11-06T07:50:17.417238Z'),
  -- 1566 void setup
  ('22f0fe90fe3044278f686a986d90873f',
   $j${"en": "void setup", "ja": "void setup", "origin": "void setup"}$j$,
   $j${"en": "Yuuji Masubuchi feat. Keiori Aihara", "origin": "--Yuuji Masubuchi feat. Keiori Aihara"}$j$, '2026-10-29T18:10:03.043149Z',
   $j${"en": "void setup", "ja": "void setup", "origin": "void setup"}$j$,
   $j${"en": "Yuuji Masubuchi feat. Keiori Aihara", "ja": "", "origin": "--Yuuji Masubuchi feat. Keiori Aihara"}$j$, '2026-11-06T07:49:05.574783Z'),
  -- 1849 Monster Strike Medley: Main Theme, Colossal Difficulty, Victory Theme
  ('d460cb3ab84b4f2985a7b5e9eb4c1e84',
   $j${"en": "Monster Strike Medley: Main Theme, Colossal Difficulty, Victory Theme", "ja": "モンスターストライクメドレー ～メインテーマ・爆絶・勝利～", "ko": "몬스터 스트라이크 메들리 ~메인 테마·폭절·승리~", "origin": "Monster Strike Medley: Main Theme, Colossal Difficulty, Victory Theme", "zh": "《怪物弹珠》串烧 ～主旋律・爆绝・胜利～"}$j$,
   $j${"origin": "--"}$j$, '2026-10-29T18:04:04.163949Z',
   $j${"en": "Monster Strike Medley: Main Theme, Colossal Difficulty, Victory Theme", "ja": "モンスターストライクメドレー ～メインテーマ・爆絶・勝利～", "ko": "몬스터 스트라이크 메들리 ~메인 테마·폭절·승리~", "origin": "Monster Strike Medley: Main Theme, Colossal Difficulty, Victory Theme", "zh": "《怪物弹珠》串烧 ～主旋律・爆绝・胜利～"}$j$,
   $j${"ja": "", "origin": "--"}$j$, '2026-11-06T08:19:30.183546Z'),
  -- 1850 bad guy
  ('afffd6e572384db9b5df65a296c1edb1',
   $j${"en": "bad guy", "ja": "bad guy", "origin": "bad guy", "zh": "bad guy", "zh_tw": "bad guy"}$j$,
   $j${"origin": "--"}$j$, '2026-10-29T18:28:05.330111Z',
   $j${"en": "bad guy", "ja": "bad guy", "origin": "bad guy", "zh": "bad guy", "zh_tw": "bad guy"}$j$,
   $j${"ja": "", "origin": "--"}$j$, '2026-11-06T08:12:49.304045Z'),
  -- 1919 Dried Flower
  ('040575dc25c645a1a1695df90313da80',
   $j${"en": "Dried Flower", "ja": "ドライフラワー", "ko": "Dried Flower", "origin": "Dried Flower", "zh": "Dried Flower", "zh_tw": "Dried Flower"}$j$,
   $j${"origin": "--"}$j$, '2026-10-29T18:36:02.079728Z',
   $j${"en": "Dried Flower", "ja": "ドライフラワー", "ko": "Dried Flower", "origin": "Dried Flower", "zh": "Dried Flower", "zh_tw": "Dried Flower"}$j$,
   $j${"ja": "", "ko": "", "origin": "--", "zh": ""}$j$, '2026-11-06T07:43:21.686014Z'),
  -- 2104 AWAY
  ('7704782f6fd74b63adbb0d5b262b0ebf',
   $j${"en": "AWAY", "ja": "AWAY", "origin": "AWAY", "zh": "来自天堂的魔鬼", "zh_tw": "來自天堂的魔鬼 [AWAY]"}$j$,
   $j${"origin": "--"}$j$, '2026-10-29T18:27:45.40808Z',
   $j${"en": "AWAY", "ja": "AWAY", "origin": "AWAY", "zh": "来自天堂的魔鬼", "zh_tw": "來自天堂的魔鬼 [AWAY]"}$j$,
   $j${"ja": "", "origin": "--", "zh": ""}$j$, '2026-11-06T08:03:37.801359Z'),
  -- 2381 Gakuen Tengoku -New Audio/Chart-
  ('0e17654b254e431dbca27441aac9f581',
   $j${"en": "Gakuen Tengoku -New Audio/Chart-", "ja": "学園天国 新音源・譜面", "origin": "Gakuen Tengoku -New Audio/Chart-"}$j$,
   $j${"en": "Finger 5", "ja": "フィンガー5", "origin": "--"}$j$, '2026-10-29T18:40:36.90816Z',
   $j${"en": "Gakuen Tengoku -New Audio/Chart-", "ja": "学園天国", "origin": "Gakuen Tengoku -New Audio/Chart-"}$j$,
   $j${"en": "Finger 5", "ja": "新音源・譜面", "origin": "--"}$j$, '2026-11-06T07:45:09.047807Z'),
  -- 2489 Tontsukatantan
  ('0bea1201a53043e18875b13a9effbc7a',
   $j${"en": "Tontsukatantan", "ja": "トンツカタンタン", "origin": "Tontsukatantan", "zh_tw": "Tontsukatantan"}$j$,
   $j${"origin": "--"}$j$, '2026-10-27T04:59:04.497108Z',
   $j${"en": "Tontsukatantan", "ja": "トンツカタンタン", "origin": "Tontsukatantan", "zh_tw": "Tontsukatantan"}$j$,
   $j${"ja": "", "origin": "--"}$j$, '2026-11-06T07:44:45.426289Z'),
  -- 2520 Hakujitsu
  ('38e5db10fb074f6b938664f10dc6e132',
   $j${"en": "Hakujitsu", "ja": "白日", "ko": "HAKUJITSU", "origin": "Hakujitsu", "zh": "白日", "zh_tw": "白日"}$j$,
   $j${"en": "Innocence Enzai Bengoushi", "origin": "--Innocence Enzai Bengoushi"}$j$, '2026-10-29T18:48:12.783932Z',
   $j${"en": "Hakujitsu", "ja": "白日", "ko": "HAKUJITSU", "origin": "Hakujitsu", "zh": "白日", "zh_tw": "白日"}$j$,
   $j${"en": "Innocence Enzai Bengoushi", "ja": "", "ko": "", "origin": "--Innocence Enzai Bengoushi", "zh": ""}$j$, '2026-11-06T07:52:41.477548Z'),
  -- 2958 Byoushinwo Kamu
  ('5e6463f0647a4f85b55875bb51c4f347',
   $j${"en": "Byoushinwo Kamu", "ja": "秒針を噛む", "ko": "Byoushinwo Kamu", "origin": "Byoushinwo Kamu", "zh": "咬住时针", "zh_tw": "Byoushinwo Kamu"}$j$,
   $j${"en": "ZUTOMAYO", "ja": "ずっと真夜中でいいのに。", "origin": "--ZUTOMAYO", "zh": "永远是深夜有多好。", "zh_tw": "--ZUTOMAYO"}$j$, '2026-10-29T18:29:22.952634Z',
   $j${"en": "Byoushinwo Kamu", "ja": "秒針を噛む", "ko": "Byoushinwo Kamu", "origin": "Byoushinwo Kamu", "zh": "Byoushinwo Kamu", "zh_tw": "Byoushinwo Kamu"}$j$,
   $j${"en": "ZUTOMAYO", "ja": "ずっと真夜中でいいのに。", "origin": "--ZUTOMAYO", "zh": "ZUTOMAYO", "zh_tw": "--ZUTOMAYO"}$j$, '2026-11-06T07:59:40.698845Z');

ALTER TABLE public.charts DISABLE TRIGGER charts_touch_update_at;
UPDATE public.charts c
   SET title_translations = r.title_before, subtitle_translations = r.subtitle_before, update_at = r.update_before
  FROM restore_titles r
 WHERE c.id = r.id AND c.title_translations = r.title_left AND c.subtitle_translations = r.subtitle_left AND c.update_at = r.update_left;
ALTER TABLE public.charts ENABLE TRIGGER charts_touch_update_at;

INSERT INTO internal.schema_migrations(version) VALUES (21);
