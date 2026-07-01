package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 各除外ルールに該当する「弱い」暗証番号は isWeak が true を返すこと。
func TestIsWeak_除外される(t *testing.T) {
	cases := map[string][]string{
		"連続同一桁":   {"6000", "8110", "2922", "0000", "1111", "9999"},
		"連番":      {"1234", "4321", "0123", "3210", "6789", "9876"},
		"飛ばし繰り返し": {"1212", "9494", "8181", "0103", "0161", "8343"},
		"キーパッド隣接": {"1236", "1478", "1402", "9061"},
		"角→角":     {"1300", "3100", "1700", "1900", "3700", "7900"},
		"2と0":     {"2099", "8023", "2055", "0255"},
	}
	for rule, list := range cases {
		for _, s := range list {
			if !isWeak([]byte(s)) {
				t.Errorf("[%s] %s は除外されるべきだが許可された", rule, s)
			}
		}
	}
}

// 全ルールを満たす暗証番号は isWeak が false を返すこと。
func TestIsWeak_許可される(t *testing.T) {
	for _, s := range []string{"6018", "7605", "6401", "0461", "0164", "6103"} {
		if isWeak([]byte(s)) {
			t.Errorf("%s は有効なはずだが除外された", s)
		}
	}
}

// adjacent はキーパッド上で上下左右・斜めに隣接するキーのみ true を返すこと。
func TestAdjacent(t *testing.T) {
	adj := [][2]byte{{'1', '2'}, {'1', '4'}, {'1', '5'}, {'5', '9'}, {'8', '0'}, {'7', '0'}, {'9', '0'}}
	for _, p := range adj {
		if !adjacent(p[0], p[1]) {
			t.Errorf("%c と %c は隣接のはず", p[0], p[1])
		}
	}
	notAdj := [][2]byte{{'1', '3'}, {'1', '9'}, {'1', '6'}, {'4', '0'}, {'1', '0'}}
	for _, p := range notAdj {
		if adjacent(p[0], p[1]) {
			t.Errorf("%c と %c は非隣接のはず", p[0], p[1])
		}
	}
}

// 全 10000 通りを走査し、有効な暗証番号が想定数で、弱いものが残らないこと。
func TestFullScan(t *testing.T) {
	const wantValid = 166
	valid := 0
	for n := 0; n < 10000; n++ {
		s := fmt.Sprintf("%04d", n)
		if isWeak([]byte(s)) {
			continue
		}
		valid++
		// 有効と判定されたものに弱いパターンが紛れていないか二重チェック
		for i := 1; i < defaultDigits; i++ {
			if s[i] == s[i-1] {
				t.Errorf("有効判定の %s に連続同一桁が残っている", s)
			}
		}
		if s[0] == s[2] || s[1] == s[3] {
			t.Errorf("有効判定の %s に飛ばし繰り返しが残っている", s)
		}
	}
	if valid != wantValid {
		t.Errorf("有効な暗証番号の数 = %d, 期待値 = %d", valid, wantValid)
	}
}

// samplePIN は指定桁数の数字列を返し、局所制約（canFollow）を満たすこと。
func TestSamplePIN(t *testing.T) {
	for _, n := range []int{minDigits, defaultDigits, 6, maxDigits} {
		ways := buildTable(n)
		for i := 0; i < 500; i++ {
			pw, err := samplePIN(ways, n)
			if err != nil {
				t.Fatalf("samplePIN(%d) がエラー: %v", n, err)
			}
			if len(pw) != n {
				t.Fatalf("桁数が %d（期待 %d）: %s", len(pw), n, pw)
			}
			if !allCanFollow(pw) {
				t.Fatalf("局所制約違反の生成: %s", pw)
			}
		}
	}
}

// allCanFollow は pw の各隣接位置が canFollow を満たすか（連番以外の局所制約に合格か）を返す。
func allCanFollow(pw []byte) bool {
	prev2, prev1 := sentinel, sentinel
	for _, c := range pw {
		if c < '0' || c > '9' {
			return false
		}
		if !canFollow(prev2, prev1, c) {
			return false
		}
		prev2, prev1 = prev1, c
	}
	return true
}

// isSequence は pw が完全な昇順または降順の連番か（isWeak の連番ルール相当）を返す。
func isSequence(pw []byte) bool {
	asc, desc := true, true
	for i := 1; i < len(pw); i++ {
		if pw[i]-pw[i-1] != 1 {
			asc = false
		}
		if pw[i-1]-pw[i] != 1 {
			desc = false
		}
	}
	return asc || desc
}

// canFollow が isWeak の「連番以外」の局所ルールと厳密に一致することを全数で検証する。
func TestCanFollowMatchesIsWeak(t *testing.T) {
	for _, L := range []int{4, 5} {
		upper := 1
		for i := 0; i < L; i++ {
			upper *= 10
		}
		for v := 0; v < upper; v++ {
			s := []byte(fmt.Sprintf("%0*d", L, v))
			local := allCanFollow(s)
			weak := isWeak(s)
			if !local {
				// 局所制約違反は必ず isWeak が拾う（局所ルールは isWeak の部分集合）
				if !weak {
					t.Fatalf("%s: 局所違反だが isWeak=false", s)
				}
			} else if weak && !isSequence(s) {
				// 局所 OK で isWeak=true になり得るのは連番のときだけ
				t.Fatalf("%s: 局所 OK かつ非連番なのに isWeak=true", s)
			}
		}
	}
}

// buildTable の数え上げが、全列挙した局所制約合格数と一致することを検証する（重み付けの正しさ）。
func TestBuildTableCounts(t *testing.T) {
	for _, L := range []int{4, 5, 6} {
		ways := buildTable(L)
		got := ways[L][idx(sentinel)][idx(sentinel)]
		want, upper := 0, 1
		for i := 0; i < L; i++ {
			upper *= 10
		}
		for v := 0; v < upper; v++ {
			if allCanFollow([]byte(fmt.Sprintf("%0*d", L, v))) {
				want++
			}
		}
		if got != want {
			t.Errorf("L=%d: buildTable 総数 %d, 全列挙 %d", L, got, want)
		}
	}
}

// samplePIN が 4 桁の有効候補をすべて生成し得る（カバレッジ）ことを確認する。
func TestSampleCoverage(t *testing.T) {
	valid := map[string]bool{}
	for v := 0; v < 10000; v++ {
		s := fmt.Sprintf("%04d", v)
		if !isWeak([]byte(s)) {
			valid[s] = true
		}
	}
	ways := buildTable(4)
	seen := map[string]bool{}
	for i := 0; i < len(valid)*400; i++ {
		pw, err := samplePIN(ways, 4)
		if err != nil {
			t.Fatal(err)
		}
		if isWeak(pw) { // 連番（稀）は対象外
			continue
		}
		s := string(pw)
		if !valid[s] {
			t.Fatalf("有効集合外の %s が生成された", s)
		}
		seen[s] = true
	}
	if len(seen) != len(valid) {
		t.Errorf("生成されたユニーク候補 %d, 有効候補 %d（未出現あり）", len(seen), len(valid))
	}
}

// isWeak は 4 桁より長い入力でも panic せず、桁数に応じた一般化ルールが働くこと。
func TestIsWeak_可変桁(t *testing.T) {
	// 飛ばし繰り返し（i桁目==i+2桁目）の一般化を isolate して検証する。
	// 前提: 先頭4桁 "0160" は単体では有効（どのルールにも該当しない）。
	if isWeak([]byte("0160")) {
		t.Fatal(`前提崩れ: 4桁 "0160" は有効であるべき（飛ばし繰り返しの一般化を isolate できない）`)
	}
	// "016010" が弱いのは pw[3]==pw[5]（i=3）のみ。4桁固定ロジック
	// （pw[0]==pw[2] || pw[1]==pw[3]）では捕捉できず、一般化で初めて検出される。
	// "010101" は i=0 で該当する従来相当ケース。
	for _, s := range []string{"016010", "010101"} {
		if !isWeak([]byte(s)) {
			t.Errorf("%s（%d桁）は飛ばし繰り返しで除外されるべき", s, len(s))
		}
	}

	// 飛ばし繰り返し以外のルールも可変長で panic せず働くこと。
	others := map[string][]string{
		"連続同一桁":   {"600000", "8110000000"},
		"連番":      {"123456", "0123456789"},
		"キーパッド隣接": {"741203", "1478520369"},
	}
	for rule, list := range others {
		for _, s := range list {
			if !isWeak([]byte(s)) {
				t.Errorf("[%s] %s（%d桁）は除外されるべき", rule, s, len(s))
			}
		}
	}

	// 各桁数で有効な候補が少なくとも1つ存在すること（無限ループ防止の担保）。
	for _, n := range []int{minDigits, 6, 8, maxDigits} {
		if !hasValidPIN(n) {
			t.Errorf("%d桁で有効な暗証番号が1つも見つからない", n)
		}
	}
}

// hasValidPIN は n 桁で isWeak を通過する数字列が存在するかを、
// 決定的なシードのランダム探索で確かめる（有効な候補は早期に見つかる）。
func hasValidPIN(n int) bool {
	rng := rand.New(rand.NewSource(1))
	pw := make([]byte, n)
	for try := 0; try < 1_000_000; try++ {
		for i := range pw {
			pw[i] = byte('0' + rng.Intn(10))
		}
		if !isWeak(pw) {
			return true
		}
	}
	return false
}

// parseFlags は -d/--digits と -n/--number を解釈し、省略時は既定値、範囲外はエラー。
func TestParseFlags(t *testing.T) {
	ok := []struct {
		args       []string
		wantDigits int
		wantCount  int
	}{
		{nil, defaultDigits, defaultCount},
		{[]string{"-d", "6"}, 6, defaultCount},
		{[]string{"--digits", "8"}, 8, defaultCount},
		{[]string{"-digits", "10"}, 10, defaultCount},
		{[]string{"-n", "5"}, defaultDigits, 5},
		{[]string{"--number", "10"}, defaultDigits, 10},
		{[]string{"-d", "8", "-n", "3"}, 8, 3},
		{[]string{"-d", "4", "-n", "1"}, 4, 1},
		{[]string{"-n", "100"}, defaultDigits, 100}, // 上限ちょうど
	}
	for _, c := range ok {
		d, n, _, err := parseFlags(c.args)
		if err != nil {
			t.Errorf("parseFlags(%v) が予期せぬエラー: %v", c.args, err)
			continue
		}
		if d != c.wantDigits || n != c.wantCount {
			t.Errorf("parseFlags(%v) = (桁数%d, 個数%d), 期待 (桁数%d, 個数%d)", c.args, d, n, c.wantDigits, c.wantCount)
		}
	}

	ng := [][]string{
		{"-d", "3"},          // 桁数 下限未満
		{"-d", "11"},         // 桁数 上限超過
		{"-d", "abc"},        // 非数値
		{"-n", "0"},          // 個数 下限未満
		{"-n", "101"},        // 個数 上限超過
		{"-n", "abc"},        // 非数値
		{"6"},                // 位置引数は受け付けない（黙って無視させない）
		{"6", "-d", "8"},     // 位置引数が先頭にあると後続フラグも解析されない
		{"-d", "8", "extra"}, // 余分な位置引数
	}
	for _, args := range ng {
		if _, _, _, err := parseFlags(args); err == nil {
			t.Errorf("parseFlags(%v) はエラーになるべき", args)
		}
	}
}

// -h / --help は flag.ErrHelp を返し、usage を標準出力へ出すこと（エラー終了ではない）。
func TestParseFlags_ヘルプ(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			orig := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("os.Pipe: %v", err)
			}
			defer func() { os.Stdout = orig }() // panic 時も含め確実に復元する
			os.Stdout = w

			_, _, _, perr := parseFlags([]string{arg})
			w.Close()
			out, _ := io.ReadAll(r)

			if !errors.Is(perr, flag.ErrHelp) {
				t.Errorf("parseFlags([%q]) は flag.ErrHelp を返すべき: %v", arg, perr)
			}
			for _, want := range []string{"-digits", "-number"} {
				if !strings.Contains(string(out), want) {
					t.Errorf("usage に %s の説明が含まれるべき: %q", want, out)
				}
			}
		})
	}
}

// parseFlags は -s/--step を解釈し、ステップモード時は count を 1 に強制すること。
func TestParseFlags_ステップモード(t *testing.T) {
	cases := []struct {
		args       []string
		wantDigits int
	}{
		{[]string{"-s"}, defaultDigits},
		{[]string{"--step"}, defaultDigits},
		{[]string{"-step"}, defaultDigits},
		{[]string{"-s", "-d", "6"}, 6},
	}
	for _, c := range cases {
		d, count, step, err := parseFlags(c.args)
		if err != nil {
			t.Errorf("parseFlags(%v) が予期せぬエラー: %v", c.args, err)
			continue
		}
		if !step {
			t.Errorf("parseFlags(%v): stepMode=false、true を期待", c.args)
		}
		if d != c.wantDigits {
			t.Errorf("parseFlags(%v): digits=%d、期待 %d", c.args, d, c.wantDigits)
		}
		if count != 1 {
			t.Errorf("parseFlags(%v): count=%d、ステップモードは 1 を期待", c.args, count)
		}
	}

	// -s と -n の併用はエラーになること
	for _, args := range [][]string{
		{"-s", "-n", "5"},
		{"-s", "--number", "3"},
		{"--step", "-n", "1"},
	} {
		if _, _, _, err := parseFlags(args); err == nil {
			t.Errorf("parseFlags(%v) はエラーになるべき", args)
		}
	}
}

// ---- スケジュールパース ----

// parseSchedule は正しい書式の文字列を Schedule に変換できること。
func TestParseSchedule_正常系(t *testing.T) {
	cases := []struct {
		input     string
		wantDays  []int
		wantStart string
		wantEnd   string
	}{
		// 単一曜日（日本語）
		{"日 09:00-11:00", []int{0}, "09:00", "11:00"},
		// 単一曜日（英語）
		{"Sun 09:00-11:00", []int{0}, "09:00", "11:00"},
		// レンジ（月-金）
		{"月-金 20:00-22:00", []int{1, 2, 3, 4, 5}, "20:00", "22:00"},
		// レンジ（Mon-Fri）
		{"Mon-Fri 20:00-22:00", []int{1, 2, 3, 4, 5}, "20:00", "22:00"},
		// 週末レンジ（土-日）: 内部順序 5→6 で日曜が末尾に来る
		{"土-日 10:00-12:00", []int{6, 0}, "10:00", "12:00"},
		{"Sat-Sun 10:00-12:00", []int{6, 0}, "10:00", "12:00"},
		// カンマ区切り
		{"土,日 10:00-12:00", []int{6, 0}, "10:00", "12:00"},
		// 全曜日
		{"月-日 00:00-23:59", []int{1, 2, 3, 4, 5, 6, 0}, "00:00", "23:59"},
		// 余分なスペースはトリムされる
		{"水  14:00-16:00", []int{3}, "14:00", "16:00"},
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			got, err := parseSchedule(c.input)
			if err != nil {
				t.Fatalf("parseSchedule(%q) がエラー: %v", c.input, err)
			}
			if !reflect.DeepEqual(got.Days, c.wantDays) {
				t.Errorf("Days = %v, 期待 %v", got.Days, c.wantDays)
			}
			if got.Start != c.wantStart || got.End != c.wantEnd {
				t.Errorf("時刻 = %s-%s, 期待 %s-%s", got.Start, got.End, c.wantStart, c.wantEnd)
			}
		})
	}
}

// parseSchedule は不正な書式でエラーを返すこと。
func TestParseSchedule_エラー系(t *testing.T) {
	ng := []string{
		"月-金",             // 時刻なし
		"20:00-22:00",     // 曜日なし
		"Xxx 20:00-22:00", // 不明な曜日
		"月 20:00",         // 終了時刻なし
		"月 25:00-22:00",   // 無効な時刻
		"月 20:00-61:00",   // 無効な時刻
		"",                // 空文字
	}
	for _, s := range ng {
		if _, err := parseSchedule(s); err == nil {
			t.Errorf("parseSchedule(%q) はエラーになるべき", s)
		}
	}
}

// inSchedule は時刻・曜日ごとに正しく true/false を返すこと。
func TestInSchedule(t *testing.T) {
	// 2026-06-29(月), 2026-06-30(火), 2026-07-01(水), 2026-07-04(土), 2026-07-05(日)
	mon := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	sat := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	sun := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	withHM := func(base time.Time, h, m int) time.Time {
		return time.Date(base.Year(), base.Month(), base.Day(), h, m, 0, 0, time.UTC)
	}

	tue := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	// 月-金 20:00-22:00
	wdSch := []Schedule{{Days: []int{1, 2, 3, 4, 5}, Start: "20:00", End: "22:00"}}
	// 土-日 10:00-12:00
	weSch := []Schedule{{Days: []int{6, 0}, Start: "10:00", End: "12:00"}}
	// 月のみ 22:00-02:00（日をまたぐ夜間スケジュール）
	overnightMon := []Schedule{{Days: []int{1}, Start: "22:00", End: "02:00"}}

	cases := []struct {
		name      string
		schedules []Schedule
		t         time.Time
		want      bool
	}{
		{"月曜20:30→範囲内", wdSch, withHM(mon, 20, 30), true},
		{"月曜21:59→範囲内", wdSch, withHM(mon, 21, 59), true},
		{"月曜22:00→終端は含まない", wdSch, withHM(mon, 22, 0), false},
		{"月曜19:59→開始前", wdSch, withHM(mon, 19, 59), false},
		{"土曜20:30→曜日外", wdSch, withHM(sat, 20, 30), false},
		{"土曜10:30→範囲内", weSch, withHM(sat, 10, 30), true},
		{"日曜10:30→範囲内", weSch, withHM(sun, 10, 30), true},
		{"月曜10:30→曜日外", weSch, withHM(mon, 10, 30), false},
		{"空スケジュール→常に false", []Schedule{}, withHM(mon, 20, 30), false},
		// 夜間スケジュール（Start > End）の日またぎ
		{"夜間: 月曜23:00→範囲内(当日夜)", overnightMon, withHM(mon, 23, 0), true},
		{"夜間: 火曜01:30→範囲内(翌日未明)", overnightMon, withHM(tue, 1, 30), true},
		{"夜間: 火曜02:00→終端は含まない", overnightMon, withHM(tue, 2, 0), false},
		{"夜間: 火曜23:00→曜日指定は月曜のみなので範囲外", overnightMon, withHM(tue, 23, 0), false},
		{"夜間: 月曜21:59→開始前", overnightMon, withHM(mon, 21, 59), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := inSchedule(c.schedules, c.t)
			if got != c.want {
				t.Errorf("inSchedule = %v, 期待 %v (時刻: %s)", got, c.want, c.t.Format("Mon 15:04"))
			}
		})
	}
}

// gateCheck はスケジュール・遅延の各組み合わせで正しく動作すること。
func TestGateCheck(t *testing.T) {
	now := time.Date(2026, 7, 1, 21, 0, 0, 0, time.UTC) // 水曜 21:00

	// スケジュール内（水曜 20:00-22:00）
	inSch := []Schedule{{Days: []int{3}, Start: "20:00", End: "22:00"}}
	// スケジュール外（月曜のみ）
	outSch := []Schedule{{Days: []int{1}, Start: "20:00", End: "22:00"}}

	cases := []struct {
		name       string
		cfg        Config
		pending    *PendingReq
		wantAllow  bool
		wantChange bool // needsSave
	}{
		{
			name:      "制限なし→即時許可",
			cfg:       Config{},
			pending:   nil,
			wantAllow: true, wantChange: false,
		},
		{
			name:      "遅延のみ・保留なし→新規作成",
			cfg:       Config{DelaySeconds: 1800},
			pending:   nil,
			wantAllow: false, wantChange: true,
		},
		{
			name:    "遅延のみ・タイマー待機中",
			cfg:     Config{DelaySeconds: 1800},
			pending: &PendingReq{RequestedAt: now.Add(-10 * time.Minute), DelaySeconds: 1800},
			// 10分経過・30分待機なのであと20分
			wantAllow: false, wantChange: false,
		},
		{
			name:      "遅延のみ・タイマー切れ→許可・クリア",
			cfg:       Config{DelaySeconds: 1800},
			pending:   &PendingReq{RequestedAt: now.Add(-31 * time.Minute), DelaySeconds: 1800},
			wantAllow: true, wantChange: true,
		},
		{
			name:      "スケジュール内・遅延なし→許可",
			cfg:       Config{Schedules: inSch},
			pending:   nil,
			wantAllow: true, wantChange: false,
		},
		{
			name:      "スケジュール外→常に却下",
			cfg:       Config{Schedules: outSch},
			pending:   nil,
			wantAllow: false, wantChange: false,
		},
		{
			name: "スケジュール外・タイマー切れでも却下（B案）",
			cfg:  Config{Schedules: outSch, DelaySeconds: 1800},
			// タイマーは切れているがスケジュール外
			pending:   &PendingReq{RequestedAt: now.Add(-60 * time.Minute), DelaySeconds: 1800},
			wantAllow: false, wantChange: false,
		},
		{
			name:      "スケジュール内・遅延あり・保留なし→新規作成",
			cfg:       Config{Schedules: inSch, DelaySeconds: 1800},
			pending:   nil,
			wantAllow: false, wantChange: true,
		},
		{
			name:      "スケジュール内・タイマー切れ→許可・クリア",
			cfg:       Config{Schedules: inSch, DelaySeconds: 1800},
			pending:   &PendingReq{RequestedAt: now.Add(-31 * time.Minute), DelaySeconds: 1800},
			wantAllow: true, wantChange: true,
		},
		{
			// 遅延30分・タイマー切れから35分放置（猶予期間=遅延時間の30分を超過）→ 失効・再要求
			name:      "遅延のみ・実行可能を長時間放置→失効し再要求",
			cfg:       Config{DelaySeconds: 1800},
			pending:   &PendingReq{RequestedAt: now.Add(-65 * time.Minute), DelaySeconds: 1800},
			wantAllow: false, wantChange: true,
		},
		{
			// タイマー切れ直後（猶予期間内）はまだ失効しない
			name:      "遅延のみ・タイマー切れ直後は猶予期間内なので失効しない",
			cfg:       Config{DelaySeconds: 1800},
			pending:   &PendingReq{RequestedAt: now.Add(-59 * time.Minute), DelaySeconds: 1800},
			wantAllow: true, wantChange: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pending := c.pending
			cfg := c.cfg
			allow, change, _ := gateCheck(&cfg, &pending, now)
			if allow != c.wantAllow {
				t.Errorf("proceed = %v, 期待 %v", allow, c.wantAllow)
			}
			if change != c.wantChange {
				t.Errorf("needsSave = %v, 期待 %v", change, c.wantChange)
			}
			// タイマー切れ許可の場合、pending がクリアされているか
			if c.wantAllow && c.wantChange && pending != nil {
				t.Errorf("許可後に pending がクリアされていない")
			}
			// 保留新規作成の場合、pending が設定されているか
			if !c.wantAllow && c.wantChange && pending == nil {
				t.Errorf("新規作成後に pending が nil のまま")
			}
		})
	}
}

// configSet は --delay か --schedule の片方だけを指定した場合、
// もう片方の既存設定を勝手に 0/nil へ戻さず維持すること（回帰テスト）。
func TestConfigSet_未指定項目は維持される(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := configSet([]string{"--delay", "30m"}); err != nil {
		t.Fatalf("1回目の configSet がエラー: %v", err)
	}
	c, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig がエラー: %v", err)
	}
	if c.DelaySeconds != 1800 {
		t.Fatalf("1回目適用後の DelaySeconds = %d, 期待 1800", c.DelaySeconds)
	}

	// --schedule のみ指定 → 既存の delay (30分) が保持されているべき
	if err := configSet([]string{"--schedule", "月-金 09:00-18:00"}); err != nil {
		t.Fatalf("2回目の configSet がエラー: %v", err)
	}
	c, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig がエラー: %v", err)
	}
	if c.PendingConfig == nil {
		t.Fatal("2回目は遅延ありのため保留が作成されるはず")
	}
	if c.PendingConfig.NewDelaySeconds != 1800 {
		t.Errorf("保留中の設定変更の NewDelaySeconds = %d, 期待 1800（既存の遅延が維持されるべき）", c.PendingConfig.NewDelaySeconds)
	}
	if len(c.PendingConfig.NewSchedules) != 1 {
		t.Errorf("保留中の設定変更の NewSchedules 件数 = %d, 期待 1", len(c.PendingConfig.NewSchedules))
	}
}

// configSet は「保留中（タイマー待機中）に片方だけを指定して更新」した場合、
// 保留中にステージ済みのもう片方の値を維持すること（現在アクティブな設定に
// 巻き戻さない）（回帰テスト）。
func TestConfigSet_保留中の後勝ち更新は既存の保留内容を維持する(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// 初回適用: delay=30分（即時適用）
	if err := configSet([]string{"--delay", "30m"}); err != nil {
		t.Fatalf("1回目の configSet がエラー: %v", err)
	}

	// delay を 1時間へ変更要求 → 保留作成（NewDelaySeconds=3600）
	if err := configSet([]string{"--delay", "1h"}); err != nil {
		t.Fatalf("2回目の configSet がエラー: %v", err)
	}
	c, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig がエラー: %v", err)
	}
	if c.PendingConfig == nil || c.PendingConfig.NewDelaySeconds != 3600 {
		t.Fatalf("2回目後の保留 NewDelaySeconds = %v, 期待 3600", c.PendingConfig)
	}

	// タイマー待機中に schedule のみ追加指定 → 保留中の NewDelaySeconds(3600) が
	// 維持されるべき（現在アクティブな DelaySeconds(1800) に巻き戻ってはいけない）
	if err := configSet([]string{"--schedule", "月-金 09:00-18:00"}); err != nil {
		t.Fatalf("3回目の configSet がエラー: %v", err)
	}
	c, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig がエラー: %v", err)
	}
	if c.PendingConfig == nil {
		t.Fatal("3回目後も保留が存在するはず")
	}
	if c.PendingConfig.NewDelaySeconds != 3600 {
		t.Errorf("3回目後の NewDelaySeconds = %d, 期待 3600（保留中の1時間への変更が維持されるべき）", c.PendingConfig.NewDelaySeconds)
	}
	if len(c.PendingConfig.NewSchedules) != 1 {
		t.Errorf("3回目後の NewSchedules 件数 = %d, 期待 1", len(c.PendingConfig.NewSchedules))
	}
}

// generatePINs は count 個・桁数一致・非弱・互いに重複しない PIN を返すこと。
func TestGeneratePINs(t *testing.T) {
	cases := []struct{ digits, count int }{
		{defaultDigits, defaultCount}, // 既定（4桁1個）
		{4, maxCount},                 // 4桁10個（166候補からの重複排除が効く）
		{6, 5},
		{maxDigits, 3},
	}
	for _, c := range cases {
		pins, err := generatePINs(c.digits, c.count)
		if err != nil {
			t.Errorf("generatePINs(%d,%d) がエラー: %v", c.digits, c.count, err)
			continue
		}
		if len(pins) != c.count {
			t.Errorf("generatePINs(%d,%d) の件数 = %d, 期待 %d", c.digits, c.count, len(pins), c.count)
		}
		seen := make(map[string]bool, len(pins))
		for _, p := range pins {
			if len(p) != c.digits {
				t.Errorf("生成 %q の桁数 = %d, 期待 %d", p, len(p), c.digits)
			}
			if isWeak([]byte(p)) {
				t.Errorf("生成 %q が弱判定される", p)
			}
			if seen[p] {
				t.Errorf("生成 %q が重複している", p)
			}
			seen[p] = true
		}
	}
}
